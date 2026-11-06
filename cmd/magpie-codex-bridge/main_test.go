package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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
	for _, arg := range os.Args[1:] {
		if arg == "--print-startup-config" {
			return fixtureStartupProbe(os.Args[1:])
		}
	}
	if handled, code := authStatusFixture(); handled {
		return code
	}
	if handled, code := authCompatFixture(); handled {
		return code
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
		if request.Method == "configRequirements/read" {
			if handled, code := fixtureRequirementsRead(request.ID, scanner.Bytes()); handled && code != 0 {
				return code
			}
		}
		if request.Method == "initialize" {
			if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "fixture-hold-initialize")); err == nil {
				// The error-path test releases this sibling only after observing
				// the other child's error, forcing the otherwise racy ordering.
				if os.WriteFile(filepath.Join(os.Getenv("HOME"), "fixture-initialize-held"), nil, 0600) != nil {
					return 15
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := waitAdapterFixtureFile(ctx, filepath.Join(os.Getenv("HOME"), "fixture-initialize-release"))
				cancel()
				if err != nil {
					return 16
				}
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "synthetic-plugin-error")); err == nil {
				response := map[string]any{"id": request.ID, "error": json.RawMessage(desktopPluginFixtureError)}
				if json.NewEncoder(os.Stdout).Encode(response) != nil {
					return 4
				}
				if os.WriteFile(filepath.Join(os.Getenv("HOME"), "fixture-initialize-written"), nil, 0600) != nil {
					return 17
				}
				continue
			}
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
			if os.WriteFile(filepath.Join(os.Getenv("HOME"), "fixture-initialize-written"), nil, 0600) != nil {
				return 17
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

// waitAdapterFixtureFile waits for an explicit process/test acknowledgement, not an
// elapsed-time assumption. The context is only a last-resort failure bound.
func waitAdapterFixtureFile(ctx context.Context, path string) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// The one-shot fixture never opens credentials or a service. Its controlled
// response is read only from the private fixture home prepared by the test.
func fixtureStartupProbe(args []string) int {
	var clientInfo string
	for i, arg := range args {
		if arg == "--startup-client-info" && i+1 < len(args) {
			clientInfo = args[i+1]
		}
	}
	if clientInfo == "" || os.WriteFile(filepath.Join(os.Getenv("HOME"), "received-startup-client-info.json"), []byte(clientInfo), 0600) != nil {
		return 10
	}
	// The one-shot path must not receive initialize or other JSONL traffic.
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 1))
	if err != nil || len(input) != 0 {
		return 11
	}
	response, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "synthetic-startup-response.json"))
	if err != nil {
		return 12
	}
	fmt.Fprintln(os.Stderr, "fixture-secret-stderr-canary")
	if _, err := os.Stdout.Write(response); err != nil {
		return 13
	}
	if os.WriteFile(filepath.Join(os.Getenv("HOME"), "clean-exit"), []byte("startup"), 0600) != nil {
		return 14
	}
	return 0
}

const desktopStartupClientInfo = `{"name":"codex_desktop","title":"Codex Desktop","version":"26.1029.7803.0"}`
const fixtureStartupSuccess = `{"appServerVersion":"0.166.0-alpha.6","configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true}}`

