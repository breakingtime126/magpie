package appserverbridge

import (
	"context"
	"encoding/json"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// handshakeObservation retains only the initializing request's ID in memory.
// It observes the actual router and completed writes, never alters a delivery.
type handshakeObservation struct {
	pendingStage nativehandshake.Stage
	ctx     context.Context
	id      json.RawMessage
	ready   bool
	written bool
}

func newHandshakeObservation(ctx context.Context) *handshakeObservation {
	if !nativehandshake.Enabled(ctx) {
		return nil
	}
	return &handshakeObservation{ctx: ctx}
}

func (o *handshakeObservation) client(r *Router, previous string, line []byte, deliveries []Delivery) {
	if o == nil {
		return
	}
	o.pendingStage = nativehandshake.ProtocolPendingNone
	m, err := parseObject(line)
	if err != nil || text(m, "method") != "initialize" {
		return
	}
	nativehandshake.Emit(o.ctx, nativehandshake.InitializeReceived)
	if previous == "cold" && r.phase == "initializing" {
		o.id = append(json.RawMessage(nil), m["id"]...)
		return
	}
	for _, d := range deliveries {
		response, err := parseObject(d.Message)
		if err == nil && d.Backend == "" && sameJSON(response["id"], m["id"]) && exists(response, "error") {
			nativehandshake.Emit(o.ctx, nativehandshake.InitializationRejected)
			return
		}
	}
}

func (o *handshakeObservation) pending(r *Router, backend string, line []byte) bool {
	if o == nil {
		return false
	}
	o.pendingStage = nativehandshake.ProtocolPendingNone
	m, err := parseObject(line)
	if err != nil {
		return false
	}
	key, err := idKey(m["id"])
	if err != nil {
		return false
	}
	p, ok := r.pending[key]
	if !ok || p.backend != backend {
		return false
	}
	o.pendingStage = protocolPendingStage(p.group)
	return r.phase == "initializing" && p.group.method == "initialize"
}

func (o *handshakeObservation) routed(r *Router, previous, backend string, line []byte, initReply bool, routeErr error) {
	if o == nil {
		return
	}
	if routeErr != nil {
		o.protocolFailure(r, backend, line, routeErr)
		nativehandshake.Emit(o.ctx, nativehandshake.ProtocolFailed)
		return
	}
	if initReply {
		m, _ := parseObject(line)
		stage := nativehandshake.PrimaryInitializeReply
		if backend == r.backends[1].ID {
			stage = nativehandshake.SecondaryInitializeReply
		}
		if exists(m, "error") {
			nativehandshake.Emit(o.ctx, nativehandshake.InitializationRejected)
			stage = nativehandshake.PrimaryInitializeError
			if backend == r.backends[1].ID {
				stage = nativehandshake.SecondaryInitializeError
			}
		}
		nativehandshake.Emit(o.ctx, stage)
	}
	if previous == "initializing" && r.phase == "failed" {
		nativehandshake.Emit(o.ctx, nativehandshake.InitializationRejected)
	}
	if previous == "initializing" && r.phase == "ready" && len(o.id) != 0 {
		o.ready = true
		nativehandshake.Emit(o.ctx, nativehandshake.InitializationReady)
	}
}

func (o *handshakeObservation) wrote(r *Router, d Delivery, err error) {
	if o == nil {
		return
	}
	if err != nil {
		nativehandshake.Emit(o.ctx, nativehandshake.StreamWriteFailed)
		return
	}
	m, parseErr := parseObject(d.Message)
	if parseErr != nil {
		return
	}
	if d.Backend != "" {
		if text(m, "method") != "initialize" {
			return
		}
		stage := nativehandshake.PrimaryInitializeWritten
		if d.Backend == r.backends[1].ID {
			stage = nativehandshake.SecondaryInitializeWritten
		}
		nativehandshake.Emit(o.ctx, stage)
		return
	}
	if !o.ready || o.written || r.phase != "ready" || !sameJSON(m["id"], o.id) || !exists(m, "result") || exists(m, "error") || exists(m, "method") {
		return
	}
	o.written = true
	nativehandshake.Emit(o.ctx, nativehandshake.InitializeOutputWritten)
}

// streamEnded records only the fixed source identity, never input or error text.
func (o *handshakeObservation) streamEnded(r *Router, backend string) {
	if o == nil {
		return
	}
	stage := nativehandshake.ClientStreamEnded
	if backend != "" {
		if backend == r.backends[0].ID {
			stage = nativehandshake.PrimaryStreamEnded
		} else {
			stage = nativehandshake.SecondaryStreamEnded
		}
	}
	nativehandshake.Emit(o.ctx, stage)
}
