package appserverbridge

import (
	"bytes"
	"testing"
	"time"
)

// These messages are inert synthetic canaries, not valid device proofs.
func proofRouter167(t *testing.T) *Router {
	t.Helper()
	r, err := NewRouter([]Backend{{ID: "primary", Provider: "openai"}, {ID: "secondary", Provider: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	r.phase = "ready"
	r.auth.primaryProofCandidate = true
	r.admitPrimaryStartup167() // Synthetic admission oracle only.
	r.auth.attestationNegotiated = true
	return r
}

func TestPrimaryProof167CapabilitiesDoNotMutateOrReachSecondary(t *testing.T) {
	r := proofRouter167(t)
	p := object{"clientInfo": raw(object{"name": raw("synthetic")}), "capabilities": raw(object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true), "experimentalApi": raw(true)})}
	original := raw(p)
	if err := r.auth.negotiate(object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true)}); err != nil {
		t.Fatal("candidate negotiation failed")
	}
	for _, backend := range []string{"primary", "secondary"} {
		projected, err := r.primaryCapabilities167(backend, p)
		if err != nil {
			t.Fatal(err)
		}
		caps, _ := child(projected, "capabilities")
		want := raw(backend == "primary")
		if !bytes.Equal(caps["requestAttestation"], want) || !bytes.Equal(caps["explicitGatewayOauth"], want) || !bytes.Equal(caps["experimentalApi"], raw(true)) {
			t.Fatal("capability projection changed frontend semantics")
		}
	}
	if !bytes.Equal(raw(p), original) {
		t.Fatal("input capabilities mutated")
	}
	r.auth.primaryProofCandidate = false
	if r.auth.negotiate(object{"requestAttestation": raw(true)}) == nil {
		t.Fatal("default mode allowed proof")
	}
}

func TestPrimaryProof167OriginAdmissionAndOpaqueRoundTrip(t *testing.T) {
	r := proofRouter167(t)
	request := []byte(`{"id":7,"method":"attestation/generate","params":{}}`)
	if out, err := r.attestationMessage("secondary", request); err == nil || len(out) != 0 {
		t.Fatal("secondary proof escaped")
	}
	r.auth.primaryProofAdmitted = false
	if out, err := r.attestationMessage("primary", request); err != nil || len(out) != 1 || out[0].Backend != "primary" {
		t.Fatal("unadmitted proof did not receive a genuine origin error")
	}
	r.auth.primaryProofAdmitted = true
	out, err := r.attestationMessage("primary", request)
	if err != nil || len(out) != 1 || out[0].Backend != "" {
		t.Fatal("primary request not relayed")
	}
	m, _ := parseObject(out[0].Message)
	response := object{"id": m["id"], "result": raw(object{"token": raw("PUBLIC-SYNTHETIC-NOT-A-PROOF")})}
	handled, out, err := r.primaryProofResponse167(response)
	if !handled || err != nil || len(out) != 1 || out[0].Backend != "primary" {
		t.Fatal("proof reply lost origin")
	}
	returned, _ := parseObject(out[0].Message)
	if !bytes.Equal(returned["id"], raw(7)) || !bytes.Equal(returned["result"], response["result"]) {
		t.Fatal("opaque result changed")
	}
	if handled, out, err := r.primaryProofResponse167(object{"id": m["id"], "result": response["result"]}); !handled || err != nil || len(out) != 0 {
		t.Fatal("duplicate reply forwarded")
	}
}

