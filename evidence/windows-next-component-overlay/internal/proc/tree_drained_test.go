package proc

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type drainFixture struct {
	root                                chan struct{}
	activeCount                         atomic.Uint32
	stops, queries, releases            atomic.Int32
	queryFails, stopFails, releaseFails atomic.Bool
	rootErr                             error
}

func newDrainFixture() (*drainFixture, *DrainedTree) {
	f := &drainFixture{root: make(chan struct{})}
	d := newDrainedTree(f.root, func() error { return f.rootErr }, func() error {
		f.stops.Add(1)
		if f.stopFails.Load() {
			return errors.New("stop")
		}
		return nil
	}, func() (uint32, error) {
		f.queries.Add(1)
		if f.queryFails.Load() {
			return 0, errors.New("query")
		}
		return f.activeCount.Load(), nil
	}, func() error {
		if f.releaseFails.Load() {
			return errors.New("release")
		}
		f.releases.Add(1)
		return nil
	})
	return f, d
}
func TestDrainedTreeWaitsForRootAndAllDescendants(t *testing.T) {
	f, d := newDrainFixture()
	f.activeCount.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := d.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if f.queries.Load() != 0 || f.releases.Load() != 0 {
		t.Fatal("root was not reaped")
	}
	close(f.root)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel2()
	if err := d.Wait(ctx2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if f.releases.Load() != 0 || f.stops.Load() == 0 {
		t.Fatal("released a job with producers")
	}
	f.activeCount.Store(0)
	if err := d.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.releases.Load() != 1 {
		t.Fatal("job was not released after verified zero")
	}
}
func TestDrainedTreeErrorsRetainOwnedJob(t *testing.T) {
	for _, stage := range []string{"query", "stop", "release"} {
		t.Run(stage, func(t *testing.T) {
			f, d := newDrainFixture()
			close(f.root)
			flag := &f.queryFails
			if stage == "stop" {
				flag = &f.stopFails
			}
			if stage == "release" {
				flag = &f.releaseFails
			}
			flag.Store(true)
			if err := d.Wait(context.Background()); err == nil {
				t.Fatal("unknown drain accepted")
			}
			if f.releases.Load() != 0 {
				t.Fatal("released unproven ownership")
			}
			flag.Store(false)
			if err := d.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			if f.releases.Load() != 1 {
				t.Fatal("retry did not drain the same job")
			}
		})
	}
}
func TestDrainedTreeUnexpectedRootWaitErrorRetainsJob(t *testing.T) {
	f, d := newDrainFixture()
	f.rootErr = errors.New("root reap uncertain")
	close(f.root)
	if err := d.Wait(context.Background()); !errors.Is(err, f.rootErr) {
		t.Fatal(err)
	}
	if f.releases.Load() != 0 || f.stops.Load() == 0 {
		t.Fatal("unknown root was treated as drained")
	}
}
func TestDrainedTreeTerminationExitIsNotDrainFailure(t *testing.T) {
	f, d := newDrainFixture()
	f.rootErr = &exec.ExitError{}
	close(f.root)
	if err := d.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.releases.Load() != 1 {
		t.Fatal("verified terminated job retained")
	}
}
func TestDrainedTreeConcurrentWaitAndStop(t *testing.T) {
	f, d := newDrainFixture()
	close(f.root)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Stop(); err != nil {
				t.Error(err)
			}
			if err := d.Wait(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.releases.Load() != 1 {
		t.Fatal("job released more than once")
	}
}
func TestDrainedTreeCancelledGateDoesNotReleaseJob(t *testing.T) {
	f, d := newDrainFixture()
	close(f.root)
	d.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-d.gate
	if f.releases.Load() != 0 {
		t.Fatal("cancelled wait released handle")
	}
	if err := d.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestDrainedTreeRetainsBeforeAssignmentAndKeepsFailureOwnership(t *testing.T) {
	boom := errors.New("injected")
	for _, stage := range []string{"retain", "assign", "success"} {
		retained, assigned := false, false
		err := retainBeforeAssign(func() error {
			if stage == "retain" {
				return boom
			}
			retained = true
			return nil
		}, func() error {
			if !retained {
				t.Fatal("assigned before retaining job")
			}
			assigned = true
			if stage == "assign" {
				return boom
			}
			return nil
		})
		if stage == "retain" && (retained || assigned) || stage == "assign" && (!retained || !assigned) {
			t.Fatal("lost failure cleanup ownership")
		}
		if stage == "success" && err != nil || stage != "success" && !errors.Is(err, boom) {
			t.Fatal(err)
		}
	}
}

func TestDrainedTreeJoinedExitAndUnprovenRootCannotReleaseEmptyJob(t *testing.T) {
	f, d := newDrainFixture()
	f.rootErr = errors.Join(&exec.ExitError{}, errors.New("root reap unproven"))
	close(f.root)
	if err := d.Wait(context.Background()); err == nil {
		t.Fatal("joined startup failure was mistaken for an ordinary exit")
	}
	if f.releases.Load() != 0 {
		t.Fatal("empty unassigned job discharged unproven root")
	}
}