func desktopStartupArgs() []string {
	return []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", "--print-startup-config", "--startup-client-info", desktopStartupClientInfo}
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

func TestStartupSignOutDoesNotReadConfiguration(t *testing.T) {
	flag := "--startup-sign-out"
	for _, args := range [][]string{
		{flag}, {flag, "secret"}, {flag + "=secret"},
		{flag, "app-server"}, {"app-server", flag},
		{"-c", "features.code_mode_host=true", "app-server", flag, "--analytics-default-enabled"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			// Nil dependencies prove neither environment nor configuration is accessed.
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
			want := "magpie-codex-bridge: " + errUnsupportedStartupSignOut.Error() + "\n"
			if code != 2 || output.Len() != 0 || diagnostics.String() != want {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

func TestStartupProbeDispatch(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    []string
		options appserverbridge.LaunchOptions
	}{
		{name: "observed Desktop argv", args: desktopStartupArgs(), options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
		{name: "plain app-server", args: []string{"app-server", "--print-startup-config", "--startup-client-info", desktopStartupClientInfo}},
		{name: "client info first", args: []string{"app-server", "--startup-client-info", desktopStartupClientInfo, "--print-startup-config"}},
		{name: "controls after probe", args: []string{"app-server", "--print-startup-config", "--startup-client-info", desktopStartupClientInfo, "--config", "features.code_mode_host=true", "--analytics-default-enabled"}, options: appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bridge.json")
			cfg := appserverbridge.Config{StartupProbe: appserverbridge.StartupProbeCompareUnrestricted}
			title := "Codex Desktop"
			wantClientInfo := appserverbridge.StartupClientInfo{Name: "codex_desktop", Title: &title, Version: "26.1029.7803.0"}
			ctx := context.WithValue(context.Background(), struct{}{}, "startup-sentinel")
			var output bufferCloser
			var diagnostics bytes.Buffer
			var calls []string
			code := execute(ctx, tt.args, nil, &output, &diagnostics, dependencies{
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
				probe: func(got context.Context, config appserverbridge.Config, options appserverbridge.LaunchOptions, clientInfo appserverbridge.StartupClientInfo) ([]byte, error) {
					calls = append(calls, "probe")
					if got != ctx || !reflect.DeepEqual(config, cfg) || options != tt.options || !reflect.DeepEqual(clientInfo, wantClientInfo) {
						t.Fatal("probe inputs changed")
					}
					return []byte(fixtureStartupSuccess), nil
				},
				// A nil run dependency deliberately panics if JSONL dispatch occurs.
			})
			if code != 0 || diagnostics.Len() != 0 || output.String() != fixtureStartupSuccess+"\n" || !reflect.DeepEqual(calls, []string{"env", "load", "probe"}) {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, output.String(), diagnostics.String(), calls)
			}
		})
	}
}

func TestStartupProbeFailuresAreFixedAndDoNotLeak(t *testing.T) {
	const canary = "private-secret-canary"
	for _, scenario := range []string{"missing path", "relative path", "load failure", "disabled", "unknown policy", "already canceled", "probe failure", "probe canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "already canceled" {
				cancel()
			}
			var output bufferCloser
			var diagnostics bytes.Buffer
			probeCalled := false
			code := execute(ctx, desktopStartupArgs(), nil, &output, &diagnostics, dependencies{
				getenv: func(string) string {
					if scenario == "missing path" {
						return ""
					}
					if scenario == "relative path" {
						return canary + ".json"
					}
					return filepath.Join(t.TempDir(), canary+".json")
				},
				load: func(string) (appserverbridge.Config, error) {
					cfg := appserverbridge.Config{StartupProbe: appserverbridge.StartupProbeCompareUnrestricted}
					switch scenario {
					case "missing path", "relative path":
						t.Fatal("invalid path reached config load")
					case "load failure":
						return cfg, errors.New(canary)
					case "disabled":
						cfg.StartupProbe = ""
					case "unknown policy":
						cfg.StartupProbe = canary
					}
					return cfg, nil
				},
				probe: func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, appserverbridge.StartupClientInfo) ([]byte, error) {
					probeCalled = true
					if scenario == "probe canceled" {
						cancel()
						return []byte(canary), context.Canceled
					}
					return []byte(canary), errors.New(canary)
				},
			})
			wantProbe := scenario == "probe failure" || scenario == "probe canceled"
			if code != 1 || output.String() != startupProbeError+"\n" || diagnostics.String() != "magpie-codex-bridge: magpie startup probe unavailable\n" || probeCalled != wantProbe {
				t.Fatalf("code=%d stdout=%q stderr=%q probe=%v", code, output.String(), diagnostics.String(), probeCalled)
			}
			if strings.Contains(output.String()+diagnostics.String(), canary) {
				t.Fatal("probe failure leaked private details")
			}
		})
	}
}

