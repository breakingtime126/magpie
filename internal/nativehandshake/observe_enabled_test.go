//go:build nativehandshakediag

package nativehandshake

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func handshakeOwnedFixture(t *testing.T) (base, root, exe string) {
	t.Helper()
	// Windows temp ancestors may have short names. Canonicalize only the
	// test-owned base, as filepath's own tempDirCanonical fixture does. The
	// production guard must still reject a changed resolved executable path.
	rawBase := t.TempDir()
	base, err := filepath.EvalSymlinks(rawBase)
	if err != nil {
		t.Fatal("owned fixture base resolution failed")
	}
	root = filepath.Join(base, "original-desktop-acceptance-"+strings.Repeat("a", 32), "app")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal("owned fixture directory creation failed")
	}
	exe = filepath.Join(root, "magpie-codex-bridge.exe")
	if err := os.WriteFile(exe, []byte("inert"), 0600); err != nil {
		t.Fatal("owned fixture executable creation failed")
	}
	rawExe := filepath.Join(rawBase, filepath.Base(filepath.Dir(root)), "app", filepath.Base(exe))
	_, _, rawAccepted := diagnosticLocation(rawExe)
	dir, role, ok := diagnosticLocation(exe)
	t.Logf("owned fixture: baseCanonicalChanged=%t rawCandidateAccepted=%t canonicalCandidateAccepted=%t",
		!strings.EqualFold(rawBase, base), rawAccepted, ok)
	if !ok || dir != root || role != "bridge" {
		resolved, resolveErr := filepath.EvalSymlinks(exe)
		t.Fatalf("owned runtime rejected: absolute=%t layout=%t resolved=%t samePath=%t admitted=%t directoryMatched=%t roleMatched=%t",
			filepath.IsAbs(exe), filepath.Base(root) == "app" && ownedParent.MatchString(filepath.Base(filepath.Dir(root))),
			resolveErr == nil, strings.EqualFold(resolved, exe), ok, dir == root, role == "bridge")
	}
	return base, root, exe
}

func assertHandshakeAliasRejected(t *testing.T, alias, exe string) {
	t.Helper()
	dir := filepath.Dir(alias)
	if !filepath.IsAbs(alias) || filepath.Base(dir) != "app" || !ownedParent.MatchString(filepath.Base(filepath.Dir(dir))) || filepath.Base(alias) != filepath.Base(exe) {
		t.Fatal("alias fixture lost owned path shape")
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil || !strings.EqualFold(resolved, exe) || strings.EqualFold(alias, resolved) {
		t.Fatal("alias fixture did not resolve to the distinct owned path")
	}
	if _, _, ok := diagnosticLocation(alias); ok {
		t.Fatal("noncanonical or linked executable path admitted")
	}
}

func TestHandshakeOwnedDirectoryAndExclusiveBounds(t *testing.T) {
	base, root, _ := handshakeOwnedFixture(t)
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

func TestHandshakeOwnedDirectoryRejectsAliasesAndLinks(t *testing.T) {
	t.Run("dot-segment", func(t *testing.T) {
		_, root, exe := handshakeOwnedFixture(t)
		// Do not use Join: it would clean away the alias before admission.
		alias := root + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(exe)
		assertHandshakeAliasRejected(t, alias, exe)
	})
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			base, root, exe := handshakeOwnedFixture(t)
			linkedRoot := filepath.Join(base, "linked", filepath.Base(filepath.Dir(root)), "app")
			parent, target, link := linkedRoot, exe, filepath.Join(linkedRoot, filepath.Base(exe))
			if kind == "directory" {
				parent, target, link = filepath.Dir(linkedRoot), root, linkedRoot
			}
			if err := os.MkdirAll(parent, 0700); err != nil {
				t.Fatal("linked fixture parent creation failed")
			}
			if err := os.Symlink(target, link); err != nil {
				if runtime.GOOS == "windows" {
					t.Skip("Windows symlink fixture unavailable; no privilege change attempted")
				}
				t.Fatal("linked fixture creation failed")
			}
			alias := filepath.Join(linkedRoot, filepath.Base(exe))
			assertHandshakeAliasRejected(t, alias, exe)
		})
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
