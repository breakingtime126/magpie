package appserverbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const trustedStatusMode = "primary-trusted-stdio-v0.167.0-alpha.3.2"
const publicStatusToken = "NOT-A-CREDENTIAL-PUBLIC-STDIO-CANARY"

func trustedStatusRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.auth = (Config{AuthStatus: trustedStatusMode}).authCompatibility()
	return r
}

func TestTrustedStatusPreservesFlagsAndRelaysOnlyPrimary(t *testing.T) {
	for _, include := range []string{"", "null", "false", "true"} {
		for _, refresh := range []string{"", "null", "false", "true"} {
			params := object{}
			if include != "" {
				params["includeToken"] = json.RawMessage(include)
			}
			if refresh != "" {
				params["refreshToken"] = json.RawMessage(refresh)
			}
			r := trustedStatusRouter(t)
			out := client(t, r, `{"id":"original","method":"getAuthStatus","params":`+string(raw(params))+`,"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
			if len(out) != 1 || out[0].Backend != "openai" || len(r.pending) != 1 {
				t.Fatal("trusted status was not a single primary request")
			}
			sent, _ := parseObject(out[0].Message)
			if string(sent["params"]) != string(raw(params)) || exists(sent, "trace") {
				t.Fatal("native flags changed or trace was forwarded")
			}
			token := "null"
			if include == "true" {
				token = string(raw(publicStatusToken))
			}
			result := `{"authMethod":"chatgpt","authToken":` + token + `,"requiresOpenaiAuth":true}`
			response := reply(t, r, out[0], result)
			if len(response) != 1 || response[0].Backend != "" {
				t.Fatal("native auth response was sent to a backend")
			}
			got, _ := parseObject(response[0].Message)
			if string(got["id"]) != `"original"` || string(got["result"]) != result || len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("source result or completed request bookkeeping changed")
			}
		}
	}
}

func TestTrustedStatusKnownMetadataAndStrictExportSubset(t *testing.T) {
	methods := []string{"null", `"apikey"`, `"chatgpt"`, `"chatgptAuthTokens"`, `"headers"`, `"agentIdentity"`, `"personalAccessToken"`, `"bedrockApiKey"`, `"bedrockAccessKeys"`}
	for _, method := range methods {
		for _, token := range []string{"null", string(raw(publicStatusToken))} {
			r := trustedStatusRouter(t)
			sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":true}}`)[0]
			result := `{"authMethod":` + method + `,"authToken":` + token + `,"requiresOpenaiAuth":true}`
			out := reply(t, r, sent, result)
			got, _ := parseObject(out[0].Message)
			allowed := token == "null" || method == `"apikey"` || method == `"chatgpt"`
			if exists(got, "result") != allowed || (allowed && string(got["result"]) != result) {
				t.Fatal("native metadata or conservative export contract changed")
			}
			if !allowed && strings.Contains(string(out[0].Message), publicStatusToken) {
				t.Fatal("rejected token was relayed")
			}
		}
	}
	for _, params := range []string{`{}`, `{"includeToken":null}`, `{"includeToken":false}`} {
		r := trustedStatusRouter(t)
		sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":`+params+`}`)[0]
		out := reply(t, r, sent, `{"authMethod":"apikey","authToken":"`+publicStatusToken+`","requiresOpenaiAuth":true}`)
		expectError(t, out)
		if strings.Contains(string(out[0].Message), publicStatusToken) {
			t.Fatal("unsolicited token was relayed")
		}
	}
}

func TestTrustedStatusRejectsMalformedResultsAndSanitizesNativeErrors(t *testing.T) {
	for _, result := range []string{
		`{"authMethod":"future","authToken":null,"requiresOpenaiAuth":true}`,
		`{"authMethod":"apikey","authToken":"","requiresOpenaiAuth":true}`,
		`{"authMethod":"apikey","authToken":4,"requiresOpenaiAuth":true}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":null}`,
		`{"authMethod":"apikey","authToken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false,"extra":"PUBLIC_EXTRA"}`,
		`{"authMethod":null,"authToken":null}`,
		`{"authMethod":null,"authToken":null,"authToken":null,"requiresOpenaiAuth":false}`,
	} {
		r := trustedStatusRouter(t)
		sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":true}}`)[0]
		expectError(t, reply(t, r, sent, result))
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("rejected status retained its active budget")
		}
	}
	r := trustedStatusRouter(t)
	sent := client(t, r, `{"id":"error-id","method":"getAuthStatus","params":{}}`)[0]
	request, _ := parseObject(sent.Message)
	out := server(t, r, "openai", `{"id":`+string(request["id"])+`,"error":{"code":-32037,"message":"PUBLIC_ERROR_CANARY","data":{"token":"`+publicStatusToken+`"}}}`)
	got, _ := parseObject(out[0].Message)
	failure, _ := child(got, "error")
	if len(failure) != 2 || string(failure["code"]) != "-32037" || text(failure, "message") != "primary trusted stdio auth status unavailable" ||
		strings.Contains(string(out[0].Message), "CANARY") || string(got["id"]) != `"error-id"` {
		t.Fatal("native error code/ID changed or text/data escaped")
	}
}

