package nativeexepath

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "WindowsApps")
	dir := filepath.Join(parent, "OpenAI.Codex_fixture", "app", "resources")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codex.exe")
	if err := os.WriteFile(path, []byte("inert, never executed"), 0600); err != nil {
		t.Fatal(err)
	}
	return parent, path
}

func TestResolveMetadata(t *testing.T) {
	_, path := fixture(t)
	got, err := Resolve(path)
	if err != nil || !strings.EqualFold(got, path) {
		t.Fatal(got, err)
	}
	if _, err := Resolve(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := Resolve(filepath.Join(filepath.Dir(path), "absent.exe")); err == nil {
		t.Fatal("missing accepted")
	}
	for _, bad := range []string{`\\server\share\codex.exe`, `\\?\C:\codex.exe`, `\\.\C:\codex.exe`, "codex.exe"} {
		if _, err := Resolve(bad); err == nil {
			t.Fatal("non-local input accepted")
		}
	}
}

func TestResolveWithoutParentEnumeration(t *testing.T) {
	parent, path := fixture(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := "*" + user.User.Sid.String()
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	// This changes ONLY a fresh test directory, never WindowsApps or the OS.
	// DENY list-directory is not inherited; existing descendants remain readable.
	if out, err := exec.Command(icacls, parent, "/deny", sid+":(RD)").CombinedOutput(); err != nil {
		t.Fatalf("isolated fixture ACL setup: %v %s", err, out)
	}
	defer func() {
		if out, err := exec.Command(icacls, parent, "/remove:d", sid).CombinedOutput(); err != nil {
			t.Errorf("isolated fixture ACL restore: %v %s", err, out)
		}
	}()
	if _, err := filepath.EvalSymlinks(path); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("fixture did not reproduce old enumeration failure: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("fixture target is not readable", err)
	}
	f.Close()
	got, err := Resolve(path)
	if err != nil || !strings.EqualFold(got, path) {
		t.Fatal("authorized handle resolution failed", got, err)
	}
}

func TestResolveRejectsLeafLinkAndExposesAncestorJunction(t *testing.T) {
	parent, path := fixture(t)
	link := filepath.Join(filepath.Dir(path), "linked.exe")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal("CI must support symbolic-link fixture", err)
	}
	if _, err := Resolve(link); err == nil {
		t.Fatal("leaf symlink accepted")
	}
	alias := filepath.Join(filepath.Dir(parent), "junction")
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if out, err := exec.Command(cmd, "/d", "/c", "mklink", "/J", alias, parent).CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v %s", err, out)
	}
	defer os.Remove(alias)
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		t.Fatal(err)
	}
	aliased := filepath.Join(alias, rel)
	got, err := Resolve(aliased)
	if err != nil || !strings.EqualFold(got, path) || strings.EqualFold(got, aliased) {
		t.Fatal("ancestor alias not exposed for caller rejection", got, err)
	}
}

func TestFinalNameBoundedAndFailClosed(t *testing.T) {
	for _, s := range []string{`\\?\UNC\server\share\x.exe`, `\\?\Volume{abc}\x.exe`, `\\.\C:\x.exe`, `\Device\disk\x.exe`} {
		if _, err := finalDOSPath(s); err == nil {
			t.Fatal("namespace accepted")
		}
	}
	if got, err := finalDOSPath(`\\?\C:\safe\codex.exe`); err != nil || got != `C:\safe\codex.exe` {
		t.Fatal(got, err)
	}
	if _, err := readFinalName(func([]uint16) (uint32, error) { return 0, windows.ERROR_ACCESS_DENIED }); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal(err)
	}
	if _, err := readFinalName(func([]uint16) (uint32, error) { return 40000, nil }); err == nil {
		t.Fatal("oversize accepted")
	}
	calls := 0
	got, err := readFinalName(func(b []uint16) (uint32, error) {
		calls++
		if calls == 1 {
			return 1024, nil
		}
		s, _ := windows.UTF16FromString(`\\?\C:\safe\codex.exe`)
		copy(b, s)
		return uint32(len(s)-1), nil
	})
	if err != nil || calls != 2 || got != `C:\safe\codex.exe` {
		t.Fatal(got, err, calls)
	}
}
