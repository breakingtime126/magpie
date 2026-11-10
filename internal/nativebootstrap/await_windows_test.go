package nativebootstrap

import (
	"context"
	"testing"
	"time"
)

func TestWindowsAwaitPreservesReservationUntilOwnerCancels(t *testing.T) {
	r := uniqueRecord(t)
	o, err := Listen(r, ownPeer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan accepted, 1)
	go func() { l, e := o.Await(ctx); done <- accepted{l, e} }()
	timer := time.NewTimer(handshakeLimit + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case a := <-done:
		if a.lease != nil {
			closeLease(t, a.lease)
		}
		t.Fatal("unconnected owner expired", a.err)
	case <-timer.C:
	}
	if duplicate, e := Listen(r, ownPeer(t)); e == nil {
		duplicate.Close()
		t.Fatal("Await released reservation")
	}
	cancel()
	a := getAccepted(t, done)
	if a.err == nil {
		closeLease(t, a.lease)
		t.Fatal("cancellation accepted lease")
	}
}
