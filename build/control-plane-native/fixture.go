package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// These three SSE event shapes are shared with the working official-runtime
// fixture. They contain assistant text only, never a tool/command or proof.
// Source: build/test-official-runtime.py Fixture.events; exact native source
// c40729ea5cf8e947481b130c59807273eda2f8e9, protocol/models.rs.
type fixture struct {
	server              *httptest.Server
	name                string
	model, instructions string
	mu                  sync.Mutex
	requests, failures  int
}

func newFixture(name string) *fixture {
	return newFixtureWithModel(name, modelName, "")
}

func newFixtureWithModel(name, model, instructions string) *fixture {
	f := &fixture{name: name, model: model, instructions: instructions}
	s := httptest.NewUnstartedServer(http.HandlerFunc(f.serveHTTP))
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.ReadTimeout = 10 * time.Second
	s.Config.WriteTimeout = 10 * time.Second
	s.Config.IdleTimeout = 10 * time.Second
	s.Config.MaxHeaderBytes = 32 << 10
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.Start()
	f.server = s
	return f
}

func loopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback() && u.Port() != ""
}

func (f *fixture) fail(w http.ResponseWriter) {
	f.mu.Lock()
	f.failures++
	f.mu.Unlock()
	http.Error(w, "synthetic fixture rejected request", http.StatusBadRequest)
}

func (f *fixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.URL.RawQuery != "" || r.ContentLength > maxMessage ||
		r.Header.Get("Upgrade") != "" || r.Header.Get("Content-Encoding") != "" {
		f.fail(w)
		return
	}
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key", "X-Oai-Attestation"} {
		if r.Header.Get(key) != "" {
			f.fail(w)
			return
		}
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxMessage+1))
	if err != nil || len(data) > maxMessage {
		f.fail(w)
		return
	}
	var req map[string]json.RawMessage
	if json.Unmarshal(data, &req) != nil || req == nil || stringField(req, "model") != f.model {
		f.fail(w)
		return
	}
	if f.instructions != "" {
		if _, err := decodeConfigJSON(data); err != nil || !selfAuthoredRequest(req, f.name) {
			f.fail(w)
			return
		}
	}
	var stream bool
	if json.Unmarshal(req["stream"], &stream) != nil || !stream {
		f.fail(w)
		return
	}
	other := "alpha"
	if f.name == "alpha" {
		other = "beta"
	}
	if bytes.Contains(data, []byte("synthetic-"+other+"-turn-")) {
		f.fail(w)
		return
	}
	f.mu.Lock()
	n := f.requests + 1
	if n > 2 || !bytes.Contains(req["input"], []byte(fmt.Sprintf("synthetic-%s-turn-%d", f.name, n))) {
		f.failures++
		f.mu.Unlock()
		http.Error(w, "synthetic fixture rejected request", http.StatusBadRequest)
		return
	}
	f.requests = n
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	for _, event := range responseEvents(f.name, n) {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonBytes(event)); err != nil {
			f.mu.Lock()
			f.failures++
			f.mu.Unlock()
			return
		}
	}
}

func responseEvents(name string, serial int) []any {
	id := fmt.Sprintf("%s-response-%d", name, serial)
	return []any{
		map[string]any{"type": "response.created", "response": map[string]any{"id": id}},
		map[string]any{"type": "response.output_item.done", "item": map[string]any{
			"type": "message", "role": "assistant", "id": fmt.Sprintf("%s-message-%d", name, serial),
			"content": []any{map[string]any{"type": "output_text", "text": "fixture-answer-" + name}},
		}},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"id": id, "usage": map[string]any{"input_tokens": 0, "input_tokens_details": nil,
				"output_tokens": 0, "output_tokens_details": nil, "total_tokens": 0},
		}},
	}
}

func (f *fixture) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests, f.failures
}
func (f *fixture) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = f.server.Config.Shutdown(ctx)
	f.server.CloseClientConnections()
	f.server.Close()
}

func nativeConfig(provider, endpoint, catalog string) ([]byte, error) {
	return nativeConfigForModel(provider, endpoint, catalog, modelName)
}

func nativeConfigForModel(provider, endpoint, catalog, model string) ([]byte, error) {
	if (provider != "alpha_provider" && provider != "beta_provider") || !loopbackURL(endpoint) {
		return nil, failure("fixture_invalid")
	}
	if model != modelName && model != "test-"+strings.TrimSuffix(provider, "_provider") {
		return nil, failure("fixture_invalid")
	}
	// JSON string quoting is valid for these TOML basic strings, including
	// Windows paths. No env_key, credentials, commands, hooks or MCP are set.
	text := fmt.Sprintf(
		"model = %s\nmodel_provider = %s\nmodel_catalog_json = %s\n"+
			"cli_auth_credentials_store = \"file\"\napproval_policy = \"never\"\nsandbox_mode = \"read-only\"\n"+
			"chatgpt_base_url = %s\ncheck_for_update_on_startup = false\n"+
			"[analytics]\nenabled = false\n[cloud.skills]\nenabled = false\n"+
			"[features]\napps = false\nplugins = false\n"+
			"[model_providers.%s]\nname = %s\nbase_url = %s\nwire_api = \"responses\"\n"+
			"request_max_retries = 0\nstream_max_retries = 0\nstream_idle_timeout_ms = 15000\n"+
			"requires_openai_auth = false\nsupports_websockets = false\n",
		jsonBytes(model), jsonBytes(provider), jsonBytes(catalog),
		jsonBytes(endpoint+"/backend-api"), provider, jsonBytes("Synthetic "+provider), jsonBytes(endpoint+"/v1"))
	if strings.Contains(text, "auth.json") {
		return nil, failure("fixture_invalid")
	}
	return []byte(text), nil
}
