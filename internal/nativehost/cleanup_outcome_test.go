package nativehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativeconnect"
)

type failedAwaitOwner struct {
	*fakeOwner
	failure error
}

func (o *failedAwaitOwner) Await(context.Context) (ownerLease, error) { return o.lease, o.failure }

func TestHostFailedAwaitCleanupCanRecoverIndependentlyOfOutcome(t *testing.T) {
	cfg, root, launcher, cap, owner := fixture(t)
	failure := errors.New("synthetic failed bootstrap")
	drainFailure := errors.New("synthetic lease not drained")
	owner.lease.err = drainFailure
	awaiting := &failedAwaitOwner{fakeOwner: owner, failure: failure}
	h, err := start(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return launcher, nil },
		listen:  func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return awaiting, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("failed Await did not finish cleanup attempt")
	}
	if err := closeHost(t, h); !errors.Is(err, drainFailure) {
		t.Fatal("actual failed drain was hidden", err)
	}
	if outcome, done := h.Outcome(); !done || !errors.Is(outcome, failure) {
		t.Fatal("bootstrap outcome lost", outcome, done)
	}
	// Close has joined the supervisor, so no background cleanup can read this
	// injected result while it is changed. The same actual Host is retried.
	owner.lease.err = nil
	if err := closeHost(t, h); err != nil {
		t.Fatal("historical failure prevented verified cleanup", err)
	}
	if outcome, done := h.Outcome(); !done || !errors.Is(outcome, failure) {
		t.Fatal("successful drain erased bootstrap failure", outcome, done)
	}
	if owner.lease.closes.Load() < 2 {
		t.Fatal("lease cleanup was not retried")
	}
}
