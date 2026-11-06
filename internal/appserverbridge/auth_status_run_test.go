package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These peers are copied test executables with private fixture homes. They do
// not inspect accounts, credentials, official runtimes or network services.
const authStatusFixtureResult = `{ "requiresOpenaiAuth" : false, "auth\u0054oken" : null, "authMethod" : null }`

func authStatusFixture() (bool, int) {
	home := os.Getenv("HOME")
	if _, err := os.Stat(filepath.Join(home, "fixture-auth-status")); os.IsNotExist(err) {
		return false, 0
	} else if err != nil {
		return true, 51
	}
	log, err := os.OpenFile(filepath.Join(home, "fixture-auth-status-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 52
	}
	defer log.Close()
	fmt.Fprintln(os.Stderr, "auth-status-private-stderr-canary")
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request json.RawMessage
		if err := decoder.Decode(&request); err == io.EOF {
			break
		} else if err != nil {
			return true, 53
		}
		if _, err := fmt.Fprintln(log, string(request)); err != nil {
			return true, 54
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(request, &msg) != nil {
			return true, 55
		}
		var method string
		if json.Unmarshal(msg["method"], &method) != nil {
			return true, 56
		}
		var body []byte
		switch method {
		case "initialize":
			body = []byte(`"result":{"userAgent":"auth-status-fixture","platformFamily":"fixture","platformOs":"fixture"}`)
		case "initialized":
			continue
		case "getAuthStatus":
			body, err = os.ReadFile(filepath.Join(home, "fixture-auth-status-response"))
			if err != nil {
				return true, 57
			}
		default:
			return true, 58
		}
		if _, err := fmt.Fprintf(os.Stdout, "{\"id\":%s,%s}\n", msg["id"], body); err != nil {
			return true, 59
		}
	}
	if os.WriteFile(filepath.Join(home, "fixture-auth-status-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 60
	}
	return true, 0
}

// JSON injection deliberately uses only APIs present at the previous base.
// Copying these test files and their fixture dispatch hooks to that base gives
// an assertion-level red rather than an undefined-field compile failure.
func authStatusRunConfig(t *testing.T, enabled bool) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	if enabled {
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatal(err)
		}
		settings["auth_status"] = json.RawMessage(`"primary-no-auth-v0.167.0-alpha.3.2"`)
		data, err = json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, backend := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(backend.Home, "fixture-auth-status"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backend.Home, "fixture-auth-status-response"), []byte(`"result":`+authStatusFixtureResult), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func authStatusRunCompact(t *testing.T, data string) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(data)); err != nil {
		t.Fatal(err)
	}
	return compact.String()
}

func authStatusRunInitialize(t *testing.T, bridge *runningBridge, stream *requirementsStream) {
	t.Helper()
	response := stream.ask(t, bridge, `{"id":"init","method":"initialize","params":{"clientInfo":{"name":"auth-status-test","version":"1"},"capabilities":{}}}`)
	if string(response["id"]) != `"init"` || !exists(response, "result") || exists(response, "error") {
		t.Fatal("no-auth-only fixture did not initialize")
	}
	// Native readiness does not depend on an initialized notification.
}

func authStatusRunError(t *testing.T, response object, code int) {
	t.Helper()
	failure, err := child(response, "error")
	if err != nil || exists(response, "result") || len(failure) != 2 || string(failure["code"]) != fmt.Sprint(code) {
		t.Fatalf("no-auth response was not the expected bounded error: %s", raw(response))
	}
	if code != -32601 && code != -32602 && text(failure, "message") != "primary no-auth status unavailable" {
		t.Fatal("source no-auth error message was exposed or changed")
	}
}

