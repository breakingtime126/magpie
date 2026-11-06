package appserverbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A copied test executable exercises real pipes, process pin checks, isolated
// homes, routing and EOF cleanup without credentials or external services.
func controlPlaneFixture() (bool, int) {
	home := os.Getenv("HOME")
	if _, err := os.Stat(filepath.Join(home, "fixture-control-plane")); os.IsNotExist(err) {
		return false, 0
	} else if err != nil {
		return true, 71
	}
	log, err := os.OpenFile(filepath.Join(home, "fixture-control-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 72
	}
	defer log.Close()
	provider, err := strconv.Unquote(strings.TrimPrefix(os.Args[len(os.Args)-1], "model_provider="))
	if err != nil {
		return true, 73
	}
	model := "gpt"
	if provider != "openai" {
		model = "deepseek-flash-ft"
	}
	thread := object{"id": raw(provider + "-thread"), "modelProvider": raw(provider), "model": raw(model), "turns": raw([]any{})}
	turn := 0
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request json.RawMessage
		if err := decoder.Decode(&request); err == io.EOF {
			break
		} else if err != nil {
			return true, 74
		}
		fmt.Fprintln(log, string(request))
		m, err := parseObject(request)
		if err != nil {
			return true, 75
		}
		method := text(m, "method")
		p, _ := child(m, "params")
		var result json.RawMessage
		switch method {
		case "initialize":
			result = raw(object{"userAgent": raw("control-fixture"), "codexHome": raw(os.Getenv("CODEX_HOME")),
				"platformFamily": raw("fixture"), "platformOs": raw("fixture")})
		case "initialized":
			continue
		case "configRequirements/read":
			body, err := os.ReadFile(filepath.Join(home, "fixture-control-policy"))
			if err != nil {
				return true, 76
			}
			fmt.Fprintf(os.Stdout, "{\"id\":%s,%s}\n", m["id"], body)
			continue
		case "model/list":
			result = raw(object{"data": raw([]json.RawMessage{fixtureModel(model, true)}), "nextCursor": raw(nil)})
		case "thread/start":
			if text(p, "modelProvider") != provider || text(p, "model") != model {
				return true, 77
			}
			result = raw(object{"thread": raw(thread), "modelProvider": raw(provider), "model": raw(model)})
		case "thread/read":
			if text(p, "threadId") != text(thread, "id") {
				return true, 78
			}
			result = raw(object{"thread": raw(thread)})
		case "turn/start":
			if text(p, "threadId") != text(thread, "id") {
				return true, 79
			}
			turn++
			result = raw(object{"turn": raw(object{"id": raw(fmt.Sprintf("%s-turn-%d", provider, turn)),
				"items": raw([]any{}), "status": raw("inProgress"), "error": raw(nil)})})
		default:
			// In particular native config/read must never reach this fixture.
			return true, 80
		}
		fmt.Println(string(raw(object{"id": m["id"], "result": result})))
		if method == "turn/start" {
			id := fmt.Sprintf("%s-turn-%d", provider, turn)
			fmt.Println(string(raw(object{"method": raw("item/agentMessage/delta"), "params": raw(object{
				"threadId": thread["id"], "turnId": raw(id), "itemId": raw(id + "-message"), "delta": raw("synthetic " + provider),
			})})))
			fmt.Println(string(raw(object{"method": raw("turn/completed"), "params": raw(object{
				"threadId": thread["id"], "turn": raw(object{"id": raw(id), "items": raw([]any{}), "status": raw("completed"), "error": raw(nil)}),
			})})))
		}
	}
	if os.WriteFile(filepath.Join(home, "fixture-control-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 81
	}
	return true, 0
}

func controlPlaneRunConfig(t *testing.T, enabled bool) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	cfg.Backends[0].ID, cfg.Backends[0].Provider = "openai", "openai"
	cfg.Backends[1].ID, cfg.Backends[1].Provider = "aihub", "aihub"
	if enabled {
		cfg.ControlPlane = ControlPlaneRouting167
		cfg.ControlHome = t.TempDir()
	}
	for _, b := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-control-plane"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-control-policy"), []byte(`"result":`+controlPlaneNullRequirements), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func controlPlaneRunNext(t *testing.T, stream *requirementsStream) object {
	t.Helper()
	select {
	case line := <-stream.lines:
		m, err := parseObject(line)
		if err != nil {
			t.Fatal("invalid fixture notification")
		}
		return m
	case <-time.After(30 * time.Second):
		t.Fatal("fixture notification did not arrive")
		return nil
	}
}

