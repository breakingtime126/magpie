// Package nativeendpoint stages original-socket inheritance for a cooperative
// Windows child. It has no production caller and does not prove native-runtime
// handle retention. See README.md before considering integration.
package nativeendpoint

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrState   = errors.New("native endpoint is not available in this lifecycle state")
	ErrCommand = errors.New("native endpoint requires an unstarted, directly owned command with file stdio")
)

const shutdownLimit = 3 * time.Second

type child interface {
	Wait() error
	Kill()
}
type backend interface {
	net.Listener
	start(*exec.Cmd) (child, error)
	stopAccept() error
}

// Endpoint is a single child and its original listener. Start must succeed
// before serving. It deliberately exposes neither File nor SyscallConn: a
// duplicate socket or an independently closed handle would break ownership.
// A child must retain its inherited listener until it and all descendants have
// stopped producing requests. This requirement is NOT verified by this type.
type Endpoint struct {
	socket                     backend
	address                    net.TCPAddr
	mu                         sync.Mutex
	attempted, stopping, ready bool
	startDone                  chan struct{}
	process                    child
	firstWaitDone              chan struct{}
	connections                map[*connection]struct{}
	acceptors                  sync.WaitGroup
	stop, done                 chan struct{}
	stopOnce                   sync.Once
	cleanupErr                 error
	childDrainErr              error
	failure                    *drainFailure
	waitingRetry               bool
	retry                      chan struct{}
}

type drainFailure struct {
	done chan struct{}
	err  error
}

func newEndpoint(socket backend) (*Endpoint, error) {
	a, ok := socket.Addr().(*net.TCPAddr)
	if !ok || !a.IP.Equal(net.IPv4(127, 0, 0, 1)) || a.Zone != "" || a.Port <= 0 {
		_ = socket.Close()
		return nil, errors.New("native endpoint requires a bound IPv4 loopback socket")
	}
	e := &Endpoint{socket: socket, address: *a, connections: map[*connection]struct{}{}, stop: make(chan struct{}), done: make(chan struct{})}
	e.failure = &drainFailure{done: make(chan struct{})}
	e.retry = make(chan struct{}, 1)
	e.address.IP = append(net.IP(nil), a.IP...)
	go e.cleanup()
	return e, nil
}

// Start makes one attempt. It does not resolve an executable, edit its
// arguments/environment, or establish executable trust. Only this Endpoint may
// Wait or Kill the command. Cancellation belongs to Shutdown, not exec.Cmd.
func (e *Endpoint) Start(cmd *exec.Cmd) error {
	if e == nil || e.socket == nil {
		return ErrState
	}
	if cmd == nil || cmd.Process != nil || cmd.ProcessState != nil || !filepath.IsAbs(cmd.Path) || cmd.Cancel != nil || len(cmd.ExtraFiles) != 0 {
		return ErrCommand
	}
	for _, stream := range []any{cmd.Stdin, cmd.Stdout, cmd.Stderr} {
		if stream != nil {
			if _, ok := stream.(*os.File); !ok {
				return ErrCommand
			}
		}
	}
	e.mu.Lock()
	if e.attempted || e.stopping {
		e.mu.Unlock()
		return ErrState
	}
	e.attempted = true
	e.startDone = make(chan struct{})
	started := e.startDone
	e.mu.Unlock()
	p, err := e.socket.start(cmd)
	if p == nil && err == nil {
		err = errors.New("native endpoint start returned no owned child")
	}
	e.mu.Lock()
	e.process = p
	e.ready = p != nil && err == nil && !e.stopping
	if p != nil {
		e.firstWaitDone = make(chan struct{})
	}
	done := e.firstWaitDone
	close(started)
	stopped := e.stopping
	e.mu.Unlock()
	if p != nil {
		go func() {
			waitErr := p.Wait()
			e.mu.Lock()
			e.childDrainErr = waitErr
			close(done)
			e.mu.Unlock()
			e.requestStop()
		}()
	}
	if err != nil || stopped {
		e.requestStop()
		return errors.Join(err, ErrState)
	}
	return nil
}

