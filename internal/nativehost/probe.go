package nativehost

import (
	"context"
	"errors"

	"github.com/yetone/magpie/internal/nativehandshake"
)

type awaitResult struct {
	lease ownerLease
	err   error
}

func (h *Host) acceptLaunch(lease ownerLease, err error) {
	if err != nil && h.ctx.Err() == nil {
		h.baseErr = err
	}
	if err == nil && lease == nil {
		h.baseErr = ErrUnavailable
	}
	if err == nil && lease != nil {
		if h.consumed.CompareAndSwap(false, true) {
			nativehandshake.Emit(h.ctx, nativehandshake.HostJSONLAdmitted)
		}
		select {
		case <-lease.Context().Done():
		case <-h.ctx.Done():
		}
	}
}

// The optional inspection and normal launch reserve separate one-use pipes.
// Direct JSONL is allowed; an admitted probe must finish before JSONL admission.
// Both Await workers are joined before publishing done or retrying cleanup.
func (h *Host) serveWithProbe() {
	probeCtx, cancelProbe := context.WithCancel(h.ctx)
	defer cancelProbe()
	probeDone := make(chan awaitResult, 1)
	launchDone := make(chan awaitResult, 1)
	go func() {
		lease, err := h.probeOwner.Await(probeCtx)
		if err == nil && lease == nil {
			err = ErrUnavailable
		}
		if err == nil {
			if !h.phase.CompareAndSwap(0, 1) && h.phase.Load() != 1 {
				err = ErrUnavailable
			}
		}
		if err == nil {
			h.probeAdmitted.Store(true)
			nativehandshake.Emit(h.ctx, nativehandshake.HostProbeAdmitted)
			probe, ok := lease.(probeLease)
			if !ok {
				err = ErrUnavailable
			} else {
				err = probe.ValidateProbe(probeCtx, h.probeCheck)
				if err == nil {
					nativehandshake.Emit(h.ctx, nativehandshake.HostProbeAcknowledged)
				}
			}
		}
		if err == nil {
			select {
			case <-lease.Context().Done():
			case <-probeCtx.Done():
			}
			ctx, stop := context.WithTimeout(context.Background(), cleanupLimit)
			err = lease.Close(ctx)
			stop()
		}
		probeDone <- awaitResult{lease, err}
	}()
	go func() {
		lease, err := h.owner.Await(h.ctx)
		launchDone <- awaitResult{lease, err}
	}()
	var launch awaitResult
	probeJoined := false
	select {
	case result := <-probeDone:
		h.probeLease = result.lease
		probeJoined = true
		if result.err != nil && h.ctx.Err() == nil && !(result.lease == nil && h.phase.Load() == 3 && errors.Is(result.err, context.Canceled)) {
			h.baseErr = result.err
			h.cancel()
		}
		launch = <-launchDone
	case launch = <-launchDone:
		// Retire only an unused probe. An acknowledged probe must stay live
		// through the client's final peer check and disconnect.
		if !h.probeAdmitted.Load() || launch.err != nil || launch.lease == nil {
			cancelProbe()
		}
	}
	h.lease = launch.lease
	if launch.err == nil && launch.lease != nil {
		if h.consumed.CompareAndSwap(false, true) {
			nativehandshake.Emit(h.ctx, nativehandshake.HostJSONLAdmitted)
		}
	}
	if !probeJoined {
		result := <-probeDone
		h.probeLease = result.lease
		if result.err != nil && h.ctx.Err() == nil && !(result.lease == nil && h.phase.Load() == 3 && errors.Is(result.err, context.Canceled)) {
			h.baseErr = result.err
			h.cancel()
		}
		// Only an unused probe may be retired by direct JSONL admission.
		if h.phase.Load() == 1 && h.ctx.Err() == nil {
			h.baseErr = ErrUnavailable
			h.cancel()
		}
	}
	h.acceptLaunch(launch.lease, launch.err)
}
