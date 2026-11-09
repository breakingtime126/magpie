package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
)

// These independent modes select reviewed, deliberately narrow auth contracts.
const AttestationUnavailable = "unavailable-v0.166"
const GatewayOAuthPrimaryRead = "primary-read-only-v0.166"
const AttestationUnavailable167 = "unavailable-v0.167.0-alpha.3.2"
const GatewayOAuthPrimaryRead167 = "primary-read-only-v0.167.0-alpha.3.2"

var errAuthCompatibility = errors.New("unsupported authentication compatibility message")
var errGatewayRead = errors.New("gateway OAuth status unavailable")

type attestationRate struct {
	tokens float64
	last   time.Time
}

type authCompatibility struct {
	// Internal only: the shared-route owner controls startup and creation admission.
	primaryProofCandidate   bool
	primaryProofAdmitted    bool
	primaryProofCalls       map[string]bool
	primaryProofStartup     bool
	primaryProofFailed      bool
	primaryCreations        map[*request]bool
	primaryProofCancelled   map[string]bool
	primaryProofCancelOrder []string

	authStatusNoAuth       bool
	authStatusTrusted      bool
	rejectHumanApproval    bool
	attestationUnavailable bool
	gatewayRead            bool
	gatewayProviderName    string
	attestationNegotiated  bool
	gatewayNegotiated      bool
	now                    func() time.Time
	rates                  map[string]attestationRate
}

// negotiate commits both flags together and never changes the client's object.
func (a *authCompatibility) negotiate(caps object) error {
	// This separate opt-in changes approval behavior in 0.167 and has not
	// been reviewed. Reject it without rewriting the client's capability.
	if value, present := caps["requiredHumanApproval"]; present && a.rejectHumanApproval {
		if !bytes.Equal(bytes.TrimSpace(value), []byte("false")) {
			return errAuthCompatibility
		}
	}
	attestation, gateway := false, false
	for _, item := range []struct {
		name    string
		allowed bool
		target  *bool
	}{
		{"requestAttestation", a.attestationUnavailable || a.primaryProofCandidate, &attestation},
		{"explicitGatewayOauth", a.gatewayRead || a.primaryProofCandidate, &gateway},
	} {
		value, present := caps[item.name]
		if !present {
			continue
		}
		if !requirementsBool(value) {
			return errAuthCompatibility
		}
		enabled := bytes.Equal(bytes.TrimSpace(value), []byte("true"))
		if enabled && !item.allowed {
			return errAuthCompatibility
		}
		*item.target = enabled
	}
	a.attestationNegotiated, a.gatewayNegotiated = attestation, gateway
	return nil
}

// authCompatibilityID accepts the pinned RequestId enum, not arbitrary JSON
// numbers. Preserve the original bytes for the reply, but canonicalize integer
// identity so -0 and 0 cannot evade a live request collision.
func authCompatibilityID(value json.RawMessage) (string, error) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 {
		return "", errAuthCompatibility
	}
	if value[0] == '"' {
		var id string
		if json.Unmarshal(value, &id) != nil || len(id) > 256 || !startupText(id) {
			return "", errAuthCompatibility
		}
		return "s:" + id, nil
	}
	start := 0
	if value[0] == '-' {
		start = 1
	}
	if start == len(value) {
		return "", errAuthCompatibility
	}
	for _, c := range value[start:] {
		if c < '0' || c > '9' {
			return "", errAuthCompatibility
		}
	}
	if len(value)-start > 1 && value[start] == '0' {
		return "", errAuthCompatibility
	}
	n, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil {
		return "", errAuthCompatibility
	}
	return "n:" + strconv.FormatInt(n, 10), nil
}

