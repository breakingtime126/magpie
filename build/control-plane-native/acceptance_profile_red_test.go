package main

import (
	"path/filepath"
	"testing"
)

// This file uses only APIs present at 5a9e2f9e49bc00bd87bffd1385d7f4469389f838.
// Copy it alone to that base for an assertion-level red, not a compile failure.
func TestStandalone4239AcceptanceOptInAccepted(t *testing.T) {
	root := t.TempDir()
	args := []string{"-bridge", filepath.Join(root, "bridge.exe"), "-codex", filepath.Join(root, "codex.exe"),
		"-catalog", filepath.Join(root, "models.json"), "-scratch-parent", root,
		"-acceptance-profile", "app-server-4239"}
	if _, err := arguments(args); err != nil {
		t.Fatal("explicit standalone 4239 acceptance profile was rejected", err)
	}
}
