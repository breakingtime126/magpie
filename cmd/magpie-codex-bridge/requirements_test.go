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

// A fixture file opts in only this copied test process. Existing synthetic
// children retain their previous behavior when that file is absent.
func fixtureRequirementsRead(id json.RawMessage, request []byte) (bool, int) {
	home := os.Getenv("HOME")
	result, err := os.ReadFile(filepath.Join(home, "synthetic-requirements-response.json"))
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		return true, 15
	}
	if os.WriteFile(filepath.Join(home, "received-requirements.json"), request, 0600) != nil {
		return true, 16
	}
	fmt.Fprintln(os.Stderr, "requirements-secret-canary")
	response := map[string]json.RawMessage{"id": id, "result": json.RawMessage(result)}
	if json.NewEncoder(os.Stdout).Encode(response) != nil {
		return true, 17
	}
	return true, 0
}

// The complete pinned 0.166 ConfigRequirements fields are present, including
// explicit nulls. This is a synthetic restrictive policy, not an account read.
func adapterRequirementsFixture() map[string]any {
	fields := strings.Fields("modelProvider modelProviders allowedLoginMethods cliAuthCredentialsStore chatgptBaseUrl additionalDeveloperInstructions allowedApprovalPolicies allowedApprovalsReviewers allowedSandboxModes allowedWindowsSandboxImplementations allowedPermissionProfiles defaultPermissions allowedWebSearchModes allowManagedHooksOnly allowBrowserAndComputerUse allowAppshots allowRemoteControl computerUse browserUse inAppBrowser featureRequirements hooks enforceResidency network application autoReview models sqliteHome logDir modelCatalogJson checkForUpdateOnStartup allowLoginShell feedback otel")
	policy := make(map[string]any, len(fields))
	for _, field := range fields {
		policy[field] = nil
	}
	policy["allowedLoginMethods"] = []string{"api", "chatgpt"}
	policy["allowRemoteControl"] = false
	return map[string]any{"supportsIndependentSpeedModes": true, "requirements": policy}
}

func TestBuiltAdapterConfigRequirements(t *testing.T) {
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
	cfg := appserverbridge.Config{RequirementsRead: appserverbridge.ConfigRequirementsCompareSafe}
	result, err := json.Marshal(adapterRequirementsFixture())
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
			"synthetic-code-mode-policy.json":      policy,
			"synthetic-requirements-response.json": result,
		} {
			if err := os.WriteFile(filepath.Join(home, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{
			ID: id, Provider: "fixture-" + id, Command: commandPath, Home: home, CodexHome: codex, StartupProbeSHA256: digest,
		})
	}
	if cfg.StartupProbe != "" {
		t.Fatal("requirements-only test enabled startup inspection")
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
			t.Error("requirements adapter did not reap its children")
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
			t.Fatal("requirements adapter timed out")
			return nil
		}
	}
	init := ask(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"requirements-adapter-test","version":"1"}}}`)
	if string(init["id"]) != "1" || len(init["result"]) == 0 || len(init["error"]) != 0 {
		t.Fatal("compiled adapter did not initialize")
	}
	if _, err := fmt.Fprintln(input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	response := ask(`{"id":9007199254740993,"method":"configRequirements/read","params":{}}`)
	var got, want any
	if err := json.Unmarshal(response["result"], &got); err != nil {
		t.Fatal("compiled adapter returned no requirements result", err)
	}
	if err := json.Unmarshal(result, &want); err != nil {
		t.Fatal(err)
	}
	if string(response["id"]) != "9007199254740993" || len(response["error"]) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatal("compiled adapter lost the nonempty requirements policy or request ID")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("requirements adapter did not stop on EOF")
	}
	<-readDone
	if waitErr != nil || diagnostics.Len() != 0 || readErr != nil || strings.Count(transcript.String(), "\n") != 2 || strings.Contains(transcript.String(), "secret") {
		t.Fatalf("requirements adapter stdout=%q stderr=%q wait=%v read=%v", transcript.String(), diagnostics.String(), waitErr, readErr)
	}
	for _, backend := range cfg.Backends {
		recorded, err := os.ReadFile(filepath.Join(backend.Home, "received-requirements.json"))
		if err != nil {
			t.Fatal(err)
		}
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(recorded, &request); err != nil || request.Method != "configRequirements/read" || string(request.Params) != "null" {
			t.Fatalf("backend %s did not receive params:null: %s", backend.ID, recorded)
		}
		marker, err := os.ReadFile(filepath.Join(backend.Home, "clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatalf("backend %s did not exit cleanly: %q, %v", backend.ID, marker, err)
		}
		if _, err := os.Stat(filepath.Join(backend.Home, "received-startup-client-info.json")); !os.IsNotExist(err) {
			t.Fatal("requirements-only mode unexpectedly ran a startup probe")
		}
	}
}
