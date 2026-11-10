package appserverbridge

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

// ownedEndpointStreams retains raw Windows pipes until all actual I/O has
// returned. Canceling the wrapper never closes a file underneath a blocked OS
// Read/Write. A failed process drain or uncooperative frontend may leave this
// owner quarantined indefinitely.
// Its goroutine, scanner and outstanding writers retain all needed resources.
type ownedEndpointStreams struct {
	input    io.WriteCloser
	output   io.ReadCloser
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	writes   sync.WaitGroup
	reads    sync.WaitGroup
	released chan struct{}
	once     sync.Once
	closeErr error // published by released
}

func newOwnedEndpointStreams(input io.WriteCloser, output io.ReadCloser) *ownedEndpointStreams {
	ctx, cancel := context.WithCancel(context.Background())
	return &ownedEndpointStreams{input: input, output: output, ctx: ctx, cancel: cancel, released: make(chan struct{})}
}
func (s *ownedEndpointStreams) Write(data []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	s.writes.Add(1)
	s.mu.Unlock()
	// The OS writer owns its copy even after the bridge stops waiting for it.
	copied := append([]byte(nil), data...)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { defer s.writes.Done(); n, err := s.input.Write(copied); done <- result{n, err} }()
	select {
	case r := <-done:
		return r.n, r.err
	case <-s.ctx.Done():
		return 0, s.ctx.Err()
	}
}
func (s *ownedEndpointStreams) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	s.reads.Add(1)
	s.mu.Unlock()
	type result struct {
		data []byte
		n    int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer s.reads.Done()
		buffer := make([]byte, len(p))
		n, err := s.output.Read(buffer)
		done <- result{buffer, n, err}
	}()
	select {
	case r := <-done:
		copy(p, r.data[:r.n])
		return r.n, r.err
	case <-s.ctx.Done():
		return 0, s.ctx.Err()
	}
}

func (s *ownedEndpointStreams) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	return nil // This closes admission, not the retained raw pipes.
}
func (s *ownedEndpointStreams) retainCleanup() {
	s.once.Do(func() {
		_ = s.Close()
		go func() {
			// The two handles are independent. Closing stdin after writes end
			// delivers EOF without waiting for stdout to finish first.
			inputClosed := make(chan error, 1)
			go func() { s.writes.Wait(); inputClosed <- closeEndpointPipe(s.input) }()
			s.reads.Wait()
			outputErr := closeEndpointPipe(s.output)
			s.closeErr = errors.Join(<-inputClosed, outputErr)
			close(s.released)
		}()
	})
}
func (s *ownedEndpointStreams) waitReleased(ctx context.Context) error {
	s.retainCleanup()
	select {
	case <-s.released:
		return s.closeErr
	case <-ctx.Done():
		return errors.New("owned bridge stdio remains quarantined")
	}
}

func closeEndpointPipe(pipe io.Closer) error {
	if pipe == nil {
		return nil
	}
	err := pipe.Close()
	// Tolerate only an ordinary already-closed handle, never a joined or
	// wrapped error that may carry an independent cleanup failure.
	if err == os.ErrClosed {
		return nil
	}
	if pathErr, ok := err.(*os.PathError); ok && pathErr.Err == os.ErrClosed {
		return nil
	}
	return err
}

// Explicit files keep exec.Cmd.Wait independent of parent pipe cleanup. With
// StdinPipe/StdoutPipe, Wait can otherwise close a synchronous pipe while its
// Read is blocked by a surviving descendant, hiding the root-exit observation.
func ownedCommandPipes(command *exec.Cmd) (io.WriteCloser, io.ReadCloser, func(), error) {
	childIn, parentIn, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	parentOut, childOut, err := os.Pipe()
	if err != nil {
		childIn.Close()
		parentIn.Close()
		return nil, nil, nil, err
	}
	command.Stdin = childIn
	command.Stdout = childOut
	closeChild := func() { childIn.Close(); childOut.Close() }
	return parentIn, parentOut, closeChild, nil
}
