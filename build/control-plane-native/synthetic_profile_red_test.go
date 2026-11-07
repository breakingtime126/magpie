package main

import (
	"path/filepath"
	"testing"
)

// This file also runs on the old parser without requiring any new symbol.
func TestSelfAuthoredAcceptanceWithoutCatalog(t *testing.T) {
	root := t.TempDir()
	_, err := arguments([]string{"-bridge", filepath.Join(root, "bridge.exe"),
		"-codex", filepath.Join(root, "codex.exe"), "-scratch-parent", root,
		"-acceptance-profile", "synthetic-models-4239"})
	if err != nil {
		t.Fatal("explicit self-authored model profile was rejected", err)
	}
}
