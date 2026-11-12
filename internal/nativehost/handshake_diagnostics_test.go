package nativehost

import (
	"context"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativehandshake"
)

func TestHandshakeHostActualBootstrapMilestones(t *testing.T) {
	for _, mode := range []string{"unused", "direct", "probe", "refused"} {
		t.Run(mode, func(t *testing.T) {
			cfg, root, pinned, cap, original := fixture(t)
			cfg.StartupProbe = appserverbridge.StartupProbeCompareUnrestricted167
			var mu sync.Mutex
			events := map[nativehandshake.Stage]int{}
			ctx := nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) { mu.Lock(); events[s]++; mu.Unlock() })
			has := func(s nativehandshake.Stage) bool { mu.Lock(); defer mu.Unlock(); return events[s] > 0 }
			leaseCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lease := &checkedProbeLease{fakeLease: &fakeLease{ctx: leaseCtx, cancel: cancel}, started: make(chan struct{}), finish: make(chan struct{}), validated: make(chan error, 1)}
			probe := &checkedOwner{ready: make(chan struct{}), lease: lease, awaiting: make(chan struct{})}
			launch := &checkedOwner{ready: make(chan struct{}), lease: original.lease, awaiting: make(chan struct{})}
			h, err := startWithProbeListener(ctx, cap, cfg, root, dependencies{resolve: func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, listen: func(_ nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
				launch.peer = p
				return launch, nil
			}}, nil, func(_ nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
				probe.peer = p
				return probe, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := closeHost(t, h); err != nil {
					t.Error(err)
				}
			})
			<-probe.awaiting
			<-launch.awaiting
			if !has(nativehandshake.HostListening) || has(nativehandshake.HostJSONLAdmitted) {
				t.Fatal("listener fabricated launch")
			}
			if mode == "probe" || mode == "refused" {
				close(probe.ready)
				waitProbeLease(t, lease)
				if !has(nativehandshake.HostProbeAdmitted) || has(nativehandshake.HostProbeAcknowledged) || has(nativehandshake.HostJSONLAdmitted) {
					t.Fatal("pending probe fabricated completion")
				}
				if mode == "refused" {
					cap.valid.Store(false)
				}
				close(lease.finish)
				err = <-lease.validated
				if mode == "refused" {
					if err == nil || has(nativehandshake.HostProbeAcknowledged) {
						t.Fatal("refused completion acknowledged")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				waitHostState(t, func() bool { return has(nativehandshake.HostProbeAcknowledged) })
				cancel()
				waitHostState(t, func() bool { return lease.closes.Load() > 0 })
			}
			if mode == "direct" || mode == "probe" {
				close(launch.ready)
				waitHostState(t, func() bool { return has(nativehandshake.HostJSONLAdmitted) })
			}
			if err := closeHost(t, h); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if events[nativehandshake.HostJSONLAdmitted] > 1 || events[nativehandshake.InitializeOutputWritten] > 0 {
				t.Fatal("duplicate admission or fabricated backend handshake")
			}
		})
	}
}
