package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

func TestDesktopPluginMalformedArgumentsArePrivateAndPreConfig(t *testing.T) {
	var cases [][]string
	for _, value := range []string{
		strings.Replace(desktopPluginLiteral, "=true", "=false", 1),
		strings.Replace(desktopPluginLiteral, "=true", "=TRUE", 1),
		strings.Replace(desktopPluginLiteral, "=true", "=\"true\"", 1),
		strings.Replace(desktopPluginLiteral, "=true", " = true", 1),
		strings.Replace(desktopPluginLiteral, "plugins.", "Plugins.", 1),
		strings.Replace(desktopPluginLiteral, "codex_app", "Codex_App", 1),
		strings.Replace(desktopPluginLiteral, "codex-app-tools", "other-plugin", 1),
		strings.Replace(desktopPluginLiteral, "@openai-bundled", "@other-marketplace", 1),
		strings.Replace(desktopPluginLiteral, "mcp_servers.codex_app.enabled=true", "enabled=true", 1),
		strings.Replace(desktopPluginLiteral, "codex_app.enabled=true", "other_server.enabled=true", 1),
		strings.Replace(desktopPluginLiteral, "enabled=true", "enabled={value=true}", 1),
		strings.Replace(desktopPluginLiteral, "plugins.", "plugins.\"", 1),
		desktopPluginLiteral + " ", " " + desktopPluginLiteral,
		desktopPluginLiteral + "\n", desktopPluginLiteral + "\x00",
		desktopPluginLiteral + "\nmodel_provider=private-canary",
	} {
		for _, flag := range []string{"-c", "--config"} {
			cases = append(cases, []string{flag, value, "app-server"}, []string{"app-server", flag, value})
		}
	}
	for _, first := range []string{"-c", "--config"} {
		for _, second := range []string{"-c", "--config"} {
			cases = append(cases, []string{first, desktopPluginLiteral, "app-server", second, desktopPluginLiteral})
		}
	}
	cases = append(cases,
		[]string{"-c", desktopPluginLiteral},
		[]string{"app-server", "--config=" + desktopPluginLiteral},
		[]string{"app-server", "-c" + desktopPluginLiteral},
		append(desktopStartupArgs(), "-c", desktopPluginLiteral),
		append([]string{"-c", desktopPluginLiteral}, desktopStartupArgs()...),
	)
	for _, args := range cases {
		var out bufferCloser
		var diagnostics bytes.Buffer
		// Nil dependencies prove that even an environment read is forbidden.
		code := execute(context.Background(), args, nil, &out, &diagnostics, dependencies{})
		if code != 2 || out.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: "+errUnsupportedArguments.Error()+"\n" {
			t.Fatal("malformed plugin argument was accepted, read config or exposed input")
		}
	}
}

func TestDesktopPluginSignOutPrecedesAllConfiguration(t *testing.T) {
	for _, args := range [][]string{
		{"-c", desktopPluginLiteral, "app-server", "--startup-sign-out"},
		{"app-server", "--config", desktopPluginLiteral, "--startup-sign-out=private-canary"},
		{"-c", "private-canary", "app-server", "--startup-sign-out", "-c", desktopPluginLiteral},
		append(desktopStartupArgs(), "-c", desktopPluginLiteral, "--startup-sign-out"),
	} {
		var out bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), args, nil, &out, &diagnostics, dependencies{})
		if code != 2 || out.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: "+errUnsupportedStartupSignOut.Error()+"\n" {
			t.Fatal("sign-out reached configuration or exposed arguments")
		}
	}
}

