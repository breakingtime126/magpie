package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "app-server" && os.Getenv("MAGPIE_NATIVE_FIXTURE") != "" {
		if os.Getenv("MAGPIE_NATIVE_FIXTURE") == "rpc" {
			_, _ = io.WriteString(os.Stdout, "{\"id\":7,\"method\":\"unreviewed/nativeRpc\",\"params\":{}}\n")
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
		}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var m message
			if json.Unmarshal(scanner.Bytes(), &m) != nil {
				os.Exit(2)
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": m.ID, "result": map[string]any{"ok": true}})
		}
		os.Exit(0)
	}
	testenv.Main(m)
}

func TestEnvironmentRejectsUnknownAndAuthentication(t *testing.T) {
	if err := checkEnvironment([]string{"HOME=synthetic", "SystemRoot=synthetic", "PATH=synthetic"}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"OPENAI_API_KEY=x", "CODEX_AUTH_TOKEN=x", "GITHUB_TOKEN=x", "HTTPS_PROXY=x", "NODE_OPTIONS=x", "SECRET=x", "not-an-assignment"} {
		if checkEnvironment([]string{entry}) == nil {
			t.Fatal("unexpected environment accepted")
		}
	}
	if checkEnvironment([]string{"PATH=one", "Path=two"}) == nil {
		t.Fatal("duplicate case-insensitive environment accepted")
	}
}