func (a *authCompatibility) takeAttestation(backend string) bool {
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	if a.rates == nil {
		a.rates = make(map[string]attestationRate, 2)
	}
	bucket, found := a.rates[backend]
	if !found {
		if len(a.rates) >= 2 {
			return false
		}
		bucket = attestationRate{tokens: 64, last: now}
	} else if now.After(bucket.last) {
		bucket.tokens += now.Sub(bucket.last).Seconds() * 32
		if bucket.tokens > 64 {
			bucket.tokens = 64
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		a.rates[backend] = bucket
		return false
	}
	bucket.tokens--
	a.rates[backend] = bucket
	return true
}

// The source oracle is openai/codex at
// 4f093f2748667f3b0e6a07e3d4055121cbd3ce64, protocol/v2/attestation.rs
// and common.rs. This is a genuine RPC failure, never a fabricated token or
// header. How the official server maps that failure is outside this bridge.
func (r *Router) attestationMessage(backend string, line []byte) ([]Delivery, error) {
	if r.auth.primaryProofCandidate {
		return r.primaryProofRequest167(backend, line)
	}
	if r.byID[backend].ID == "" || r.phase != "ready" ||
		!r.auth.attestationUnavailable || !r.auth.attestationNegotiated {
		return nil, errAuthCompatibility
	}
	if len(line) > 4<<10 || validateRequirementsJSON(line) != nil {
		return nil, errAuthCompatibility
	}
	m, ok := requirementsObject(line, "id", "method", "params")
	if !ok || !requirementsEnum(m["method"], "attestation/generate") {
		return nil, errAuthCompatibility
	}
	if _, ok := requirementsObject(m["params"]); !ok {
		return nil, errAuthCompatibility
	}
	key, err := authCompatibilityID(m["id"])
	if err != nil {
		return nil, errAuthCompatibility
	}
	for _, call := range r.calls {
		if call.backend != backend {
			continue
		}
		existing, e := authCompatibilityID(call.id)
		if e == nil && existing == key {
			return nil, errAuthCompatibility
		}
	}
	if !r.auth.takeAttestation(backend) {
		return nil, errAuthCompatibility
	}
	d := fail(m["id"], -32601, "attestation/generate is unavailable through this bridge")
	d.Backend = backend
	return []Delivery{d}, nil
}

func gatewayStatus(value json.RawMessage) bool {
	return requirementsEnum(value, "notReady", "started", "succeeded", "failed")
}

// Only the complete reviewed public view is accepted. In particular, an error
// or unknown field is never silently redacted into a successful status result.
func validateGatewayRead(line []byte, backend Backend, expectedName string) (object, error) {
	if len(line) > 16<<10 || validateRequirementsJSON(line) != nil {
		return nil, errGatewayRead
	}
	m, ok := requirementsObject(line, "id", "result")
	if !ok {
		return nil, errGatewayRead
	}
	if _, err := authCompatibilityID(m["id"]); err != nil {
		return nil, errGatewayRead
	}
	result, ok := requirementsObject(m["result"], "providerId", "providerName", "required", "status", "error")
	if !ok || backend.Provider == "" || expectedName == "" || len(expectedName) > 128 ||
		!startupText(expectedName) ||
		!requirementsEnum(result["providerId"], backend.Provider) ||
		!requirementsEnum(result["providerName"], expectedName) ||
		!requirementsBool(result["required"]) || !requirementsNull(result["error"]) {
		return nil, errGatewayRead
	}
	required := bytes.Equal(bytes.TrimSpace(result["required"]), []byte("true"))
	if (required && !gatewayStatus(result["status"])) || (!required && !requirementsNull(result["status"])) {
		return nil, errGatewayRead
	}
	return result, nil
}

func (r *Router) gatewayNotification(backend string, line []byte) ([]Delivery, error) {
	b, known := r.byID[backend]
	if !known || len(line) > 16<<10 || validateRequirementsJSON(line) != nil {
		return nil, errAuthCompatibility
	}
	m, ok := requirementsObject(line, "method", "params")
	if !ok || !requirementsEnum(m["method"], "account/gatewayOAuth/changed") {
		return nil, errAuthCompatibility
	}
	p, ok := requirementsObject(m["params"], "authUrl", "providerId", "status", "error")
	if !ok || !requirementsNull(p["authUrl"]) || !requirementsNull(p["error"]) ||
		!requirementsEnum(p["providerId"], b.Provider) || !gatewayStatus(p["status"]) {
		return nil, errAuthCompatibility
	}
	if r.auth.primaryProofCandidate && backend == r.backends[0].ID && !r.primaryRouteReady167() {
		return nil, nil // Drop, never buffer, notifications from a changing route.
	}
	// Validate before suppression: no alternate shape can become an RPC call,
	// and no authorization URL, error text or second account reaches the client.
	if backend != r.backends[0].ID || !r.auth.gatewayRead ||
		!r.auth.gatewayNegotiated {
		return nil, nil
	}
	if r.phase != "ready" {
		return nil, errAuthCompatibility
	}
	return []Delivery{{Backend: "", Message: append([]byte(nil), line...)}}, nil
}

// validateServerEnvelopeKeys prevents permissive object decoding from choosing
// the last of duplicate routing keys. Nested values remain opaque here, so
// unrelated tool payloads keep the normal message size and decoder limits.
// Method-specific authentication validators still inspect their entire input.
func validateServerEnvelopeKeys(line []byte) error {
	if len(line) > MaxMessageBytes {
		return errAuthCompatibility
	}
	d := json.NewDecoder(bytes.NewReader(line))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errAuthCompatibility
	}
	seen := make(map[string]bool, 8)
	for count := 0; d.More(); count++ {
		if count >= 16 {
			return errAuthCompatibility
		}
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !startupText(key) || seen[key] {
			return errAuthCompatibility
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return errAuthCompatibility
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return errAuthCompatibility
	}
	if _, err := d.Token(); err != io.EOF {
		return errAuthCompatibility
	}
	return nil
}
