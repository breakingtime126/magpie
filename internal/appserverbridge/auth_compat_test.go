package appserverbridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func authCompatRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.auth = authCompatibility{attestationUnavailable: true, gatewayRead: true, gatewayProviderName: "OpenAI"}
	if err := r.auth.negotiate(object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true)}); err != nil {
		t.Fatal(err)
	}
	r.auth.now = func() time.Time { return time.Unix(1000, 0) }
	return r
}

func TestAuthCompatNegotiationStrictAndAtomic(t *testing.T) {
	for _, name := range []string{"requestAttestation", "explicitGatewayOauth"} {
		for _, invalid := range []string{"null", "0", "1", `"true"`, "{}", "[]"} {
			a := authCompatibility{attestationUnavailable: true, gatewayRead: true}
			caps := object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true)}
			caps[name] = json.RawMessage(invalid)
			before := raw(caps)
			if a.negotiate(caps) == nil || a.attestationNegotiated || a.gatewayNegotiated || !bytes.Equal(before, raw(caps)) {
				t.Fatalf("accepted or mutated %s=%s", name, invalid)
			}
		}
		a := authCompatibility{}
		if a.negotiate(object{name: raw(true)}) == nil {
			t.Fatal("disabled mode accepted")
		}
		if a.negotiate(object{name: raw(false)}) != nil {
			t.Fatal("false rejected")
		}
	}
	a := authCompatibility{attestationUnavailable: true, gatewayRead: true}
	caps := object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true)}
	before := raw(caps)
	if a.negotiate(caps) != nil || !a.attestationNegotiated || !a.gatewayNegotiated || !bytes.Equal(before, raw(caps)) {
		t.Fatal("true negotiation failed")
	}
	if a.negotiate(object{}) != nil || a.attestationNegotiated || a.gatewayNegotiated {
		t.Fatal("absent negotiation retained flags")
	}
}

func TestAuthCompatRoutesTypedIDsWithoutState(t *testing.T) {
	r := authCompatRouter(t)
	for _, backend := range []string{"openai", "aihub"} {
		for _, id := range []string{`""`, `"7"`, "7", "-0", "-9223372036854775808", "9223372036854775807", `"\u0061"`} {
			for reuse := 0; reuse < 2; reuse++ {
				out, err := r.attestationMessage(backend, []byte(`{"id":`+id+`,"method":"attestation/generate","params":{}}`))
				if err != nil || len(out) != 1 || out[0].Backend != backend {
					t.Fatalf("route %s/%s: %v %v", backend, id, out, err)
				}
				m, err := parseObject(out[0].Message)
				if err != nil || len(m) != 2 || string(m["id"]) != id || exists(m, "result") {
					t.Fatalf("wrong response: %s", out[0].Message)
				}
				e, _ := child(m, "error")
				if len(e) != 2 || string(e["code"]) != "-32601" || text(e, "message") != "attestation/generate is unavailable through this bridge" {
					t.Fatalf("wrong error: %s", out[0].Message)
				}
			}
		}
	}
	if len(r.calls) != 0 || len(r.pending) != 0 || len(r.reverseCalls) != 0 || len(r.threads) != 0 || len(r.auth.rates) != 2 {
		t.Fatal("attestation changed ownership or request state")
	}
}

func TestAuthCompatRejectsRawMalformedMessages(t *testing.T) {
	valid := `{"id":1,"method":"attestation/generate","params":{}}`
	cases := []string{
		`{"id":1,"id":2,"method":"attestation/generate","params":{}}`,
		`{"id":1,"method":"other","method":"attestation/generate","params":{}}`,
		`{"id":1,"method":"attestation/generate","params":{"x":1,"x":2}}`,
		strings.Replace(valid, `"params":{}`, `"params":null`, 1),
		strings.Replace(valid, `,"params":{}`, "", 1),
		strings.Replace(valid, `"params":{}`, `"params":[]`, 1),
		strings.Replace(valid, `"params":{}`, `"params":{"threadId":"a"}`, 1),
		strings.Replace(valid, `"id":1`, `"jsonrpc":"2.0","id":1`, 1),
		strings.Replace(valid, `"id":1`, `"result":{},"id":1`, 1),
		valid + "{}",
		valid + strings.Repeat(" ", 4096),
		"[]",
	}
	for _, id := range []string{"null", "true", "false", "1.0", "1e0", "9223372036854775808", "-9223372036854775809", "01", "+1", `"\ud800"`, `"\u0000"`, string(raw(strings.Repeat("a", 257)))} {
		cases = append(cases, strings.Replace(valid, `"id":1`, `"id":`+id, 1))
	}
	cases = append(cases, strings.Replace(valid, `"id":1`, "\"id\":\""+string([]byte{0xff})+"\"", 1))
	for i, message := range cases {
		r := authCompatRouter(t)
		out, err := r.attestationMessage("openai", []byte(message))
		if err == nil || len(out) != 0 || len(r.auth.rates) != 0 {
			t.Fatalf("case %d accepted or retained state", i)
		}
	}
}

