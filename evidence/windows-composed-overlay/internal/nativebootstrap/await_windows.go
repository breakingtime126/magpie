package nativebootstrap

import "context"

// Await reserves this one generation until its owner context ends or one peer
// connects. Only the connected handshake has the five-second limit. Unlike
// repeated Accept calls, this neither releases/reclaims the name nor reconnects.
// Close requests cancellation; await its return or Lease.Close for cleanup.
func (o *Owner) Await(parent context.Context) (*Lease, error) { return o.accept(parent, true) }
