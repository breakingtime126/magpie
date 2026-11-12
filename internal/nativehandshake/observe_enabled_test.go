//go:build nativehandshakediag

package nativehandshake

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHandshakeOwnedDirectoryAndExclusiveBounds(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "original-desktop-acceptance-"+strings.Repeat("a", 32), "app")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "magpie-codex-bridge.exe")
	if err := os.WriteFile(exe, []byte("inert"), 0600); err != nil {
		t.Fatal(err)
	}
	if dir, role, ok := diagnosticLocation(exe); !ok || dir != root || role != "bridge" {
		t.Fatal("owned runtime rejected")
	}
	for _, path := range []string{filepath.Join(base, "magpie-codex-bridge.exe"), filepath.Join(root, "other.exe"), "magpie-codex-bridge.exe"} {
		if _, _, ok := diagnosticLocation(path); ok {
			t.Fatal("unowned runtime admitted")
		}
	}
	for i := 1; i <= maxFiles; i++ {
		r := openRecorder(root, "bridge")
		if r == nil {
			t.Fatal("exclusive slot missing")
		}
		var workers sync.WaitGroup
		for n := 0; n < 100; n++ {
			workers.Add(1)
			go func() { defer workers.Done(); r.emit(AdapterEntered); r.emit(InitializeOutputWritten) }()
		}
		workers.Wait()
		r.file.Close()
		data, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("native-handshake-bridge.%02d.log", i)))
		if err != nil || string(data) != "native-backend-handshake-v1 sequence=1 stage=adapter-entered\nnative-backend-handshake-v1 sequence=2 stage=initialize-output-written\n" {
			t.Fatal("unbounded, duplicate or nonfixed output", err)
		}
	}
	if openRecorder(root, "bridge") != nil || openRecorder(root, "private-role") != nil {
		t.Fatal("capacity or role bypass")
	}
}

func TestHandshakeFailedSinkStopsAndDefaultTestBinaryIsInert(t *testing.T) {
	if processEnabled() {
		t.Fatal("ordinary test executable opened process log")
	}
	r := openRecorder(t.TempDir(), "host")
	if r == nil {
		t.Fatal("fixture open")
	}
	r.file.Close()
	r.emit(HostListening)
	if r.file != nil {
		t.Fatal("failed sink retained")
	}
	r.emit(HostJSONLAdmitted)
}
