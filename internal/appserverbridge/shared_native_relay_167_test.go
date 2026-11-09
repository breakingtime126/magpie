package appserverbridge

import "testing"

func sharedRelayReady167(t *testing.T) *Router {
	t.Helper()
	r := sharedThreadReady167(t)
	r.auth.attestationNegotiated = true
	r.auth.gatewayNegotiated = true
	if !r.primaryRouteReady167() {
		t.Fatal("complete startup did not admit primary route")
	}
	return r
}

func sharedRelayRelease167(t *testing.T, r *Router, id string) []Delivery {
	t.Helper()
	cwd := t.TempDir()
	request := sharedThreadRequest167("thread/start", "openai", cwd)
	request["id"] = raw(id)
	out := client(t, r, string(raw(request)))
	check := sharedThreadConfigCheck167(t, r, out[0], cwd)
	return reply(t, r, check, string(raw(sharedThreadConfigResult167(t, r, "openai", cwd))))
}

func TestSharedNativeRelay167CreationCancelsProofAndLateReply(t *testing.T) {
	r := sharedRelayReady167(t)
	proof := server(t, r, "openai", `{"id":71,"method":"attestation/generate","params":{}}`)
	if len(proof) != 1 || proof[0].Backend != "" {
		t.Fatal("admitted primary proof was not relayed")
	}
	public, _ := parseObject(proof[0].Message)
	out := sharedRelayRelease167(t, r, "start")
	if len(out) != 2 || out[0].Backend != "openai" || out[1].Backend != "openai" || r.primaryRouteReady167() {
		t.Fatal("creation did not suspend and cancel proof before forwarding")
	}
	cancel, _ := parseObject(out[0].Message)
	if !sameJSON(cancel["id"], raw(71)) || !exists(cancel, "error") || exists(cancel, "result") {
		t.Fatal("cancellation fabricated a token or lost native correlation")
	}
	late, err := r.Client(raw(object{"id": public["id"], "result": raw(object{"token": raw("PUBLIC-FAKE-LATE-PROOF")})}))
	if err != nil || len(late) != 0 {
		t.Fatal("late cancelled proof was not discarded")
	}
	blocked := server(t, r, "openai", `{"id":72,"method":"attestation/generate","params":{}}`)
	if len(blocked) != 1 || blocked[0].Backend != "openai" {
		t.Fatal("prewarm proof escaped creation window")
	}
	message, _ := parseObject(blocked[0].Message)
	if !exists(message, "error") || exists(message, "result") {
		t.Fatal("prewarm failure was manufactured as a successful proof")
	}
	sharedLiveComplete167(t, r, out[1], "created")
	if !r.primaryRouteReady167() || r.sharedNativeLive167["created"] != "openai" {
		t.Fatal("validated native creation did not restore primary admission")
	}
	fresh := server(t, r, "openai", `{"id":73,"method":"attestation/generate","params":{}}`)
	if len(fresh) != 1 || fresh[0].Backend != "" {
		t.Fatal("a later real proof request could not be relayed")
	}
}

func TestSharedNativeRelay167AllConcurrentCreationsMustSucceed(t *testing.T) {
	for _, failure := range []bool{false, true} {
		r := sharedRelayReady167(t)
		first := sharedRelayRelease167(t, r, "first")[0]
		second := sharedRelayRelease167(t, r, "second")[0]
		sharedLiveComplete167(t, r, first, "first-thread")
		if r.primaryRouteReady167() {
			t.Fatal("one of two creations restored proof")
		}
		if failure {
			q, _ := parseObject(second.Message)
			out, err := r.Server("openai", raw(object{"id": q["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE-NATIVE-CANARY")})}))
			if err == nil || len(out) != 0 || r.primaryRouteReady167() || !r.auth.primaryProofFailed {
				t.Fatal("uncertain native creation did not terminate admission")
			}
		} else {
			sharedLiveComplete167(t, r, second, "second-thread")
			if !r.primaryRouteReady167() {
				t.Fatal("all validated creations did not restore proof")
			}
		}
	}
}

func TestSharedNativeRelay167ChangedNativeProviderIsFatal(t *testing.T) {
	r := sharedRelayReady167(t)
	d := sharedRelayRelease167(t, r, "start")[0]
	q, _ := parseObject(d.Message)
	out, err := r.Server("openai", raw(object{"id": q["id"], "result": raw(object{"modelProvider": raw("other"), "thread": raw(object{"id": raw("changed"), "modelProvider": raw("other")})})}))
	if err == nil || len(out) != 0 || r.primaryRouteReady167() || !r.auth.primaryProofFailed {
		t.Fatal("changed native provider retained proof admission")
	}
}

func TestSharedNativeRelay167GatewayReadUsesPrimaryAndAdmission(t *testing.T) {
	r := sharedRelayReady167(t)
	read := client(t, r, `{"id":"gateway","method":"account/gatewayOAuth/read","params":{}}`)
	if len(read) != 1 || read[0].Backend != "openai" {
		t.Fatal("gateway read was not primary-only")
	}
	good := `{"providerId":"openai","providerName":"OpenAI","required":false,"status":null,"error":null}`
	out := reply(t, r, read[0], good)
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatal("genuine primary gateway read was lost")
	}
	read = client(t, r, `{"id":"inflight","method":"account/gatewayOAuth/read","params":{}}`)
	creation := sharedRelayRelease167(t, r, "start")[0]
	expectError(t, reply(t, r, read[0], good))
	expectError(t, client(t, r, `{"id":"blocked","method":"account/gatewayOAuth/read","params":{}}`))
	if out := server(t, r, "openai", `{"method":"account/gatewayOAuth/changed","params":{"providerId":"openai","status":"notReady","authUrl":null,"error":null}}`); len(out) != 0 {
		t.Fatal("gateway notification escaped suspended admission")
	}
	sharedLiveComplete167(t, r, creation, "created")
	for _, method := range []string{"account/gatewayOAuth/login", "account/login/start", "account/login/cancel"} {
		expectError(t, client(t, r, string(raw(object{"id": raw(method), "method": raw(method), "params": raw(object{})}))))
	}
}
