package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func desktopPluginFixtureConfig(t *testing.T) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	cfg.DesktopPluginOverride = DesktopPluginCodexAppTools167
	return cfg
}

func TestDesktopPluginModeExactDefaultOffAndVersionBound(t *testing.T) {
	base := desktopPluginFixtureConfig(t)
	for _, mode := range []string{
		"", DesktopPluginCodexAppTools167, "codex-app-tools-v0.167",
		"codex-app-tools-v0.166", DesktopPluginCodexAppTools167 + " ",
		strings.ToUpper(DesktopPluginCodexAppTools167), "true",
	} {
		cfg := base
		cfg.DesktopPluginOverride = mode
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := LoadConfig(bytes.NewReader(data))
		valid := mode == "" || mode == DesktopPluginCodexAppTools167
		if (err == nil) != valid {
			t.Fatal("unknown or partial launch compatibility mode accepted")
		}
		if valid && got.DesktopPluginOverride != mode {
			t.Fatal("explicit launch compatibility choice changed during load")
		}
		if mode == "" && bytes.Contains(data, []byte("desktop_plugin_override")) {
			t.Fatal("omitted compatibility mode acquired a default")
		}
	}
	// Every same-version setting remains an independent choice.
	for mask := 0; mask < 16; mask++ {
		cfg := base
		if mask&1 != 0 {
			cfg.StartupProbe = StartupProbeCompareUnrestricted167
		}
		if mask&2 != 0 {
			cfg.Attestation = AttestationUnavailable167
		}
		if mask&4 != 0 {
			cfg.GatewayOAuth = GatewayOAuthPrimaryRead167
			cfg.GatewayOAuthProviderName = "Fixture"
		}
		if mask&8 != 0 {
			cfg.RequirementsRead = ConfigRequirementsCompareNull167
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal("independent exact-version mode combination rejected")
		}
	}
	for _, legacy := range []func(*Config){
		func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted },
		func(c *Config) { c.Attestation = AttestationUnavailable },
		func(c *Config) {
			c.GatewayOAuth = GatewayOAuthPrimaryRead
			c.GatewayOAuthProviderName = "Fixture"
		},
		func(c *Config) { c.RequirementsRead = ConfigRequirementsCompareSafe },
	} {
		cfg := base
		legacy(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("new launch mode accepted a legacy 0.166 contract")
		}
		var output discardRequirementsOutput
		if err := RunWithOptions(context.Background(), cfg, LaunchOptions{CodexAppToolsMCPEnabled: true}, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
			t.Fatal("mixed-version launch reached a child")
		}
		assertDesktopPluginHomesEmpty(t, cfg)
	}
}

func assertDesktopPluginHomesEmpty(t *testing.T, cfg Config) {
	t.Helper()
	for _, backend := range cfg.Backends {
		for _, home := range []string{backend.Home, backend.CodexHome} {
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected launch started a child or changed private state")
			}
		}
	}
}

func TestDesktopPluginRunRequiresExplicitModeAndBothPins(t *testing.T) {
	base := desktopPluginFixtureConfig(t)
	for _, mode := range []string{"", "private-canary", DesktopPluginCodexAppTools167} {
		for index := 0; index < 2; index++ {
			for _, pin := range []string{"", "private-canary", strings.Repeat("0", 64)} {
				for _, requested := range []bool{false, true} {
					if mode == "" && !requested {
						continue // An ordinary launch intentionally needs no pin.
					}
					cfg := base
					cfg.Backends = append([]Backend(nil), base.Backends...)
					cfg.DesktopPluginOverride = mode
					cfg.Backends[index].StartupProbeSHA256 = pin
					var output discardRequirementsOutput
					err := RunWithOptions(context.Background(), cfg, LaunchOptions{CodexAppToolsMCPEnabled: requested}, io.NopCloser(strings.NewReader("")), &output)
					if err == nil || len(output.data) != 0 || strings.Contains(err.Error(), "private-canary") {
						t.Fatal("disabled mode or invalid pin was accepted or leaked")
					}
					assertDesktopPluginHomesEmpty(t, cfg)
				}
			}
		}
	}
	// Valid pins cannot substitute for the separate authorization.
	cfg := base
	cfg.DesktopPluginOverride = ""
	var output discardRequirementsOutput
	err := RunWithOptions(context.Background(), cfg, LaunchOptions{CodexAppToolsMCPEnabled: true}, io.NopCloser(strings.NewReader("")), &output)
	if err == nil || len(output.data) != 0 {
		t.Fatal("valid pins enabled the default-off launch mode")
	}
	assertDesktopPluginHomesEmpty(t, cfg)
}

