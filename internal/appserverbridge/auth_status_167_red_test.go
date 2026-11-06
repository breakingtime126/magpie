package appserverbridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// This file uses only APIs present at 4e75e65d04f36618fd05dcd858dd19ceb3bc0a4c.
// Copy it alone to that base: the new mode must fail an assertion rather than
// compilation. No executable or real account is launched by these tests.
func TestAuthStatusNoAuth167BehavioralRed(t *testing.T) {
	cfg := fixtureConfig(t)
	for i := range cfg.Backends {
		cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	settings, err := parseObject(raw(cfg))
	if err != nil {
		t.Fatal(err)
	}
	settings["auth_status"] = raw("primary-no-auth-v0.167.0-alpha.3.2")
	cfg, err = LoadConfig(bytes.NewReader(raw(settings)))
	if err != nil {
		t.Fatalf("exact default-off no-auth mode was not accepted: %v", err)
	}
	r := testRouter(t, false)
	r.auth = cfg.authCompatibility()
	out := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":true,"refreshToken":false},"trace":null}`)
	if len(out) != 1 || out[0].Backend != "openai" {
		t.Fatal("includeToken:true did not reach only the primary")
	}
	m, _ := parseObject(out[0].Message)
	if len(m) != 3 || string(m["params"]) != `{"includeToken":true,"refreshToken":false}` {
		t.Fatal("request params changed or trace propagated")
	}
	response := reply(t, r, out[0], `{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}`)
	result, _ := parseObject(response[0].Message)
	if string(result["result"]) != `{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}` {
		t.Fatal("genuine native no-auth result rejected")
	}
	out = client(t, r, `{"id":2,"method":"getAuthStatus","params":{"includeToken":true}}`)
	response = reply(t, r, out[0], `{"authMethod":null,"authToken":"PRIVATE_TOKEN_CANARY","requiresOpenaiAuth":false}`)
	expectError(t, response)
	if strings.Contains(string(response[0].Message), "PRIVATE_") {
		t.Fatal("rejected token reached the client")
	}
}

// The correlation integrity repair applies even with every auth mode disabled.
// Both duplicate orders and decoded aliases must fail before either ID is used.
func TestBackendDuplicateEnvelopeBehavioralRed(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, key := range []string{`"id"`, `"\u0069d"`} {
			r := testRouter(t, false)
			first := client(t, r, `{"id":"first","method":"account/read","params":{}}`)[0]
			second := client(t, r, `{"id":"second","method":"account/read","params":{}}`)[0]
			a, _ := parseObject(first.Message)
			b, _ := parseObject(second.Message)
			left, right := a["id"], b["id"]
			if reverse {
				left, right = right, left
			}
			line := `{"id":` + string(left) + "," + key + ":" + string(right) + `,"result":{"fixture":"PRIVATE_CANARY"}}`
			out, err := r.Server("openai", []byte(line))
			if err == nil || len(out) != 0 || len(r.pending) != 2 || len(r.active) != 2 {
				t.Fatal("ambiguous backend response correlated or delivered a payload")
			}
		}
	}
}

func TestBackendEnvelopeRepairPreservesOpaquePayloads(t *testing.T) {
	r := testRouter(t, false)
	sent := client(t, r, `{"id":"first","method":"account/read","params":{}}`)[0]
	m, _ := parseObject(sent.Message)
	// Only top-level duplicates are covered globally. Existing result payload
	// semantics and legitimate text, including control escapes, are unchanged.
	payload := json.RawMessage(`{"fixture":"line\n\t�","nested":{"x":1,"x":2}}`)
	out := server(t, r, "openai", string(raw(object{"id": m["id"], "result": payload})))
	if len(out) != 1 || out[0].Backend != "" || !strings.Contains(string(out[0].Message), `"nested":{"x":1,"x":2}`) {
		t.Fatal("global envelope repair constrained an opaque result")
	}
}
