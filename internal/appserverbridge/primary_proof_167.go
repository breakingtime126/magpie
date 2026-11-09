package appserverbridge

import (
	"bytes"
	"encoding/json"
	"sort"
)

// releasePrimaryProof167 must accompany every removal from Router.calls,
// including resolution and origin-ID reuse. Existing staleCalls tombstones
// then suppress late replies without delivering them to any backend.
func (r *Router) releasePrimaryProof167(publicKey string) {
	delete(r.auth.primaryProofCalls, publicKey)
}

// primaryCapabilities167 preserves the aggregate frontend contract on primary
// only. It never mutates the original params or capabilities. Selecting this
// candidate is not route admission and does not permit forwarding a proof.
func (r *Router) primaryCapabilities167(backend string, params object) (object, error) {
	if !r.auth.primaryProofCandidate {
		return params, nil
	}
	caps, err := child(params, "capabilities")
	if err != nil || r.byID[backend].ID == "" {
		return nil, errAuthCompatibility
	}
	out := make(object, len(params))
	for k, v := range params {
		out[k] = v
	}
	projected := make(object, len(caps))
	for k, v := range caps {
		projected[k] = v
	}
	if backend != r.backends[0].ID {
		projected["requestAttestation"] = raw(false)
		projected["explicitGatewayOauth"] = raw(false)
	}
	out["capabilities"] = raw(projected)
	return out, nil
}

