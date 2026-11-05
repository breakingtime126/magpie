package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "app-server" ||
		len(os.Args) > 3 && os.Args[1] == "-c" && os.Args[3] == "app-server" {
		os.Exit(fixtureServer())
	}
	testenv.Main(m)
}

// This synthetic policy models only the reviewed argv merge contract. It does
// not execute an official backend or prove that backend's runtime merge behavior.
type fixtureCodeModePolicy struct {
	Enabled                  bool `json:"enabled"`
	DisableInProcessFallback bool `json:"disable_in_process_fallback"`
}

// Only a copied native test executable reaches this path; no model is called.
func fixtureServer() int {
	args, err := json.Marshal(os.Args[1:])
	if err != nil || os.WriteFile(filepath.Join(os.Getenv("HOME"), "received-argv.json"), args, 0600) != nil {
		return 7
	}
	policyPath := filepath.Join(os.Getenv("HOME"), "synthetic-code-mode-policy.json")
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return 8
	}
	var policy fixtureCodeModePolicy
	if json.Unmarshal(data, &policy) != nil {
		return 8
	}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "features.code_mode_host=true":
			// A scalar can replace the structured policy. Never accept it here.
			return 9
		case "features.code_mode_host.enabled=true":
			policy.Enabled = true
		}
	}
	data, err = json.Marshal(policy)
	if err != nil || os.WriteFile(filepath.Join(os.Getenv("HOME"), "received-code-mode-policy.json"), data, 0600) != nil {
		return 8
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 3
		}
		if request.Method == "initialize" {
			response := map[string]any{
				"id": request.ID,
				"result": map[string]string{
					"userAgent":      "native-adapter-fixture",
					"platformFamily": "fixture",
					"platformOs":     "fixture",
				},
			}
			if json.NewEncoder(os.Stdout).Encode(response) != nil {
				return 4
			}
		}
	}
	if scanner.Err() != nil {
		return 5
	}
	if os.WriteFile(filepath.Join(os.Getenv("HOME"), "clean-exit"), []byte("EOF"), 0600) != nil {
		return 6
	}
	return 0
}

type bufferCloser struct{ bytes.Buffer }

func (*bufferCloser) Close() error { return nil }

