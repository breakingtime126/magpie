package appserverbridge

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStartupProbeConfigurationIsExplicitAndPinned(t *testing.T) {
	cfg := fixtureConfig(t)
	if cfg.StartupProbe != "" {
		t.Fatal("fixture unexpectedly opts in to startup probing")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.StartupProbe = "automatic"
	if err := cfg.Validate(); err == nil {
		t.Fatal("accepted unknown startup probe mode")
	}
	cfg.StartupProbe = StartupProbeCompareUnrestricted
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled startup probe without binary pins")
	}
	cfg.Backends[0].StartupProbeSHA256 = strings.Repeat("a", 64)
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled startup probe without the second binary pin")
	}
	cfg.Backends[1].StartupProbeSHA256 = strings.Repeat("B", 64)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Loading validates only the pin shape, not a claim that the executable
	// matches. ProbeStartup verifies both digests before starting either one.
	loaded, err := LoadConfig(bytes.NewReader(data))
	if err != nil || loaded.StartupProbe != cfg.StartupProbe {
		t.Fatalf("explicit probe configuration failed to load: %v", err)
	}
	for _, pin := range []string{"", "abc", strings.Repeat("x", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		cfg.Backends[1].StartupProbeSHA256 = pin
		if err := cfg.Validate(); err == nil {
			t.Fatal("accepted missing or malformed binary pin")
		}
	}
	for _, backend := range cfg.Backends {
		for _, home := range []string{backend.Home, backend.CodexHome} {
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("config validation changed backend home: %v", err)
			}
		}
	}
}

func TestStartupProbeProfilesRequireBothPins(t *testing.T) {
	base := fixtureConfig(t)
	for _, mode := range []string{StartupProbeCompareUnrestricted, probe167Mode} {
		t.Run(mode, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			cfg.StartupProbe = mode
			for _, pin := range []string{"", "abc", strings.Repeat("x", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
				for _, index := range []int{0, 1} {
					cfg.Backends[0].StartupProbeSHA256 = strings.Repeat("a", 64)
					cfg.Backends[1].StartupProbeSHA256 = strings.Repeat("B", 64)
					cfg.Backends[index].StartupProbeSHA256 = pin
					if err := cfg.Validate(); err == nil {
						t.Fatal("profile accepted a missing or malformed pin")
					}
				}
			}
			cfg.Backends[0].StartupProbeSHA256 = strings.Repeat("a", 64)
			cfg.Backends[1].StartupProbeSHA256 = strings.Repeat("B", 64)
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadConfig(bytes.NewReader(data))
			if err != nil || loaded.StartupProbe != mode {
				t.Fatalf("reviewed profile failed to load: %v", err)
			}
		})
	}
}

func TestStartupProbeProfilesRejectMixedCompatibilityVersions(t *testing.T) {
	base := startupProbeFixtureConfig(t)
	for _, mode := range []string{"", StartupProbeCompareUnrestricted, probe167Mode} {
		for mask := 0; mask < 8; mask++ {
			cfg := base
			cfg.StartupProbe = mode
			if mask&1 != 0 {
				cfg.RequirementsRead = ConfigRequirementsCompareSafe
			}
			if mask&2 != 0 {
				cfg.Attestation = AttestationUnavailable
			}
			if mask&4 != 0 {
				cfg.GatewayOAuth = GatewayOAuthPrimaryRead
				cfg.GatewayOAuthProviderName = "Fixture provider"
			}
			wantValid := mode != probe167Mode || mask == 0
			if err := cfg.Validate(); (err == nil) != wantValid {
				t.Fatalf("mixed-profile validation: mode=%q mask=%d err=%v", mode, mask, err)
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(bytes.NewReader(data)); (err == nil) != wantValid {
				t.Fatal("loading bypassed compatibility version validation")
			}
		}
	}
	for _, mode := range []string{"", probe167Mode} {
		for _, kind := range []string{"requirements", "attestation", "gateway"} {
			cfg := base
			cfg.StartupProbe = mode
			switch kind {
			case "requirements":
				cfg.RequirementsRead = "compare-safe-v0.167.0-alpha.3.2"
			case "attestation":
				cfg.Attestation = "unavailable-v0.167.0-alpha.3.2"
			case "gateway":
				cfg.GatewayOAuth = "primary-read-only-v0.167.0-alpha.3.2"
				cfg.GatewayOAuthProviderName = "Fixture provider"
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("new startup profile widened a JSONL compatibility mode")
			}
		}
	}
	for _, backend := range base.Backends {
		for _, home := range []string{backend.Home, backend.CodexHome} {
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatal("profile validation changed backend state")
			}
		}
	}
}