func TestScratchMustStartEmptyAndWritesCannotReplace(t *testing.T) {
	root := t.TempDir()
	if err := emptyDirectory(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.toml")
	if err := writeNew(path, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if emptyDirectory(root) == nil {
		t.Fatal("preexisting state accepted")
	}
	if writeNew(path, []byte("replacement")) == nil {
		t.Fatal("preexisting file overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatal("existing data changed")
	}
}

func TestExactCatalogAndBinaryPinsFailClosed(t *testing.T) {
	if _, err := selectedCatalog([]byte("{\"models\":[]}")); err != failure("catalog_hash_mismatch") {
		t.Fatal("unpinned catalog accepted")
	}
	path := filepath.Join(t.TempDir(), "codex.exe")
	if err := os.WriteFile(path, []byte("not official"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinary(path); err != failure("native_hash_mismatch") {
		t.Fatal("unpinned binary accepted")
	}
}

func TestNativeConfigHasNoCredentialOrExternalEndpoint(t *testing.T) {
	config, err := nativeConfig("alpha_provider", "http://127.0.0.1:1234", filepath.Join(t.TempDir(), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"requires_openai_auth = false", "supports_websockets = false", "cli_auth_credentials_store = \"file\"", "request_max_retries = 0", "stream_max_retries = 0"} {
		if !strings.Contains(string(config), want) {
			t.Fatal("missing fixture isolation field")
		}
	}
	for _, forbidden := range []string{"api.openai.com", "chatgpt.com", "env_key", "bearer_token", "auth.json", "mcp_servers"} {
		if strings.Contains(string(config), forbidden) {
			t.Fatal("unexpected credential or external endpoint")
		}
	}
	for _, endpoint := range []string{"https://127.0.0.1:1234", "http://localhost:1234", "http://example.com:1234", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234?x=y"} {
		if _, err := nativeConfig("alpha_provider", endpoint, "synthetic"); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}

func TestSyntheticSSEIsAssistantTextOnly(t *testing.T) {
	f := newFixture("alpha")
	t.Cleanup(f.close)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	for n := 1; n <= 2; n++ {
		body := jsonBytes(map[string]any{"model": modelName, "stream": true,
			"input": []any{map[string]any{"role": "user", "content": "synthetic-alpha-turn-" + string(rune('0'+n))}}})
		response, err := client.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Contains(data, []byte("fixture-answer-alpha")) {
			t.Fatal("synthetic response failed")
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		var kinds []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var event map[string]json.RawMessage
			if !strings.HasPrefix(line, "data: ") || json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				t.Fatal("invalid SSE")
			}
			kinds = append(kinds, stringField(event, "type"))
			if stringField(event, "type") == "response.output_item.done" {
				item, err := object(event["item"])
				if err != nil || stringField(item, "type") != "message" || stringField(item, "role") != "assistant" {
					t.Fatal("non-text output")
				}
			}
		}
		if strings.Join(kinds, ",") != "response.created,response.output_item.done,response.completed" {
			t.Fatal("unexpected events")
		}
	}
	requests, failures := f.counts()
	if requests != 2 || failures != 0 {
		t.Fatal("fixture counts changed")
	}
}

func TestFixtureRejectsCredentialRoutesAndCrossProviderInput(t *testing.T) {
	for _, test := range []struct{ name, method, path, body, header string }{
		{"authorization", "POST", "/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"synthetic-alpha-turn-1"}`, "Authorization"},
		{"cookie", "POST", "/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"synthetic-alpha-turn-1"}`, "Cookie"},
		{"proof", "POST", "/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"synthetic-alpha-turn-1"}`, "X-Oai-Attestation"},
		{"other-route", "GET", "/backend-api/wham/config/bundle", "", ""},
		{"cross-provider", "POST", "/v1/responses", `{"model":"gpt-5.5","stream":true,"input":"synthetic-beta-turn-1"}`, ""},
		{"bad-model", "POST", "/v1/responses", `{"model":"other","stream":true,"input":"synthetic-alpha-turn-1"}`, ""},
		{"large", "POST", "/v1/responses", strings.Repeat("x", maxMessage+1), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture("alpha")
			defer f.close()
			client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			req, err := http.NewRequest(test.method, f.server.URL+test.path, strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if test.header != "" {
				req.Header.Set(test.header, "SYNTHETIC-REJECT")
			}
			r, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			n, failures := f.counts()
			if r.StatusCode == 200 || n != 0 || failures != 1 {
				t.Fatal("unsafe fixture request accepted")
			}
		})
	}
}

func TestProtocolStopsAtUnreviewedRPCAndTools(t *testing.T) {
	if _, err := decodeMessage([]byte(`{"id":7,"method":"attestation/generate","params":{}}`)); err != failure("unexpected_server_request") {
		t.Fatal("server RPC accepted")
	}
	for _, kind := range []string{"commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall"} {
		m := message{Method: "item/started", Params: jsonBytes(map[string]any{"item": map[string]any{"type": kind}})}
		if checkNotification(m) == nil {
			t.Fatal("tool activity accepted")
		}
	}
	if _, err := decodeMessage(bytes.Repeat([]byte("x"), maxMessage+1)); err == nil {
		t.Fatal("oversized message accepted")
	}
}

func testPeer(t *testing.T, mode string) *peer {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	env := append(childEnvironment(root, root), "MAGPIE_NATIVE_FIXTURE="+mode)
	p, err := startPeer(ctx, exe, env, root)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.close(false); cancel() })
	return p
}

func TestPeerOwnsProcessAndCleanEOF(t *testing.T) {
	p := testPeer(t, "echo")
	raw, err := p.rpc("synthetic/read", map[string]any{})
	if err != nil || string(raw) != `{"ok":true}` {
		t.Fatal("synthetic exchange failed")
	}
	if err := p.close(true); err != nil {
		t.Fatal(err)
	}
}

func TestPeerStopsUnreviewedServerRequest(t *testing.T) {
	p := testPeer(t, "rpc")
	_, err := p.wait(func(message) bool { return false })
	if err != failure("unexpected_server_request") || p.unexpected.Load() != 1 {
		t.Fatal("unreviewed RPC did not stop")
	}
}

func passedSummary() summary {
	return summary{Schema: 1, Scope: "synthetic-loopback-only", Status: "passed", Stage: "complete",
		NativeSHA256: nativeSHA, BinaryVerified: true, VersionVerified: true, ConfigReads: 9,
		LayerReads: 6, StableLayerRereads: 3, LayerVersionChanges: 0, CatalogModels: 2,
		Threads: 2, Turns: 4, AssistantMessages: 4, ThreadReads: 2, OwnershipRejections: 2,
		ThreadListQueries: 2, ThreadListPages: 4, ThreadListItems: 4,
		AlphaRequests: 2, BetaRequests: 2, AdapterCleanExit: true, CleanupComplete: true}
}
func TestSummaryNeverAcceptsWeakenedSuccess(t *testing.T) {
	if !validSummary(passedSummary()) {
		t.Fatal("valid summary rejected")
	}
	for _, change := range []func(*summary){
		func(s *summary) { s.Turns = 3 }, func(s *summary) { s.FixtureErrors = 1 },
		func(s *summary) { s.ConfigReads = 3 }, func(s *summary) { s.LayerReads = 5 },
		func(s *summary) { s.StableLayerRereads = 2 }, func(s *summary) { s.LayerVersionChanges = 1 },
		func(s *summary) { s.NativeSHA256 = "unverified" }, func(s *summary) { s.CleanupComplete = false },
		func(s *summary) { s.UnexpectedServerRequests = 1 }, func(s *summary) { s.Stage = "PRIVATE_PATH" },
		func(s *summary) { s.Failure = "PRIVATE_NATIVE_ERROR" },
		func(s *summary) { s.ThreadListQueries = 1 }, func(s *summary) { s.ThreadListPages = 3 },
		func(s *summary) { s.ThreadListItems = 0 },
	} {
		s := passedSummary()
		change(&s)
		if validSummary(s) {
			t.Fatal("weakened or private summary accepted")
		}
	}
}

func TestCatalogRequiresBothActualAliasesAndOneDefault(t *testing.T) {
	valid := `{"data":[{"model":"magpie/alpha/gpt-5.5","isDefault":true,"hidden":false},{"model":"magpie/beta/gpt-5.5","isDefault":false,"hidden":false}],"nextCursor":null}`
	def, n, err := checkCatalog([]byte(valid))
	if err != nil || n != 2 || def != "magpie/alpha/gpt-5.5" {
		t.Fatal("valid catalog rejected")
	}
	for _, raw := range []string{strings.Replace(valid, "beta", "alpha", 1), strings.Replace(valid, "\"isDefault\":false", "\"isDefault\":true", 1), strings.Replace(valid, "\"isDefault\":true", "\"isDefault\":false", 1)} {
		if _, _, err := checkCatalog([]byte(raw)); err == nil {
			t.Fatal("invalid catalog accepted")
		}
	}
}

func TestCaptureAndVersionOutputAreBounded(t *testing.T) {
	var capture boundedCapture
	if _, err := capture.Write(bytes.Repeat([]byte("x"), 16384)); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Write([]byte("SECRET-OVERFLOW")); err != nil {
		t.Fatal(err)
	}
	data, overflow := capture.bytes()
	if !overflow || len(data) != 16384 || bytes.Contains(data, []byte("SECRET")) {
		t.Fatal("capture bound failed")
	}
	var output limitedOutput
	if _, err := output.Write(bytes.Repeat([]byte("x"), 1025)); err == nil || len(output.data) != 0 {
		t.Fatal("version output bound failed")
	}
}
