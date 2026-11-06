package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/proc"
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

func TestBuiltAdapterAuthStatusPrimaryOnlyAndSafeFailures(t *testing.T) {
	adapter := builtAdapterPath(t)
	root := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(root, "native-auth-status-fixture.exe")
	if err := os.WriteFile(commandPath, binary, 0700); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	cfg := appserverbridge.Config{}
	for _, id := range []string{"first", "second"} {
		home, codex := filepath.Join(root, id+"-home"), filepath.Join(root, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		policy, err := json.Marshal(fixtureCodeModePolicy{DisableInProcessFallback: true})
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{
			"synthetic-code-mode-policy.json": policy,
			"fixture-auth-status":             nil,
			"fixture-auth-status-response":    []byte(`"result":` + authStatusFixtureResult),
		} {
			if err := os.WriteFile(filepath.Join(home, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{
			ID: id, Provider: "fixture-" + id, Command: commandPath, Home: home, CodexHome: codex, StartupProbeSHA256: digest,
		})
	}
	// Use only old-base Go APIs so an old compiled adapter fails behaviorally
	// on the unknown mode, not because the test cannot compile.
	configBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(configBytes, &settings); err != nil {
		t.Fatal(err)
	}
	settings["auth_status"] = json.RawMessage(`"primary-no-auth-v0.167.0-alpha.3.2"`)
	configBytes, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "bridge.json")
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configEnv, configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := proc.CommandContext(ctx, adapter, "app-server", "--stdio")
	command.Env = os.Environ()
	command.Dir = root
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	tree, err := proc.StartTreeStrict(command)
	if err != nil {
		input.Close()
		output.Close()
		t.Fatal(err)
	}
	finished, readDone := make(chan struct{}), make(chan struct{})
	lines := make(chan []byte, 8)
	var transcript bytes.Buffer
	var waitErr, readErr error
	go func() {
		defer close(readDone)
		defer close(lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			fmt.Fprintln(&transcript, string(line))
			select {
			case lines <- line:
			default:
				readErr = fmt.Errorf("unexpected excess adapter output")
				return
			}
		}
		readErr = scanner.Err()
	}()
	// Drain stdout before Wait closes the native pipe.
	go func() {
		<-readDone
		waitErr = tree.Wait()
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		input.Close()
		output.Close()
		<-tree.Stop(time.Second)
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("auth status adapter did not reap its children")
		}
		<-readDone
	})
	ask := func(request string) map[string]json.RawMessage {
		t.Helper()
		if _, err := fmt.Fprintln(input, request); err != nil {
			t.Fatal(err)
		}
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("adapter stdout ended before the auth status response")
			}
			var response map[string]json.RawMessage
			if err := json.Unmarshal(line, &response); err != nil {
				t.Fatalf("adapter stdout was not JSONL: %q", line)
			}
			return response
		case <-ctx.Done():
			t.Fatal("auth status adapter timed out")
			return nil
		}
	}
	init := ask(`{"id":"init","method":"initialize","params":{"clientInfo":{"name":"auth-status-adapter-test","version":"1"},"capabilities":{}}}`)
	if string(init["id"]) != `"init"` || len(init["result"]) == 0 || len(init["error"]) != 0 {
		t.Fatal("compiled no-auth-only adapter did not initialize")
	}
	// Exercise native readiness without an initialized notification.
	params := `{ "refreshToken" : false, "include\u0054oken" : true }`
	response := ask(`{"id":9007199254740993,"method":"getAuthStatus","params":` + params + `,"trace":{"traceparent":"private-trace-canary","tracestate":null}}`)
	var wantResult, wantParams bytes.Buffer
	if err := json.Compact(&wantResult, []byte(authStatusFixtureResult)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&wantParams, []byte(params)); err != nil {
		t.Fatal(err)
	}
	if string(response["id"]) != "9007199254740993" || len(response["error"]) != 0 || string(response["result"]) != wantResult.String() {
		t.Fatal("compiled adapter changed primary no-auth result bytes or native ID")
	}
	assertError := func(response map[string]json.RawMessage, code int) {
		t.Helper()
		var failure map[string]json.RawMessage
		if len(response["result"]) != 0 || json.Unmarshal(response["error"], &failure) != nil || len(failure) != 2 || string(failure["code"]) != fmt.Sprint(code) {
			t.Fatalf("compiled no-auth response was not the expected bounded error: %s", response["error"])
		}
		var message string
		if json.Unmarshal(failure["message"], &message) != nil || (code != -32602 && message != "primary no-auth status unavailable") {
			t.Fatal("compiled adapter exposed a source error message")
		}
	}
	invalid := ask(`{"id":"invalid","method":"getAuthStatus","params":{"refreshToken":true}}`)
	assertError(invalid, -32602)
	cases := []struct {
		body string
		code int
	}{
		{`"result":{"authMethod":null,"authToken":"private-token-canary","requiresOpenaiAuth":false}`, -32000},
		{`"error":{"code":-32037,"message":"private-error-canary","data":{"token":"private-data-canary"}}`, -32037},
	}
	for _, tc := range cases {
		if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-status-response"), []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		response := ask(`{"id":"unsafe","method":"getAuthStatus","params":{"includeToken":true}}`)
		if string(response["id"]) != `"unsafe"` {
			t.Fatal("compiled adapter lost the source error correlation")
		}
		assertError(response, tc.code)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("auth status adapter did not stop on EOF")
	}
	<-readDone
	if waitErr != nil || readErr != nil || diagnostics.Len() != 0 || strings.Count(transcript.String(), "\n") != 5 || strings.Contains(transcript.String(), "private-") {
		t.Fatalf("auth status adapter stdout=%q stderr=%q wait=%v read=%v", transcript.String(), diagnostics.String(), waitErr, readErr)
	}
	expectedParams := []string{wantParams.String(), `{"includeToken":true}`, `{"includeToken":true}`}
	for i, backend := range cfg.Backends {
		log, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-status-requests"))
		if err != nil {
			t.Fatal(err)
		}
		reads, initializations := 0, 0
		for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			var msg map[string]json.RawMessage
			if json.Unmarshal([]byte(line), &msg) != nil {
				t.Fatal("malformed child auth status request log")
			}
			var method string
			json.Unmarshal(msg["method"], &method)
			switch method {
			case "initialize":
				initializations++
				var params map[string]json.RawMessage
				if json.Unmarshal(msg["params"], &params) != nil || string(params["capabilities"]) != "{}" {
					t.Fatal("compiled auth status mode changed capabilities")
				}
			case "getAuthStatus":
				if i != 0 || reads >= len(expectedParams) {
					t.Fatal("compiled adapter fanned out, retried, or fell back")
				}
				if len(msg) != 3 || len(msg["trace"]) != 0 || string(msg["params"]) != expectedParams[reads] {
					t.Fatal("compiled adapter changed includeToken:true or forwarded trace")
				}
				reads++
			default:
				t.Fatalf("unexpected compiled child method: %s", method)
			}
		}
		wantReads := 0
		if i == 0 {
			wantReads = len(expectedParams)
		}
		if initializations != 1 || reads != wantReads {
			t.Fatalf("compiled backend %s initialization=%d reads=%d, want 1/%d", backend.ID, initializations, reads, wantReads)
		}
		marker, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-status-clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatal("compiled no-auth child did not exit cleanly")
		}
		if _, err := os.Stat(filepath.Join(backend.Home, "received-startup-client-info.json")); !os.IsNotExist(err) {
			t.Fatal("status-only mode unexpectedly ran startup inspection")
		}
	}
}