func TestDesktopPluginProbeRejectsDirectOptionBeforeChildren(t *testing.T) {
	cfg := desktopPluginFixtureConfig(t)
	cfg.StartupProbe = StartupProbeCompareUnrestricted167
	info := StartupClientInfo{Name: "codex_desktop", Version: "26.1103.3517.0"}
	for _, configured := range []bool{false, true} {
		selected := cfg
		if !configured {
			selected.DesktopPluginOverride = ""
		}
		// Public entry point must reject before launching any real process.
		got, err := ProbeStartup(context.Background(), selected, LaunchOptions{CodexAppToolsMCPEnabled: true}, info)
		if err != errStartupProbe || got != nil {
			t.Fatal("direct probe accepted a normal-start plugin override")
		}
		assertDesktopPluginHomesEmpty(t, selected)
	}
	// This proves the boundary precedes configuration and digest checks too.
	got, err := ProbeStartup(context.Background(), Config{}, LaunchOptions{CodexAppToolsMCPEnabled: true}, StartupClientInfo{})
	if err != errStartupProbe || got != nil {
		t.Fatal("direct probe did not reject the unsupported option first")
	}
}

func TestDesktopPluginModeDoesNotInjectIntoProbe(t *testing.T) {
	cfg := desktopPluginFixtureConfig(t)
	cfg.StartupProbe = StartupProbeCompareUnrestricted167
	cfg.Backends[1].Provider = "other"
	info := StartupClientInfo{Name: "fixture", Version: "1"}
	identity, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, err := probeStartup(context.Background(), cfg, LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}, info, time.Minute,
		func(_ context.Context, b Backend, _ string, args []string) ([]byte, error) {
			calls++
			want := []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled", "-c", "model_provider=" + string(raw(b.Provider)), "--print-startup-config", "--startup-client-info", string(identity)}
			if !reflect.DeepEqual(args, want) {
				t.Fatal("launch compatibility setting injected a plugin override into inspection")
			}
			return []byte(strings.Replace(probeSuccessFixture, "0.166.0-alpha.6", "0.167.0-alpha.3.2", 1)), nil
		})
	if err != nil || len(got) == 0 || calls != 2 {
		t.Fatal("same-version launch mode changed ordinary one-shot inspection")
	}
	assertDesktopPluginHomesEmpty(t, cfg)
}

func TestDesktopPluginModeGrantsNoAuthOrHumanApprovalCapability(t *testing.T) {
	cfg := Config{DesktopPluginOverride: DesktopPluginCodexAppTools167}
	for _, fields := range []string{
		`"requiredHumanApproval":true`,
		`"requiredHumanApproval":null`,
		`"requiredHumanApproval":true,"requiredHumanApproval":false`,
		`"requestAttestation":true`,
		`"explicitGatewayOauth":true`,
	} {
		r, err := NewRouter([]Backend{{ID: "first", Provider: "openai"}, {ID: "second", Provider: "other"}})
		if err != nil {
			t.Fatal(err)
		}
		r.auth = cfg.authCompatibility()
		out := client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{`+fields+`}}}`)
		expectError(t, out)
		if len(r.pending) != 0 || r.phase != "cold" {
			t.Fatal("launch-only opt-in weakened the capability boundary")
		}
	}
}

func TestDesktopPluginExactChildArgumentsAndIndependentStorage(t *testing.T) {
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true, CodexAppToolsMCPEnabled: true}
	want := []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled", "-c", CodexAppToolsMCPOverride, "-c", `model_provider="other"`}
	first, second := options.backendArgs("openai"), options.backendArgs("other")
	if !reflect.DeepEqual(second, want) {
		t.Fatal("exact plugin override or independent provider selection changed")
	}
	first[5] = "changed"
	if !reflect.DeepEqual(second, want) {
		t.Fatal("child arguments share mutable storage")
	}
	// Config alone never influences argument construction.
	ordinary := (LaunchOptions{}).backendArgs("other")
	if !reflect.DeepEqual(ordinary, []string{"app-server", "-c", `model_provider="other"`}) {
		t.Fatal("ordinary launch unexpectedly acquired a plugin override")
	}
}

func TestDesktopPluginModePreservesAbsentAndFalseHumanApproval(t *testing.T) {
	for _, capabilities := range []string{"{}", `{"requiredHumanApproval":false}`} {
		r, err := NewRouter([]Backend{{ID: "first", Provider: "openai"}, {ID: "second", Provider: "other"}})
		if err != nil {
			t.Fatal(err)
		}
		r.auth = (Config{DesktopPluginOverride: DesktopPluginCodexAppTools167}).authCompatibility()
		params := json.RawMessage(`{"clientInfo":{"name":"fixture","version":"1"},"capabilities":` + capabilities + `}`)
		out := client(t, r, string(raw(object{"id": raw(1), "method": raw("initialize"), "params": params})))
		if len(out) != 2 {
			t.Fatal("launch-only mode rejected an absent or false human-approval capability")
		}
		for _, delivery := range out {
			message, err := parseObject(delivery.Message)
			if err != nil || !sameJSON(message["params"], params) {
				t.Fatal("launch-only mode changed initialization capabilities")
			}
		}
	}
}
