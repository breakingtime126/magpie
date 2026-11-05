package appserverbridge

import "testing"

// These names are real 0.166 protocol methods, not a promise that the bridge
// implements all Desktop startup traffic. In particular, getAuthStatus may
// refresh credentials even with refreshToken=false, and config requirements
// can serialize secret-bearing provider definitions. Never guess or fabricate
// a successful response merely to get a Desktop bootstrap past this boundary.
func TestDesktopBootstrapAuthAndConfigRemainUnsupported(t *testing.T) {
	for _, request := range []string{
		`{"id":9007199254740993,"method":"getAuthStatus","params":{}}`,
		`{"id":9007199254740993,"method":"getAuthStatus","params":{"includeToken":false,"refreshToken":false}}`,
		`{"id":9007199254740993,"method":"getAuthStatus","params":{"includeToken":null,"refreshToken":null}}`,
		`{"id":9007199254740993,"method":"getAuthStatus","params":{"includeToken":true}}`,
		`{"id":9007199254740993,"method":"getAuthStatus","params":{"refreshToken":true}}`,
		`{"id":9007199254740993,"method":"configRequirements/read"}`,
		`{"id":9007199254740993,"method":"configRequirements/read","params":null}`,
		`{"id":9007199254740993,"method":"config/read","params":{"includeLayers":true}}`,
		`{"id":9007199254740993,"method":"account/login/start","params":{"type":"chatgpt"}}`,
	} {
		r := testRouter(t, true)
		out := client(t, r, request)
		expectError(t, out)
		m, _ := parseObject(out[0].Message)
		e, _ := child(m, "error")
		if string(m["id"]) != "9007199254740993" || string(e["code"]) != "-32601" || exists(m, "result") {
			t.Fatalf("unsupported bootstrap method was represented as supported: %s", out[0].Message)
		}
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("unsupported bootstrap reached a backend or retained request state")
		}
		// A local unsupported response must not break subsequent safe requests.
		next := client(t, r, `{"id":2,"method":"model/list"}`)
		if len(next) != 1 || next[0].Backend != "" {
			t.Fatal("model catalog unavailable after local rejection")
		}
		m, _ = parseObject(next[0].Message)
		if !exists(m, "result") || exists(m, "error") {
			t.Fatal("local rejection poisoned the connection")
		}
	}
}

func TestDesktopBootstrapMalformedParamsDoNotRelaxObjectValidation(t *testing.T) {
	for _, params := range []string{`[]`, `true`, `"unexpected"`} {
		r := testRouter(t, true)
		out := client(t, r, `{"id":2,"method":"configRequirements/read","params":`+params+`}`)
		expectError(t, out)
		m, _ := parseObject(out[0].Message)
		e, _ := child(m, "error")
		if string(e["code"]) != "-32602" || len(r.pending) != 0 {
			t.Fatal("malformed unsupported request changed validation")
		}
	}
}