func TestRejectedStartupProbeArgumentsNeverReadConfiguration(t *testing.T) {
	cases := [][]string{
		{"--print-startup-config"}, {"--startup-client-info", desktopStartupClientInfo},
		{"app-server", "--print-startup-config"}, {"app-server", "--startup-client-info", desktopStartupClientInfo},
		{"--print-startup-config", "app-server", "--startup-client-info", desktopStartupClientInfo},
		{"--startup-client-info", desktopStartupClientInfo, "app-server", "--print-startup-config"},
		{"app-server", "--print-startup-config", "--startup-client-info"},
		{"app-server", "--print-startup-config", "--startup-client-info=" + desktopStartupClientInfo},
		{"app-server", "--print-startup-config=true", "--startup-client-info", desktopStartupClientInfo},
	}
	for _, suffix := range [][]string{
		{"--print-startup-config"}, {"--startup-client-info", desktopStartupClientInfo},
		{"--stdio"}, {"--listen", "stdio://"}, {"--listen=stdio://"},
		{"--remote-control"}, {"--managed-daemon"}, {"generate-json-schema"}, {"generate-ts"},
		{"--check"}, {"--help"}, {"--version"}, {"--unknown=private-secret-canary"},
		{"-c", "features.code_mode_host=true"}, {"--analytics-default-enabled"},
	} {
		cases = append(cases, append(desktopStartupArgs(), suffix...))
	}
	for _, transport := range [][]string{{"--stdio"}, {"--listen", "stdio://"}, {"--listen=stdio://"}} {
		args := append([]string{"app-server"}, transport...)
		cases = append(cases, append(args, "--print-startup-config", "--startup-client-info", desktopStartupClientInfo))
	}
	for _, info := range []string{
		"", "null", "[]", "{}", "{broken", desktopStartupClientInfo + " {}",
		`{"name":"codex_desktop","version":"1","extra":"private-secret-canary"}`,
		`{"name":"first","name":"second","version":"1"}`,
		`{"name":"codex_desktop","version":1}`,
		`{"name":"codex_desktop","version":"1","title":false}`,
		`{"name":"codex_desktop\nprivate-secret-canary","version":"1"}`,
		`{"name":"codex_desktop","version":"1\u0000private-secret-canary"}`,
		strings.Repeat("x", maxArgumentBytes+1),
	} {
		cases = append(cases, []string{"app-server", "--print-startup-config", "--startup-client-info", info})
	}
	for i, args := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			// Nil dependencies prove rejection precedes even environment reads.
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
			if code != 2 || output.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: "+errUnsupportedArguments.Error()+"\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

// The combined argv reflects the installed launcher's append construction,
// not a captured Desktop launch or startup-probe compatibility test.
func TestStartupSignOutTakesPrecedenceOverInspection(t *testing.T) {
	for _, args := range [][]string{
		{"--print-startup-config", "--startup-sign-out"},
		{"--startup-sign-out", "--print-startup-config"},
		append(make([]string, maxArgumentCount+1), "--startup-sign-out"),
		{strings.Repeat("x", maxArgumentBytes+1), "--startup-sign-out"},
		{"--unknown=private-secret-canary", "--startup-sign-out"},
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

func TestArgumentLimitsRejectBeforeConfiguration(t *testing.T) {
	flag := "--print-startup-config"
	atCountLimit := make([]string, maxArgumentCount)
	atCountLimit[0] = flag
	for _, tt := range []struct {
		name string
		args []string
		err  error
	}{
		{name: "count limit", args: atCountLimit, err: errUnsupportedArguments},
		{name: "count exceeded", args: append(append([]string(nil), atCountLimit...), ""), err: errUnsupportedArguments},
		{name: "byte limit", args: []string{flag, strings.Repeat("x", maxArgumentBytes-len(flag))}, err: errUnsupportedArguments},
		{name: "byte limit exceeded", args: []string{flag, strings.Repeat("x", maxArgumentBytes-len(flag)+1)}, err: errUnsupportedArguments},
		{name: "single argument exceeded", args: []string{strings.Repeat("x", maxArgumentBytes+1)}, err: errUnsupportedArguments},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseArgs(tt.args)
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

func TestStartupProbeArgumentByteLimit(t *testing.T) {
	base := []string{"app-server", "--print-startup-config", "--startup-client-info", `{"name":"desktop","version":"1"}`}
	total := 0
	for _, arg := range base {
		total += len(arg)
	}
	for _, size := range []int{maxArgumentBytes - 1, maxArgumentBytes, maxArgumentBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			args := append([]string(nil), base...)
			args[len(args)-1] += strings.Repeat(" ", size-total)
			op, _, _, err := parseArgs(args)
			if size <= maxArgumentBytes {
				if err != nil || op != startupProbe {
					t.Fatalf("valid bounded probe rejected: operation=%v err=%v", op, err)
				}
				return
			}
			if !errors.Is(err, errUnsupportedArguments) {
				t.Fatal("oversized valid probe was accepted")
			}
			var output bufferCloser
			var diagnostics bytes.Buffer
			if code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{}); code != 2 || output.Len() != 0 {
				t.Fatal("oversized probe reached configuration or wrote stdout")
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

func TestBuiltAdapterStartupProbe(t *testing.T) {
	adapter := builtAdapterPath(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	for _, profile := range []struct {
		mode     string
		response string
		identity string
		other    string
	}{
		{appserverbridge.StartupProbeCompareUnrestricted, fixtureStartupSuccess, desktopStartupClientInfo, fixtureStartupSuccess167},
		{startupProbe167Mode, fixtureStartupSuccess167, desktopStartupClientInfo167, fixtureStartupSuccess},
	} {
		t.Run(profile.mode, func(t *testing.T) {
			for _, tt := range []struct {
				name         string
				enabled      bool
				signOut      bool
				badDigest    bool
				secondResult string
				wantChildren bool
				wantCode     int
				wantOutput   string
			}{
				{name: "enabled success", enabled: true, wantChildren: true, wantOutput: profile.response + "\n"},
				{name: "default disabled", wantCode: 1, wantOutput: startupProbeError + "\n"},
				{name: "sign out before invalid config", enabled: true, signOut: true, wantCode: 2},
				{name: "second digest mismatch starts neither", enabled: true, badDigest: true, wantCode: 1, wantOutput: startupProbeError + "\n"},
				{name: "restricted second result fails closed", enabled: true, secondResult: strings.Replace(profile.response, `"requirements":null`, `"requirements":{"secret":"fixture-secret-stdout-canary"}`, 1), wantChildren: true, wantCode: 1, wantOutput: startupProbeError + "\n"},
				{name: "other profile result fails closed", enabled: true, secondResult: profile.other, wantChildren: true, wantCode: 1, wantOutput: startupProbeError + "\n"},
				{name: "unknown second field fails closed", enabled: true, secondResult: strings.Replace(profile.response, "{", `{"secret":"fixture-secret-stdout-canary",`, 1), wantChildren: true, wantCode: 1, wantOutput: startupProbeError + "\n"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					root := t.TempDir()
					commandPath := filepath.Join(root, "native-probe-fixture.exe")
					if err := os.WriteFile(commandPath, binary, 0700); err != nil {
						t.Fatal(err)
					}
					cfg := appserverbridge.Config{}
					if tt.enabled {
						cfg.StartupProbe = profile.mode
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
						if err := os.WriteFile(filepath.Join(home, "synthetic-code-mode-policy.json"), policy, 0600); err != nil {
							t.Fatal(err)
						}
						response := profile.response + "\n"
						if id == "second" && tt.secondResult != "" {
							response = tt.secondResult + "\n"
						}
						if err := os.WriteFile(filepath.Join(home, "synthetic-startup-response.json"), []byte(response), 0600); err != nil {
							t.Fatal(err)
						}
						backend := appserverbridge.Backend{ID: id, Provider: "fixture-" + id, Command: commandPath, Home: home, CodexHome: codex}
						if tt.enabled {
							backend.StartupProbeSHA256 = digest
						}
						if id == "second" && tt.badDigest {
							backend.StartupProbeSHA256 = strings.Repeat("0", 64)
						}
						cfg.Backends = append(cfg.Backends, backend)
					}
					data, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					if tt.signOut {
						data = []byte("invalid-config-fixture-secret-canary")
					}
					configPath := filepath.Join(root, "bridge.json")
					if err := os.WriteFile(configPath, data, 0600); err != nil {
						t.Fatal(err)
					}
					t.Setenv(configEnv, configPath)
					var before map[string]string
					if !tt.wantChildren {
						before = snapshot(t, root)
					}
					args := desktopStartupArgs()
					args[len(args)-1] = profile.identity
					if tt.signOut {
						args = append(args, "--startup-sign-out")
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					command := proc.CommandContext(ctx, adapter, args...)
					command.Env = os.Environ()
					command.Dir = root
					// A nil stdin is intentional: a one-shot operation has no initialize exchange.
					var output, diagnostics bytes.Buffer
					command.Stdout = &output
					command.Stderr = &diagnostics
					tree, err := proc.StartTreeStrict(command)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { <-tree.Stop(time.Second) }()
					waitErr := tree.Wait()
					if ctx.Err() != nil || command.ProcessState == nil {
						t.Fatalf("one-shot adapter did not exit: %v", waitErr)
					}
					code := command.ProcessState.ExitCode()
					if code != tt.wantCode || (waitErr == nil) != (code == 0) || output.String() != tt.wantOutput {
						t.Fatalf("code=%d want=%d stdout=%q stderr=%q wait=%v", code, tt.wantCode, output.String(), diagnostics.String(), waitErr)
					}
					wantDiagnostics := ""
					if tt.signOut {
						wantDiagnostics = "magpie-codex-bridge: " + errUnsupportedStartupSignOut.Error() + "\n"
					} else if tt.wantCode != 0 {
						wantDiagnostics = "magpie-codex-bridge: magpie startup probe unavailable\n"
					}
					if diagnostics.String() != wantDiagnostics || strings.Contains(output.String()+diagnostics.String(), "canary") {
						t.Fatalf("unsafe or unexpected output: stdout=%q stderr=%q", output.String(), diagnostics.String())
					}
					if !tt.wantChildren {
						if !reflect.DeepEqual(before, snapshot(t, root)) {
							t.Fatal("rejected startup probe started children or changed fixture state")
						}
						return
					}
					for _, backend := range cfg.Backends {
						recorded, err := os.ReadFile(filepath.Join(backend.Home, "received-argv.json"))
						if err != nil {
							t.Fatal(err)
						}
						var childArgs []string
						if err := json.Unmarshal(recorded, &childArgs); err != nil {
							t.Fatal(err)
						}
						wantArgs := []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled", "-c", fmt.Sprintf("model_provider=%q", backend.Provider), "--print-startup-config", "--startup-client-info", profile.identity}
						if !reflect.DeepEqual(childArgs, wantArgs) {
							t.Fatalf("backend %s arguments=%q want=%q", backend.ID, childArgs, wantArgs)
						}
						info, err := os.ReadFile(filepath.Join(backend.Home, "received-startup-client-info.json"))
						if err != nil || string(info) != profile.identity {
							t.Fatalf("backend %s client info=%q err=%v", backend.ID, info, err)
						}
						policyBytes, err := os.ReadFile(filepath.Join(backend.Home, "received-code-mode-policy.json"))
						if err != nil {
							t.Fatal(err)
						}
						var policy fixtureCodeModePolicy
						if err := json.Unmarshal(policyBytes, &policy); err != nil || policy != (fixtureCodeModePolicy{Enabled: true, DisableInProcessFallback: true}) {
							t.Fatalf("backend %s structured policy changed: %s", backend.ID, policyBytes)
						}
						marker, err := os.ReadFile(filepath.Join(backend.Home, "clean-exit"))
						if err != nil || string(marker) != "startup" {
							t.Fatalf("backend %s did not finish the one-shot probe: %q, %v", backend.ID, marker, err)
						}
					}
				})
			}
		})
	}
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
	testAdapterLifecycleConfig(t, adapter, args, wantChildPrefix, nil)
}

func testAdapterLifecycleConfig(t *testing.T, adapter string, args, wantChildPrefix []string, configure func(*appserverbridge.Config)) {
	t.Helper()
	testAdapterLifecycleCase(t, adapter, args, wantChildPrefix, configure, false)
}

func testAdapterLifecycleCase(t *testing.T, adapter string, args, wantChildPrefix []string, configure func(*appserverbridge.Config), initializeError bool) {
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
	if configure != nil {
		configure(&cfg)
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
			Error  json.RawMessage   `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout was not JSONL: %q: %v", line, err)
		}
		want := map[string]string{"userAgent": "native-adapter-fixture", "platformFamily": "fixture", "platformOs": "fixture"}
		valid := response.ID == 42 && reflect.DeepEqual(response.Result, want) && len(response.Error) == 0
		if initializeError {
			valid = response.ID == 42 && response.Result == nil && string(response.Error) == desktopPluginFixtureError
		}
		if !valid {
			t.Fatalf("unexpected initialize response: %q", line)
		}
	case <-finished:
		t.Fatal("adapter exited before its initialization response")
	case <-ctx.Done():
		t.Fatal("adapter initialization timed out")
	}
	if initializeError {
		// A failed fanout may reach the client before its sibling has written
		// a response. Client EOF closes child stdout immediately, so establish
		// successful writes before asserting that both fixtures take EOF.
		held := filepath.Join(cfg.Backends[0].Home, "fixture-initialize-held")
		if err := waitAdapterFixtureFile(ctx, held); err != nil {
			t.Fatalf("sibling did not reach the held initialize response: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cfg.Backends[0].Home, "fixture-initialize-written")); !os.IsNotExist(err) {
			t.Fatal("sibling response was not held until the native error reached the client")
		}
		release := filepath.Join(cfg.Backends[0].Home, "fixture-initialize-release")
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Fatal(err)
		}
		for _, backend := range cfg.Backends {
			if err := waitAdapterFixtureFile(ctx, filepath.Join(backend.Home, "fixture-initialize-written")); err != nil {
				t.Fatalf("backend %s did not finish its initialize response before client EOF: %v", backend.ID, err)
			}
		}
	}
	if !initializeError {
		if _, err := fmt.Fprintln(inputWriter, `{"method":"initialized"}`); err != nil {
			t.Fatal(err)
		}
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
