package appserverbridge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Admission only: no executable is launched and no native record is loaded.
// This test uses old symbols so the exact previous checkout fails behaviorally.
func TestBridgeOwnedCatalogOptionIsExplicitlyAdmitted(t *testing.T) {
	controlHome := t.TempDir()
	catalog := filepath.Join(controlHome, "authored-catalog.json")
	if err := os.WriteFile(catalog, []byte(`{"models":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(t.TempDir(), "not-launched.exe")
	if err := os.WriteFile(command, []byte("MZ independent inert fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	backends := []Backend{
		{ID: "one", Provider: "first_provider", Command: command, Home: t.TempDir(), CodexHome: t.TempDir(), StartupProbeSHA256: strings.Repeat("a", 64)},
		{ID: "two", Provider: "second_provider", Command: command, Home: t.TempDir(), CodexHome: t.TempDir(), StartupProbeSHA256: strings.Repeat("a", 64)},
	}
	cfg := object{"backends": raw(backends), "control_plane": raw(ControlPlaneRouting167), "control_home": raw(controlHome), "bridge_model_catalog_json": raw(catalog)}
	if _, err := LoadConfig(bytes.NewReader(raw(cfg))); err != nil {
		t.Fatal("explicit bridge-owned catalog option was rejected before startup loading")
	}
}
