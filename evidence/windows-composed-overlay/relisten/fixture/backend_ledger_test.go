package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBackendLedgerDetectsDuplicatePhaseStart(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "backend-starts"), 0700); err != nil {
		t.Fatal(err)
	}
	phase := filepath.Join(root, "primary-started-first.json")
	if err := os.WriteFile(phase, []byte(`{"authored_backend":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := backendStartSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	// Same order as helper entry and backend: record process entry, then fail
	// trying to republish the already-existing first-generation phase marker.
	if err := recordBackendStart(root); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(phase, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if f != nil {
		f.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatal("negative control did not hit duplicate phase marker", err)
	}
	after, err := backendStartSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(before, after) || len(after) != len(before)+1 {
		t.Fatal("wrongly started backend was not independently detected")
	}
	unchanged, err := backendStartSnapshot(root)
	if err != nil || !slices.Equal(after, unchanged) {
		t.Fatal("idle snapshot changed", err)
	}
}

func TestBackendLedgerFailsClosed(t *testing.T) {
	root := t.TempDir()
	if _, err := backendStartSnapshot(root); err == nil {
		t.Fatal("missing ledger accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "backend-starts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend-starts", "invalid"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := backendStartSnapshot(root); err == nil {
		t.Fatal("malformed marker accepted")
	}
}