func TestRunControlPlane167TwoNativePeersAndFreshReads(t *testing.T) {
	cfg := controlPlaneRunConfig(t, true)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	init := stream.ask(t, bridge, `{"id":"init","method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`)
	initial, _ := child(init, "result")
	control, _ := filepath.EvalSymlinks(cfg.ControlHome)
	if text(initial, "codexHome") != control {
		t.Fatal("control frontend exposed a private native home")
	}
	askConfig := func() object {
		m := stream.ask(t, bridge, `{"id":"config","method":"config/read","params":{"includeLayers":false}}`)
		return m
	}
	first := askConfig()
	result, _ := child(first, "result")
	config, _ := child(result, "config")
	if len(config) != 2 || text(config, "model_provider") != controlPlaneProvider167 {
		t.Fatal("config fabricated a default or policy before native catalog")
	}
	catalog := stream.ask(t, bridge, `{"id":"models","method":"model/list","params":{}}`)
	if !exists(catalog, "result") {
		t.Fatal("native model catalog failed")
	}
	current := askConfig()
	result, _ = child(current, "result")
	config, _ = child(result, "config")
	if text(config, "model") != "magpie/openai/gpt" {
		t.Fatal("config did not use the actual merged native default")
	}
	for _, b := range cfg.Backends {
		model := "gpt"
		if b.ID != "openai" {
			model = "deepseek-flash-ft"
		}
		started := stream.ask(t, bridge, string(raw(object{"id": raw("start-" + b.ID), "method": raw("thread/start"), "params": raw(object{
			"model": raw(alias(b.ID, model)), "modelProvider": raw(controlPlaneProvider167),
			"approvalPolicy": raw("on-request"), "permissions": raw("native-profile"),
		})})))
		value, _ := child(started, "result")
		thread, _ := child(value, "thread")
		if text(value, "modelProvider") != controlPlaneProvider167 || text(thread, "modelProvider") != controlPlaneProvider167 {
			t.Fatal("native peer provider escaped frontend namespace")
		}
		for i := 0; i < 2; i++ {
			response := stream.ask(t, bridge, string(raw(object{"id": raw(fmt.Sprintf("%s-turn-%d", b.ID, i)), "method": raw("turn/start"), "params": raw(object{
				"threadId": thread["id"], "input": raw([]object{{"type": raw("text"), "text": raw("synthetic routing check"), "text_elements": raw([]any{})}}),
			})})))
			if !exists(response, "result") {
				t.Fatal("native synthetic turn failed")
			}
			for _, want := range []string{"item/agentMessage/delta", "turn/completed"} {
				msg := controlPlaneRunNext(t, stream)
				params, _ := child(msg, "params")
				if text(msg, "method") != want || text(params, "threadId") != text(thread, "id") {
					t.Fatal("turn notification crossed its native owner")
				}
			}
		}
		read := stream.ask(t, bridge, string(raw(object{"id": raw("read-" + b.ID), "method": raw("thread/read"), "params": raw(object{"threadId": thread["id"]})})))
		value, _ = child(read, "result")
		thread, _ = child(value, "thread")
		if text(thread, "modelProvider") != controlPlaneProvider167 {
			t.Fatal("read returned native provider instead of logical namespace")
		}
	}
	// Rejected sibling policy must not reuse the previously accepted view.
	secondary := filepath.Join(cfg.Backends[1].Home, "fixture-control-policy")
	if err := os.WriteFile(secondary, []byte(`"error":{"code":-32000,"message":"PRIVATE_POLICY","data":"PRIVATE_TOKEN"}`), 0600); err != nil {
		t.Fatal(err)
	}
	failed := askConfig()
	failure, _ := child(failed, "error")
	if text(failure, "message") != controlPlaneUnavailable167 || exists(failed, "result") {
		t.Fatal("fresh native policy error became config success")
	}
	if err := os.WriteFile(secondary, []byte(`"result":`+controlPlaneNullRequirements), 0600); err != nil {
		t.Fatal(err)
	}
	if !exists(askConfig(), "result") {
		t.Fatal("fresh recovery failed")
	}
	bridge.input.Close()
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Contains(string(stream.transcript), "PRIVATE_") {
		t.Fatal("native diagnostic data escaped the bridge")
	}
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-control-requests"))
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			m, _ := parseObject([]byte(line))
			method := text(m, "method")
			counts[method]++
			p, _ := child(m, "params")
			if method == "config/read" || (method == "configRequirements/read" && string(m["params"]) != "null") {
				t.Fatal("native config was read or policy params changed")
			}
			if method == "thread/start" && (text(p, "modelProvider") != b.Provider ||
				text(p, "approvalPolicy") != "on-request" || text(p, "permissions") != "native-profile") {
				t.Fatal("native provider or permissions changed")
			}
		}
		if counts["configRequirements/read"] != 4 || counts["model/list"] != 1 || counts["thread/start"] != 1 ||
			counts["turn/start"] != 2 || counts["thread/read"] != 1 {
			t.Fatal("native requests were cached, broadcast or retried")
		}
		if _, err := os.Stat(filepath.Join(b.Home, "fixture-control-clean-exit")); err != nil {
			t.Fatal("native peer did not exit on EOF")
		}
	}
	if entries, err := os.ReadDir(cfg.ControlHome); err != nil || len(entries) != 0 {
		t.Fatal("bridge generated a fake native configuration in control_home")
	}
}

func TestRunControlPlane167SoleModePinsAndDefault(t *testing.T) {
	for _, index := range []int{0, 1} {
		for _, pin := range []string{"", strings.Repeat("0", 64)} {
			cfg := controlPlaneRunConfig(t, true)
			cfg.Backends[index].StartupProbeSHA256 = pin
			var output discardRequirementsOutput
			if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
				t.Fatal("routing-only mode accepted a missing or mismatched pin")
			}
			for _, b := range cfg.Backends {
				for _, marker := range []string{"fixture-report", "fixture-control-requests"} {
					if _, err := os.Stat(filepath.Join(b.Home, marker)); !os.IsNotExist(err) {
						t.Fatal("a child started before both pins passed")
					}
				}
			}
		}
	}
	cfg := controlPlaneRunConfig(t, false)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	authStatusRunInitialize(t, bridge, stream)
	response := stream.ask(t, bridge, `{"id":"disabled","method":"config/read","params":{"includeLayers":false}}`)
	failure, _ := child(response, "error")
	if exists(response, "result") || string(failure["code"]) != "-32601" {
		t.Fatal("routing control plane was not default-off")
	}
	bridge.input.Close()
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
}
