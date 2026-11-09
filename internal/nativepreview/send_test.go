package nativepreview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func putFixtureConfig(config map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		child, ok := config[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			config[part] = child
		}
		config = child
	}
	config[parts[len(parts)-1]] = value
}
func textFixtureConfig(config map[string]any, mode string) {
	for path, value := range textOverrides() {
		var decoded any
		_ = json.Unmarshal([]byte(value), &decoded)
		putFixtureConfig(config, path, decoded)
	}
	config["mcp_servers"] = map[string]any{}
	config["hooks"], config["notify"] = nil, nil
	switch mode {
	case "send-policy":
		putFixtureConfig(config, "features.apps", true)
	case "send-mcp":
		config["mcp_servers"] = map[string]any{"unexpected": map[string]any{"enabled": true, "command": "not-executed"}}
	case "send-notify":
		config["notify"] = []any{"not-executed"}
	case "send-hook":
		config["hooks"] = map[string]any{"unexpected": "not-executed"}
	}
}
func fixtureProvider() string {
	if strings.Contains(strings.Join(os.Args, " "), `model_provider="magpie_preview_aihub"`) {
		return "magpie_preview_aihub"
	}
	return "openai"
}
func fixtureNotice(method string, params any) {
	data, _ := json.Marshal(map[string]any{"method": method, "params": params})
	fmt.Println(string(data))
}
func textFixtureThread(request map[string]json.RawMessage, mode, home string) any {
	var params map[string]json.RawMessage
	_ = json.Unmarshal(request["params"], &params)
	var model string
	_ = json.Unmarshal(params["model"], &model)
	environments := []any{}
	if mode == "send-environment" {
		environments = append(environments, map[string]any{"environmentId": "unexpected"})
	}
	if mode == "send-fallback" {
		model = "unexpected-model"
	}
	thread := map[string]any{"id": "owned-thread", "model": model, "modelProvider": fixtureProvider(), "ephemeral": true, "environments": environments, "parentThreadId": nil, "forkedFromId": nil}
	fixtureNotice("thread/started", map[string]any{"thread": thread})
	sources := []any{}
	if mode == "send-instructions" {
		sources = append(sources, "not-read")
	}
	return map[string]any{"thread": thread, "model": model, "modelProvider": fixtureProvider(), "cwd": home, "instructionSources": sources}
}
func textFixtureTurn(request map[string]json.RawMessage, mode, home string) any {
	turn := map[string]any{"id": "owned-turn", "status": "inProgress", "error": nil}
	fixtureNotice("turn/started", map[string]any{"threadId": "owned-thread", "turn": turn})
	if mode == "send-cancel" {
		if os.WriteFile(filepath.Join(home, "fixture-send-ready"), nil, 0600) != nil {
			os.Exit(11)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	itemType := "agentMessage"
	if mode == "send-tool" {
		itemType = "commandExecution"
	}
	thread := "owned-thread"
	if mode == "send-foreign" {
		thread = "foreign-thread"
	}
	item := map[string]any{"id": "reply-item", "type": itemType, "text": "", "phase": "final_answer"}
	fixtureNotice("item/started", map[string]any{"threadId": thread, "turnId": "owned-turn", "item": item})
	item["text"] = "Synthetic reply from " + fixtureProvider()
	fixtureNotice("item/completed", map[string]any{"threadId": thread, "turnId": "owned-turn", "item": item})
	turn["status"] = "completed"
	if mode == "send-native-error" {
		turn["status"] = "failed"
		turn["error"] = map[string]any{"message": "PUBLIC-NATIVE-ERROR-CANARY"}
	}
	fixtureNotice("turn/completed", map[string]any{"threadId": "owned-thread", "turn": turn})
	return map[string]any{"turn": turn}
}

func prepareSend(t *testing.T, provider string) *Controller {
	t.Helper()
	c := fixtureController(t)
	if provider == "aihub" && c.ConfigureAIHub("https://example.invalid/v1", "PUBLIC-AIHUB-KEY") != nil {
		t.Fatal("config")
	}
	if _, err := c.Discover(context.Background(), provider); err != nil {
		t.Fatal("discover")
	}
	return c
}
func TestShortTextUsesBoundProviderAndNewEmptyEnvironmentThread(t *testing.T) {
	for _, provider := range []string{"openai", "aihub"} {
		t.Run(provider, func(t *testing.T) {
			c := prepareSend(t, provider)
			reply, err := c.Send(context.Background(), provider, "self-authored-test", "A public short synthetic message")
			expected := provider
			if provider == "aihub" {
				expected = "magpie_preview_aihub"
			}
			if err != nil || reply.Text != "Synthetic reply from "+expected {
				t.Fatal("synthetic text result")
			}
			home := filepath.Join(c.root, provider, "home")
			requests, err := os.ReadFile(filepath.Join(home, "fixture-requests"))
			if err != nil {
				t.Fatal(err)
			}
			var methods []string
			for _, line := range strings.Split(strings.TrimSpace(string(requests)), "\n") {
				var request struct {
					Method string                     `json:"method"`
					Params map[string]json.RawMessage `json:"params"`
				}
				if json.Unmarshal([]byte(line), &request) != nil {
					t.Fatal("fixture log")
				}
				methods = append(methods, request.Method)
				if request.Method == "thread/start" {
					for _, key := range []string{"environments", "dynamicTools", "selectedCapabilityRoots"} {
						if !emptyArray(request.Params[key]) {
							t.Fatal("environment/tool root supplied")
						}
					}
					if string(request.Params["ephemeral"]) != "true" || request.Params["sandbox"] != nil || request.Params["permissions"] != nil || request.Params["approvalPolicy"] != nil {
						t.Fatal("security mode was changed")
					}
				}
			}
			if strings.Join(methods, ",") != "initialize,initialized,config/read,model/list,thread/start,turn/start" {
				t.Fatal("unexpected native request sequence")
			}
			if strings.Contains(string(requests), "PUBLIC-AIHUB-KEY") {
				t.Fatal("key entered RPC")
			}
		})
	}
}
func TestTextGuardMismatchStopsBeforeThreadOrTurn(t *testing.T) {
	for _, mode := range []string{"send-policy", "send-mcp", "send-notify", "send-hook", "send-stale", "send-environment", "send-fallback", "send-instructions"} {
		t.Run(mode, func(t *testing.T) {
			c := prepareSend(t, "openai")
			if os.WriteFile(filepath.Join(c.primaryHome, "fixture-mode"), []byte(mode), 0600) != nil {
				t.Fatal("fixture mode")
			}
			reply, err := c.Send(context.Background(), "openai", "self-authored-test", "hello")
			if err != ErrUnavailable || reply.Text != "" {
				t.Fatal("guard failed open")
			}
			data, _ := os.ReadFile(filepath.Join(c.primaryHome, "fixture-requests"))
			if strings.Contains(string(data), `"method":"turn/start"`) {
				t.Fatal("guard failure reached turn")
			}
			if (mode == "send-policy" || mode == "send-mcp" || mode == "send-notify" || mode == "send-hook" || mode == "send-stale") && strings.Contains(string(data), `"method":"thread/start"`) {
				t.Fatal("pre-thread guard failed")
			}
		})
	}
}
func TestTextRejectsForeignToolAndNativeFailureWithoutSourceError(t *testing.T) {
	for _, mode := range []string{"send-tool", "send-foreign", "send-native-error"} {
		t.Run(mode, func(t *testing.T) {
			c := prepareSend(t, "openai")
			if os.WriteFile(filepath.Join(c.primaryHome, "fixture-mode"), []byte(mode), 0600) != nil {
				t.Fatal("fixture mode")
			}
			reply, err := c.Send(context.Background(), "openai", "self-authored-test", "hello")
			if err != ErrUnavailable || reply.Text != "" || strings.Contains(err.Error(), "PUBLIC-") {
				t.Fatal("unsafe notification/result accepted")
			}
		})
	}
}
func TestTextSelectionInvalidatesOnConfigurationChanges(t *testing.T) {
	c := prepareSend(t, "openai")
	if c.ConfigureAIHub("https://example.invalid/v1", "PUBLIC-AIHUB-KEY") != nil {
		t.Fatal("config")
	}
	if _, err := c.Send(context.Background(), "aihub", "self-authored-test", "hello"); err != ErrUnavailable {
		t.Fatal("cross-provider catalog reused")
	}
	if _, err := c.Discover(context.Background(), "aihub"); err != nil {
		t.Fatal("discovery")
	}
	if c.ConfigureAIHub("https://example.invalid/v1", "PUBLIC-NEW-KEY") != nil {
		t.Fatal("config")
	}
	if _, err := c.Send(context.Background(), "aihub", "self-authored-test", "hello"); err != ErrUnavailable {
		t.Fatal("old credential catalog reused")
	}
	if c.SelectExecutable(c.executable) != nil {
		t.Fatal("select")
	}
	if c.State().CanSend {
		t.Fatal("executable change kept catalog")
	}
}
func TestOldRuntimeCannotAdvertiseOrExecuteGuardedText(t *testing.T) {
	c := fixtureController(t)
	if os.WriteFile(filepath.Join(c.primaryHome, "fixture-version"), []byte("codex-cli 0.166.0-alpha.6"), 0600) != nil {
		t.Fatal("fixture version")
	}
	if _, err := c.Discover(context.Background(), "openai"); err != nil {
		t.Fatal("old discovery regressed")
	}
	if c.State().CanSend {
		t.Fatal("old version advertised send")
	}
	if _, err := c.Send(context.Background(), "openai", "self-authored-test", "hello"); err != ErrUnavailable {
		t.Fatal("old version sent")
	}
}
func TestTextCancellationDoesNotRetry(t *testing.T) {
	c := prepareSend(t, "openai")
	if os.WriteFile(filepath.Join(c.primaryHome, "fixture-mode"), []byte("send-cancel"), 0600) != nil {
		t.Fatal("fixture mode")
	}
	done := make(chan error, 1)
	go func() { _, err := c.Send(context.Background(), "openai", "self-authored-test", "hello"); done <- err }()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-ticker.C:
			_, err := os.Stat(filepath.Join(c.primaryHome, "fixture-send-ready"))
			ready = err == nil
		case <-timer.C:
			c.Cancel()
			<-done
			t.Fatal("fixture not ready")
		}
	}
	c.Cancel()
	select {
	case err := <-done:
		if err != ErrUnavailable {
			t.Fatal("cancel result")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("text cleanup stalled")
	}
	data, _ := os.ReadFile(filepath.Join(c.primaryHome, "fixture-requests"))
	if strings.Count(string(data), `"method":"turn/start"`) != 1 {
		t.Fatal("request retried")
	}
}

func TestEffectiveTextGuardRequiresEveryReportedGate(t *testing.T) {
	spec := peerSpec{provider: "openai"}
	config := map[string]any{"model_provider": "openai"}
	textFixtureConfig(config, "")
	encode := func() json.RawMessage { raw, _ := json.Marshal(map[string]any{"config": config}); return raw }
	if !matchesTextGuard(encode(), spec) {
		t.Fatal("complete synthetic guard rejected")
	}
	for key := range textOverrides() {
		parts := strings.Split(key, ".")
		node := config
		for _, part := range parts[:len(parts)-1] {
			node = node[part].(map[string]any)
		}
		leaf := parts[len(parts)-1]
		saved := node[leaf]
		delete(node, leaf)
		if matchesTextGuard(encode(), spec) {
			t.Fatal("missing effective gate accepted", key)
		}
		node[leaf] = nil
		if matchesTextGuard(encode(), spec) {
			t.Fatal("null effective gate accepted", key)
		}
		node[leaf] = saved
	}
	config["mcp_servers"] = nil
	if matchesTextGuard(encode(), spec) {
		t.Fatal("unknown MCP set accepted")
	}
	config["mcp_servers"] = map[string]any{"disabled": map[string]any{"enabled": false}}
	if !matchesTextGuard(encode(), spec) {
		t.Fatal("explicit disabled MCP rejected")
	}
}
func TestEmptyEnvironmentRequiresAnActualEmptyArray(t *testing.T) {
	for _, raw := range []string{"", "null", "{}", "false", "[{}]"} {
		if emptyArray(json.RawMessage(raw)) {
			t.Fatal("nonempty/unknown environments accepted")
		}
	}
	if !emptyArray(json.RawMessage("[]")) {
		t.Fatal("empty selection rejected")
	}
}
func TestCompletedTextCannotCrossTurnOrNestedModel(t *testing.T) {
	stream := &textStream{spec: peerSpec{provider: "openai"}, model: "selected", thread: "thread", turn: "turn", turnRequested: true, items: map[string]string{}, finished: map[string]bool{}}
	cases := []struct{ method, data string }{
		{"thread/settings/updated", `{"threadId":"thread","threadSettings":{"model":"selected","modelProvider":"openai","collaborationMode":{"settings":{"model":"other"}}}}`},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"foreign","status":"completed","error":null}}`},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed","error":{"message":"PUBLIC-ERROR"}}}`},
	}
	for _, test := range cases {
		if stream.notification(test.method, json.RawMessage(test.data)) != ErrUnavailable || stream.completed {
			t.Fatal("mismatched completion accepted")
		}
	}
}