func TestTrustedStatusDoesNotExpandOtherAuthenticationPaths(t *testing.T) {
	r := trustedStatusRouter(t)
	for _, method := range []string{"account/login/start", "account/login/cancel", "account/logout", "account/chatgptAuthTokens/refresh", "account/gatewayOAuth/read"} {
		expectError(t, client(t, r, `{"id":1,"method":"`+method+`","params":{}}`))
	}
	for _, caps := range []object{{"requestAttestation": raw(true)}, {"explicitGatewayOauth": raw(true)}} {
		a := (Config{AuthStatus: trustedStatusMode}).authCompatibility()
		if a.negotiate(caps) == nil {
			t.Fatal("trusted status enabled another authentication capability")
		}
	}
	for _, phase := range []string{"cold", "initializing", "failed"} {
		r := trustedStatusRouter(t)
		r.phase = phase
		expectError(t, client(t, r, `{"id":1,"method":"getAuthStatus","params":{}}`))
		if len(r.pending) != 0 {
			t.Fatal("trusted status bypassed readiness")
		}
	}
	r = trustedStatusRouter(t)
	sent := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":true}}`)[0]
	request, _ := parseObject(sent.Message)
	if out, err := r.Server("aihub", raw(object{"id": request["id"], "result": raw(object{"authMethod": raw("apikey"), "authToken": raw(publicStatusToken), "requiresOpenaiAuth": raw(true)})})); err == nil || len(out) != 0 {
		t.Fatal("secondary answered a primary auth request")
	}
}

func TestTrustedStatusConfigurationIsExplicitAndPinned(t *testing.T) {
	cfg := fixtureConfig(t)
	cfg.AuthStatus = trustedStatusMode
	cfg.Backends[0].Provider = "openai"
	for i := range cfg.Backends {
		cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	if err := cfg.Validate(); err != nil || !cfg.auth167() {
		t.Fatal("explicit pinned trusted status rejected")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.AuthStatus = "primary-trusted-stdio" },
		func(c *Config) { c.AuthStatus = "primary-trusted-stdio-v0.167.0-alpha.3.3" },
		func(c *Config) { c.Backends[0].Provider = "aihub" },
		func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		func(c *Config) { c.Backends[1].StartupProbeSHA256 = "" },
		func(c *Config) { c.Attestation = AttestationUnavailable },
	} {
		changed := cfg
		changed.Backends = append([]Backend(nil), cfg.Backends...)
		mutate(&changed)
		if changed.Validate() == nil {
			t.Fatal("invalid trusted status configuration accepted")
		}
	}
}

// Existing fixture binaries only: no native CLI, login, real credential or service.
// This test uses pre-existing Config/Run APIs so old code fails at behavior/config
// acceptance rather than requiring a new exported symbol merely to compile.
func TestRunTrustedStatusRelaysPublicTokenToStdioOnly(t *testing.T) {
	cfg := authStatusRunConfig(t, false)
	cfg.AuthStatus = trustedStatusMode
	cfg.Backends[0].Provider = "openai"
	result := `{"authMethod":"chatgpt","authToken":"` + publicStatusToken + `","requiresOpenaiAuth":true}`
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-status-response"), []byte(`"result":`+result), 0600); err != nil {
		t.Fatal(err)
	}
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	params := `{"includeToken":true,"refreshToken":true}`
	response := stream.ask(t, bridge, `{"id":"relay","method":"getAuthStatus","params":`+params+`}`)
	if string(response["id"]) != `"relay"` || string(response["result"]) != result {
		t.Fatal("explicit native relay result changed")
	}
	authStatusRunFinish(t, cfg, bridge, stream, []string{params}, 2)
}

func TestTrustedStatusRetainsAccountViewAndRejectsInvalidRequests(t *testing.T) {
	r := trustedStatusRouter(t)
	out := client(t, r, `{"id":1,"method":"account/read","params":{"refreshToken":false}}`)
	if len(out) != 1 || out[0].Backend != "openai" {
		t.Fatal("existing account view stopped using primary")
	}
	notification := `{"method":"account/updated","params":{"authMode":"apikey","planType":null}}`
	if out := server(t, r, "aihub", notification); len(out) != 0 {
		t.Fatal("secondary account notification became visible")
	}
	if out := server(t, r, "openai", notification); len(out) != 1 || out[0].Backend != "" {
		t.Fatal("primary account notification stopped reaching the frontend")
	}
	for _, params := range []string{`null`, `[]`, `{"includeToken":1}`, `{"refreshToken":"true"}`, `{"extra":true}`, `{"includeToken":true,"includeToken":false}`} {
		r := trustedStatusRouter(t)
		expectError(t, client(t, r, `{"id":1,"method":"getAuthStatus","params":`+params+`}`))
		if len(r.pending) != 0 {
			t.Fatal("invalid params reached a child")
		}
	}
}

func TestTrustedStatusUsesNativeIntegerIDIdentity(t *testing.T) {
	for _, first := range []string{"getAuthStatus", "account/read"} {
		r := trustedStatusRouter(t)
		client(t, r, `{"id":-0,"method":"`+first+`","params":{}}`)
		if out, err := r.Client([]byte(`{"id":0,"method":"getAuthStatus","params":{}}`)); err == nil || len(out) != 0 {
			t.Fatal("native-equivalent active ID was accepted")
		}
	}
	r := trustedStatusRouter(t)
	client(t, r, `{"id":-0,"method":"getAuthStatus","params":{}}`)
	if out, err := r.Client([]byte(`{"id":0,"method":"account/read","params":{}}`)); err == nil || len(out) != 0 {
		t.Fatal("ordinary request collided with trusted auth ID")
	}
}
