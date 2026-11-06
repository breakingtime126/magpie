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
	// Exact auth modes now have independent 0.167 opt-ins. The startup
	// profile still cannot promote legacy modes or enable requirements reads.
	for _, mode := range []string{"", StartupProbeCompareUnrestricted, probe167Mode, "compare-unrestricted-v0.167"} {
		for _, requirements := range []string{"", ConfigRequirementsCompareSafe, "compare-safe-v0.167.0-alpha.3.2"} {
			for _, attestation := range []string{"", AttestationUnavailable, AttestationUnavailable167, "unavailable-v0.167", "unavailable-v0.167.0-alpha.3.3"} {
				for _, gateway := range []string{"", GatewayOAuthPrimaryRead, GatewayOAuthPrimaryRead167, "primary-read-only-v0.167", "primary-read-only-v0.167.0-alpha.3.3"} {
					cfg := base
					cfg.StartupProbe = mode
					cfg.RequirementsRead = requirements
					cfg.Attestation = attestation
					cfg.GatewayOAuth = gateway
					cfg.GatewayOAuthProviderName = ""
					if gateway != "" {
						cfg.GatewayOAuthProviderName = "Fixture provider"
					}
					known := (mode == "" || mode == StartupProbeCompareUnrestricted || mode == probe167Mode) &&
						(requirements == "" || requirements == ConfigRequirementsCompareSafe) &&
						(attestation == "" || attestation == AttestationUnavailable || attestation == AttestationUnavailable167) &&
						(gateway == "" || gateway == GatewayOAuthPrimaryRead || gateway == GatewayOAuthPrimaryRead167)
					legacy := mode == StartupProbeCompareUnrestricted || requirements == ConfigRequirementsCompareSafe ||
						attestation == AttestationUnavailable || gateway == GatewayOAuthPrimaryRead
					newer := mode == probe167Mode || attestation == AttestationUnavailable167 || gateway == GatewayOAuthPrimaryRead167
					wantValid := known && !(legacy && newer)
					if err := cfg.Validate(); (err == nil) != wantValid {
						t.Fatalf("compatibility matrix: probe=%q requirements=%q attestation=%q gateway=%q wantValid=%t err=%v", mode, requirements, attestation, gateway, wantValid, err)
					}
					data, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadConfig(bytes.NewReader(data))
					if (err == nil) != wantValid {
						t.Fatalf("JSON loading bypassed matrix: probe=%q requirements=%q attestation=%q gateway=%q wantValid=%t err=%v", mode, requirements, attestation, gateway, wantValid, err)
					}
					if wantValid && (loaded.StartupProbe != mode || loaded.RequirementsRead != requirements ||
						loaded.Attestation != attestation || loaded.GatewayOAuth != gateway || loaded.GatewayOAuthProviderName != cfg.GatewayOAuthProviderName) {
						t.Fatal("JSON loading changed an explicitly selected compatibility mode")
					}
				}
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