func TestConfigPathNeverMergesAncestorFieldsOrMutatesInput(t *testing.T) {
	for _, raw := range []string{
		`{"shell_tool":false,"features":{}}`,
		`{"enabled":false,"cloud":{"skills":{}}}`,
		`{"cloud":{"enabled":false,"skills":{}}}`,
		`{"agents":{},"enabled":false}`,
	} {
		var config map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &config) != nil {
			t.Fatal("fixture")
		}
		before, _ := json.Marshal(config)
		for _, path := range []string{"features.shell_tool", "cloud.skills.enabled", "agents.enabled", "orchestrator.mcp.enabled"} {
			if value := configPath(config, path); value != nil {
				t.Fatal("ancestor value leaked into missing nested gate", path)
			}
		}
		after, _ := json.Marshal(config)
		if string(before) != string(after) {
			t.Fatal("config lookup mutated input")
		}
	}
	config := map[string]any{"model_provider": "openai"}
	textFixtureConfig(config, "")
	config["shell_tool"] = false
	delete(config["features"].(map[string]any), "shell_tool")
	raw, _ := json.Marshal(map[string]any{"config": config})
	if matchesTextGuard(raw, peerSpec{provider: "openai"}) {
		t.Fatal("ancestor shell gate accepted")
	}
	textFixtureConfig(config, "")
	cloud := config["cloud"].(map[string]any)
	cloud["enabled"] = false
	delete(cloud["skills"].(map[string]any), "enabled")
	raw, _ = json.Marshal(map[string]any{"config": config})
	if matchesTextGuard(raw, peerSpec{provider: "openai"}) {
		t.Fatal("ancestor enabled gate accepted")
	}
}