func TestPrimaryProof167RejectsMalformedRepliesAndRevokedAdmission(t *testing.T) {
	for _, result := range []string{`null`, `{}`, `{"token":null}`, `{"token":7}`, `{"token":"canary","extra":true}`, `{"token":"first","token":"second"}`} {
		r := proofRouter167(t)
		out, err := r.attestationMessage("primary", []byte(`{"id":"canary","method":"attestation/generate","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		m, _ := parseObject(out[0].Message)
		response, _ := parseObject([]byte(`{"id":` + string(m["id"]) + `,"result":` + result + `}`))
		if _, out, err := r.primaryProofResponse167(response); err == nil || len(out) != 0 {
			t.Fatal("malformed proof response forwarded")
		}
	}
	r := proofRouter167(t)
	out, _ := r.attestationMessage("primary", []byte(`{"id":1,"method":"attestation/generate","params":{}}`))
	m, _ := parseObject(out[0].Message)
	r.auth.primaryProofAdmitted = false
	if _, out, err := r.primaryProofResponse167(object{"id": m["id"], "result": raw(object{"token": raw("CANARY")})}); err == nil || len(out) != 0 {
		t.Fatal("revoked admission still delivered proof")
	}
}

func TestPrimaryProof167FailureReclaimsOrigin(t *testing.T) {
	r := proofRouter167(t)
	out, err := r.attestationMessage("primary", []byte(`{"id":42,"method":"attestation/generate","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := parseObject(out[0].Message)
	key, _ := idKey(m["id"])
	errorValue := raw(object{"code": raw(-32601), "message": raw("PUBLIC-SYNTHETIC-FAILURE")})
	handled, out, err := r.primaryProofResponse167(object{"id": m["id"], "error": errorValue})
	if !handled || err != nil || len(out) != 1 || out[0].Backend != "primary" {
		t.Fatal("frontend failure lost origin")
	}
	reply, _ := parseObject(out[0].Message)
	if !bytes.Equal(reply["error"], errorValue) || !bytes.Equal(reply["id"], raw(42)) {
		t.Fatal("frontend failure changed")
	}
	if len(r.calls) != 0 || len(r.reverseCalls) != 0 || len(r.auth.primaryProofCalls) != 0 || !r.auth.primaryProofCancelled[key] {
		t.Fatal("proof metadata retained after error delivery")
	}
	late := object{"id": m["id"], "result": raw(object{"token": raw("STALE-CANARY")})}
	if handled, out, err := r.primaryProofResponse167(late); !handled || err != nil || len(out) != 0 {
		t.Fatal("completed proof reply was not safely discarded")
	}
}

func TestPrimaryProof167RepeatedCyclesReclaimLiveRoutes(t *testing.T) {
	r := proofRouter167(t)
	now := time.Unix(1, 0)
	r.auth.now = func() time.Time { return now }
	for i := 0; i < 8193; i++ {
		now = now.Add(time.Second)
		// The native origin may reuse an ID after its callback completes.
		out, err := r.primaryProofRequest167("primary", []byte(`{"id":7,"method":"attestation/generate","params":{}}`))
		if err != nil || len(out) != 1 || out[0].Backend != "" {
			t.Fatal("completed calls imposed a lifetime request limit")
		}
		m, _ := parseObject(out[0].Message)
		publicID := m["id"]
		reply := object{"id": publicID, "error": raw(object{"code": raw(-32601), "message": raw("synthetic failure")})}
		if i%2 == 0 {
			reply = object{"id": publicID, "result": raw(object{"token": raw("SYNTHETIC_NOT_A_PROOF")})}
		}
		handled, out, err := r.primaryProofResponse167(reply)
		if !handled || err != nil || len(out) != 1 || out[0].Backend != "primary" {
			t.Fatal("synthetic reply did not retain its origin")
		}
		if len(r.calls) != 0 || len(r.reverseCalls) != 0 || len(r.auth.primaryProofCalls) != 0 || len(r.auth.primaryProofCancelled) > 4096 || len(r.auth.primaryProofCancelOrder) > 4096 || len(r.staleCalls) > 256 || len(r.staleOrder) > 256 {
			t.Fatal("proof routing or tombstones grew beyond live bounds")
		}
		if handled, out, err := r.primaryProofResponse167(object{"id": publicID, "result": raw(object{"token": raw("LATE_SYNTHETIC")})}); !handled || err != nil || len(out) != 0 {
			t.Fatal("duplicate completed reply was forwarded")
		}
	}
}
