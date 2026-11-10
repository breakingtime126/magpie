package nativeendpoint

import (
	"context"
	"errors"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeChild struct {
	done              chan struct{}
	killed            chan struct{}
	killOnce, endOnce sync.Once
	auto              bool
	err               error
}

func newChild(auto bool) *fakeChild {
	return &fakeChild{done: make(chan struct{}), killed: make(chan struct{}), auto: auto}
}
func (p *fakeChild) Wait() error { <-p.done; return p.err }
func (p *fakeChild) Kill() {
	p.killOnce.Do(func() {
		close(p.killed)
		if p.auto {
			p.end()
		}
	})
}
func (p *fakeChild) end() { p.endOnce.Do(func() { close(p.done) }) }

type fakeSocket struct {
	done   chan struct{}
	conns  chan net.Conn
	once   sync.Once
	closed atomic.Bool
	calls  atomic.Int32
	launch func(*exec.Cmd) (child, error)
}

func socketFixture() *fakeSocket {
	return &fakeSocket{done: make(chan struct{}), conns: make(chan net.Conn, 1)}
}
func (s *fakeSocket) Accept() (net.Conn, error) {
	select {
	case c := <-s.conns:
		return c, nil
	case <-s.done:
		return nil, net.ErrClosed
	}
}
func (s *fakeSocket) stopAccept() error                { s.once.Do(func() { close(s.done) }); return nil }
func (s *fakeSocket) Close() error                     { s.closed.Store(true); return s.stopAccept() }
func (s *fakeSocket) Addr() net.Addr                   { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345} }
func (s *fakeSocket) start(c *exec.Cmd) (child, error) { s.calls.Add(1); return s.launch(c) }
func commandFixture(t *testing.T) *exec.Cmd {
	t.Helper()
	p, err := filepath.Abs("synthetic-unused-executable")
	if err != nil {
		t.Fatal(err)
	}
	return exec.Command(p)
}
func endpointFixture(t *testing.T, s *fakeSocket) *Endpoint {
	t.Helper()
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	return e
}
func await(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(time.Second):
		t.Fatal("fixture did not drain")
	}
}