func authStatusRunFinish(t *testing.T, cfg Config, bridge *runningBridge, stream *requirementsStream, params []string, responses int) {
	t.Helper()
	if err := bridge.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != responses || strings.Contains(string(stream.transcript), "private-") {
		t.Fatalf("unexpected no-auth client transcript: %q", stream.transcript)
	}
	for i, backend := range cfg.Backends {
		log, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-status-requests"))
		if err != nil {
			t.Fatal(err)
		}
		reads, initializations := 0, 0
		for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			msg, err := parseObject([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			switch text(msg, "method") {
			case "initialize":
				initializations++
				p, err := child(msg, "params")
				if err != nil || string(p["capabilities"]) != "{}" {
					t.Fatal("auth status silently enabled an authentication capability")
				}
			case "getAuthStatus":
				if i != 0 || reads >= len(params) {
					t.Fatal("auth status fanned out, retried, or fell back to the secondary")
				}
				if len(msg) != 3 || exists(msg, "trace") || string(msg["params"]) != authStatusRunCompact(t, params[reads]) {
					t.Fatal("primary params changed or trace was forwarded")
				}
				reads++
			default:
				t.Fatalf("unexpected child method: %s", text(msg, "method"))
			}
		}
		wantReads := 0
		if i == 0 {
			wantReads = len(params)
		}
		if initializations != 1 || reads != wantReads {
			t.Fatalf("backend %s initialization=%d auth reads=%d, want 1/%d", backend.ID, initializations, reads, wantReads)
		}
		exit, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-status-clean-exit"))
		if err != nil || string(exit) != "EOF" {
			t.Fatal("no-auth child did not exit cleanly on client EOF")
		}
	}
}

func TestRunAuthStatusPrimaryOnlyPreservesIncludeTokenAndSourceBytes(t *testing.T) {
	cfg := authStatusRunConfig(t, true)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	cases := []struct {
		id     string
		params string
		trace  string
	}{
		{`"empty"`, `{}`, ""},
		{"9007199254740993", `{ "includeToken" : true, "refreshToken" : false }`, `,"trace":null`},
		{"-9223372036854775808", `{"refreshToken":null,"include\u0054oken":true}`, `,"trace":{"traceparent":"private-trace-canary","tracestate":null}`},
		{`"null-flags"`, `{"includeToken":null,"refreshToken":null}`, ""},
		{`"false-flags"`, `{"includeToken":false,"refreshToken":false}`, ""},
	}
	var params []string
	for _, tc := range cases {
		response := stream.ask(t, bridge, fmt.Sprintf(`{"id":%s,"method":"getAuthStatus","params":%s%s}`, tc.id, tc.params, tc.trace))
		if string(response["id"]) != tc.id || exists(response, "error") || string(response["result"]) != authStatusRunCompact(t, authStatusFixtureResult) {
			t.Fatal("primary native no-auth result, source bytes, or request ID changed")
		}
		params = append(params, tc.params)
	}
	invalid := []string{
		`{"id":"invalid","method":"getAuthStatus"}`,
		`{"id":"invalid","method":"getAuthStatus","params":null}`,
		`{"id":"invalid","method":"getAuthStatus","params":{"refreshToken":true}}`,
		`{"id":"invalid","method":"getAuthStatus","params":{"includeToken":"true"}}`,
		`{"id":"invalid","method":"getAuthStatus","params":{"unknown":false}}`,
		`{"id":"invalid","method":"getAuthStatus","params":{},"trace":{"unknown":"private-trace-canary"}}`,
	}
	for _, request := range invalid {
		response := stream.ask(t, bridge, request)
		if string(response["id"]) != `"invalid"` {
			t.Fatal("invalid no-auth request lost its ID")
		}
		authStatusRunError(t, response, -32602)
	}
	authStatusRunFinish(t, cfg, bridge, stream, params, 1+len(cases)+len(invalid))
}

func TestRunAuthStatusRejectsTokenCanariesAndPreservesNativeErrorCode(t *testing.T) {
	cfg := authStatusRunConfig(t, true)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	cases := []struct {
		name string
		body string
		code int
	}{
		{"token", `"result":{"authMethod":null,"authToken":"private-token-canary","requiresOpenaiAuth":false}`, -32000},
		{"method", `"result":{"authMethod":"apikey","authToken":null,"requiresOpenaiAuth":false}`, -32000},
		{"required", `"result":{"authMethod":null,"authToken":null,"requiresOpenaiAuth":true}`, -32000},
		{"missing", `"result":{"authMethod":null,"requiresOpenaiAuth":false}`, -32000},
		{"unknown", `"result":{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false,"extra":"private-extra-canary"}`, -32000},
		{"duplicate", `"result":{"authMethod":null,"authToken":"private-token-canary","auth\u0054oken":null,"requiresOpenaiAuth":false}`, -32000},
		{"native-error", `"error":{"code":-32037,"message":"private-error-canary","data":{"token":"private-data-canary"}}`, -32037},
		{"malformed-error", `"error":{"code":null,"message":"private-error-canary"}`, -32000},
	}
	var params []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-status-response"), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			response := stream.ask(t, bridge, `{"id":"unsafe","method":"getAuthStatus","params":{"includeToken":true}}`)
			if string(response["id"]) != `"unsafe"` {
				t.Fatal("unsafe primary response lost correlation")
			}
			authStatusRunError(t, response, tc.code)
		})
		params = append(params, `{"includeToken":true}`)
	}
	// A later success must come from a fresh primary response, not a cached
	// earlier success or the always-safe response installed in the secondary.
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-status-response"), []byte(`"result":`+authStatusFixtureResult), 0600); err != nil {
		t.Fatal(err)
	}
	response := stream.ask(t, bridge, `{"id":"recovery","method":"getAuthStatus","params":{}}`)
	if exists(response, "error") || string(response["result"]) != authStatusRunCompact(t, authStatusFixtureResult) {
		t.Fatal("fresh safe primary response did not recover")
	}
	params = append(params, "{}")
	authStatusRunFinish(t, cfg, bridge, stream, params, 2+len(cases))
}

func TestRunAuthStatusAloneChecksBothPinsBeforeEitherChild(t *testing.T) {
	for _, index := range []int{0, 1} {
		for _, pin := range []string{"", strings.Repeat("0", 64)} {
			t.Run(fmt.Sprintf("backend-%d-missing-%t", index, pin == ""), func(t *testing.T) {
				cfg := authStatusRunConfig(t, true)
				cfg.Backends[index].StartupProbeSHA256 = pin
				var output discardRequirementsOutput
				if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
					t.Fatal("auth-status-only mode accepted an unpinned backend")
				}
				for _, backend := range cfg.Backends {
					for _, marker := range []string{"fixture-report", "fixture-auth-status-requests", "fixture-auth-status-clean-exit"} {
						if _, err := os.Stat(filepath.Join(backend.Home, marker)); !os.IsNotExist(err) {
							t.Fatal("a child started before both auth-status pins passed")
						}
					}
				}
			})
		}
	}
}

func TestRunAuthStatusDefaultRemainsUnsupported(t *testing.T) {
	cfg := authStatusRunConfig(t, false)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	response := stream.ask(t, bridge, `{"id":"disabled","method":"getAuthStatus","params":{"includeToken":true}}`)
	authStatusRunError(t, response, -32601)
	authStatusRunFinish(t, cfg, bridge, stream, nil, 2)
}