func TestAcceptedServerArguments(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		options appserverbridge.LaunchOptions
	}{
		{args: []string{"app-server"}},
		{args: []string{"app-server", "--listen", "stdio://"}},
		{args: []string{"app-server", "--listen=stdio://"}},
		{args: []string{"app-server", "--stdio"}},
		{args: []string{"-c", "features.code_mode_host=true", "app-server"}, options: appserverbridge.LaunchOptions{CodeModeHost: true}},
		{args: []string{"app-server", "-c", "features.code_mode_host=true"}, options: appserverbridge.LaunchOptions{CodeModeHost: true}},
		{args: []string{"--config", "features.code_mode_host=true", "app-server"}, options: appserverbridge.LaunchOptions{CodeModeHost: true}},
		{args: []string{"app-server", "--config", "features.code_mode_host=true"}, options: appserverbridge.LaunchOptions{CodeModeHost: true}},
		{args: []string{"app-server", "--analytics-default-enabled"}, options: appserverbridge.LaunchOptions{AnalyticsDefaultEnabled: true}},
		{args: []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
		{args: []string{"app-server", "--analytics-default-enabled", "-c", "features.code_mode_host=true"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
		{args: []string{"app-server", "--config", "features.code_mode_host=true", "--listen", "stdio://", "--analytics-default-enabled"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
		{args: []string{"--config", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", "--stdio"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
		{args: []string{"-c", "features.code_mode_host=true", "app-server", "--listen=stdio://", "--analytics-default-enabled"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bridge.json")
			cfg := appserverbridge.Config{WorkingDirectory: "sentinel"}
			input := io.NopCloser(strings.NewReader("request\n"))
			var output bufferCloser
			var diagnostics bytes.Buffer
			var calls []string
			ctx := context.WithValue(context.Background(), struct{}{}, "sentinel")
			code := execute(ctx, tt.args, input, &output, &diagnostics, dependencies{
				getenv: func(key string) string {
					if key != configEnv {
						t.Fatalf("unexpected environment read %q", key)
					}
					calls = append(calls, "env")
					return path
				},
				load: func(got string) (appserverbridge.Config, error) {
					if got != path {
						t.Fatal("configuration path changed")
					}
					calls = append(calls, "load")
					return cfg, nil
				},
				run: func(got context.Context, config appserverbridge.Config, options appserverbridge.LaunchOptions, in io.ReadCloser, out io.WriteCloser) error {
					calls = append(calls, "run")
					if got != ctx || !reflect.DeepEqual(config, cfg) || options != tt.options || in != input || out != &output {
						t.Fatal("bridge inputs changed")
					}
					_, err := io.WriteString(out, "{\"id\":1,\"result\":{}}\n")
					return err
				},
			})
			if code != 0 || diagnostics.Len() != 0 || output.String() != "{\"id\":1,\"result\":{}}\n" || !reflect.DeepEqual(calls, []string{"env", "load", "run"}) {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, output.String(), diagnostics.String(), calls)
			}
		})
	}
}

func TestRejectedArgumentsDoNotReadConfigOrStartChildren(t *testing.T) {
	cases := [][]string{
		nil, {"serve"}, {"--help", "--unknown"}, {"--version", "app-server"},
		{"app-server", "--help", "--unknown"}, {"app-server", "--listen"},
		{"app-server", "--listen", "tcp://127.0.0.1:1234"},
		{"app-server", "--listen=stdio://", "--listen=stdio://"},
		{"app-server", "--listen", "stdio://", "trailing"},
		{"app-server", "--listen=STDIO://"}, {"app-server", "--listen=stdio:///"},
		{"app-server", "--stdio", "--stdio"},
		{"app-server", "--stdio", "--listen=stdio://"},
		{"app-server", "--listen", "stdio://", "--stdio"},
		{"app-server", "--"}, {"app-server", "--check"},
	}
	for _, flag := range []string{"--unknown", "--config", "-c", "--model", "-m", "--provider", "--model-provider", "--sandbox", "--ask-for-approval", "--dangerously-bypass-approvals-and-sandbox", "--full-auto", "--enable", "--disable", "--remote-control", "--managed-daemon", "--strict-config", "--telemetry", "--cwd", "-C"} {
		cases = append(cases, []string{"app-server", flag}, []string{"app-server", flag, "secret"}, []string{"app-server", flag + "=secret"})
	}
	cases = append(cases,
		[]string{"app-server", "app-server"},
		[]string{"--stdio", "app-server"},
		[]string{"--listen", "stdio://", "app-server"},
		[]string{"--listen=stdio://", "app-server"},
		[]string{"--analytics-default-enabled", "app-server"},
		[]string{"app-server", "--analytics-default-enabled", "--analytics-default-enabled"},
		[]string{"app-server", "--analytics-default-enabled=true"},
		[]string{"app-server", "--analytics-default-enabled=false"},
		[]string{"app-server", "--analytics-default-enabled", "secret"},
		[]string{"app-server", "-cfeatures.code_mode_host=true"},
		[]string{"app-server", "--config=features.code_mode_host=true"},
		[]string{"-c", "features.code_mode_host=true"},
		[]string{"-c", "features.code_mode_host=true", "--check"},
		[]string{"-c", "features.code_mode_host=true", "--help"},
		[]string{"-c", "features.code_mode_host=true", "--version"},
		[]string{"-c", "features.code_mode_host=true", "app-server", "--help"},
		[]string{"app-server", "--analytics-default-enabled", "--help"},
		[]string{"-c", "features.code_mode_host=true", "app-server", "--config", "features.code_mode_host=true"},
		[]string{"app-server", "-c", "features.code_mode_host=true", "-c", "features.code_mode_host=true"},
		[]string{"--config", "features.code_mode_host=true", "-c", "features.code_mode_host=true", "app-server"},
	)
	for _, value := range []string{
		"features.code_mode_host=false", "features.code_mode_host=1", "features.code_mode_host=TRUE",
		"features.code_mode_host = true", "features.code_mode_host=true ", "features.code_mode_host=true\n",
		`features.code_mode_host="true"`, `features.code_mode_host='true'`,
		`"features".code_mode_host=true`, `features."code_mode_host"=true`,
		"features.code_mode_host.enabled=true", "features.code_mode_host={enabled=true}",
		"features.code_mode_host=true\nmodel_provider=secret", "features.code_mode_host=true\x00",
		"features.prefer_mxc=true", "features.prefer_mxc=false",
		"features.windows_sandbox_service=true", "features.windows_sandbox_service=false",
		"model_provider=secret", "model=secret", "model_providers.secret.base_url=secret",
		"forced_login_method=secret", "cli_auth_credentials_store=secret", "plugins.secret.enabled=true",
		"sandbox_mode=secret", "approval_policy=secret", "permissions=secret", "features.unknown=true", "",
	} {
		for _, flag := range []string{"-c", "--config"} {
			cases = append(cases, []string{flag, value, "app-server"}, []string{"app-server", flag, value})
		}
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			// Nil dependencies deliberately panic if even an environment read occurs.
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
			if code != 2 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "unsupported arguments") || strings.Contains(diagnostics.String(), "secret") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

func TestUnsupportedStartupOperationsDoNotReadConfiguration(t *testing.T) {
	for _, tt := range []struct {
		flag string
		err  error
	}{
		{flag: "--print-startup-config", err: errUnsupportedStartupInspection},
		{flag: "--startup-client-info", err: errUnsupportedStartupInspection},
		{flag: "--startup-sign-out", err: errUnsupportedStartupSignOut},
	} {
		for _, args := range [][]string{
			{tt.flag}, {tt.flag, "secret"}, {tt.flag + "=secret"},
			{tt.flag, "app-server"}, {"app-server", tt.flag},
			{"-c", "features.code_mode_host=true", "app-server", tt.flag, "--analytics-default-enabled"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				var output bufferCloser
				var diagnostics bytes.Buffer
				// Nil dependencies prove neither environment nor configuration is accessed.
				code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
				want := "magpie-codex-bridge: " + tt.err.Error() + "\n"
				if code != 2 || output.Len() != 0 || diagnostics.String() != want {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
				}
			})
		}
	}
}

// The combined argv reflects the installed launcher's append construction,
// not a captured Desktop launch or startup-probe compatibility test.
func TestStartupSignOutTakesPrecedenceOverInspection(t *testing.T) {
	for _, args := range [][]string{
		{"--print-startup-config", "--startup-sign-out"},
		{"--startup-sign-out", "--print-startup-config"},
		{"--startup-client-info", "secret", "--startup-sign-out"},
		{"--startup-sign-out", "--startup-client-info", "secret"},
		{"--print-startup-config", "--startup-client-info=secret", "--startup-sign-out=secret"},
		{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", "--print-startup-config", "--startup-client-info", `{"name":"codex_desktop","title":"Codex Desktop","version":"26.1029.7803.0"}`, "--startup-sign-out"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
			if code != 2 || output.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: "+errUnsupportedStartupSignOut.Error()+"\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

func TestArgumentLimitsApplyBeforeInspection(t *testing.T) {
	flag := "--print-startup-config"
	atCountLimit := make([]string, maxArgumentCount)
	atCountLimit[0] = flag
	for _, tt := range []struct {
		name string
		args []string
		err  error
	}{
		{name: "count limit", args: atCountLimit, err: errUnsupportedStartupInspection},
		{name: "count exceeded", args: append(append([]string(nil), atCountLimit...), ""), err: errUnsupportedArguments},
		{name: "byte limit", args: []string{flag, strings.Repeat("x", maxArgumentBytes-len(flag))}, err: errUnsupportedStartupInspection},
		{name: "byte limit exceeded", args: []string{flag, strings.Repeat("x", maxArgumentBytes-len(flag)+1)}, err: errUnsupportedArguments},
		{name: "single argument exceeded", args: []string{strings.Repeat("x", maxArgumentBytes+1)}, err: errUnsupportedArguments},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseArgs(tt.args)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error=%v, want %v", err, tt.err)
			}
			var output bufferCloser
			var diagnostics bytes.Buffer
			code := execute(context.Background(), tt.args, nil, &output, &diagnostics, dependencies{})
			if code != 2 || output.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: "+tt.err.Error()+"\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

func TestHelpAndVersionNeedNoConfiguration(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"-V"}, {"app-server", "--help"}, {"app-server", "-h"}} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
		if code != 0 || diagnostics.Len() != 0 || !strings.HasPrefix(output.String(), identity) {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, output.String(), diagnostics.String())
		}
	}
}

func TestConfigPathMustBeExplicitAndAbsolute(t *testing.T) {
	for _, path := range []string{"", "bridge.json", "./bridge.json", "../bridge.json", "~/bridge.json", "C:bridge.json"} {
		for _, args := range [][]string{{"app-server"}, {"--check"}} {
			var output bufferCloser
			var diagnostics bytes.Buffer
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{getenv: func(string) string { return path }})
			if code != 2 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "absolute") {
				t.Fatalf("path=%q code=%d", path, code)
			}
		}
	}
}

func TestLoadFailureNeverStartsChildrenOrLeaksDetails(t *testing.T) {
	for _, args := range [][]string{{"app-server"}, {"--check"}} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{
			getenv: func(string) string {
				return filepath.Join(t.TempDir(), "secret.json")
			},
			load: func(string) (appserverbridge.Config, error) {
				return appserverbridge.Config{}, errors.New("private secret")
			},
		})
		if code != 2 || output.Len() != 0 || strings.Contains(diagnostics.String(), "secret") {
			t.Fatalf("code=%d stderr=%q", code, diagnostics.String())
		}
	}
}

// These inert bytes satisfy the configuration's native-file inspection. They
// are never executed: check must stop before the run dependency is called.
func TestCheckLoadsRealConfigWithoutStartingChildrenOrChangingState(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "inert.exe")
	if err := os.WriteFile(command, []byte("MZinert-test-fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appserverbridge.Config{}
	for _, id := range []string{"first", "second"} {
		home := filepath.Join(root, id+"-home")
		codex := filepath.Join(root, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{ID: id, Provider: "openai", Command: command, Home: home, CodexHome: codex})
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(context.Background(), []string{"--check"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string { return path },
		load:   loadConfig,
	})
	if code != 0 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "no children started") {
		t.Fatalf("code=%d stderr=%q", code, diagnostics.String())
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("check changed fixture state")
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestInvalidRealConfigsAreReadOnly(t *testing.T) {
	for _, contents := range []string{"", "{}", "null", "{broken", `{"backends":[],"unknown":"secret"}`, strings.Repeat("x", (1<<20)+1)} {
		root := t.TempDir()
		path := filepath.Join(root, "bridge.json")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, root)
		if _, err := loadConfig(path); err == nil {
			t.Fatal("accepted invalid configuration")
		}
		if !reflect.DeepEqual(before, snapshot(t, root)) {
			t.Fatal("invalid configuration changed state")
		}
	}
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing.json")} {
		if _, err := loadConfig(path); err == nil {
			t.Fatal("accepted non-file configuration")
		}
	}
}

func TestServerErrorsOnlyUseGenericStderr(t *testing.T) {
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(context.Background(), []string{"app-server"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string {
			return filepath.Join(t.TempDir(), "bridge.json")
		},
		load: func(string) (appserverbridge.Config, error) {
			return appserverbridge.Config{}, nil
		},
		run: func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			return errors.New("secret payload")
		},
	})
	if code != 1 || output.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: app-server bridge failed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
	}
}

func TestCanceledContextDoesNotStartChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(ctx, []string{"app-server"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string {
			return filepath.Join(t.TempDir(), "bridge.json")
		},
		load: func(string) (appserverbridge.Config, error) {
			return appserverbridge.Config{}, nil
		},
	})
	if code != 0 || output.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatal("cancellation produced an error or stdout")
	}
}

func TestNativeAdapterInitializesAndReapsBothChildrenOnEOF(t *testing.T) {
	testAdapterLifecycle(t, "", []string{"app-server", "--stdio"}, []string{"app-server"})
}

func TestNativeAdapterDesktopArguments(t *testing.T) {
	testDesktopArgumentLifecycle(t, "")
}

func TestBuiltAdapterStdio(t *testing.T) {
	testAdapterLifecycle(t, builtAdapterPath(t), []string{"app-server", "--stdio"}, []string{"app-server"})
}

func TestBuiltAdapterDesktopArguments(t *testing.T) {
	testDesktopArgumentLifecycle(t, builtAdapterPath(t))
}

func builtAdapterPath(t *testing.T) string {
	t.Helper()
	adapter := os.Getenv("MAGPIE_BRIDGE_ADAPTER_TEST_EXE")
	if adapter == "" {
		t.Skip("set MAGPIE_BRIDGE_ADAPTER_TEST_EXE to a built native adapter to exercise main")
	}
	if !filepath.IsAbs(adapter) {
		t.Fatal("MAGPIE_BRIDGE_ADAPTER_TEST_EXE must be absolute")
	}
	info, err := os.Stat(adapter)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("MAGPIE_BRIDGE_ADAPTER_TEST_EXE must name an existing native adapter")
	}
	return adapter
}

