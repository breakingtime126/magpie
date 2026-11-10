package nativeendpoint

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This backend reproduces a deadline interrupt without depending on OS timing.
// Accept cannot return until stopAccept; Close and child drain stay independent.
type shutdownAcceptSocket struct {
	entered, interrupted chan struct{}
	interruptOnce        sync.Once
	accepts              atomic.Int32
	closed               atomic.Bool
	conn                 net.Conn
	err                  error
	process              *shutdownAcceptChild
}

func (s *shutdownAcceptSocket) Accept() (net.Conn, error) {
	if s.accepts.Add(1) == 1 {
		close(s.entered)
	}
	<-s.interrupted
	return s.conn, s.err
}
func (s *shutdownAcceptSocket) stopAccept() error {
	s.interruptOnce.Do(func() { close(s.interrupted) })
	return nil
}
func (s *shutdownAcceptSocket) Close() error {
	s.closed.Store(true)
	return s.stopAccept()
}
func (s *shutdownAcceptSocket) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}
func (s *shutdownAcceptSocket) start(*exec.Cmd) (child, error) { return s.process, nil }

type shutdownAcceptChild struct {
	done, killed      chan struct{}
	endOnce, killOnce sync.Once
}

func (p *shutdownAcceptChild) Wait() error { <-p.done; return nil }
func (p *shutdownAcceptChild) Kill()       { p.killOnce.Do(func() { close(p.killed) }) }
func (p *shutdownAcceptChild) end()        { p.endOnce.Do(func() { close(p.done) }) }

func shutdownAcceptFixture(t *testing.T, conn net.Conn, acceptErr error) (*Endpoint, *shutdownAcceptSocket) {
	t.Helper()
	s := &shutdownAcceptSocket{
		entered: make(chan struct{}), interrupted: make(chan struct{}), conn: conn, err: acceptErr,
		process: &shutdownAcceptChild{done: make(chan struct{}), killed: make(chan struct{})},
	}
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.process.end()
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	path, err := filepath.Abs("synthetic-unused-accept-executable")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(exec.Command(path)); err != nil {
		t.Fatal(err)
	}
	return e, s
}

func shutdownAcceptAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("accept/shutdown fixture did not reach its barrier")
	}
}

func shutdownAcceptResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("accept/shutdown fixture did not return")
		return nil
	}
}

func shutdownAcceptTimeout() error {
	return &net.OpError{Op: "accept", Net: "tcp4", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, Err: os.ErrDeadlineExceeded}
}

func TestEndpointShutdownMakesBlockedAcceptTerminal(t *testing.T) {
	for _, result := range []string{"temporary-timeout", "accepted-connection"} {
		t.Run(result, func(t *testing.T) {
			var raw, peer net.Conn
			var acceptErr error
			if result == "temporary-timeout" {
				acceptErr = shutdownAcceptTimeout()
				if ne, ok := acceptErr.(net.Error); !ok || !ne.Timeout() || !ne.Temporary() {
					t.Fatal("fixture must reproduce a temporary deadline error")
				}
			} else {
				raw, peer = net.Pipe()
				defer raw.Close()
				defer peer.Close()
			}
			e, s := shutdownAcceptFixture(t, raw, acceptErr)
			accepted := make(chan error, 1)
			go func() {
				conn, err := e.Accept()
				if conn != nil {
					_ = conn.Close()
					err = errors.New("connection admitted after shutdown")
				}
				accepted <- err
			}()
			shutdownAcceptAwait(t, s.entered)
			stopped := make(chan error, 1)
			go func() { stopped <- e.Shutdown(context.Background()) }()
			shutdownAcceptAwait(t, s.interrupted)
			if err := shutdownAcceptResult(t, accepted); err != ErrState {
				t.Fatalf("pending Accept returned %v, want terminal ErrState", err)
			}
			shutdownAcceptAwait(t, s.process.killed)
			if s.closed.Load() {
				t.Fatal("released original listener before child drain")
			}
			if peer != nil {
				_ = peer.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := peer.Write([]byte("closed")); err == nil {
					t.Fatal("connection accepted after revocation was not closed")
				} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
					t.Fatal("connection remained open after revocation")
				}
			}
			if conn, err := e.Accept(); conn != nil || err != ErrState || s.accepts.Load() != 1 {
				t.Fatal("revoked endpoint called backend Accept again")
			}
			s.process.end()
			if err := shutdownAcceptResult(t, stopped); err != nil {
				t.Fatal(err)
			}
			if !s.closed.Load() {
				t.Fatal("listener not released after confirmed child drain")
			}
		})
	}
}

func TestEndpointShutdownDoesNotLogHTTPAcceptRetry(t *testing.T) {
	e, s := shutdownAcceptFixture(t, nil, shutdownAcceptTimeout())
	var diagnostics bytes.Buffer
	server := &http.Server{ErrorLog: log.New(&diagnostics, "", 0)}
	served := make(chan error, 1)
	go func() { served <- server.Serve(e) }()
	shutdownAcceptAwait(t, s.entered)
	stopped := make(chan error, 1)
	go func() { stopped <- e.Shutdown(context.Background()) }()
	shutdownAcceptAwait(t, s.interrupted)
	shutdownAcceptAwait(t, s.process.killed)
	if s.closed.Load() {
		t.Fatal("HTTP shutdown released listener before child drain")
	}
	s.process.end()
	if err := shutdownAcceptResult(t, stopped); err != nil {
		t.Fatal(err)
	}
	if err := shutdownAcceptResult(t, served); err != ErrState {
		t.Fatalf("Serve returned %v, want terminal ErrState", err)
	}
	if got := diagnostics.String(); got != "" {
		t.Fatalf("intentional shutdown emitted an HTTP diagnostic: %s", got)
	}
	if s.accepts.Load() != 1 {
		t.Fatal("HTTP server retried the stopped backend")
	}
}

func TestEndpointAcceptPreservesErrorsBeforeShutdown(t *testing.T) {
	for _, original := range []error{shutdownAcceptTimeout(), errors.New("unexpected accept failure")} {
		t.Run(original.Error(), func(t *testing.T) {
			e, s := shutdownAcceptFixture(t, nil, original)
			// Release the backend while Endpoint admission is still active.
			_ = s.stopAccept()
			if conn, err := e.Accept(); conn != nil || err != original {
				t.Fatalf("pre-shutdown Accept returned %v, want original %v", err, original)
			}
			if s.closed.Load() {
				t.Fatal("unexpected Accept error closed the endpoint")
			}
		})
	}
}