// Accept admits connections only after the child was started. Closing or child
// exit revokes admission before closing any socket. The accepted connections
// are tracked so shutdown closes them, even if a caller forgets to do so.
func (e *Endpoint) Accept() (net.Conn, error) {
	if e == nil || e.socket == nil {
		return nil, ErrState
	}
	e.mu.Lock()
	if e.stopping || !e.ready {
		e.mu.Unlock()
		return nil, ErrState
	}
	e.acceptors.Add(1)
	e.mu.Unlock()
	defer e.acceptors.Done()
	raw, err := e.socket.Accept()
	if err != nil {
		e.mu.Lock()
		stopping := e.stopping
		e.mu.Unlock()
		if stopping {
			// stopAccept interrupts the pending call without releasing the bind.
			// Its deadline error must not tell callers to retry revoked admission.
			return nil, ErrState
		}
		return nil, err
	}
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		_ = raw.Close()
		return nil, ErrState
	}
	c := &connection{Conn: raw, owner: e}
	e.connections[c] = struct{}{}
	e.mu.Unlock()
	return c, nil
}

func (e *Endpoint) Addr() net.Addr {
	if e == nil || e.socket == nil {
		return nil
	}
	a := e.address
	a.IP = append(net.IP(nil), a.IP...)
	return &a
}

func (e *Endpoint) requestStop() {
	e.stopOnce.Do(func() { e.mu.Lock(); e.stopping = true; e.mu.Unlock(); close(e.stop) })
}

func (e *Endpoint) cleanup() {
	<-e.stop
	e.mu.Lock()
	started := e.startDone
	e.mu.Unlock()
	if started != nil {
		<-started
	}
	e.mu.Lock()
	p, firstWaitDone := e.process, e.firstWaitDone
	conns := make([]*connection, 0, len(e.connections))
	for c := range e.connections {
		conns = append(conns, c)
	}
	e.mu.Unlock()
	// Keep the listening socket bound until the owned child tree is confirmed drained.
	// A timeout does not release a URL a still-running producer could retry.
	if err := e.socket.stopAccept(); err != nil {
		e.mu.Lock()
		e.cleanupErr = errors.Join(e.cleanupErr, err)
		e.mu.Unlock()
	}
	if p != nil {
		p.Kill()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if firstWaitDone != nil {
		<-firstWaitDone
	}
	e.mu.Lock()
	drainErr := e.childDrainErr
	e.mu.Unlock()
	for drainErr != nil {
		// Quarantine the still-bound listener on an unproven drain. The owned
		// goroutine keeps the socket and Job reachable even if a caller drops
		// Endpoint after an error; a later Shutdown retries the SAME owned Job.
		e.mu.Lock()
		e.failure.err = drainErr
		e.waitingRetry = true
		close(e.failure.done)
		e.mu.Unlock()
		<-e.retry
		drainErr = p.Wait()
	}
	err := e.socket.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	e.acceptors.Wait()
	e.mu.Lock()
	e.cleanupErr = errors.Join(e.cleanupErr, err)
	e.mu.Unlock()
	close(e.done)
}

// Shutdown is bounded even if a child or an OS startup call does not finish.
// On timeout cleanup remains owned and the listening socket stays bound until
// the child tree is drained. A failed drain retains the socket and reports an
// error; it never declares the endpoint reusable. This drains socket operations, NOT HTTP handler work:
// the serving layer must separately cancel and drain its handlers.
func (e *Endpoint) Shutdown(parent context.Context) error {
	if e == nil || e.socket == nil {
		return ErrState
	}
	if parent == nil {
		return errors.New("native endpoint shutdown requires context")
	}
	e.requestStop()
	e.mu.Lock()
	if e.waitingRetry {
		e.waitingRetry = false
		e.failure = &drainFailure{done: make(chan struct{})}
		select {
		case e.retry <- struct{}{}:
		default:
		}
	}
	failure := e.failure
	e.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, shutdownLimit)
	defer cancel()
	select {
	case <-e.done:
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.cleanupErr
	case <-failure.done:
		return failure.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Endpoint) Close() error { return e.Shutdown(context.Background()) }

type connection struct {
	net.Conn
	owner *Endpoint
	once  sync.Once
	err   error
}

func (c *connection) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		if errors.Is(c.err, net.ErrClosed) {
			c.err = nil
		}
		c.owner.mu.Lock()
		delete(c.owner.connections, c)
		c.owner.cleanupErr = errors.Join(c.owner.cleanupErr, c.err)
		c.owner.mu.Unlock()
	})
	return c.err
}

// inheritOriginal changes only the inheritance bit. A restoration failure
// returns the acquired child as well as the error so its owner can stop/reap it.
func inheritOriginal(start func() (child, error), get func() (uint32, error), set func(uint32) error) (p child, result error) {
	original, err := get()
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, set(original&1)) }()
	if err = set(1); err != nil {
		return nil, err
	}
	return start()
}