func TestTurnReceiptErrorAndUnfinishedMessageCannotSucceed(t *testing.T) {
	stream := &textStream{thread: "thread", turn: "turn", turnRequested: true, items: map[string]string{"unfinished": "agentMessage"}, finished: map[string]bool{}}
	if stream.bindTurn(json.RawMessage(`{"id":"turn","status":"completed","error":{"message":"PUBLIC-ERROR"}}`)) != ErrUnavailable {
		t.Fatal("error-bearing turn receipt accepted")
	}
	if stream.notification("turn/completed", json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed","error":null}}`)) != ErrUnavailable || stream.completed {
		t.Fatal("unfinished message accepted as full completion")
	}
}

func TestCodeModeHostMustBeStartupBooleanFalse(t *testing.T) {
	config := map[string]any{"model_provider": "openai"}
	textFixtureConfig(config, "")
	putFixtureConfig(config, "features.code_mode_host", map[string]any{"enabled": false, "disable_in_process_fallback": true})
	raw, _ := json.Marshal(map[string]any{"config": config})
	if matchesTextGuard(raw, peerSpec{provider: "openai"}) {
		t.Fatal("host object can change worker fallback; must not accept as boolean false")
	}
	args := strings.Join(nativeArguments(peerSpec{provider: "openai"}, textOverrides()), " ")
	if !strings.Contains(args, "features.code_mode_host=false") {
		t.Fatal("host gate not installed at process startup")
	}
}