func TestInheritanceTransactionRestoresOriginalBit(t *testing.T) {
	for _, original := range []uint32{0, 1, 2, 3} {
		var writes []uint32
		p := newChild(true)
		got, err := inheritOriginal(func() (child, error) { return p, nil }, func() (uint32, error) { return original, nil }, func(v uint32) error { writes = append(writes, v); return nil })
		if err != nil || got != p || len(writes) != 2 || writes[0] != 1 || writes[1] != original&1 {
			t.Fatalf("original %d: child=%v error=%v writes=%v", original, got, err, writes)
		}
	}
}
func TestInheritanceTransactionFailsClosed(t *testing.T) {
	boom := errors.New("injected")
	for _, stage := range []string{"get", "set", "start", "restore"} {
		t.Run(stage, func(t *testing.T) {
			calls, sets := 0, 0
			p := newChild(true)
			got, err := inheritOriginal(func() (child, error) {
				calls++
				if stage == "start" {
					return nil, boom
				}
				return p, nil
			}, func() (uint32, error) {
				if stage == "get" {
					return 0, boom
				}
				return 2, nil
			}, func(v uint32) error {
				sets++
				if stage == "set" && sets == 1 || stage == "restore" && sets == 2 {
					return boom
				}
				return nil
			})
			if !errors.Is(err, boom) {
				t.Fatal(err)
			}
			if stage == "get" && (calls != 0 || sets != 0) || stage == "set" && (calls != 0 || sets != 2) {
				t.Fatalf("failure launched or failed restoration: %d %d", calls, sets)
			}
			if stage == "restore" && got != p {
				t.Fatal("lost ownership after restoration failed")
			}
		})
	}
}
func TestEndpointRejectsCommandsAndRepeatedStart(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if _, err := e.Accept(); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	bad := commandFixture(t)
	bad.Stdout = io.Discard
	cancel := commandFixture(t)
	cancel.Cancel = func() error { return nil }
	relative := exec.Command("relative")
	for _, cmd := range []*exec.Cmd{nil, bad, cancel, relative} {
		if err := e.Start(cmd); !errors.Is(err, ErrCommand) {
			t.Fatal(err)
		}
	}
	if s.calls.Load() != 0 {
		t.Fatal("invalid command reached launch")
	}
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(commandFixture(t)); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("restarted endpoint")
	}
}
func TestEndpointKeepsListenerUntilChildReaped(t *testing.T) {
	s := socketFixture()
	p := newChild(false)
	t.Cleanup(p.end)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	await(t, p.killed)
	if s.closed.Load() {
		t.Fatal("released listener while child could produce requests")
	}
	if _, err = e.Accept(); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	p.end()
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.closed.Load() {
		t.Fatal("did not release listener after child reaped")
	}
}
func TestEndpointShutdownDuringStartRetainsOwnership(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	entered, release := make(chan struct{}), make(chan struct{})
	s.launch = func(*exec.Cmd) (child, error) { close(entered); <-release; return p, nil }
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	cmd := commandFixture(t)
	go func() { result <- e.Start(cmd) }()
	await(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if s.closed.Load() {
		t.Fatal("closed original socket while startup owned it")
	}
	close(release)
	if err = <-result; !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	await(t, p.done)
}
func TestEndpointFailedRestoreStopsAcquiredChild(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	boom := errors.New("restore")
	s.launch = func(*exec.Cmd) (child, error) { return p, boom }
	e := endpointFixture(t, s)
	if err := e.Start(commandFixture(t)); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := e.Accept(); !errors.Is(err, ErrState) {
		t.Fatal("failed start admitted a connection")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	await(t, p.done)
}
func TestEndpointShutdownClosesConnectionsAndBlockedAccept(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer b.Close()
	s.conns <- a
	c, err := e.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	acceptDone := make(chan struct{})
	go func() { defer close(acceptDone); _, _ = e.Accept() }()
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	await(t, acceptDone)
	if _, err = b.Write([]byte("synthetic")); err == nil {
		t.Fatal("connection survived shutdown")
	}
}
func TestEndpointChildExitClosesEndpoint(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	p.end()
	await(t, e.done)
	if !s.closed.Load() {
		t.Fatal("listener outlived producer")
	}
}
func TestEndpointConcurrentShutdownIsIdempotent(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.calls.Load() != 1 || !s.closed.Load() {
		t.Fatal("ownership changed during close")
	}
}

type retryChild struct {
	done   chan struct{}
	failed atomic.Bool
	killed sync.Once
}

func (p *retryChild) Kill() { p.killed.Do(func() { close(p.done) }) }
func (p *retryChild) Wait() error {
	<-p.done
	if p.failed.Load() {
		return errors.New("owned job query failed")
	}
	return nil
}
func TestEndpointUnprovenDrainRetainsListenerAndRetriesSameChild(t *testing.T) {
	s := socketFixture()
	p := &retryChild{done: make(chan struct{})}
	p.failed.Store(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.failed.Store(false)
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err == nil {
		t.Fatal("query failure became a clean shutdown")
	}
	if s.closed.Load() {
		t.Fatal("unknown live producers lost original listener")
	}
	p.failed.Store(false)
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.closed.Load() || s.calls.Load() != 1 {
		t.Fatal("retry replaced child or failed release")
	}
}
func TestEndpointZeroValueFailsClosed(t *testing.T) {
	e := &Endpoint{}
	if err := e.Start(commandFixture(t)); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if _, err := e.Accept(); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if err := e.Close(); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
}

type joinedDrainChild struct{ retryChild }

func (p *joinedDrainChild) Wait() error {
	<-p.done
	if p.failed.Load() {
		return errors.Join(&exec.ExitError{}, errors.New("drain not established"))
	}
	return nil
}
func TestEndpointJoinedExitDrainErrorDoesNotReleaseListener(t *testing.T) {
	s := socketFixture()
	p := &joinedDrainChild{retryChild: retryChild{done: make(chan struct{})}}
	p.failed.Store(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e, err := newEndpoint(s)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.failed.Store(false)
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err == nil {
		t.Fatal("joined drain failure discarded")
	}
	if s.closed.Load() {
		t.Fatal("released listener without drain proof")
	}
	p.failed.Store(false)
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
}
