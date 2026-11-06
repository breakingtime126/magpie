package appserverbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type requirementsStream struct {
	lines      chan []byte
	done       chan struct{}
	transcript []byte
	err        error
}

func readRequirementsStream(t *testing.T, bridge *runningBridge) *requirementsStream {
	t.Helper()
	stream := &requirementsStream{lines: make(chan []byte, 16), done: make(chan struct{})}
	go func() {
		defer close(stream.done)
		defer close(stream.lines)
		scanner := bufio.NewScanner(bridge.output)
		scanner.Buffer(make([]byte, 64<<10), MaxMessageBytes+1)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			stream.transcript = append(stream.transcript, line...)
			stream.transcript = append(stream.transcript, '\n')
			select {
			case stream.lines <- line:
			default:
				stream.err = fmt.Errorf("unexpected excess fixture output")
				return
			}
		}
		stream.err = scanner.Err()
	}()
	t.Cleanup(func() {
		bridge.output.Close()
		select {
		case <-stream.done:
		case <-time.After(30 * time.Second):
			t.Error("requirements output reader did not stop")
		}
	})
	return stream
}

func (stream *requirementsStream) ask(t *testing.T, bridge *runningBridge, request string) object {
	t.Helper()
	if _, err := fmt.Fprintln(bridge.input, request); err != nil {
		t.Fatal(err)
	}
	select {
	case line, ok := <-stream.lines:
		if !ok {
			t.Fatal("bridge output ended before requirements response")
		}
		result, err := parseObject(line)
		if err != nil {
			t.Fatalf("bridge stdout was not JSONL: %q", line)
		}
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("requirements response timed out")
		return nil
	}
}

func requirementsFixtureConfig(t *testing.T) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	cfg.RequirementsRead = ConfigRequirementsCompareSafe
	for _, backend := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(backend.Home, "fixture-mode"), []byte("requirements"), 0600); err != nil {
			t.Fatal(err)
		}
		writeRequirementsFixture(t, backend, safeRequirementsFixture())
	}
	return cfg
}

func writeRequirementsFixture(t *testing.T, backend Backend, result object) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(backend.Home, "fixture-requirements"), raw(result), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertRequirementsFixtureRequests(t *testing.T, cfg Config, count int) {
	t.Helper()
	for _, backend := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(backend.Home, "fixture-requests"))
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			request, err := parseObject([]byte(line))
			if err != nil {
				t.Fatal("fixture recorded malformed child traffic")
			}
			if text(request, "method") == "configRequirements/read" {
				found++
				if string(request["params"]) != "null" {
					t.Fatalf("backend %s received noncanonical requirements params: %s", backend.ID, line)
				}
			}
		}
		if found != count {
			t.Fatalf("backend %s received %d requirements reads, want %d", backend.ID, found, count)
		}
		if _, err := os.Stat(filepath.Join(backend.Home, "fixture-clean-exit")); err != nil {
			t.Fatalf("backend %s did not exit on EOF: %v", backend.ID, err)
		}
	}
}

