package nativeconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func launcherFixture(t *testing.T) (string, TrustedLauncherIdentity) {
	t.Helper()
	dir := t.TempDir()
	content := []byte("synthetic inert artifact, never executable")
	sum := sha256.Sum256(content)
	e := TrustedLauncherIdentity{"magpie-codex-bridge.exe", hex.EncodeToString(sum[:]), strings.Repeat("a", 40)}
	if err := os.WriteFile(filepath.Join(dir, e.Filename), content, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, e
}

func TestPackagedLauncherExplicitIdentity(t *testing.T) {
	dir, e := launcherFixture(t)
	for _, name := range []string{"magpie-codex-bridge.exe", "magpie-codex-bridge-windows-amd64.exe"} {
		if name != e.Filename {
			if err := os.Rename(filepath.Join(dir, e.Filename), filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			e.Filename = name
		}
		got, err := VerifyPackagedLauncher(dir, e)
		if err != nil || got.Path != filepath.Join(dir, name) || got.SHA256 != e.SHA256 || got.SourceRevision != e.SourceRevision {
			t.Fatalf("explicit package artifact not verified: %+v, %v", got, err)
		}
	}
}

func TestResolvePackagedLauncherUsesOwnPackage(t *testing.T) {
	dir, e := launcherFixture(t)
	gui := filepath.Join(dir, "Magpie-Native-Codex-Experimental.exe")
	if err := os.WriteFile(gui, []byte("synthetic GUI location"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePackagedLauncher(gui, e.SHA256, e.SourceRevision)
	if err != nil || got.Path != filepath.Join(dir, e.Filename) {
		t.Fatal("explicit own-executable package layout was not resolved")
	}
	for _, input := range []struct{ executable, digest, revision string }{
		{gui, "", e.SourceRevision}, {gui, e.SHA256, ""},
		{"Magpie-Native-Codex-Experimental.exe", e.SHA256, e.SourceRevision},
		{filepath.Join(dir, "other.exe"), e.SHA256, e.SourceRevision},
		{filepath.Join(dir, "missing", filepath.Base(gui)), e.SHA256, e.SourceRevision},
		{gui, strings.Repeat("b", 64), e.SourceRevision},
	} {
		if _, err := resolvePackagedLauncher(input.executable, input.digest, input.revision); !errors.Is(err, ErrLauncherIdentity) {
			t.Fatal("unverified package location or trust admitted")
		}
	}
}

func TestResolvePackagedLauncherMissingBuildTrust(t *testing.T) {
	// Tests do not mutate process trust. The normal test build injects no pins.
	if packagedLauncherSHA256 != "" || packagedLauncherSourceRevision != "" {
		t.Fatal("test unexpectedly contains production build trust")
	}
	if got, err := ResolvePackagedLauncher(); !errors.Is(err, ErrLauncherIdentity) || got != (PackagedLauncher{}) {
		t.Fatal("ordinary build resolved an untrusted launcher")
	}
}

func TestPackagedLauncherRejectsMissingTrust(t *testing.T) {
	dir, e := launcherFixture(t)
	for name, change := range map[string]func(*TrustedLauncherIdentity){
		"empty":          func(e *TrustedLauncherIdentity) { *e = TrustedLauncherIdentity{} },
		"wrong hash":     func(e *TrustedLauncherIdentity) { e.SHA256 = strings.Repeat("b", 64) },
		"bad hash":       func(e *TrustedLauncherIdentity) { e.SHA256 = strings.Repeat("z", 64) },
		"empty revision": func(e *TrustedLauncherIdentity) { e.SourceRevision = "" },
		"short revision": func(e *TrustedLauncherIdentity) { e.SourceRevision = "abcdef12" },
		"upper revision": func(e *TrustedLauncherIdentity) { e.SourceRevision = strings.Repeat("A", 40) },
		"traversal":      func(e *TrustedLauncherIdentity) { e.Filename = "../magpie-codex-bridge.exe" },
		"absolute":       func(e *TrustedLauncherIdentity) { e.Filename = filepath.Join(dir, e.Filename) },
		"other binary":   func(e *TrustedLauncherIdentity) { e.Filename = "codex.exe" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := e
			change(&bad)
			if got, err := VerifyPackagedLauncher(dir, bad); !errors.Is(err, ErrLauncherIdentity) || got != (PackagedLauncher{}) {
				t.Fatal("untrusted launcher admitted")
			}
		})
	}
	for _, bad := range []string{"", ".", "relative", dir + string(filepath.Separator) + ".", filepath.Join(dir, "missing")} {
		if _, err := VerifyPackagedLauncher(bad, e); err == nil {
			t.Fatal("invalid package root admitted")
		}
	}
}

func TestPackagedLauncherDoesNotTrustSiblingManifest(t *testing.T) {
	dir, e := launcherFixture(t)
	path := filepath.Join(dir, e.Filename)
	changed := []byte("replacement artifact")
	sum := sha256.Sum256(changed)
	for name, data := range map[string][]byte{e.Filename: changed, "BUILD.json": []byte(`{"adapterSHA256":"` + hex.EncodeToString(sum[:]) + `"}`)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := VerifyPackagedLauncher(dir, e); err == nil {
		t.Fatal("modified launcher and sibling manifest admitted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPackagedLauncher(dir, e); err == nil {
		t.Fatal("missing launcher admitted")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPackagedLauncher(dir, e); err == nil {
		t.Fatal("directory launcher admitted")
	}
}

func TestPackagedLauncherRejectsLinkedPaths(t *testing.T) {
	dir, e := launcherFixture(t)
	t.Run("file", func(t *testing.T) {
		other := t.TempDir()
		if err := os.Symlink(filepath.Join(dir, e.Filename), filepath.Join(other, e.Filename)); err != nil {
			t.Skip("symlink creation unavailable")
		}
		if _, err := VerifyPackagedLauncher(other, e); err == nil {
			t.Fatal("linked launcher admitted")
		}
	})
	t.Run("directory", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(dir, link); err != nil {
			t.Skip("symlink creation unavailable")
		}
		if _, err := VerifyPackagedLauncher(link, e); err == nil {
			t.Fatal("linked package admitted")
		}
	})
}
