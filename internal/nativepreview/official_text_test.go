package nativepreview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Explicit CI opt-in only. This uses an existing, independently verified official
// executable and a new own context; it never downloads native, logs in, sets up a
// sandbox or imports user authentication; its inference destination is loopback.
// Native startup itself is not asserted to be zero-write or zero-network.
func TestOfficialNativePreviewText(t *testing.T) {
	if os.Getenv("MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST") != "1" {
		t.Skip("official runtime loopback check is opt-in")
	}
	stage := "test-setup"
	passed := false
	var fixture *previewTextFixture
	defer func() {
		class := "failed-no-request"
		if fixture != nil {
			fixture.mu.Lock()
			if fixture.requests > 0 {
				class = "failed-request-seen"
			}
			if fixture.failures > 0 {
				class = "failed-fixture-rejected"
			}
			fixture.mu.Unlock()
		}
		if passed {
			class = "passed"
		}
		if !previewDiagnosticStageAllowed(stage) {
			stage = "unclassified"
		}
		fmt.Printf("native-preview-diagnostic stage=%s class=%s\n", stage, class)
	}()
	if !officialPreviewTransportReady {
		stage = "test-transport"
		t.Fatal("official test transport preservation unavailable")
	}
	command := os.Getenv("MAGPIE_PREVIEW_OFFICIAL_NATIVE")
	if command == "" || !filepath.IsAbs(command) {
		t.Fatal("official native test input unavailable")
	}
	c, err := New()
	if err != nil {
		t.Fatal("fresh native context unavailable")
	}
	defer func() { _ = c.Close(); _ = os.RemoveAll(c.root) }()
	if c.SelectExecutable(command) != nil {
		t.Fatal("official native selection unavailable")
	}
	catalog, err := previewOriginalCatalog("alpha")
	if err != nil {
		t.Fatal("original catalog unavailable")
	}
	catalogPath := filepath.Join(c.root, "self-authored-catalog.json")
	if os.WriteFile(catalogPath, catalog, 0600) != nil {
		t.Fatal("original catalog write failed")
	}
	// No permission, sandbox, credential-store or managed-policy override.
	config := "model = \"test-alpha\"\nmodel_catalog_json = " + strconv.Quote(catalogPath) + "\ncheck_for_update_on_startup = false\n[analytics]\nenabled = false\n"
	home, codexHome := filepath.Join(c.root, "aihub", "home"), filepath.Join(c.root, "aihub", "codex")
	if os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0600) != nil {
		t.Fatal("own native config write failed")
	}
	fixture = &previewTextFixture{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(fixture.serveHTTP))
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.ReadTimeout = 10 * time.Second
	server.Config.WriteTimeout = 10 * time.Second
	server.Config.MaxHeaderBytes = 32 << 10
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Start()
	defer server.Close()
	spec := peerSpec{command: c.executable, digest: c.digest, home: home, codexHome: codexHome, provider: "aihub", endpoint: server.URL + "/v1", key: previewPublicKey, only167: true}
	spec.stage = func(label string) { stage = label }
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	// This is the exact production single-text native protocol path. HTTP is
	// only this loopback test spec; ConfigureAIHub remains HTTPS-only.
	reply, err := sendText(ctx, spec, "test-alpha", previewPublicPrompt)
	fixture.mu.Lock()
	requests, failures := fixture.requests, fixture.failures
	fixture.mu.Unlock()
	if err != nil || reply.Text != "fixture-answer-alpha" || requests != 1 || failures != 0 {
		t.Fatal("official native loopback text did not complete")
	}
	passed = true
	stage = "done"
	t.Log("official-native-loopback-text: passed; synthetic service only")
}

const previewPublicKey = "MAGPIE-PUBLIC-LOOPBACK-CANARY"
const previewPublicPrompt = "synthetic-alpha-short-text"
const syntheticReasoningDescription = "Synthetic fixed response without reasoning."

// Reused verbatim in substance from build/control-plane-native/synthetic_models.go:
// records written for this local fixture, not copied from any upstream catalog.
func previewOriginalCatalog(name string) ([]byte, error) {
	priority := 0
	switch name {
	case "alpha":
	case "beta":
		priority = 1
	default:
		return nil, ErrUnavailable
	}
	model := map[string]any{
		"slug": "test-" + name, "display_name": syntheticDisplayName(name),
		"description":             "Local fixed-text protocol fixture; not a hosted model.",
		"default_reasoning_level": "none",
		"supported_reasoning_levels": []any{map[string]any{
			"effort": "none", "description": syntheticReasoningDescription,
		}},
		"shell_type": "disabled", "visibility": "list", "supported_in_api": true,
		"priority": priority, "support_verbosity": false, "default_verbosity": nil,
		"truncation_policy":            map[string]any{"mode": "tokens", "limit": 1024},
		"experimental_supported_tools": []any{}, "input_modalities": []string{"text"},
		"include_apps_usage_instructions": false, "include_skills_usage_instructions": false,
		"include_plugin_usage_instructions": false, "supports_reasoning_summary_parameter": false,
		"default_reasoning_summary": "none", "node_repl_disabled": true,
		"supports_search_tool": false, "apply_patch_tool_type": nil,
		"upgrade": nil, "availability_nux": nil, "additional_speed_tiers": []any{},
		"service_tiers":  []any{},
		"model_messages": map[string]any{"instructions_template": syntheticInstructions(name)},
	}
	// Omit guardian: an empty policy map would change approval behavior.
	// This test makes no sandbox or approval-policy override.
	return json.Marshal(map[string]any{"models": []any{model}})
}

