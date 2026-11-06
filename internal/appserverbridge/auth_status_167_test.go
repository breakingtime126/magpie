package appserverbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const authStatusTestResult = `{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}`

func authStatusTestRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.auth = (Config{AuthStatus: AuthStatusPrimaryNoAuth167}).authCompatibility()
	return r
}

func authStatusTestCompact(t *testing.T, encoded string) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(encoded)); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func authStatusTestError(t *testing.T, out []Delivery, code string) {
	t.Helper()
	expectError(t, out)
	m, _ := parseObject(out[0].Message)
	failure, err := child(m, "error")
	if err != nil || len(m) != 2 || len(failure) != 2 || string(failure["code"]) != code ||
		text(failure, "message") != "primary no-auth status unavailable" || exists(m, "result") ||
		strings.Contains(string(out[0].Message), "PRIVATE_") {
		t.Fatalf("no-auth failure changed or exposed source text: %s", out[0].Message)
	}
}

func TestAuthStatus167PreservesAllOptionalBooleanForms(t *testing.T) {
	for _, include := range []string{"", "null", "false", "true"} {
		for _, refresh := range []string{"", "null", "false"} {
			t.Run(include+"/"+refresh, func(t *testing.T) {
				fields := []string{}
				if include != "" {
					fields = append(fields, `"include\u0054oken" : `+include)
				}
				if refresh != "" {
					fields = append(fields, `"refreshToken" : `+refresh)
				}
				params := "{ " + strings.Join(fields, ", ") + " }"
				r := authStatusTestRouter(t)
				out := client(t, r, `{"id":9007199254740993,"method":"getAuthStatus","params":`+params+`,"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
				if len(out) != 1 || out[0].Backend != "openai" || len(r.pending) != 1 {
					t.Fatalf("request did not go only to primary: %v", out)
				}
				m, _ := parseObject(out[0].Message)
				if len(m) != 3 || exists(m, "trace") || text(m, "method") != "getAuthStatus" ||
					string(m["params"]) != authStatusTestCompact(t, params) || strings.Contains(string(out[0].Message), "PRIVATE_") {
					t.Fatal("params changed or unscoped trace reached a child")
				}
				source := `{ "requiresOpenaiAuth" : false, "auth\u0054oken" : null, "authMethod" : null }`
				response := reply(t, r, out[0], source)
				if len(response) != 1 || response[0].Backend != "" {
					t.Fatal("primary result missing")
				}
				result, _ := parseObject(response[0].Message)
				if len(result) != 2 || string(result["id"]) != "9007199254740993" ||
					string(result["result"]) != authStatusTestCompact(t, source) ||
					len(r.active) != 0 || len(r.pending) != 0 || r.activeBytes != 0 {
					t.Fatal("genuine primary result, ID or completion state changed")
				}
			})
		}
	}
}

func TestAuthStatus167StrictEnvelopeAndParams(t *testing.T) {
	valid := `{"id":1,"method":"getAuthStatus","params":{}}`
	cases := []string{
		strings.Replace(valid, `,"params":{}`, "", 1),
		strings.Replace(valid, `"params":{}`, `"params":null`, 1),
		strings.Replace(valid, `"params":{}`, `"params":[]`, 1),
		strings.Replace(valid, `"params":{}`, `"params":false`, 1),
		strings.Replace(valid, `"id":1`, `"id":1,"id":2`, 1),
		strings.Replace(valid, `"id":1`, `"id":1,"\u0069d":2`, 1),
		strings.Replace(valid, `"params":{}`, `"params":{},"p\u0061rams":{}`, 1),
		strings.Replace(valid, `"method":"getAuthStatus"`, `"method":"account/read","method":"getAuthStatus"`, 1),
		strings.Replace(valid, `"method":"getAuthStatus"`, `"method":"getAuthStatus","m\u0065thod":"account/read"`, 1),
		valid + "{}",
		`{"method":"getAuthStatus","params":{}}`,
	}
	for _, field := range []string{
		`"refreshToken":true`, `"refreshToken":"false"`, `"refreshToken":0`,
		`"includeToken":0`, `"includeToken":"true"`, `"includeToken":[]`,
		`"includeToken":{}`, `"unknown":"PRIVATE_PARAM"`, `"IncludeToken":false`,
		`"includeToken":true,"include\u0054oken":false`,
		`"refreshToken":true,"refreshToken":false`,
		`"refreshToken":false,"refreshToken":true`,
	} {
		cases = append(cases, strings.Replace(valid, `"params":{}`, `"params":{`+field+"}", 1))
	}
	for _, field := range []string{
		`"jsonrpc":"2.0"`, `"result":{}`, `"error":null`, `"unknown":"PRIVATE_ENVELOPE"`,
		`"trace":true`, `"trace":[]`, `"trace":{"traceparent":0}`,
		`"trace":{"unknown":"PRIVATE_TRACE"}`, `"trace":{"traceparent":"PRIVATE_TRACE","trace\u0070arent":null}`,
		`"trace":null,"tr\u0061ce":null`, `"Trace":null`,
		`"trace":{"traceparent":"\ud800"}`, `"trace":{"traceparent":"\u0000"}`,
	} {
		cases = append(cases, valid[:len(valid)-1]+","+field+"}")
	}
	for _, id := range []string{"null", "true", "false", "1.0", "1e0", "9223372036854775808", "-9223372036854775809", "01", "+1", `"\ud800"`, `"\u0000"`, string(raw(strings.Repeat("a", 257)))} {
		cases = append(cases, strings.Replace(valid, `"id":1`, `"id":`+id, 1))
	}
	cases = append(cases, strings.Replace(valid, `"id":1`, "\"id\":\""+string([]byte{0xff})+"\"", 1))
	for i, input := range cases {
		r := authStatusTestRouter(t)
		if validateAuthStatusRequest167([]byte(input)) {
			t.Fatalf("validator accepted malformed request %d", i)
		}
		out, err := r.Client([]byte(input))
		if err == nil {
			expectError(t, out)
		} else if len(out) != 0 {
			t.Fatalf("fatal malformed request %d delivered data", i)
		}
		for _, d := range out {
			if d.Backend != "" || strings.Contains(string(d.Message), "PRIVATE_") {
				t.Fatalf("malformed request %d reached a child or exposed params", i)
			}
		}
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatalf("malformed request %d retained state", i)
		}
	}
}

func TestAuthStatus167NativeIDsTraceAndBounds(t *testing.T) {
	for _, id := range []string{`""`, `"7"`, "7", "-0", "-9223372036854775808", "9223372036854775807", `"\u0061"`, string(raw(strings.Repeat("a", 256)))} {
		r := authStatusTestRouter(t)
		out := client(t, r, `{"\u0069d":`+id+`,"method":"get\u0041uthStatus","params":{},"tr\u0061ce":null}`)
		if len(out) != 1 || out[0].Backend != "openai" {
			t.Fatal("valid native ID or escaped exact key rejected")
		}
		response := reply(t, r, out[0], authStatusTestResult)
		m, _ := parseObject(response[0].Message)
		if string(m["id"]) != id {
			t.Fatal("native ID bytes changed")
		}
	}
	request := func(trace string) string {
		return `{"id":1,"method":"getAuthStatus","params":{},"trace":` + trace + "}"
	}
	for _, trace := range []string{"null", "{}", `{"traceparent":null}`, `{"traceparent":"","tracestate":""}`,
		`{"traceparent":` + string(raw(strings.Repeat("a", 1024))) + "}",
		`{"tracestate":` + string(raw(strings.Repeat("é", 512))) + "}",
		"{" + strings.Repeat(" ", (16<<10)-2) + "}"} {
		if !validateAuthStatusRequest167([]byte(request(trace))) {
			t.Fatal("valid bounded trace shape rejected")
		}
	}
	for _, trace := range []string{
		`{"traceparent":` + string(raw(strings.Repeat("a", 1025))) + "}",
		`{"tracestate":` + string(raw(strings.Repeat("é", 513))) + "}",
		"{" + strings.Repeat(" ", (16<<10)-1) + "}"} {
		if validateAuthStatusRequest167([]byte(request(trace))) {
			t.Fatal("trace bound bypassed")
		}
	}
	valid := request("null")
	if !validateAuthStatusRequest167([]byte(valid+strings.Repeat(" ", (64<<10)-len(valid)))) ||
		validateAuthStatusRequest167([]byte(valid+strings.Repeat(" ", (64<<10)+1-len(valid)))) {
		t.Fatal("whole-request size boundary changed")
	}
}

func TestAuthStatus167RejectsWholeUnsafeResult(t *testing.T) {
	results := []string{"null", "{}", "[]",
		`{"authMethod":null,"authToken":"PRIVATE_TOKEN","requiresOpenaiAuth":false}`,
		`{"authMethod":"","authToken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":"apikey","authToken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":true}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":null}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":"false"}`,
		`{"authMethod":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":null,"authToken":null}`,
		`{"authToken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false,"extra":"PRIVATE_EXTRA"}`,
		`{"authMethod":null,"authToken":"PRIVATE_TOKEN","auth\u0054oken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false,"requiresOpenaiAuth":false}`,
	}
	r := authStatusTestRouter(t)
	for _, result := range results {
		sent := client(t, r, `{"id":"same","method":"getAuthStatus","params":{"includeToken":true}}`)
		authStatusTestError(t, reply(t, r, sent[0], result), "-32000")
		if len(r.active) != 0 || len(r.pending) != 0 || r.activeBytes != 0 {
			t.Fatal("unsafe result retained a request")
		}
	}
	sent := client(t, r, `{"id":"same","method":"getAuthStatus","params":{}}`)
	out := reply(t, r, sent[0], authStatusTestResult)
	m, _ := parseObject(out[0].Message)
	if string(m["result"]) != authStatusTestResult {
		t.Fatal("fresh primary success did not recover after failures")
	}
}

func TestAuthStatus167NativeErrorsKeepInt64CodeOnly(t *testing.T) {
	for _, code := range []string{"-9223372036854775808", "9223372036854775807", "-32037", "0"} {
		for _, message := range []string{`"PRIVATE_MESSAGE\n\t\r\b\f\u0000"`, `"�"`, `"\ufffd"`, `"\ud83d\ude00"`, `"\\ud800"`} {
			for _, suffix := range []string{"", `,"data":{"token":"PRIVATE_DATA\n\t\u0000","nested":[false,null,7]}`} {
				r := authStatusTestRouter(t)
				sent := client(t, r, `{"id":"native-error","method":"getAuthStatus","params":{"includeToken":true}}`)[0]
				m, _ := parseObject(sent.Message)
				line := `{"id":` + string(m["id"]) + `,"error":{"code":` + code + `,"message":` + message + suffix + "}}"
				authStatusTestError(t, server(t, r, "openai", line), code)
			}
		}
	}
}

func TestAuthStatus167MalformedErrorAndEnvelopeNeverSucceed(t *testing.T) {
	bodies := []string{
		`"error":null`, `"error":[]`, `"error":{}`,
		`"error":{"code":null,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":1.0,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":1e0,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":"-32037","message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":9223372036854775808,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":-9223372036854775809,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":-32037,"message":null}`,
		`"error":{"code":-32037,"message":7}`,
		`"error":{"code":-32037}`,
		`"error":{"code":-32037,"code":-32038,"message":"PRIVATE_MESSAGE"}`,
		`"error":{"code":-32037,"message":"PRIVATE_MESSAGE","data":{"token":1,"t\u006fken":2}}`,
		`"error":{"code":-32037,"message":"\ud800"}`,
		`"error":{"code":-32037,"message":"\udc00"}`,
		`"error":{"code":-32037,"message":"\ud800\u0041"}`,
		`"error":{"code":-32037,"message":"PRIVATE_MESSAGE","extra":true}`,
		`"result":` + authStatusTestResult + `,"error":{"code":-32037,"message":"PRIVATE_MESSAGE"}`,
		`"result":` + authStatusTestResult + `,"jsonrpc":"2.0"`,
		`"result":` + authStatusTestResult + `,"trace":null`,
		`"result":` + authStatusTestResult + `,"result":` + authStatusTestResult,
		`"error":{"code":-32037,"message":"PRIVATE_MESSAGE","data":` + strings.Repeat("[", 13) + "null" + strings.Repeat("]", 13) + "}",
		`"error":{"code":-32037,"message":"PRIVATE_MESSAGE","data":[` + strings.Repeat("0,", 256) + "0]}",
		`"error":{"code":-32037,"message":"` + strings.Repeat("a", 64<<10) + `"}`,
		`"error":{"code":-32037,"message":"` + string([]byte{0xff}) + `"}`,
	}
	for i, body := range bodies {
		r := authStatusTestRouter(t)
		sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":{}}`)[0]
		m, _ := parseObject(sent.Message)
		out, err := r.Server("openai", []byte(`{"id":`+string(m["id"])+","+body+"}"))
		if err == nil {
			authStatusTestError(t, out, "-32000")
		} else if len(out) != 0 || strings.Contains(err.Error(), "PRIVATE_") {
			t.Fatalf("malformed response %d leaked source data", i)
		}
	}
}

func TestAuthStatus167DuplicateServerIDsCannotSelectOtherMethod(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, alias := range []string{`"id"`, `"\u0069d"`} {
			r := authStatusTestRouter(t)
			auth := client(t, r, `{"id":"auth","method":"getAuthStatus","params":{"includeToken":true}}`)[0]
			account := client(t, r, `{"id":"account","method":"account/read","params":{"refreshToken":false}}`)[0]
			a, _ := parseObject(auth.Message)
			b, _ := parseObject(account.Message)
			first, second := a["id"], b["id"]
			if reverse {
				first, second = second, first
			}
			line := fmt.Sprintf(`{"id":%s,%s:%s,"result":{"authMethod":null,"authToken":"PRIVATE_TOKEN","requiresOpenaiAuth":false}}`, first, alias, second)
			out, err := r.Server("openai", []byte(line))
			if err == nil || len(out) != 0 || len(r.pending) != 2 || len(r.active) != 2 || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("duplicate raw response IDs bypassed the no-auth result gate")
			}
		}
	}
}

func TestAuthStatus167WrongBackendAndNativeIDCollisions(t *testing.T) {
	r := authStatusTestRouter(t)
	sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":{}}`)[0]
	m, _ := parseObject(sent.Message)
	if out, err := r.Server("aihub", raw(object{"id": m["id"], "result": json.RawMessage(authStatusTestResult)})); err == nil || len(out) != 0 || len(r.pending) != 1 {
		t.Fatal("secondary completed a primary auth request")
	}
	for _, firstMethod := range []string{"getAuthStatus", "account/read"} {
		for _, firstID := range []string{"-0", "0"} {
			r := authStatusTestRouter(t)
			secondID := "0"
			if firstID == "0" {
				secondID = "-0"
			}
			client(t, r, fmt.Sprintf(`{"id":%s,"method":%q,"params":{}}`, firstID, firstMethod))
			if out, err := r.Client([]byte(fmt.Sprintf(`{"id":%s,"method":"getAuthStatus","params":{}}`, secondID))); err == nil || len(out) != 0 {
				t.Fatal("native zero aliases created simultaneous auth requests")
			}
		}
	}
	r = authStatusTestRouter(t)
	client(t, r, `{"id":-0,"method":"getAuthStatus","params":{}}`)
	if out, err := r.Client([]byte(`{"id":0,"method":"account/read","params":{}}`)); err == nil || len(out) != 0 {
		t.Fatal("ordinary request reused a live native auth ID")
	}
	if out := client(t, r, `{"id":"0","method":"getAuthStatus","params":{}}`); len(out) != 1 || out[0].Backend != "openai" {
		t.Fatal("numeric and string IDs were incorrectly conflated")
	}
}

func TestAuthStatus167ModeAndReadinessRemainIndependent(t *testing.T) {
	for _, phase := range []string{"cold", "initializing", "failed"} {
		r := authStatusTestRouter(t)
		r.phase = phase
		expectError(t, client(t, r, `{"id":1,"method":"getAuthStatus","params":{}}`))
		if len(r.pending) != 0 {
			t.Fatal("auth status bypassed both-child readiness")
		}
	}
	r := testRouter(t, false)
	out := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":true}}`)
	expectError(t, out)
	m, _ := parseObject(out[0].Message)
	failure, _ := child(m, "error")
	if string(failure["code"]) != "-32601" {
		t.Fatal("default mode changed")
	}
	r = authStatusTestRouter(t)
	for _, method := range []string{"account/gatewayOAuth/read", "configRequirements/read", "config/read", "account/login/start", "account/login/cancel", "account/chatgptAuthTokens/refresh"} {
		expectError(t, client(t, r, fmt.Sprintf(`{"id":1,"method":%q,"params":{}}`, method)))
	}
	for _, capabilities := range []object{
		{"requiredHumanApproval": raw(true)}, {"requestAttestation": raw(true)}, {"explicitGatewayOauth": raw(true)},
	} {
		a := (Config{AuthStatus: AuthStatusPrimaryNoAuth167}).authCompatibility()
		if a.negotiate(capabilities) == nil {
			t.Fatal("auth status silently enabled another authentication capability")
		}
	}
	caps := object{"requiredHumanApproval": raw(false), "experimentalApi": raw(true), "optOutNotificationMethods": raw([]string{"fixture/notification"})}
	before := raw(caps)
	a := (Config{AuthStatus: AuthStatusPrimaryNoAuth167}).authCompatibility()
	if a.negotiate(caps) != nil || !bytes.Equal(before, raw(caps)) {
		t.Fatal("auth status mutated existing capability fields")
	}
}

func TestAuthStatus167ExactConfigAndVersionGates(t *testing.T) {
	base := fixtureConfig(t)
	base.AuthStatus = AuthStatusPrimaryNoAuth167
	for i := range base.Backends {
		base.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"partial":         func(c *Config) { c.AuthStatus = "primary-no-auth" },
		"future":          func(c *Config) { c.AuthStatus = "primary-no-auth-v0.167.0-alpha.3.3" },
		"old attestation": func(c *Config) { c.Attestation = AttestationUnavailable },
		"old gateway": func(c *Config) {
			c.GatewayOAuth = GatewayOAuthPrimaryRead
			c.GatewayOAuthProviderName = "OpenAI"
		},
		"old requirements": func(c *Config) { c.RequirementsRead = ConfigRequirementsCompareSafe },
		"old startup":      func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted },
		"first pin":        func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		"second pin":       func(c *Config) { c.Backends[1].StartupProbeSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			change(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("invalid no-auth mode or cross-version/pin combination accepted")
			}
		})
	}
	cfg := base
	cfg.Attestation = AttestationUnavailable167
	cfg.GatewayOAuth = GatewayOAuthPrimaryRead167
	cfg.GatewayOAuthProviderName = "OpenAI"
	cfg.RequirementsRead = ConfigRequirementsCompareNull167
	cfg.StartupProbe = StartupProbeCompareUnrestricted167
	cfg.DesktopPluginOverride = DesktopPluginCodexAppTools167
	if err := cfg.Validate(); err != nil {
		t.Fatal("independent same-version modes rejected:", err)
	}
}

func TestAuthStatus167ResponseBoundaries(t *testing.T) {
	wrap := func(data string) string {
		return `{"id":"transport","error":{"code":-32037,"message":"ok","data":` + data + "}}"
	}
	at := wrap("null")
	at += strings.Repeat(" ", (64<<10)-len(at))
	if !authStatusResponseJSON([]byte(at)) || authStatusResponseJSON([]byte(at+" ")) {
		t.Fatal("response byte boundary changed")
	}
	for _, count := range []int{256, 257} {
		array := "[" + strings.TrimSuffix(strings.Repeat("0,", count), ",") + "]"
		if authStatusResponseJSON([]byte(wrap(array))) != (count == 256) {
			t.Fatal("response container boundary changed")
		}
	}
	for _, count := range []int{10, 11} {
		data := strings.Repeat("[", count) + "null" + strings.Repeat("]", count)
		if authStatusResponseJSON([]byte(wrap(data))) != (count == 10) {
			t.Fatal("response depth boundary changed")
		}
	}
}
