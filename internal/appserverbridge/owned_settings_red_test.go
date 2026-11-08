package appserverbridge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Uses only symbols present at 39a73bf4: copy this file alone for behavioral red.
// No executable or native service is started by this admission check.
func TestOwnedSettingsModeAdmittedWithoutWriting(t *testing.T) {
	r, catalog, _ := ownedTestSetup(t)
	command := filepath.Join(t.TempDir(), "inert.exe")
	if err := os.WriteFile(command, []byte("MZ inert nonexecuted fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := range r.backends {
		r.backends[i].Command = command
		r.backends[i].Home = t.TempDir()
		r.backends[i].CodexHome = t.TempDir()
		r.backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	cfg := object{
		"backends": raw(r.backends), "control_home": raw(filepath.Dir(catalog)),
		"control_plane": raw("routing-v0.167.0-alpha.3.2"), "bridge_model_catalog_json": raw(catalog),
		"bridge_model_settings": raw("owned-file-v0.167.0-alpha.3.2"),
	}
	if _, err := LoadConfig(bytes.NewReader(raw(cfg))); err != nil {
		t.Fatal("explicit bridge-owned settings mode was rejected before startup loading")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(catalog), "config.toml")); !os.IsNotExist(err) {
		t.Fatal("mode admission wrote settings before policy and catalog validation")
	}
}
