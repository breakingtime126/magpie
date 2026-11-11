package nativehost

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativeconnect"
)

type checkedProbeLease struct {
	*fakeLease
	started           chan struct{}
	finish            chan struct{}
	validated         chan error
	completionContext context.Context
}

func (l *checkedProbeLease) ValidateProbe(ctx context.Context, check func(context.Context) error) error {
	close(l.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.finish:
	}
	err := check(ctx)
	l.completionContext = ctx
	l.validated <- err
	return err
}

type checkedOwner struct {
	ready    chan struct{}
	peer     nativebootstrap.Peer
	lease    ownerLease
	closes   atomic.Int32
	awaiting chan struct{}
}

func (o *checkedOwner) Await(ctx context.Context) (ownerLease, error) {
	close(o.awaiting)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.ready:
	}
	if err := o.peer.Check(ctx, nativebootstrap.Process{}); err != nil {
		return nil, err
	}
	if err := o.peer.Check(ctx, nativebootstrap.Process{}); err != nil {
		return nil, err
	}
	return o.lease, nil
}
func (o *checkedOwner) Close() error { o.closes.Add(1); return nil }

func probeFixture(t *testing.T, admission func(context.Context) error) (*Host, *checkedOwner, *checkedOwner, *checkedProbeLease, *fakeCapability, nativeconnect.PackagedLauncher) {
	t.Helper()
	cfg, root, pinned, cap, launch := fixture(t)
	cfg.StartupProbe = appserverbridge.StartupProbeCompareUnrestricted167
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lease := &checkedProbeLease{fakeLease: &fakeLease{ctx: ctx, cancel: cancel}, started: make(chan struct{}), finish: make(chan struct{}), validated: make(chan error, 1)}
	probeOwner := &checkedOwner{ready: make(chan struct{}), lease: lease, awaiting: make(chan struct{})}
	launchOwner := &checkedOwner{ready: make(chan struct{}), lease: launch.lease, awaiting: make(chan struct{})}
	var record nativebootstrap.Record
	h, err := startWithProbeListener(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return pinned, nil },
		listen: func(r nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
			if r != record {
				t.Error("probe and JSONL used different immutable records")
			}
			launchOwner.peer = p
			return launchOwner, nil
		},
	}, admission, func(r nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
		record = r
		probeOwner.peer = p
		return probeOwner, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeHost(t, h); err != nil {
			t.Error(err)
		}
	})
	<-probeOwner.awaiting
	<-launchOwner.awaiting
	return h, probeOwner, launchOwner, lease, cap, pinned
}
func waitHostState(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("host state not reached")
		case <-time.After(time.Millisecond):
		}
	}
}

// phase 1 reserves probe admission before Await's second peer check. Waiting
// for ValidateProbe instead proves that Await transferred the lease to Host.
func waitProbeLease(t *testing.T, lease *checkedProbeLease) {
	t.Helper()
	select {
	case <-lease.started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe lease was not acquired")
	}
}

func TestProbeCompletesWithoutConsumingJSONL(t *testing.T) {
	h, p, j, l, _, _ := probeFixture(t, nil)
	close(p.ready)
	close(l.finish)
	if err := <-l.validated; err != nil {
		t.Fatal(err)
	}
	l.cancel()
	waitHostState(t, func() bool { return l.closes.Load() > 0 })
	if h.Consumed() || h.Context().Err() != nil {
		t.Fatal("probe consumed or canceled generation")
	}
	if _, err := h.Plan(); err != nil {
		t.Fatal(err)
	}
	if err := p.peer.Check(context.Background(), nativebootstrap.Process{}); err == nil {
		t.Fatal("repeat probe accepted")
	}
	close(j.ready)
	waitHostState(t, h.Consumed)
	if h.Context().Err() != nil {
		t.Fatal("JSONL was canceled by probe completion")
	}
	if err := p.peer.Check(context.Background(), nativebootstrap.Process{}); err == nil {
		t.Fatal("probe after JSONL accepted")
	}
	if err := closeHost(t, h); err != nil {
		t.Fatal(err)
	}
	if p.closes.Load() != 1 || j.closes.Load() != 1 || j.lease.Context().Err() == nil {
		t.Fatal("owners not independently drained")
	}
}
func TestDirectJSONLSkipsUnusedProbe(t *testing.T) {
	h, p, j, _, _, _ := probeFixture(t, nil)
	close(j.ready)
	waitHostState(t, h.Consumed)
	if h.Context().Err() != nil {
		t.Fatal("unused probe cancellation killed JSONL")
	}
	if err := p.peer.Check(context.Background(), nativebootstrap.Process{}); err == nil {
		t.Fatal("late probe admitted")
	}
}
func TestProbeCompletionRevalidatesSourceAndLauncher(t *testing.T) {
	for _, mode := range []string{"source", "launcher", "gateway"} {
		t.Run(mode, func(t *testing.T) {
			var stale atomic.Bool
			failure := errors.New("synthetic stale source")
			h, p, _, l, cap, pinned := probeFixture(t, func(context.Context) error {
				if stale.Load() {
					return failure
				}
				return nil
			})
			close(p.ready)
			waitProbeLease(t, l)
			switch mode {
			case "source":
				stale.Store(true)
			case "launcher":
				if err := os.WriteFile(pinned.Path, []byte("changed synthetic bytes"), 0700); err != nil {
					t.Fatal(err)
				}
			case "gateway":
				cap.valid.Store(false)
			}
			close(l.finish)
			select {
			case err := <-l.validated:
				if err == nil {
					t.Fatal("stale probe acknowledged")
				}
			case <-h.Context().Done():
			}
			waitHostState(t, func() bool { _, done := h.Outcome(); return done })
			if h.Consumed() {
				t.Fatal("failed probe consumed JSONL")
			}
		})
	}
}
func TestProbeRevocationDrainsBothOwners(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting", true: "active"}[active], func(t *testing.T) {
			h, p, j, l, _, _ := probeFixture(t, nil)
			if active {
				close(p.ready)
				waitProbeLease(t, l)
			}
			if err := closeHost(t, h); err != nil {
				t.Fatal(err)
			}
			if p.closes.Load() != 1 || j.closes.Load() != 1 {
				t.Fatal("listener cleanup missing")
			}
			if active && l.Context().Err() == nil {
				t.Fatal("probe lease not revoked")
			}
		})
	}
}

