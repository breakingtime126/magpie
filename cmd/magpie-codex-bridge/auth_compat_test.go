package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/proc"
)

// authCompatFixture is a synthetic process, not an official runtime or proof.
func authCompatFixture() (bool, int) {
	home := os.Getenv("HOME")
	data, err := os.ReadFile(filepath.Join(home, "fixture-auth.json"))
	if os.IsNotExist(err) {
		return false, 0
	}
	var settings struct {
		Provider string
	}
	if err != nil || json.Unmarshal(data, &settings) != nil {
		return true, 31
	}
	log, err := os.OpenFile(filepath.Join(home, "fixture-auth-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 32
	}
	defer log.Close()
	fmt.Fprintln(os.Stderr, "auth-secret-stderr-canary")
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	var pendingModel json.RawMessage
	var pending json.RawMessage
	received := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if _, err := fmt.Fprintln(log, string(line)); err != nil {
			return true, 33
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(line, &msg) != nil {
			return true, 34
		}
		var method string
		json.Unmarshal(msg["method"], &method)
		if method == "" {
			if pending == nil || (string(msg["id"]) != `"shared-attestation"` && string(msg["id"]) != "9007199254740993") {
				return true, 35
			}
			var failure struct {
				Code    int
				Message string
			}
			if json.Unmarshal(msg["error"], &failure) != nil || failure.Code != -32601 || failure.Message != "attestation/generate is unavailable through this bridge" || len(msg["result"]) != 0 {
				return true, 36
			}
			received++
			if received == 2 {
				if encoder.Encode(map[string]any{"id": pending, "result": map[string]any{"thread": map[string]any{"id": settings.Provider + "-thread", "modelProvider": settings.Provider, "model": pendingModel, "turns": []any{}}, "model": pendingModel, "modelProvider": settings.Provider}}) != nil {
					return true, 37
				}
				pending = nil
			}
			continue
		}
		if method == "initialized" {
			continue
		}
		var result any = map[string]any{}
		switch method {
		case "initialize":
			result = map[string]string{"userAgent": "auth-fixture", "platformFamily": "fixture", "platformOs": "fixture"}
		case "thread/start":
			var params map[string]json.RawMessage
			json.Unmarshal(msg["params"], &params)
			pendingModel = params["model"]
			pending = msg["id"]
			received = 0
			for _, id := range []json.RawMessage{json.RawMessage(`"shared-attestation"`), json.RawMessage("9007199254740993")} {
				if encoder.Encode(map[string]any{"id": id, "method": "attestation/generate", "params": map[string]any{}}) != nil {
					return true, 38
				}
			}
			continue
		case "account/gatewayOAuth/read":
			response, err := os.ReadFile(filepath.Join(home, "fixture-auth-response.json"))
			if err != nil {
				return true, 39
			}
			result = json.RawMessage(response)
		case "account/gatewayOAuth/login", "account/gatewayOAuth/cancel":
			fmt.Fprintln(os.Stderr, "auth-secret-forbidden-call")
			return true, 40
		}
		if encoder.Encode(map[string]any{"id": msg["id"], "result": result}) != nil {
			return true, 41
		}
	}
	if scanner.Err() != nil {
		return true, 42
	}
	if os.WriteFile(filepath.Join(home, "fixture-auth-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 43
	}
	return true, 0
}

func TestBuiltAdapterAuthCompatPrewarmAndPrimaryRead(t *testing.T) {
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
	commandPath := filepath.Join(root, "native-requirements-fixture.exe")
	if err := os.WriteFile(commandPath, binary, 0700); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	cfg := appserverbridge.Config{Attestation: appserverbridge.AttestationUnavailable, GatewayOAuth: appserverbridge.GatewayOAuthPrimaryRead, GatewayOAuthProviderName: "Reviewed Fixture"}
	result, err := json.Marshal(map[string]any{"providerId": "fixture-first", "providerName": "Reviewed Fixture", "required": true, "status": "notReady", "error": nil})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		home := filepath.Join(root, id+"-home")
		codex := filepath.Join(root, id+"-codex")
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

			"fixture-auth-response.json": result,

			"fixture-auth.json": []byte(fmt.Sprintf(`{"Provider":"fixture-%s"}`, id)),
		} {
			if err := os.WriteFile(filepath.Join(home, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{
			ID: id, Provider: "fixture-" + id, Command: commandPath, Home: home, CodexHome: codex, StartupProbeSHA256: digest,

			Models: []json.RawMessage{json.RawMessage(`{"id":"fixture-model","model":"fixture-model","displayName":"fixture-model","description":"synthetic","hidden":false,"isDefault":true,"supportsPersonality":false,"supportedReasoningEfforts":[],"defaultReasoningEffort":"medium","inputModalities":["text"]}`)},
		})
	}
	if cfg.StartupProbe != "" {
		t.Fatal("auth-only test enabled startup inspection")
	}
	configBytes, err := json.Marshal(cfg)
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
	finished := make(chan struct{})
	var waitErr error
	lines := make(chan []byte, 8)
	readDone := make(chan struct{})
	var transcript bytes.Buffer
	var readErr error
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
	// Drain the native stdout pipe before Wait closes it.
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
			t.Error("auth adapter did not reap its children")
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
				t.Fatal("adapter stdout ended before its response")
			}
			var response map[string]json.RawMessage
			if err := json.Unmarshal(line, &response); err != nil {
				t.Fatalf("adapter stdout was not JSONL: %q", line)
			}
			return response
		case <-ctx.Done():
			t.Fatal("auth adapter timed out")
			return nil
		}
	}
	init := ask(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"auth-adapter-test","version":"1"},"capabilities":{"requestAttestation":true,"explicitGatewayOauth":true}}}`)
	if string(init["id"]) != "1" || len(init["result"]) == 0 || len(init["error"]) != 0 {
		t.Fatal("compiled adapter did not initialize")
	}
	if _, err := fmt.Fprintln(input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	models := ask(`{"id":2,"method":"model/list","params":{}}`)
	var catalog struct {
		Data []struct {
			Model string
		}
	}
	if string(models["id"]) != "2" || len(models["error"]) != 0 || json.Unmarshal(models["result"], &catalog) != nil || len(catalog.Data) != len(cfg.Backends) {
		t.Fatal("compiled auth fixture model discovery failed")
	}
	for _, backend := range cfg.Backends {
		found := false
		for _, card := range catalog.Data {
			if card.Model == "magpie/"+backend.ID+"/fixture-model" {
				found = true
			}
		}
		if !found {
			t.Fatalf("compiled auth fixture model missing for %s", backend.ID)
		}
	}
	for i, backend := range cfg.Backends {
		response := ask(fmt.Sprintf(`{"id":%d,"method":"thread/start","params":{"model":"magpie/%s/fixture-model"}}`, i+10, backend.ID))
		if len(response["result"]) == 0 || len(response["error"]) != 0 || len(response["method"]) != 0 {
			t.Fatal("compiled prewarm failed or reached client")
		}
	}
	for _, method := range []string{"account/gatewayOAuth/login", "account/gatewayOAuth/cancel"} {
		response := ask(fmt.Sprintf(`{"id":"mutation","method":%q,"params":null}`, method))
		if len(response["error"]) == 0 || len(response["result"]) != 0 {
			t.Fatal("compiled adapter accepted auth mutation")
		}
	}
	response := ask(`{"id":9007199254740993,"method":"account/gatewayOAuth/read","params":{}}`)
	var got, want any
	if err := json.Unmarshal(response["result"], &got); err != nil {
		t.Fatal("compiled adapter returned no gateway result", err)
	}
	if err := json.Unmarshal(result, &want); err != nil {
		t.Fatal(err)
	}
	if string(response["id"]) != "9007199254740993" || len(response["error"]) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatal("compiled adapter lost the nonempty gateway result or request ID")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("auth adapter did not stop on EOF")
	}
	<-readDone
	if waitErr != nil || diagnostics.Len() != 0 || readErr != nil || strings.Count(transcript.String(), "\n") != 7 || strings.Contains(transcript.String(), "attestation/generate") || strings.Contains(transcript.String(), "secret") {
		t.Fatalf("auth adapter stdout=%q stderr=%q wait=%v read=%v", transcript.String(), diagnostics.String(), waitErr, readErr)
	}
	for i, backend := range cfg.Backends {
		recorded, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-requests"))
		if err != nil {
			t.Fatal(err)
		}
		reads, failures := 0, 0
		ids := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
			var msg map[string]json.RawMessage
			if json.Unmarshal([]byte(line), &msg) != nil {
				t.Fatal("malformed child request log")
			}
			var method string
			json.Unmarshal(msg["method"], &method)
			switch method {
			case "initialize":
				var params struct {
					Capabilities map[string]bool
				}
				if json.Unmarshal(msg["params"], &params) != nil || !params.Capabilities["requestAttestation"] || !params.Capabilities["explicitGatewayOauth"] {
					t.Fatal("compiled adapter dropped true capabilities")
				}
			case "account/gatewayOAuth/read":
				reads++
				if string(msg["params"]) != "null" {
					t.Fatal("compiled gateway read params changed")
				}
			case "account/gatewayOAuth/login", "account/gatewayOAuth/cancel":
				t.Fatal("mutation reached child")
			case "":
				failures++
				ids[string(msg["id"])] = true
				var failure struct {
					Code    int
					Message string
				}
				if json.Unmarshal(msg["error"], &failure) != nil || failure.Code != -32601 || failure.Message != "attestation/generate is unavailable through this bridge" || len(msg["result"]) != 0 {
					t.Fatal("attestation did not receive real error")
				}
			}
		}
		wantReads := 0
		if i == 0 {
			wantReads = 1
		}
		if reads != wantReads || failures != 2 || !ids[`"shared-attestation"`] || !ids["9007199254740993"] {
			t.Fatal("compiled adapter lost child isolation, ID type, or primary-only read")
		}
		marker, err := os.ReadFile(filepath.Join(backend.Home, "fixture-auth-clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatal("compiled fixture did not exit cleanly")
		}
	}
}