func syntheticDisplayName(name string) string {
	if name == "alpha" {
		return "Synthetic Alpha"
	}
	return "Synthetic Beta"
}

func syntheticInstructions(name string) string {
	return "Return the fixed " + name + " synthetic test response."
}

type previewTextFixture struct {
	mu                 sync.Mutex
	requests, failures int
}

func (f *previewTextFixture) reject(w http.ResponseWriter) {
	f.mu.Lock()
	f.failures++
	f.mu.Unlock()
	http.Error(w, "synthetic fixture rejected request", http.StatusBadRequest)
}
func (f *previewTextFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/v1/responses" || r.URL.RawQuery != "" || r.ContentLength > 1<<20 ||
		len(r.Header.Values("Authorization")) != 1 || r.Header.Values("Authorization")[0] != "Bearer "+previewPublicKey || r.Header.Get("Upgrade") != "" || r.Header.Get("Content-Encoding") != "" {
		f.reject(w)
		return
	}
	for _, name := range []string{"Proxy-Authorization", "Cookie", "X-Api-Key", "X-Oai-Attestation", "ChatGPT-Account-Id"} {
		if len(r.Header.Values(name)) > 0 {
			f.reject(w)
			return
		}
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || !strictJSON(data) {
		f.reject(w)
		return
	}
	var request struct {
		Model  string            `json:"model"`
		Stream bool              `json:"stream"`
		Input  []json.RawMessage `json:"input"`
	}
	if json.Unmarshal(data, &request) != nil || request.Model != "test-alpha" || !request.Stream || len(request.Input) == 0 {
		f.reject(w)
		return
	}
	found := false
	for _, raw := range request.Input {
		var item struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil || (item.Type != "" && item.Type != "message") || len(item.Content) == 0 {
			f.reject(w)
			return
		}
		if item.Role != "user" && item.Role != "system" && item.Role != "developer" && item.Role != "assistant" {
			f.reject(w)
			return
		}
		for _, part := range item.Content {
			if part.Type != "input_text" && part.Type != "output_text" {
				f.reject(w)
				return
			}
			if item.Role == "user" && strings.Contains(part.Text, previewPublicPrompt) {
				found = true
			}
		}
	}
	if !found || bytes.Contains(data, []byte(previewPublicKey)) {
		f.reject(w)
		return
	}
	f.mu.Lock()
	f.requests++
	duplicate := f.requests != 1
	f.mu.Unlock()
	if duplicate {
		f.reject(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Exercise actual streaming text events, not only an output_item.done response.
	// The native app-server must translate them to bound item deltas; final text
	// remains authoritative only after completion. No tool events are emitted.
	events := []any{
		map[string]any{"type": "response.created", "response": map[string]any{"id": "preview-response"}},
		map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "role": "assistant", "id": "preview-message", "status": "in_progress", "content": []any{}}},
		map[string]any{"type": "response.content_part.added", "item_id": "preview-message", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": ""}},
		map[string]any{"type": "response.output_text.delta", "item_id": "preview-message", "output_index": 0, "content_index": 0, "delta": "fixture-answer-"},
		map[string]any{"type": "response.output_text.delta", "item_id": "preview-message", "output_index": 0, "content_index": 0, "delta": "alpha"},
		map[string]any{"type": "response.output_text.done", "item_id": "preview-message", "output_index": 0, "content_index": 0, "text": "fixture-answer-alpha"},
		map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "message", "role": "assistant", "id": "preview-message", "content": []any{map[string]any{"type": "output_text", "text": "fixture-answer-alpha"}}}},
		map[string]any{"type": "response.completed", "response": map[string]any{"id": "preview-response", "usage": map[string]any{"input_tokens": 0, "input_tokens_details": nil, "output_tokens": 0, "output_tokens_details": nil, "total_tokens": 0}}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			f.mu.Lock()
			f.failures++
			f.mu.Unlock()
			return
		}
	}
}

func previewDiagnosticStageAllowed(label string) bool {
	switch label {
	case "notification-envelope", "config-rpc-write", "config-rpc-transport", "config-rpc-canceled", "config-rpc-eof", "config-rpc-envelope", "config-rpc-id", "notification-configWarning", "notification-warning", "notification-account-updated", "config-rpc-other", "config-rpc-invalid-params", "config-rpc-invalid-request", "config-rpc-method-not-found", "config-rpc-internal", "config-rpc-server", "test-isolation", "test-transport", "test-setup", "unclassified", "admission", "version", "start", "initialize", "initialize-receipt", "config-request", "config-provider", "config-mcp", "config-hooks-notify", "models", "model-selection", "thread-request", "thread-receipt", "turn-request", "turn-receipt", "turn-stream", "reply", "close", "cleanup", "done", "notification-unsupported", "notification-thread-started", "notification-thread-settings-updated", "notification-thread-status-changed", "notification-thread-tokenUsage-updated", "notification-turn-started", "notification-turn-completed", "notification-item-started", "notification-item-completed":
		return true
	}
	for _, method := range publicNotificationMethods {
		if label == notificationDiagnosticLabel(method) {
			return true
		}
	}
	for path := range textOverrides() {
		if label == "config-"+strings.ReplaceAll(path, ".", "-") {
			return true
		}
	}
	return false
}