func testDesktopArgumentLifecycle(t *testing.T, adapter string) {
	t.Helper()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "installed Desktop argv", args: []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"}},
		{name: "override after app-server", args: []string{"app-server", "--analytics-default-enabled", "-c", "features.code_mode_host=true"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// The child uses the structured enabled field so the Desktop scalar
			// override does not replace sibling code-mode safety settings.
			prefix := []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled"}
			testAdapterLifecycle(t, adapter, tt.args, prefix)
		})
	}
}

func testAdapterLifecycle(t *testing.T, adapter string, args, wantChildPrefix []string) {
	t.Helper()
	root := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(root, "native-fixture.exe")
	if err := os.WriteFile(command, binary, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appserverbridge.Config{}
	for _, id := range []string{"first", "second"} {
		home := filepath.Join(root, id+"-home")
		codex := filepath.Join(root, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		// A synthetic pre-existing restrictive setting must survive the
		// normalized enabled override. No real Codex config is read here.
		policy := fixtureCodeModePolicy{Enabled: false, DisableInProcessFallback: true}
		policyBytes, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "synthetic-code-mode-policy.json"), policyBytes, 0600); err != nil {
			t.Fatal(err)
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{ID: id, Provider: "fixture-" + id, Command: command, Home: home, CodexHome: codex})
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configEnv, path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	var diagnostics bytes.Buffer
	finished := make(chan struct{})
	code := -1
	go func() {
		defer close(finished)
		defer inputReader.Close()
		defer outputWriter.Close()
		if adapter == "" {
			code = execute(ctx, args, inputReader, outputWriter, &diagnostics, dependencies{
				getenv: os.Getenv,
				load:   loadConfig,
				run:    appserverbridge.RunWithOptions,
			})
			return
		}
		command := proc.CommandContext(ctx, adapter, args...)
		command.Env = os.Environ()
		command.Dir = root
		command.Stdin = inputReader
		command.Stdout = outputWriter
		command.Stderr = &diagnostics
		tree, err := proc.StartTreeStrict(command)
		if err != nil {
			fmt.Fprintln(&diagnostics, "cannot start built adapter:", err)
			return
		}
		defer func() { <-tree.Stop(time.Second) }()
		if err := tree.Wait(); err != nil {
			fmt.Fprintln(&diagnostics, "built adapter failed:", err)
			return
		}
		code = 0
	}()
	// A dedicated reader drains stdout so EOF cleanup cannot deadlock on a
	// blocked output pipe. Both goroutines are joined on every failure path.
	readDone := make(chan struct{})
	lines := make(chan string, 1)
	var transcript bytes.Buffer
	var readErr error
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(outputReader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(&transcript, line)
			select {
			case lines <- line:
			default:
			}
		}
		readErr = scanner.Err()
	}()
	t.Cleanup(func() {
		cancel()
		inputWriter.Close()
		outputReader.Close()
		inputReader.Close()
		outputWriter.Close()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("adapter did not stop and reap its children")
		}
		<-readDone
	})
	if _, err := fmt.Fprintln(inputWriter, `{"id":42,"method":"initialize","params":{"clientInfo":{"name":"adapter-test","version":"1"}}}`); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-lines:
		var response struct {
			ID     int               `json:"id"`
			Result map[string]string `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout was not JSONL: %q: %v", line, err)
		}
		want := map[string]string{"userAgent": "native-adapter-fixture", "platformFamily": "fixture", "platformOs": "fixture"}
		if response.ID != 42 || !reflect.DeepEqual(response.Result, want) {
			t.Fatalf("unexpected initialize response: %q", line)
		}
	case <-finished:
		t.Fatal("adapter exited before its initialization response")
	case <-ctx.Done():
		t.Fatal("adapter initialization timed out")
	}
	if _, err := fmt.Fprintln(inputWriter, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("adapter did not finish on client EOF")
	}
	<-readDone
	if code != 0 || diagnostics.Len() != 0 || readErr != nil || strings.Count(transcript.String(), "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q read=%v", code, transcript.String(), diagnostics.String(), readErr)
	}
	for _, backend := range cfg.Backends {
		recorded, err := os.ReadFile(filepath.Join(backend.Home, "received-argv.json"))
		if err != nil {
			t.Fatalf("backend %s did not record child arguments: %v", backend.ID, err)
		}
		var childArgs []string
		if err := json.Unmarshal(recorded, &childArgs); err != nil {
			t.Fatalf("backend %s recorded invalid child arguments: %v", backend.ID, err)
		}
		wantArgs := append(append([]string(nil), wantChildPrefix...), "-c", fmt.Sprintf("model_provider=%q", backend.Provider))
		if !reflect.DeepEqual(childArgs, wantArgs) {
			t.Fatalf("backend %s arguments=%q, want %q", backend.ID, childArgs, wantArgs)
		}
		policyBytes, err := os.ReadFile(filepath.Join(backend.Home, "received-code-mode-policy.json"))
		if err != nil {
			t.Fatalf("backend %s did not record synthetic policy: %v", backend.ID, err)
		}
		var policy fixtureCodeModePolicy
		if err := json.Unmarshal(policyBytes, &policy); err != nil {
			t.Fatalf("backend %s recorded invalid synthetic policy: %v", backend.ID, err)
		}
		wantPolicy := fixtureCodeModePolicy{DisableInProcessFallback: true}
		for _, arg := range wantChildPrefix {
			if arg == "features.code_mode_host.enabled=true" {
				wantPolicy.Enabled = true
			}
		}
		if policy != wantPolicy {
			t.Fatalf("backend %s synthetic policy=%+v, want %+v", backend.ID, policy, wantPolicy)
		}
		marker, err := os.ReadFile(filepath.Join(backend.Home, "clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatalf("backend %s did not exit cleanly on EOF: %q, %v", backend.ID, marker, err)
		}
	}
}