func TestJSONLRejectsOverlappingProbe(t *testing.T) {
	h, p, j, l, _, _ := probeFixture(t, nil)
	close(p.ready)
	waitProbeLease(t, l)
	close(j.ready)
	waitHostState(t, func() bool { _, done := h.Outcome(); return done })
	if h.Consumed() {
		t.Fatal("overlapping JSONL consumed generation")
	}
	if outcome, done := h.Outcome(); !done || outcome == nil {
		t.Fatal("overlap failure was hidden")
	}
}

func TestProbeDrainFailureRemainsRetryable(t *testing.T) {
	h, p, _, l, _, _ := probeFixture(t, nil)
	failure := errors.New("synthetic probe lease not drained")
	l.err = failure
	close(p.ready)
	close(l.finish)
	if err := <-l.validated; err != nil {
		t.Fatal(err)
	}
	l.cancel()
	waitHostState(t, func() bool { _, done := h.Outcome(); return done })
	if err := closeHost(t, h); !errors.Is(err, failure) {
		t.Fatal("failed probe drain hidden", err)
	}
	l.err = nil // done joins both await workers and first cleanup attempt.
	if err := closeHost(t, h); err != nil {
		t.Fatal("probe drain could not be retried", err)
	}
	if err, done := h.Outcome(); !done || !errors.Is(err, failure) {
		t.Fatal("retry erased probe outcome", err)
	}
}

func TestProbeListenerFailureRetainsPartialOwnership(t *testing.T) {
	cfg, root, pinned, cap, probe := fixture(t)
	cfg.StartupProbe = appserverbridge.StartupProbeCompareUnrestricted167
	failure := errors.New("synthetic probe listener failure")
	listens := 0
	h, err := startWithProbeListener(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return pinned, nil },
		listen:  func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { listens++; return nil, nil },
	}, nil, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return probe, failure })
	if !errors.Is(err, failure) || h == nil || listens != 0 {
		t.Fatal("partial probe ownership lost", err)
	}
	if err := closeHost(t, h); err != nil {
		t.Fatal(err)
	}
	if probe.closes.Load() != 1 {
		t.Fatal("partial listener not closed exactly once")
	}
	if outcome, done := h.Outcome(); !done || !errors.Is(outcome, failure) {
		t.Fatal("preparation outcome lost", outcome)
	}
}

func TestAcknowledgedProbeSurvivesConcurrentJSONLAdmission(t *testing.T) {
	h, p, j, l, _, _ := probeFixture(t, nil)
	close(p.ready)
	close(l.finish)
	if err := <-l.validated; err != nil {
		t.Fatal(err)
	}
	close(j.ready)
	waitHostState(t, h.Consumed)
	// The host cannot finish joining the probe until the client has finished its
	// post-ack peer check. Independent launch admission must not cancel that lease.
	if l.Context().Err() != nil || l.completionContext.Err() != nil || h.Context().Err() != nil {
		t.Fatal("JSONL interrupted probe finalization")
	}
	l.cancel()
	waitHostState(t, h.Consumed)
	if h.Context().Err() != nil {
		t.Fatal("probe finalization canceled JSONL")
	}
}