// The caller must establish a runtime route invariant, not merely a provider
// name or a startup config snapshot. Attestation params carry no destination
// or thread identity, so this function cannot establish that invariant itself.
func (r *Router) primaryProofRequest167(backend string, line []byte) ([]Delivery, error) {
	if backend != r.backends[0].ID || !r.auth.primaryProofCandidate ||
		!r.auth.attestationNegotiated ||
		len(line) > 4<<10 || validateRequirementsJSON(line) != nil {
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
	if err != nil || len(r.calls) >= 4096 || !r.auth.takeAttestation(backend) {
		return nil, errAuthCompatibility
	}
	// Never reuse an origin ID while its request is outstanding.
	for _, call := range r.calls {
		if call.backend == backend {
			previous, e := authCompatibilityID(call.id)
			if e != nil || previous == key {
				return nil, errAuthCompatibility
			}
		}
	}
	if !r.primaryRouteReady167() || r.phase != "ready" {
		return []Delivery{primaryProofUnavailable167(backend, m["id"])}, nil
	}
	key, err = idKey(m["id"])
	if err != nil {
		return nil, errAuthCompatibility
	}
	translated := r.next("server")
	publicKey := "s:" + translated
	r.calls[publicKey] = serverCall{backend, key, m["id"], false}
	r.reverseCalls[backend+"\x00"+key] = translated
	if r.auth.primaryProofCalls == nil {
		r.auth.primaryProofCalls = map[string]bool{}
	}
	r.auth.primaryProofCalls[publicKey] = true
	m["id"] = raw(translated)
	return []Delivery{deliver("", m)}, nil
}

// primaryProofResponse167 is called before generic clientResponse. A recognized
// proof reply is routed here only, including malformed and repeated replies.
// No token, native error text or error data is stored or logged by this layer.
func (r *Router) primaryProofResponse167(m object) (bool, []Delivery, error) {
	key, err := idKey(m["id"])
	if err == nil && r.auth.primaryProofCancelled[key] {
		return true, nil, nil
	}
	if err != nil || !r.auth.primaryProofCalls[key] {
		return false, nil, nil
	}
	c, ok := r.calls[key]
	if !ok || c.answered || c.backend != r.backends[0].ID ||
		!r.primaryRouteReady167() ||
		!r.auth.attestationNegotiated || r.phase != "ready" {
		return true, nil, errAuthCompatibility
	}
	line := raw(m)
	if len(line) > 64<<10 || validateRequirementsJSON(line) != nil {
		return true, nil, errAuthCompatibility
	}
	if result, present := m["result"]; present {
		if _, valid := requirementsObject(line, "id", "result"); !valid {
			return true, nil, errAuthCompatibility
		}
		p, valid := requirementsObject(result, "token")
		var token string
		if !valid || json.Unmarshal(p["token"], &token) != nil || bytes.Equal(bytes.TrimSpace(p["token"]), []byte("null")) {
			return true, nil, errAuthCompatibility
		}
	} else {
		// Preserve an actual frontend failure, not a manufactured proof result.
		if _, valid := requirementsObject(line, "id", "error"); !valid {
			return true, nil, errAuthCompatibility
		}
		p, valid := requirementsObject(m["error"], "code", "message", "data")
		if !valid {
			p, valid = requirementsObject(m["error"], "code", "message")
		}
		var code int64
		var message string
		if !valid || json.Unmarshal(p["code"], &code) != nil || requirementsNull(p["code"]) ||
			json.Unmarshal(p["message"], &message) != nil || requirementsNull(p["message"]) {
			return true, nil, errAuthCompatibility
		}
	}
	r.retirePrimaryProof167(key)
	m["id"] = c.id
	return true, []Delivery{deliver(c.backend, m)}, nil
}

// Startup admission and creation windows are independent. A snapshot alone is
// not permission to relay proof while another primary configuration is loading.
func (r *Router) primaryRouteReady167() bool {
	return r.phase == "ready" && r.auth.primaryProofCandidate && r.auth.primaryProofStartup &&
		r.auth.primaryProofAdmitted && !r.auth.primaryProofFailed && len(r.auth.primaryCreations) == 0
}

func (r *Router) admitPrimaryStartup167() {
	if !r.auth.primaryProofCandidate || r.auth.primaryProofFailed {
		return
	}
	r.auth.primaryProofStartup = true
	r.auth.primaryProofAdmitted = len(r.auth.primaryCreations) == 0
}

func primaryProofUnavailable167(backend string, id json.RawMessage) Delivery {
	d := fail(id, -32601, "attestation/generate is unavailable during primary route admission")
	d.Backend = backend
	return d
}

// Call immediately before forwarding an actual primary start/resume. Already
// answered proofs cannot be retracted; unanswered requests receive real errors.
func (r *Router) beginPrimaryCreation167(g *request) []Delivery {
	if !r.auth.primaryProofCandidate {
		return nil
	}
	r.auth.primaryProofAdmitted = false
	if r.auth.primaryCreations == nil {
		r.auth.primaryCreations = map[*request]bool{}
	}
	if g == nil || r.auth.primaryCreations[g] || len(r.auth.primaryCreations) >= 4096 {
		r.auth.primaryProofFailed = true
	} else {
		r.auth.primaryCreations[g] = true
	}
	keys := make([]string, 0, len(r.auth.primaryProofCalls))
	for key := range r.auth.primaryProofCalls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []Delivery
	for _, key := range keys {
		if call, ok := r.calls[key]; ok {
			if !call.answered {
				out = append(out, primaryProofUnavailable167(call.backend, call.id))
			}
		}
		r.retirePrimaryProof167(key)
	}
	return out
}

// Attestation callbacks do not emit serverRequest/resolved in the pinned
// native implementation. Reclaim routing immediately after a reply or local
// cancellation; keep only bounded public-ID tombstones, never proof bytes.
func (r *Router) retirePrimaryProof167(key string) {
	if call, ok := r.calls[key]; ok {
		delete(r.reverseCalls, call.backend+"\x00"+call.key)
		delete(r.calls, key)
	}
	r.releasePrimaryProof167(key)
	if r.auth.primaryProofCancelled == nil {
		r.auth.primaryProofCancelled = map[string]bool{}
	}
	if !r.auth.primaryProofCancelled[key] {
		r.auth.primaryProofCancelled[key] = true
		r.auth.primaryProofCancelOrder = append(r.auth.primaryProofCancelOrder, key)
	}
	if !r.staleCalls[key] {
		r.staleCalls[key] = true
		r.staleOrder = append(r.staleOrder, key)
	}
	for len(r.auth.primaryProofCancelOrder) > 4096 {
		delete(r.auth.primaryProofCancelled, r.auth.primaryProofCancelOrder[0])
		r.auth.primaryProofCancelOrder = r.auth.primaryProofCancelOrder[1:]
	}
	for len(r.staleOrder) > 256 {
		delete(r.staleCalls, r.staleOrder[0])
		r.staleOrder = r.staleOrder[1:]
	}
}

// Call only after checking the actual native response's OpenAI provider. Any
// failure or unmatched completion poisons admission for this connection.
func (r *Router) finishPrimaryCreation167(g *request, success bool) error {
	if !r.auth.primaryProofCandidate {
		return nil
	}
	known := g != nil && r.auth.primaryCreations[g]
	delete(r.auth.primaryCreations, g)
	if !known || !success || r.auth.primaryProofFailed {
		r.auth.primaryProofFailed = true
		r.auth.primaryProofAdmitted = false
		return errAuthCompatibility
	}
	r.auth.primaryProofAdmitted = r.auth.primaryProofStartup && len(r.auth.primaryCreations) == 0
	return nil
}

// primaryCreationPending167 lets the router's fatal-error defer close exactly
// the window whose native operation it was handling, never an unrelated one.
func (r *Router) primaryCreationPending167(g *request) bool {
	return r.auth.primaryProofCandidate && g != nil && r.auth.primaryCreations[g]
}