func TestAuthCompatGatesCollisionsAndRate(t *testing.T) {
	message := []byte(`{"id":0,"method":"attestation/generate","params":{}}`)
	for _, phase := range []string{"cold", "initializing", "initialized"} {
		r := authCompatRouter(t)
		r.phase = phase
		if _, err := r.attestationMessage("openai", message); err == nil {
			t.Fatal("accepted before ready")
		}
	}
	for _, mode := range []string{"off", "false", "absent", "unknown"} {
		r := authCompatRouter(t)
		backend := "openai"
		switch mode {
		case "off":
			r.auth.attestationUnavailable = false
		case "false":
			_ = r.auth.negotiate(object{"requestAttestation": raw(false)})
		case "absent":
			_ = r.auth.negotiate(object{})
		case "unknown":
			backend = "unknown"
		}
		if _, err := r.attestationMessage(backend, message); err == nil {
			t.Fatal("mode gate bypassed")
		}
	}
	for _, answered := range []bool{false, true} {
		r := authCompatRouter(t)
		r.calls["s:translated"] = serverCall{backend: "openai", key: "n:-0", id: json.RawMessage("-0"), answered: answered}
		if _, err := r.attestationMessage("openai", message); err == nil {
			t.Fatal("live collision accepted")
		}
		if _, err := r.attestationMessage("aihub", message); err != nil {
			t.Fatal("cross-backend typed ID rejected")
		}
		r.calls["s:translated"] = serverCall{backend: "openai", key: "s:0", id: raw("0")}
		if _, err := r.attestationMessage("openai", message); err != nil {
			t.Fatal("different typed ID collided")
		}
	}
	r := authCompatRouter(t)
	now := time.Unix(1000, 0)
	r.auth.now = func() time.Time { return now }
	for i := 0; i < 64; i++ {
		if _, err := r.attestationMessage("openai", message); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := r.attestationMessage("openai", message); err == nil {
		t.Fatal("burst unbounded")
	}
	now = now.Add(time.Second / 32)
	if _, err := r.attestationMessage("openai", message); err != nil {
		t.Fatal("no refill")
	}
	if _, err := r.attestationMessage("openai", message); err == nil {
		t.Fatal("refill unbounded")
	}
	now = now.Add(-time.Second)
	if _, err := r.attestationMessage("openai", message); err == nil {
		t.Fatal("backward clock refilled")
	}
	for i := 0; i < 100; i++ {
		_, _ = r.attestationMessage("unknown-"+strings.Repeat("x", i), message)
	}
	if len(r.auth.rates) != 1 || len(r.calls) != 0 || len(r.pending) != 0 || len(r.reverseCalls) != 0 {
		t.Fatal("rate or request maps grew")
	}
}

func TestGatewayOAuthReadCompleteAndStrict(t *testing.T) {
	b := Backend{ID: "primary", Provider: "openai"}
	base := object{"providerId": raw("openai"), "providerName": raw("OpenAI"), "required": raw(true), "status": raw("notReady"), "error": raw(nil)}
	envelope := func(result object) []byte { return raw(object{"id": raw("read"), "result": raw(result)}) }
	for _, status := range []string{"notReady", "started", "succeeded", "failed"} {
		base["status"] = raw(status)
		got, err := validateGatewayRead(envelope(base), b, "OpenAI")
		if err != nil || !bytes.Equal(raw(got), raw(base)) {
			t.Fatal("public status changed", err)
		}
	}
	base["required"], base["status"] = raw(false), raw(nil)
	got, err := validateGatewayRead(envelope(base), b, "OpenAI")
	if err != nil || !bytes.Equal(raw(got), raw(base)) {
		t.Fatal("false/null not preserved")
	}
	for _, change := range []struct {
		name  string
		value json.RawMessage
	}{
		{"providerId", raw("aihub")}, {"providerName", raw("Wrong")}, {"providerName", raw(nil)},
		{"required", raw(nil)}, {"required", raw("false")}, {"status", raw("notReady")},
		{"error", raw("credential-shaped-error")}, {"extra", raw("proof")},
	} {
		m := clone(base)
		m[change.name] = change.value
		if _, err := validateGatewayRead(envelope(m), b, "OpenAI"); err == nil {
			t.Fatal("accepted", change.name)
		}
	}
	for name := range base {
		m := clone(base)
		delete(m, name)
		if _, err := validateGatewayRead(envelope(m), b, "OpenAI"); err == nil {
			t.Fatal("missing", name)
		}
	}
	base["required"] = raw(true)
	for _, status := range []json.RawMessage{raw(nil), raw("unknown"), raw(1)} {
		base["status"] = status
		if _, err := validateGatewayRead(envelope(base), b, "OpenAI"); err == nil {
			t.Fatal("invalid status")
		}
	}
	for _, line := range []string{
		`{"id":"read","error":{"message":"secret"}}`,
		`{"id":"read","result":{},"error":null}`,
		`{"id":"read","id":"other","result":{}}`,
		`{"id":null,"result":{}}`,
		string(envelope(base)) + strings.Repeat(" ", 16<<10),
	} {
		if _, err := validateGatewayRead([]byte(line), b, "OpenAI"); err == nil || err.Error() != "gateway OAuth status unavailable" {
			t.Fatal("untrusted read error escaped")
		}
	}
}

func TestGatewayOAuthNotificationsPrimaryOnly(t *testing.T) {
	r := authCompatRouter(t)
	line := []byte(` { "method":"account/gatewayOAuth/changed", "params":{"authUrl":null,"providerId":"openai","status":"notReady","error":null}} `)
	out, err := r.gatewayNotification("openai", line)
	if err != nil || len(out) != 1 || out[0].Backend != "" || !bytes.Equal(out[0].Message, line) {
		t.Fatal("safe primary notice changed", err)
	}
	secondary := bytes.Replace(line, []byte("openai"), []byte("aihub"), 1)
	if out, err := r.gatewayNotification("aihub", secondary); err != nil || len(out) != 0 {
		t.Fatal("secondary leaked")
	}
	for _, mode := range []string{"off", "unnegotiated"} {
		r := authCompatRouter(t)
		if mode == "off" {
			r.auth.gatewayRead = false
		} else {
			r.auth.gatewayNegotiated = false
		}
		if out, err := r.gatewayNotification("openai", line); err != nil || len(out) != 0 {
			t.Fatal("disabled notice leaked")
		}
	}
	for _, replacement := range []struct{ old, new string }{
		{`"authUrl":null`, `"authUrl":"https://example.invalid/secret"`},
		{`"error":null`, `"error":"secret"`},
		{`"status":"notReady"`, `"status":null`},
		{`"providerId":"openai"`, `"providerId":"wrong"`},
		{`"authUrl":null`, `"authUrl":null,"authUrl":null`},
		{`"method":`, `"id":null,"method":`},
		{`"method":`, `"result":{},"method":`},
		{`"method":`, `"jsonrpc":"2.0","method":`},
	} {
		bad := bytes.Replace(line, []byte(replacement.old), []byte(replacement.new), 1)
		if out, err := r.gatewayNotification("openai", bad); err == nil || len(out) != 0 {
			t.Fatal("unsafe notice accepted")
		}
		bad = bytes.Replace(bad, []byte("openai"), []byte("aihub"), 1)
		if out, err := r.gatewayNotification("aihub", bad); err == nil || len(out) != 0 {
			t.Fatal("unsafe secondary accepted")
		}
	}
	r.phase = "initialized"
	if _, err := r.gatewayNotification("openai", line); err == nil {
		t.Fatal("notice before ready")
	}
	if _, err := r.gatewayNotification("unknown", line); err == nil {
		t.Fatal("unknown backend")
	}
	if _, err := r.gatewayNotification("openai", append(line, bytes.Repeat([]byte(" "), 16<<10)...)); err == nil {
		t.Fatal("oversized notice")
	}
}

func TestGatewayOAuthReadRawEnvelopeAndUnicode(t *testing.T) {
	valid := `{"id":"read","result":{"providerId":"openai","providerName":"OpenAI","required":true,"status":"succeeded","error":null}}`
	for _, pair := range []struct{ old, new string }{
		{`"providerId":"openai"`, `"providerId":"wrong","providerId":"openai"`},
		{`"providerName":"OpenAI"`, `"providerName":"wrong","provider\u004eame":"OpenAI"`},
		{`"providerName":"OpenAI"`, `"providerName":"\ud800"`},
		{`"providerName":"OpenAI"`, `"providerName":"OpenAI\u0000"`},
		{`"id":"read"`, `"id":"other","id":"read"`},
		{`"id":"read"`, `"id":1e0`},
		{`"id":"read"`, `"id":"read","jsonrpc":"2.0"`},
		{`"id":"read"`, `"id":"read","method":"unknown"`},
		{`"required":true`, `"required":true,"required":false`},
	} {
		line := strings.Replace(valid, pair.old, pair.new, 1)
		if _, err := validateGatewayRead([]byte(line), Backend{Provider: "openai"}, "OpenAI"); err == nil {
			t.Fatal("raw envelope accepted")
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 129), "OpenAI\n"} {
		if _, err := validateGatewayRead([]byte(valid), Backend{Provider: "openai"}, name); err == nil {
			t.Fatal("invalid expected provider name")
		}
	}
}

func TestAuthCompatIntegratedNegotiationAndPrewarm(t *testing.T) {
	r, err := NewRouter([]Backend{{ID: "openai", Provider: "openai"}, {ID: "aihub", Provider: "aihub"}})
	if err != nil {
		t.Fatal(err)
	}
	r.auth = authCompatibility{attestationUnavailable: true, gatewayRead: true, gatewayProviderName: "OpenAI"}
	params := json.RawMessage(`{"clientInfo":{"name":"desktop-fixture","version":"0"},"capabilities":{"requestAttestation":true,"explicitGatewayOauth":true,"experimentalApi":true,"extensions":{"fixture":{"preserved":true}},"optOutNotificationMethods":["fixture/notice"]}}`)
	out := client(t, r, string(raw(object{"id": raw("init"), "method": raw("initialize"), "params": params})))
	if len(out) != 2 || !r.auth.attestationNegotiated || !r.auth.gatewayNegotiated {
		t.Fatal("negotiation failed")
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if !sameJSON(m["params"], params) {
			t.Fatal("desktop capabilities changed")
		}
	}
	reply(t, r, out[0], `{"userAgent":"first"}`)
	reply(t, r, out[1], `{"userAgent":"second"}`)
	client(t, r, `{"method":"initialized"}`)
	for _, backend := range []string{"openai", "aihub"} {
		got := server(t, r, backend, `{"id":8,"method":"attestation/generate","params":{}}`)
		if len(got) != 1 || got[0].Backend != backend || len(r.threads) != 0 {
			t.Fatal("prewarm routing failed")
		}
	}
	for _, caps := range []string{
		`{"requestAttestation":true}`, `{"explicitGatewayOauth":true}`,
		`{"requestAttestation":null}`, `{"explicitGatewayOauth":null}`,
	} {
		cold, _ := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
		expectError(t, client(t, cold, `{"id":1,"method":"initialize","params":{"capabilities":`+caps+`}}`))
		if len(cold.pending) != 0 || cold.phase != "cold" {
			t.Fatal("rejected initialize sent")
		}
	}
}

func TestGatewayOAuthIntegratedReadAndSanitization(t *testing.T) {
	result := `{"providerId":"openai","providerName":"OpenAI","required":false,"status":null,"error":null}`
	for _, params := range []string{"", `,"params":null`, `,"params":{}`} {
		r := authCompatRouter(t)
		out := client(t, r, `{"id":"read","method":"account/gatewayOAuth/read"`+params+`}`)
		if len(out) != 1 || out[0].Backend != "openai" {
			t.Fatal("read not primary only")
		}
		m, _ := parseObject(out[0].Message)
		if !requirementsNull(m["params"]) {
			t.Fatal("read not adapted to no arguments")
		}
		got := reply(t, r, out[0], result)
		response, _ := parseObject(got[0].Message)
		if !sameJSON(response["result"], []byte(result)) || string(response["id"]) != `"read"` {
			t.Fatal("public result changed")
		}
	}
	for _, method := range []string{"account/gatewayOAuth/login", "account/gatewayOAuth/cancel"} {
		r := authCompatRouter(t)
		expectError(t, client(t, r, `{"id":1,"method":"`+method+`"}`))
		if len(r.pending) != 0 {
			t.Fatal("mutation reached child")
		}
	}
	for _, body := range []string{
		`"error":{"code":-1,"message":"private-secret"}`,
		`"result":{"providerId":"openai","providerName":"OpenAI","required":true,"status":"failed","error":"private-secret"}`,
		`"result":{"providerId":"openai","providerName":"private-secret","required":false,"status":null,"error":null}`,
		`"result":{},"error":{"message":"private-secret"}`,
	} {
		r := authCompatRouter(t)
		d := client(t, r, `{"id":1,"method":"account/gatewayOAuth/read"}`)[0]
		m, _ := parseObject(d.Message)
		got := server(t, r, "openai", `{"id":`+string(m["id"])+`,`+body+`}`)
		expectError(t, got)
		if strings.Contains(string(got[0].Message), "private-secret") || len(r.pending) != 0 || len(r.active) != 0 {
			t.Fatal("read error leaked or retained")
		}
	}
	r := authCompatRouter(t)
	d := client(t, r, `{"id":2,"method":"account/gatewayOAuth/read"}`)[0]
	m, _ := parseObject(d.Message)
	foreign := raw(object{"id": m["id"], "result": json.RawMessage(result)})
	if out, err := r.Server("aihub", foreign); err == nil || len(out) != 0 || len(r.pending) != 1 {
		t.Fatal("secondary satisfied primary read")
	}
	reply(t, r, d, result)
	for _, enabled := range []bool{false, true} {
		r := authCompatRouter(t)
		r.auth.gatewayRead, r.auth.gatewayNegotiated = enabled, false
		expectError(t, client(t, r, `{"id":3,"method":"account/gatewayOAuth/read"}`))
		if len(r.pending) != 0 {
			t.Fatal("unnegotiated read reached child")
		}
	}
}

func TestAuthCompatServerEnvelopeRoutingKeys(t *testing.T) {
	for _, authMethod := range []string{"attestation/generate", "account/gatewayOAuth/changed"} {
		for _, methods := range [][]string{{authMethod, "account/updated"}, {"account/updated", authMethod}} {
			for _, secondKey := range []string{"method", "meth\\u006fd"} {
				r := authCompatRouter(t)
				line := []byte(`{"method":"` + methods[0] + `","` + secondKey + `":"` + methods[1] + `","params":{}}`)
				out, err := r.Server("openai", line)
				if err == nil || len(out) != 0 {
					t.Fatal("duplicate routing keys bypassed authentication validation")
				}
			}
		}
	}
	for _, authMethod := range []string{"attestation/generate", "account/gatewayOAuth/changed"} {
		for _, finalMethod := range []string{"null", `""`, "17"} {
			for _, finalKey := range []string{"method", "meth\\u006fd"} {
				r := authCompatRouter(t)
				line := []byte(`{"method":"` + authMethod + `","` + finalKey + `":` + finalMethod + `,"params":{}}`)
				out, err := r.Server("openai", line)
				if err == nil || len(out) != 0 {
					t.Fatal("nontext or empty duplicate method bypassed presence guard")
				}
			}
		}
	}
	for _, method := range []string{"null", `""`, "17"} {
		for _, key := range []string{"method", "meth\\u006fd"} {
			r := authCompatRouter(t)
			line := []byte(`{"` + key + `":` + method + `,"params":{}}`)
			out, err := r.Server("openai", line)
			if err == nil || len(out) != 0 {
				t.Fatal("standalone nontext or empty method bypassed presence guard")
			}
		}
	}

}

func TestAuthCompatServerEnvelopeKeysBoundedAndShallow(t *testing.T) {
	for _, line := range []string{
		"[]", "null", "true", "", "{", `{}{}`, `{} false`,
		`{"id":1,"id":2}`,
		`{"method":"account/updated","method":"attestation/generate"}`,
		`{"method":"account/updated","meth\u006fd":"attestation/generate"}`,
		`{"params":{},"params":{}}`,
		`{"result":{},"result":{}}`,
		`{"error":null,"error":null}`,
		`{"key":}`,
		`{"\ud800":1}`,
		`{"\u0000":1}`,
	} {
		if validateServerEnvelopeKeys([]byte(line)) == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	fields := object{}
	for i := 0; i < 16; i++ {
		fields[strings.Repeat("a", i+1)] = raw(nil)
	}
	if validateServerEnvelopeKeys(raw(fields)) != nil {
		t.Fatal("sixteen keys rejected")
	}
	fields["seventeenth"] = raw(nil)
	if validateServerEnvelopeKeys(raw(fields)) == nil {
		t.Fatal("key count unbounded")
	}
	if validateServerEnvelopeKeys(bytes.Repeat([]byte(" "), MaxMessageBytes+1)) == nil {
		t.Fatal("size unbounded")
	}

	// The routing-key guard must not apply the auth helpers' 4/16/64 KiB
	// limits, depth limit, or nested duplicate rules to unrelated tool data.
	nested := strings.Repeat("[", 32) + `{"x":1,"x":2}` + strings.Repeat("]", 32)
	line := []byte(`{"method":"fixture/tool","params":{"text":"` + strings.Repeat("x", 128<<10) + `","nested":` + nested + `}}`)
	if validateServerEnvelopeKeys(line) != nil {
		t.Fatal("unrelated payload was narrowed")
	}
	if validateServerEnvelopeKeys([]byte(" {} \n\t")) != nil {
		t.Fatal("valid whitespace rejected")
	}
}
