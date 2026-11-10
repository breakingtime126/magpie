package nativehost

import (
	"context"
	"github.com/yetone/magpie/internal/nativebootstrap"
)

type windowsOwner struct{ owner *nativebootstrap.Owner }

func (o *windowsOwner) Await(ctx context.Context) (ownerLease, error) {
	lease, err := o.owner.Await(ctx)
	if lease == nil {
		return nil, err
	}
	return lease, err // Preserve any actual cleanup ownership even on an error.
}
func (o *windowsOwner) Close() error { return o.owner.Close() }
func listenOwner(r nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
	o, e := nativebootstrap.Listen(r, p)
	if e != nil {
		return nil, e
	}
	return &windowsOwner{o}, nil
}
