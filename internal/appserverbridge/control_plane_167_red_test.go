package appserverbridge

import (
	"strings"
	"testing"
)

// Copy this file alone onto 70e603d8481c28b9f84cadf2f7183255044e11b4.
// It uses only old APIs and fails by assertion because the mode is unknown.
func TestControlPlane167RedExactModeLoads(t *testing.T) {
	cfg := fixtureConfig(t)
	for i := range cfg.Backends {
		cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	settings, err := parseObject(raw(cfg))
	if err != nil {
		t.Fatal(err)
	}
	settings["control_plane"] = raw("routing-v0.167.0-alpha.3.2")
	settings["control_home"] = raw(t.TempDir())
	if _, err := LoadConfig(strings.NewReader(string(raw(settings)))); err != nil {
		t.Fatalf("explicit routing-only control plane must load: %v", err)
	}
}