func TestFixedStageObserverPreservesFailureAndRedaction(t *testing.T) {
	c := prepareSend(t, "openai")
	if os.WriteFile(filepath.Join(c.primaryHome, "fixture-mode"), []byte("send-policy"), 0600) != nil {
		t.Fatal("fixture mode")
	}
	stage := ""
	spec := peerSpec{command: c.executable, digest: c.digest, home: c.primaryHome, codexHome: c.primaryCodexHome, provider: "openai", stage: func(label string) { stage = label }}
	reply, err := sendText(context.Background(), spec, "self-authored-test", "PUBLIC-PROMPT-CANARY")
	if err != ErrUnavailable || reply.Text != "" || stage != "config-features-apps" {
		t.Fatal("fixed guard stage or error identity changed")
	}
	peer := nativePeer{stage: func(label string) { stage = label }, notification: func(string, json.RawMessage) error { return ErrUnavailable }}
	if peer.acceptNotification(map[string]json.RawMessage{"method": json.RawMessage(`"PUBLIC-NATIVE-METHOD-CANARY"`), "params": json.RawMessage(`{}`)}) != ErrUnavailable || stage != "notification-unsupported" {
		t.Fatal("source method leaked or notification acceptance changed")
	}
	config := map[string]any{"model_provider": "openai"}
	textFixtureConfig(config, "")
	putFixtureConfig(config, "features.apps", true)
	putFixtureConfig(config, "agents.enabled", true)
	raw, _ := json.Marshal(map[string]any{"config": config})
	for i := 0; i < 20; i++ {
		if matchesTextGuard(raw, spec) || stage != "config-agents-enabled" {
			t.Fatal("guard stage order is nondeterministic")
		}
	}
}
