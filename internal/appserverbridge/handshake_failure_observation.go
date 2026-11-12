package appserverbridge

import (
	"errors"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// Fixed classes only: never emit an error string, request ID, method, or payload.
// Unknown failures deliberately collapse to Other. This is observation only.
func (o *handshakeObservation) protocolFailure(r *Router, backend string, line []byte, err error) {
	if o == nil || err == nil {
		return
	}
	source := nativehandshake.ProtocolSourceClient
	if backend != "" {
		source = nativehandshake.ProtocolSourcePrimary
		if backend == r.backends[1].ID {
			source = nativehandshake.ProtocolSourceSecondary
		}
	}
	shape := nativehandshake.ProtocolShapeInvalid
	if m, parseErr := parseObject(line); parseErr == nil {
		if text(m, "method") != "" {
			shape = nativehandshake.ProtocolShapeNotification
			if exists(m, "id") {
				shape = nativehandshake.ProtocolShapeRequest
			}
		} else if exists(m, "id") {
			shape = nativehandshake.ProtocolShapeResponse
		}
	}
	nativehandshake.Emit(o.ctx, source)
	nativehandshake.Emit(o.ctx, shape)
	pending := o.pendingStage
	if pending == 0 { pending = nativehandshake.ProtocolPendingNone }
	nativehandshake.Emit(o.ctx, pending)
	nativehandshake.Emit(o.ctx, protocolFailureClass(err))
}

func protocolFailureClass(err error) nativehandshake.Stage {
	if errors.Is(err, errAuthCompatibility) {
		return nativehandshake.ProtocolClassEnvelope
	}
	switch err.Error() {
	case "unknown or wrong-backend response id", "wrong-backend late response",
		"duplicate active client request id", "unknown or already answered backend request",
		"unknown resolved backend request", "duplicate backend request id":
		return nativehandshake.ProtocolClassCorrelation
	case "unscoped backend request is unsupported":
		return nativehandshake.ProtocolClassUnscopedRequest
	case "unsolicited unknown thread", "notification or request belongs to an unknown or foreign thread",
		"backend returned a thread from another provider", "thread id collision across backends",
		"turn has unknown thread or id":
		return nativehandshake.ProtocolClassThreadOwnership
	case sharedRouteUnavailable167:
		return nativehandshake.ProtocolClassAdmission
	case ownedCatalogUnavailable, ownedSettingsUnavailable167, "model descriptor requires id and model", "duplicate backend model", "invalid backend list data", "backend catalog exceeds bridge limit", "backend pagination did not finish":
		return nativehandshake.ProtocolClassCatalog
	case controlPlaneInitUnavailable167:
		return nativehandshake.ProtocolClassInitialization
	case "unexpected client notification":
		return nativehandshake.ProtocolClassClientNotification
	case "backend message mixes request and response fields", "backend response has both result and error":
		return nativehandshake.ProtocolClassEnvelope
	default:
		return nativehandshake.ProtocolClassOther
	}
}

func protocolPendingStage(g *request) nativehandshake.Stage {
	if g.sharedNativeAdmission167 {
		if g.sharedNativePolicy167 {
			return nativehandshake.ProtocolPendingPolicy
		}
		return nativehandshake.ProtocolPendingConfig
	}
	switch g.method {
	case "initialize":
		return nativehandshake.ProtocolPendingInitialize
	case "configRequirements/read":
		return nativehandshake.ProtocolPendingPolicy
	case "config/read":
		return nativehandshake.ProtocolPendingConfig
	case "model/list":
		return nativehandshake.ProtocolPendingCatalog
	default:
		return nativehandshake.ProtocolPendingOther
	}
}
