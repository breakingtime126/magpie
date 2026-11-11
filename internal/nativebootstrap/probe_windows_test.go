package nativebootstrap

import (
	"context"
	"testing"
	"time"
)

func TestWindowsProbeCompletionRevalidatesOwner(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "denied"}[denied], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			r := uniqueRecord(t)
			o, err := ListenStartupProbe(r, ownPeer(t))
			if err != nil {
				t.Fatal(err)
			}
			defer o.Close()
			ac := acceptAsync(ctx, o)
			client, err := ConnectStartupProbe(ctx, r.Generation, ownPeer(t))
			if err != nil {
				t.Fatal(err)
			}
			defer closeLease(t, client)
			a := getAccepted(t, ac)
			if a.err != nil {
				t.Fatal(a.err)
			}
			defer closeLease(t, a.lease)
			if a.lease.FinishProbe(ctx) == nil || client.ValidateProbe(ctx, func(context.Context) error { return nil }) == nil {
				t.Fatal("wrong completion role accepted")
			}
			called := make(chan struct{})
			validated := make(chan error, 1)
			go func() {
				validated <- a.lease.ValidateProbe(ctx, func(checkCtx context.Context) error {
					if _, ok := checkCtx.Deadline(); !ok {
						return ErrUnavailable
					}
					close(called)
					if denied {
						return ErrUnavailable
					}
					return nil
				})
			}()
			err = client.FinishProbe(ctx)
			if (err != nil) != denied {
				t.Fatal("unexpected completion outcome", err)
			}
			select {
			case <-called:
			default:
				t.Fatal("completion bypassed source validation")
			}
			if err := <-validated; (err != nil) != denied {
				t.Fatal("unexpected validation outcome", err)
			}
			if client.FinishProbe(ctx) == nil {
				t.Fatal("completion replay accepted")
			}
			if a.lease.ValidateProbe(ctx, func(context.Context) error { return nil }) == nil {
				t.Fatal("validation replay accepted")
			}
		})
	}
}

func TestWindowsNormalLeaseRejectsProbeCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := &Lease{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if l.FinishProbe(ctx) == nil || l.ValidateProbe(ctx, func(context.Context) error { return nil }) == nil {
		t.Fatal("normal launch lease accepted probe operation")
	}
}
