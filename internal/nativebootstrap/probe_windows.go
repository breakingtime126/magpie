package nativebootstrap

import (
	"context"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Completion is separate from record handoff: only the owner can authorize
// exposing a successful inspection after revalidating its current source.
type probeCompletion struct {
	mu      sync.Mutex
	used    bool
	owner   bool
	request chan probeRequest
}
type probeRequest struct {
	ctx    context.Context
	check  func(context.Context) error
	result chan error
}

// ValidateProbe waits for inspection completion before invoking check and
// acknowledging success. The callback must honor its bounded context.
func (l *Lease) ValidateProbe(ctx context.Context, check func(context.Context) error) error {
	if check == nil {
		return ErrUnavailable
	}
	return l.requestProbe(ctx, true, check)
}

// FinishProbe returns success only after the live owner revalidates completion.
// No inspection output may be exposed before it succeeds.
func (l *Lease) FinishProbe(ctx context.Context) error {
	return l.requestProbe(ctx, false, nil)
}
func (l *Lease) requestProbe(ctx context.Context, owner bool, check func(context.Context) error) error {
	if ctx == nil || l.probe == nil || l.probe.owner != owner {
		return ErrUnavailable
	}
	l.probe.mu.Lock()
	if l.probe.used {
		l.probe.mu.Unlock()
		return ErrUnavailable
	}
	l.probe.used = true
	l.probe.mu.Unlock()
	r := probeRequest{ctx: ctx, check: check, result: make(chan error, 1)}
	select {
	case l.probe.request <- r:
	case <-ctx.Done():
		l.cancel()
		return ctx.Err()
	case <-l.ctx.Done():
		return ErrUnavailable
	}
	select {
	case err := <-r.result:
		return err
	case <-ctx.Done():
		l.cancel()
		return ctx.Err()
	case <-l.ctx.Done():
		return ErrUnavailable
	}
}
func (l *Lease) completeProbe(h windows.Handle, p *peerProcess, policy Peer) error {
	var r probeRequest
	select {
	case r = <-l.probe.request:
	case <-l.ctx.Done():
		return ErrUnavailable
	}
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	err := func() error {
		// The owner may wait for the inspection itself. Once the completion
		// request arrives, both the revalidation and acknowledgement are bounded.
		if l.probe.owner {
			var request [1]byte
			if err := readExact(ctx, h, request[:]); err != nil {
				return err
			}
			if request[0] != 'Q' || !probeInputEmpty(h) {
				return ErrUnavailable
			}
		}
		bounded, stop := context.WithTimeout(ctx, handshakeLimit)
		defer stop()
		if err := p.check(bounded, h, !l.probe.owner, policy); err != nil {
			return err
		}
		if l.probe.owner {
			if err := r.check(bounded); err != nil {
				return ErrUnavailable
			}
			livePolicy := policy
			livePolicy.Check = nil
			if err := p.check(bounded, h, false, livePolicy); err != nil {
				return err
			}
			if !probeInputEmpty(h) {
				return ErrUnavailable
			}
			return writeExact(bounded, h, []byte{'A'})
		}
		if err := writeExact(bounded, h, []byte{'Q'}); err != nil {
			return err
		}
		var answer [1]byte
		if err := readExact(bounded, h, answer[:]); err != nil {
			return err
		}
		if answer[0] != 'A' || !probeInputEmpty(h) {
			return ErrUnavailable
		}
		return p.check(bounded, h, true, policy)
	}()
	r.result <- err
	return err
}

var peekProbePipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

// Reject queued bytes beyond the single completion frame. Bytes arriving later
// are rejected by the ordinary lease monitor; no completion can be replayed.
func probeInputEmpty(h windows.Handle) bool {
	var available uint32
	ok, _, _ := peekProbePipe.Call(uintptr(h), 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
	return ok != 0 && available == 0
}
