package appserverbridge

import (
	"fmt"
	"strings"
	"testing"
)

func primaryWindowRouter167(t *testing.T) *Router {
	t.Helper()
	r, err := NewRouter([]Backend{{ID: "primary", Provider: "openai"}, {ID: "secondary", Provider: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	r.phase = "ready"
	r.auth.primaryProofCandidate = true
	r.auth.attestationNegotiated = true
	return r
}

func primaryWindowAsk167(t *testing.T, r *Router, id int) []Delivery {
	t.Helper()
	out, err := r.primaryProofRequest167("primary", raw(object{"id": raw(id), "method": raw("attestation/generate"), "params": raw(object{})}))
	if err != nil || len(out) != 1 {
		t.Fatal("valid primary request did not receive one bounded response")
	}
	return out
}

func primaryWindowUnavailable167(t *testing.T, out []Delivery, id int) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "primary" {
		t.Fatal("blocked proof was not answered only to its origin")
	}
	m, _ := parseObject(out[0].Message)
	e, _ := child(m, "error")
	if !sameJSON(m["id"], raw(id)) || !sameJSON(e["code"], raw(-32601)) || exists(m, "result") || strings.Contains(string(out[0].Message), "token") {
		t.Fatal("blocked proof was not a genuine typed-ID RPC failure")
	}
}

func TestPrimaryProofWindow167StartupAndOverlappingCreations(t *testing.T) {
	r := primaryWindowRouter167(t)
	r.phase = "initializing"
	primaryWindowUnavailable167(t, primaryWindowAsk167(t, r, 1), 1)
	r.phase = "ready"
	primaryWindowUnavailable167(t, primaryWindowAsk167(t, r, 2), 2)
	first, second := &request{}, &request{}
	r.beginPrimaryCreation167(first)
	r.admitPrimaryStartup167()
	if r.primaryRouteReady167() {
		t.Fatal("startup admission bypassed an active creation window")
	}
	r.beginPrimaryCreation167(second)
	if !r.primaryCreationPending167(first) || !r.primaryCreationPending167(second) || r.primaryCreationPending167(nil) {
		t.Fatal("creation tracking lost request identity")
	}
	primaryWindowUnavailable167(t, primaryWindowAsk167(t, r, 3), 3)
	if r.finishPrimaryCreation167(first, true) != nil || r.primaryRouteReady167() {
		t.Fatal("first completion reopened overlapping window")
	}
	if r.finishPrimaryCreation167(second, true) != nil || !r.primaryRouteReady167() {
		t.Fatal("complete successful window did not restore startup admission")
	}
	out := primaryWindowAsk167(t, r, 4)
	if out[0].Backend != "" || len(r.auth.primaryCreations) != 0 {
		t.Fatal("ready primary did not relay after all creation results")
	}
}

func TestPrimaryProofWindow167CancelsOutstandingProofsAndIgnoresLateReply(t *testing.T) {
	r := primaryWindowRouter167(t)
	r.admitPrimaryStartup167()
	pending := primaryWindowAsk167(t, r, 10)
	answered := primaryWindowAsk167(t, r, 11)
	pendingMessage, _ := parseObject(pending[0].Message)
	answeredMessage, _ := parseObject(answered[0].Message)
	handled, out, err := r.primaryProofResponse167(object{"id": answeredMessage["id"], "error": raw(object{"code": raw(-32000), "message": raw("synthetic frontend failure")})})
	if !handled || err != nil || len(out) != 1 || out[0].Backend != "primary" {
		t.Fatal("fixture frontend error was not routed")
	}
	// An unrelated server request is outside proof-window cancellation scope.
	r.calls["s:unrelated"] = serverCall{backend: "secondary", key: "s:other", id: raw("other")}
	r.reverseCalls["secondary\x00s:other"] = "unrelated"
	g := &request{}
	out = r.beginPrimaryCreation167(g)
	primaryWindowUnavailable167(t, out, 10)
	if len(r.calls) != 1 || len(r.reverseCalls) != 1 || len(r.auth.primaryProofCalls) != 0 || r.primaryRouteReady167() {
		t.Fatal("creation window retained proof bookkeeping or lost unrelated call")
	}
	for key := range r.auth.primaryProofCancelled {
		if _, present := r.calls[key]; present {
			t.Fatal("retired proof retained an origin route")
		}
	}
	for _, m := range []object{pendingMessage, answeredMessage} {
		key, _ := idKey(m["id"])
		if !r.staleCalls[key] {
			t.Fatal("cancelled proof lacks generic tombstone")
		}
		late := object{"id": m["id"], "result": raw(object{"token": raw("SYNTHETIC_NOT_A_PROOF")})}
		handled, out, err = r.primaryProofResponse167(late)
		if !handled || err != nil || len(out) != 0 {
			t.Fatal("late cancelled proof was forwarded")
		}
	}
	if r.finishPrimaryCreation167(g, true) != nil {
		t.Fatal("valid window completion failed")
	}
	handled, out, err = r.primaryProofResponse167(object{"id": pendingMessage["id"], "error": raw(object{"code": raw(-32000), "message": raw("late")})})
	if !handled || err != nil || len(out) != 0 {
		t.Fatal("reopening admission revived cancelled frontend reply")
	}
}

func TestPrimaryProofWindow167FailureOrUnknownCompletionPoisonsAdmission(t *testing.T) {
	for _, kind := range []string{"failure", "unknown", "duplicate-begin", "duplicate-finish"} {
		r := primaryWindowRouter167(t)
		r.admitPrimaryStartup167()
		g := &request{}
		r.beginPrimaryCreation167(g)
		completion, success := g, true
		switch kind {
		case "failure":
			success = false
		case "unknown":
			completion = &request{}
		case "duplicate-begin":
			r.beginPrimaryCreation167(g)
		case "duplicate-finish":
			if r.finishPrimaryCreation167(g, true) != nil {
				t.Fatal("first completion failed")
			}
		}
		if r.finishPrimaryCreation167(completion, success) == nil || r.primaryRouteReady167() || r.auth.primaryProofAdmitted {
			t.Fatal("failed or unmatched creation left proof admission open")
		}
		r.admitPrimaryStartup167()
		primaryWindowUnavailable167(t, primaryWindowAsk167(t, r, 1), 1)
		if r.primaryRouteReady167() {
			t.Fatal("startup method reset poisoned connection")
		}
	}
}

func TestPrimaryProofWindow167FullCancellationBatchRemainsBounded(t *testing.T) {
	r := primaryWindowRouter167(t)
	r.admitPrimaryStartup167()
	r.auth.primaryProofCalls = map[string]bool{}
	// Construct bookkeeping only, with no proof bytes or live token generation.
	for i := 0; i < 4096; i++ {
		public := fmt.Sprintf("s:synthetic-%04d", i)
		origin := fmt.Sprintf("origin-%04d", i)
		r.calls[public] = serverCall{backend: "primary", key: "s:" + origin, id: raw(origin)}
		r.reverseCalls["primary\x00s:"+origin] = strings.TrimPrefix(public, "s:")
		r.auth.primaryProofCalls[public] = true
	}
	out := r.beginPrimaryCreation167(&request{})
	if len(out) != 4096 || len(r.calls) != 0 || len(r.reverseCalls) != 0 || len(r.auth.primaryProofCalls) != 0 || len(r.auth.primaryProofCancelled) != 4096 || len(r.staleCalls) > 256 {
		t.Fatal("full outstanding batch was not cancelled within bounds")
	}
	for _, id := range []string{"synthetic-0000", "synthetic-4095"} {
		handled, out, err := r.primaryProofResponse167(object{"id": raw(id), "result": raw(object{"token": raw("SYNTHETIC_NOT_A_PROOF")})})
		if !handled || err != nil || len(out) != 0 {
			t.Fatal("bounded generic cache eviction exposed cancelled proof")
		}
	}
}

func TestPrimaryProofWindow167SecondaryMalformedAndDefaultStayClosed(t *testing.T) {
	r := primaryWindowRouter167(t)
	for _, line := range []string{`{"id":1,"method":"attestation/generate","params":{}}`, `{"id":1,"method":"attestation/generate","params":{"unknown":true}}`} {
		if out, err := r.primaryProofRequest167("secondary", []byte(line)); err == nil || len(out) != 0 {
			t.Fatal("secondary proof request did not fail closed")
		}
	}
	if out, err := r.primaryProofRequest167("primary", []byte(`{"id":1,"method":"attestation/generate","params":{"unknown":true}}`)); err == nil || len(out) != 0 {
		t.Fatal("unadmitted malformed proof disguised as valid unavailable request")
	}
	r.auth.primaryProofCandidate = false
	if out := r.beginPrimaryCreation167(&request{}); len(out) != 0 || r.finishPrimaryCreation167(&request{}, false) != nil || len(r.auth.primaryCreations) != 0 {
		t.Fatal("default mode acquired proof window state")
	}
}

func TestPrimaryProofWindow167GatewayNotificationsValidateBeforeSuppression(t *testing.T) {
	r := primaryWindowRouter167(t)
	r.auth.gatewayRead, r.auth.gatewayNegotiated = true, true
	line := raw(object{"method": raw("account/gatewayOAuth/changed"), "params": raw(object{"authUrl": raw(nil), "providerId": raw("openai"), "status": raw("notReady"), "error": raw(nil)})})
	if out, err := r.gatewayNotification("primary", line); err != nil || len(out) != 0 {
		t.Fatal("unadmitted gateway notification was not dropped")
	}
	r.admitPrimaryStartup167()
	if out, err := r.gatewayNotification("primary", line); err != nil || len(out) != 1 {
		t.Fatal("ready primary gateway notification disappeared")
	}
	g := &request{}
	r.beginPrimaryCreation167(g)
	if out, err := r.gatewayNotification("primary", line); err != nil || len(out) != 0 {
		t.Fatal("creation window gateway notification was not dropped")
	}
	bad := raw(object{"method": raw("account/gatewayOAuth/changed"), "params": raw(object{"authUrl": raw("PRIVATE_CANARY"), "providerId": raw("openai"), "status": raw("notReady"), "error": raw(nil)})})
	if out, err := r.gatewayNotification("primary", bad); err == nil || len(out) != 0 {
		t.Fatal("blocked-route suppression bypassed notification validation")
	}
	if r.finishPrimaryCreation167(g, true) != nil {
		t.Fatal("window did not finish")
	}
	r.auth.primaryProofCandidate = false
	r.phase = "initializing"
	if out, err := r.gatewayNotification("primary", line); err == nil || len(out) != 0 {
		t.Fatal("default-mode startup notification behavior changed")
	}
}

func TestPrimaryProofWindow167UnexpectedNativeResolutionFailsClosed(t *testing.T) {
	r := primaryWindowRouter167(t)
	r.admitPrimaryStartup167()
	out := primaryWindowAsk167(t, r, 7)
	public, _ := parseObject(out[0].Message)
	key, _ := idKey(public["id"])
	r.beginPrimaryCreation167(&request{})
	r.threads["synthetic-thread"] = "primary"
	m := object{"method": raw("serverRequest/resolved"), "params": raw(object{"threadId": raw("synthetic-thread"), "requestId": raw(7)})}
	out, err := r.serverMessage("primary", "serverRequest/resolved", m)
	if err == nil || len(out) != 0 || len(r.calls) != 0 || len(r.reverseCalls) != 0 || r.auth.primaryProofCalls[key] {
		t.Fatal("unsupported native proof resolution did not fail closed")
	}
	handled, late, err := r.primaryProofResponse167(object{"id": public["id"], "result": raw(object{"token": raw("SYNTHETIC_NOT_A_PROOF")})})
	if !handled || err != nil || len(late) != 0 {
		t.Fatal("native resolution reenabled cancelled frontend reply")
	}
}