func TestDesktopPluginRunFailuresUseOnlyGenericDiagnostic(t *testing.T) {
	var out bufferCloser
	var diagnostics bytes.Buffer
	path := filepath.Join(t.TempDir(), "private-canary.json")
	calls := 0
	code := execute(context.Background(), []string{"app-server", "-c", desktopPluginLiteral}, nil, &out, &diagnostics, dependencies{
		getenv: func(string) string { return path },
		load: func(string) (appserverbridge.Config, error) {
			return appserverbridge.Config{}, nil
		},
		run: func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			calls++
			return fmt.Errorf("private-canary: backend path, digest and raw plugin error")
		},
	})
	if code != 1 || calls != 1 || out.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: app-server bridge failed\n" {
		t.Fatal("launch failure leaked private backend diagnostic")
	}
}

func TestDesktopPluginNativeChildrenReceiveExactOverride(t *testing.T) {
	testDesktopPluginLifecycle(t, "")
}

func TestBuiltAdapterDesktopPluginOverride(t *testing.T) {
	testDesktopPluginLifecycle(t, builtAdapterPath(t))
}

const desktopPluginFixtureError = `{"code":-32000,"message":"synthetic plugin unavailable"}`

func testDesktopPluginLifecycle(t *testing.T, adapter string) {
	t.Helper()
	for _, requested := range []bool{false, true} {
		t.Run(fmt.Sprintf("requested-%t", requested), func(t *testing.T) {
			args := []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"}
			want := []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled"}
			if requested {
				args = append(args, "-c", desktopPluginLiteral)
				want = append(want, "-c", desktopPluginLiteral)
			}
			var config appserverbridge.Config
			testAdapterLifecycleConfig(t, adapter, args, want, func(cfg *appserverbridge.Config) {
				cfg.DesktopPluginOverride = appserverbridge.DesktopPluginCodexAppTools167
				for i := range cfg.Backends {
					data, err := os.ReadFile(cfg.Backends[i].Command)
					if err != nil {
						t.Fatal(err)
					}
					cfg.Backends[i].StartupProbeSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
				}
				config = *cfg
			})
			// Native synthetic fixtures record argv, not official plugin behavior.
			// No launcher config, plugin cache or policy file is populated/edited.
			for _, backend := range config.Backends {
				entries, err := os.ReadDir(backend.CodexHome)
				if err != nil || len(entries) != 0 {
					t.Fatal("launch populated an isolated Codex home")
				}
				data, err := os.ReadFile(filepath.Join(backend.Home, "synthetic-code-mode-policy.json"))
				if err != nil || string(data) != "{\"enabled\":false,\"disable_in_process_fallback\":true}" {
					t.Fatal("launch rewrote pre-existing policy on disk")
				}
			}
		})
	}
	t.Run("native plugin error is not repaired", func(t *testing.T) {
		args := []string{"app-server", "-c", desktopPluginLiteral}
		want := []string{"app-server", "-c", desktopPluginLiteral}
		testAdapterLifecycleCase(t, adapter, args, want, func(cfg *appserverbridge.Config) {
			cfg.DesktopPluginOverride = appserverbridge.DesktopPluginCodexAppTools167
			for i := range cfg.Backends {
				data, err := os.ReadFile(cfg.Backends[i].Command)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Backends[i].StartupProbeSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
			}
			if err := os.WriteFile(filepath.Join(cfg.Backends[1].Home, "synthetic-plugin-error"), []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
		}, true)
	})
}

func TestDesktopPluginHelpExplainsOptInAndEffects(t *testing.T) {
	for _, fragment := range []string{
		desktopPluginLiteral, "desktop_plugin_override=codex-app-tools-v0.167.0-alpha.3.2",
		"default-off", "both SHA-256 pins", "subprocess/network effects",
		"rejected for startup inspection", "cannot mix with legacy v0.166",
	} {
		if !strings.Contains(usage, fragment) {
			t.Fatalf("help omitted reviewed launch boundary %q", fragment)
		}
	}
	op, options, _, err := parseArgs(desktopStartupArgs())
	if err != nil || op != startupProbe || !reflect.DeepEqual(options, appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}) {
		t.Fatal("ordinary startup probe inherited the normal-launch plugin control")
	}
}
