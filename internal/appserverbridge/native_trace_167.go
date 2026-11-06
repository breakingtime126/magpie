package appserverbridge

import (
	"bytes"
	"encoding/json"
)

// These are bridge-local input budgets, not limits declared by the native
// schema or a promise to recognize every future W3C trace carrier.
const nativeTrace167JSONLimit = 16 << 10
const nativeTrace167StringLimit = 1024

// NativeTrace167Valid recognizes a bounded subset of the optional trace value
// in openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9's JSONRPCRequest:
// null or an object with optional nullable traceparent/tracestate strings.
// Duplicate decoded keys, unknown fields and malformed text are rejected.
// Single JSON-escaped spellings of known keys retain ordinary JSON semantics.
//
// This checks carrier shape, not W3C header semantics, authentication or
// permissions. The native app_server_tracing.rs uses this carrier only to
// attach a telemetry span parent and ignores invalid trace contexts. The bridge
// accepts this metadata without propagating it: Router.send still reconstructs
// child envelopes without trace, so an unscoped trace never joins providers.
// The test-only observer reuses this predicate and publishes only booleans.
func NativeTrace167Valid(data []byte) bool {
	if len(data) > nativeTrace167JSONLimit || validateRequirementsJSON(data) != nil {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return true
	}
	fields, err := startupObject(data)
	if err != nil || len(fields) > 2 {
		return false
	}
	for key, encoded := range fields {
		if key != "traceparent" && key != "tracestate" {
			return false
		}
		if requirementsNull(encoded) {
			continue
		}
		var value string
		if json.Unmarshal(encoded, &value) != nil || len(value) > nativeTrace167StringLimit ||
			!startupText(value) {
			return false
		}
	}
	return true
}

// NativeRequestID167Valid applies the bridge's existing bounded native
// string-or-int64 ID rule to diagnostic envelope recognition. It does not
// authorize an RPC, return an ID, or change router correlation.
func NativeRequestID167Valid(data []byte) bool {
	_, err := authCompatibilityID(data)
	return err == nil
}

// Compare native integer identity too: -0 and 0 cannot name two simultaneous
// requests when either one is this strictly typed diagnostic operation.
func (r *Router) nativeReadIDInUse(id json.RawMessage, method string) bool {
	strict := func(method string) bool {
		return (r.auth.authStatusNoAuth && method == "getAuthStatus") ||
			(r.experimentalFeatureList167 && method == "experimentalFeature/list")
	}
	key, err := authCompatibilityID(id)
	if err != nil {
		return false
	}
	for _, pending := range r.pending {
		g := pending.group
		if !strict(method) && !strict(g.method) {
			continue
		}
		other, err := authCompatibilityID(g.id)
		if err == nil && key == other {
			return true
		}
	}
	return false
}
