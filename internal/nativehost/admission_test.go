package nativehost

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativeconnect"
)

func TestHostAdmissionRejectsBootstrapAfterPreparation(t *testing.T) {
	cfg, root, launcher, cap, owner := fixture(t)
	var allowed atomic.Bool
	var checks atomic.Int32
	allowed.Store(true)
	admission := func(context.Context) error {
		checks.Add(1)
		if !allowed.Load() {
			return ErrUnavailable
		}
		return nil
	}
	var peer nativebootstrap.Peer
	h, err := startWithAdmission(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return launcher, nil },
		listen:  func(_ nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) { peer = p; return owner, nil },
	}, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost(t, h)
	if checks.Load() == 0 {
		t.Fatal("preparation skipped admission")
	}
	if _, err := h.Plan(); err != nil {
		t.Fatal(err)
	}
	if err := peer.Check(context.Background(), nativebootstrap.Process{}); err != nil {
		t.Fatal(err)
	}
	// Simulate evidence invalidation between bootstrap's pre-disclosure and
	// post-transfer checks. No second Connect or Plan is involved.
	allowed.Store(false)
	if err := peer.Check(context.Background(), nativebootstrap.Process{}); err == nil {
		t.Fatal("bootstrap reused invalid supplier evidence")
	}
	if _, err := h.Plan(); err == nil {
		t.Fatal("Plan reused invalid supplier evidence")
	}
}

func TestHostAdmissionRejectsBeforeResourceCreation(t *testing.T) {
	cfg, root, launcher, cap, _ := fixture(t)
	failure := errors.New("synthetic denied admission")
	h, err := startWithAdmission(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return launcher, nil },
		listen: func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) {
			t.Fatal("denied admission opened owner")
			return nil, nil
		},
	}, func(context.Context) error { return failure })
	if h != nil || !errors.Is(err, failure) {
		t.Fatal("denied admission allocated owner", h, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("denied admission published resources", entries, err)
	}
}

func TestHostStartWithAdmissionRejectsNil(t *testing.T) {
	cfg, root, _, _, _ := fixture(t)
	if h, err := StartWithAdmission(context.Background(), nil, cfg, root, nil); h != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatal(h, err)
	}
}

func TestHostAdmissionRejectsCancellationDuringCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := checkAdmission(ctx, func(context.Context) error { cancel(); return nil })
	if err == nil {
		t.Fatal("callback cancellation accepted")
	}
}
