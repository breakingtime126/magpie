package proc

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// DrainedTree retains the strictly assigned Windows Job until root reaping and
// an observed zero active-process count. It exposes no handle or PID authority.
// It is a staged primitive with no production callers. Wait owns the root wait
// exactly once internally; callers must never Wait the command themselves.
type DrainedTree struct {
	rootDone  <-chan struct{}
	rootError func() error
	terminate func() error
	active    func() (uint32, error)
	release   func() error
	gate      chan struct{}
	mu        sync.Mutex
	released  bool
}

func newDrainedTree(rootDone <-chan struct{}, rootError func() error, terminate func() error, active func() (uint32, error), release func() error) *DrainedTree {
	return &DrainedTree{rootDone: rootDone, rootError: rootError, terminate: terminate, active: active, release: release, gate: make(chan struct{}, 1)}
}

// Stop requests termination of the retained Job. Failure is explicit and
// retains ownership. Root exit alone never substitutes for draining the Job.
func (t *DrainedTree) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return nil
	}
	return t.terminate()
}

// Wait observes root reaping, terminates remaining descendants, and queries
// the retained Job until ActiveProcesses is zero. Only then is its handle
// closed. A deadline, query, termination, or unexpected root-wait error retains
// the handle and is NOT proof that request producers stopped. A later Wait may
// retry the same owned Job; there is no PID reopening or replacement discovery.
func (t *DrainedTree) Wait(ctx context.Context) error {
	if ctx == nil {
		return errors.New("process drain requires context")
	}
	select {
	case t.gate <- struct{}{}:
		defer func() { <-t.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	t.mu.Lock()
	released := t.released
	t.mu.Unlock()
	if released {
		return nil
	}
	select {
	case <-t.rootDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := t.Stop(); err != nil {
		return err
	}
	if err := t.rootError(); err != nil {
		if _, expectedExit := err.(*exec.ExitError); !expectedExit {
			return err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		count, err := t.active()
		if err == nil && count == 0 {
			err = t.release()
			if err == nil {
				t.released = true
			}
		}
		t.mu.Unlock()
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// retainBeforeAssign keeps cleanup ownership even when assignment fails.
func retainBeforeAssign(retain, assign func() error) error {
	if err := retain(); err != nil {
		return err
	}
	return assign()
}
