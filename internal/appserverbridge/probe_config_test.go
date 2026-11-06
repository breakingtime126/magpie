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