func TestRunConfigRequirementsPreservesPolicyAndRefreshes(t *testing.T) {
	cfg := requirementsFixtureConfig(t)
	if cfg.StartupProbe != "" {
		t.Fatal("requirements test unexpectedly enabled startup inspection")
	}
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	init := stream.ask(t, bridge, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"requirements-fixture","version":"1"}}}`)
	if !exists(init, "result") {
		t.Fatal("synthetic initialization failed")
	}
	if _, err := fmt.Fprintln(bridge.input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	initial := safeRequirementsFixture()
	check := func(response object, id string, want object) {
		t.Helper()
		if string(response["id"]) != id || exists(response, "error") || !sameJSON(response["result"], raw(want)) {
			t.Fatalf("native requirements policy changed: %s", raw(response))
		}
	}
	check(stream.ask(t, bridge, `{"id":2,"method":"configRequirements/read","params":{}}`), "2", initial)

	updated := safeRequirementsFixture()
	policy, _ := child(updated, "requirements")
	policy["allowedLoginMethods"] = raw([]string{"api"})
	updated["requirements"] = raw(policy)
	writeRequirementsFixture(t, cfg.Backends[1], updated)
	mismatch := stream.ask(t, bridge, `{"id":3,"method":"configRequirements/read","params":null}`)
	if string(mismatch["id"]) != "3" || !exists(mismatch, "error") || exists(mismatch, "result") {
		t.Fatal("native read reused old policy or accepted differing authorities")
	}
	writeRequirementsFixture(t, cfg.Backends[0], updated)
	check(stream.ask(t, bridge, `{"id":4,"method":"configRequirements/read"}`), "4", updated)

	unsafe := safeRequirementsFixture()
	policy, _ = child(unsafe, "requirements")
	policy["modelProviders"] = raw(map[string]any{"fixture": map[string]string{"apiKey": "requirements-secret-canary"}})
	unsafe["requirements"] = raw(policy)
	for _, backend := range cfg.Backends {
		writeRequirementsFixture(t, backend, unsafe)
	}
	rejected := stream.ask(t, bridge, `{"id":5,"method":"configRequirements/read"}`)
	if string(rejected["id"]) != "5" || !exists(rejected, "error") || exists(rejected, "result") {
		t.Fatal("matching unsafe native policies were exported")
	}
	// A following fanout also establishes that the abandoned sibling was
	// consumed without poisoning the connection or producing a second reply.
	ready := stream.ask(t, bridge, `{"id":6,"method":"environment/status","params":{"environmentId":"local"}}`)
	if string(ready["id"]) != "6" || !exists(ready, "result") {
		t.Fatal("a late rejected requirements sibling poisoned the connection")
	}
	if err := bridge.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 6 || strings.Contains(string(stream.transcript), "secret") {
		t.Fatalf("unexpected native requirements stdout: %q, %v", stream.transcript, stream.err)
	}
	assertRequirementsFixtureRequests(t, cfg, 4)
}

func TestRunConfigRequirementsDisabledSendsNoRead(t *testing.T) {
	cfg := requirementsFixtureConfig(t)
	cfg.RequirementsRead = ""
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	if response := stream.ask(t, bridge, `{"id":1,"method":"initialize"}`); !exists(response, "result") {
		t.Fatal("ordinary bridge initialization failed with requirements disabled")
	}
	if _, err := fmt.Fprintln(bridge.input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	response := stream.ask(t, bridge, `{"id":2,"method":"configRequirements/read"}`)
	if string(response["id"]) != "2" || !exists(response, "error") || exists(response, "result") {
		t.Fatal("disabled requirements read was not rejected locally")
	}
	bridge.input.Close()
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 2 {
		t.Fatalf("unexpected disabled requirements output: %q, %v", stream.transcript, stream.err)
	}
	assertRequirementsFixtureRequests(t, cfg, 0)
}

func TestRunConfigRequirementsChecksBothPinsBeforeAnyChild(t *testing.T) {
	for _, pin := range []string{"", "not-a-digest", strings.Repeat("0", 64)} {
		t.Run(pin, func(t *testing.T) {
			cfg := requirementsFixtureConfig(t)
			cfg.Backends[1].StartupProbeSHA256 = pin
			var output discardRequirementsOutput
			err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output)
			if err == nil || strings.Contains(err.Error(), "not-a-digest") || len(output.data) != 0 {
				t.Fatal("invalid second requirements pin was accepted or disclosed")
			}
			for _, backend := range cfg.Backends {
				if _, err := os.Stat(filepath.Join(backend.Home, "fixture-report")); !os.IsNotExist(err) {
					t.Fatalf("backend %s started before both requirements pins passed", backend.ID)
				}
			}
		})
	}
}

type discardRequirementsOutput struct{ data []byte }

func (output *discardRequirementsOutput) Write(data []byte) (int, error) {
	output.data = append(output.data, data...)
	return len(data), nil
}

func (*discardRequirementsOutput) Close() error { return nil }

func TestConfigRequirementsReadModeAndPinsLoadIndependently(t *testing.T) {
	cfg := requirementsFixtureConfig(t)
	encoded := raw(cfg)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || string(fields["config_requirements_read"]) != `"compare-safe-v0.166"` {
		t.Fatal("requirements mode is missing its explicit configuration field")
	}
	if _, err := LoadConfig(strings.NewReader(string(encoded))); err != nil {
		t.Fatal("requirements-only mode required startup probe", err)
	}
	cfg.RequirementsRead = "future"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown requirements mode accepted")
	}
}
