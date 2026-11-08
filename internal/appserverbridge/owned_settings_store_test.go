package appserverbridge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func ownedSettingsTestHome(t *testing.T) string {
	t.Helper()
	// macOS's system temporary parent may itself be a symlink. The store
	// accepts only the explicitly resolved synthetic directory.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func ownedSettingsTestOpen(t *testing.T, home string) *ownedSettingsStore {
	t.Helper()
	s, err := openOwnedSettingsStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func ownedSettingsTestValue(model string) object {
	return object{"model": raw(model), "model_reasoning_effort": raw("none")}
}

func TestOwnedSettingsAbsentIsNotCreatedUntilCommit(t *testing.T) {
	home := ownedSettingsTestHome(t)
	s := ownedSettingsTestOpen(t, home)
	value, version, err := s.snapshot()
	if err != nil || len(value) != 0 || version != "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatal("absent settings did not have empty semantic content")
	}
	if _, err := os.Lstat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("opening settings created a config before catalog and policy checks")
	}
	next := ownedSettingsTestValue("magpie/first/synthetic-model")
	wantVersion, err := ownedSettingsVersion(next)
	if err != nil {
		t.Fatal(err)
	}
	gotVersion, err := s.commit(&version, next)
	if err != nil || gotVersion != wantVersion {
		t.Fatal("initial commit failed")
	}
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) == 0 {
		t.Fatal("successful commit did not create actual TOML")
	}
	got, diskVersion, err := parseOwnedSettings(data)
	if err != nil || diskVersion != wantVersion || !sameJSON(raw(got), raw(next)) {
		t.Fatal("persisted TOML differs from committed selection")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := ownedSettingsTestOpen(t, home)
	got, diskVersion, err = restarted.snapshot()
	if err != nil || diskVersion != gotVersion || !sameJSON(raw(got), raw(next)) {
		t.Fatal("restart lost actual settings")
	}
}

func TestOwnedSettingsExistingEmptyAndSemanticVersion(t *testing.T) {
	home := ownedSettingsTestHome(t)
	path := filepath.Join(home, ownedSettingsFilename)
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := ownedSettingsTestOpen(t, home)
	empty, _, err := s.snapshot()
	if err != nil || len(empty) != 0 {
		t.Fatal("actual empty config was not accepted")
	}
	next := ownedSettingsTestValue("magpie/test/<&>界\u2028")
	version, err := s.commit(nil, next)
	if err != nil || version != "sha256:9dfe4ca80b6de459ea8ef7d64c924cab3f0149c19564d10d3fc7eaf40d6aef84" {
		t.Fatal("semantic version did not use sorted compact UTF-8 JSON")
	}
	// TOML formatting and key order are not semantic version changes.
	reordered := "# synthetic comment\nmodel_reasoning_effort = 'none'\nmodel = 'magpie/test/<&>界\u2028'\n"
	if err := os.WriteFile(path, []byte(reordered), 0600); err != nil {
		t.Fatal(err)
	}
	got, current, err := s.snapshot()
	if err != nil || current != version || !sameJSON(raw(got), raw(next)) {
		t.Fatal("formatting changed semantic version")
	}
	got["model"] = raw("caller-mutated-copy")
	if current, err := s.commit(&version, next); err != nil || current != version {
		t.Fatal("no-op commit changed semantic content")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != reordered {
		t.Fatal("no-op commit rewrote actual file")
	}
}

func TestOwnedSettingsRejectsForeignContentAndAbsenceDrift(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[initial], func(t *testing.T) {
			home := ownedSettingsTestHome(t)
			s := ownedSettingsTestOpen(t, home)
			if initial {
				if _, err := s.commit(nil, ownedSettingsTestValue("magpie/test/one")); err != nil {
					t.Fatal(err)
				}
			}
			foreign := []byte("model = 'magpie/test/foreign'\nmodel_reasoning_effort = 'none'\n")
			if err := os.WriteFile(s.path, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.commit(nil, ownedSettingsTestValue("magpie/test/two")); err != errOwnedSettingsUnavailable {
				t.Fatal("foreign edit was overwritten")
			}
			data, err := os.ReadFile(s.path)
			if err != nil || !bytes.Equal(data, foreign) {
				t.Fatal("foreign file changed after rejection")
			}
			if _, _, err := s.snapshot(); err != errOwnedSettingsUnavailable {
				t.Fatal("poisoned store recovered silently")
			}
		})
	}
	home := ownedSettingsTestHome(t)
	s := ownedSettingsTestOpen(t, home)
	if _, err := s.commit(nil, ownedSettingsTestValue("magpie/test/one")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.commit(nil, ownedSettingsTestValue("magpie/test/two")); err != errOwnedSettingsUnavailable {
		t.Fatal("removed settings were silently recreated")
	}
}

func TestOwnedSettingsCASAndConcurrentWriters(t *testing.T) {
	s := ownedSettingsTestOpen(t, ownedSettingsTestHome(t))
	_, initial, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, model := range []string{"magpie/test/one", "magpie/test/two"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			_, err := s.commit(&initial, ownedSettingsTestValue(model))
			errorsSeen <- err
		}(model)
	}
	wg.Wait()
	close(errorsSeen)
	successes, conflicts := 0, 0
	for err := range errorsSeen {
		switch err {
		case nil:
			successes++
		case errOwnedSettingsConflict:
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("same-version writers did not have one winner")
	}
	if _, _, err := s.snapshot(); err != nil {
		t.Fatal("ordinary CAS conflict poisoned the store")
	}
}

func TestOwnedSettingsLifetimeExclusiveLock(t *testing.T) {
	home := ownedSettingsTestHome(t)
	first := ownedSettingsTestOpen(t, home)
	if other, err := openOwnedSettingsStore(home); err != errOwnedSettingsUnavailable || other != nil {
		t.Fatal("second writer acquired occupied lock")
	}
	if _, err := first.read(); err != nil {
		t.Fatal("failed second open removed first writer's lock")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ownedSettingsLockname), []byte("synthetic-stale-lock"), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := openOwnedSettingsStore(home); err != errOwnedSettingsUnavailable || other != nil {
		t.Fatal("stale lock was removed or ignored")
	}
	data, err := os.ReadFile(filepath.Join(home, ownedSettingsLockname))
	if err != nil || string(data) != "synthetic-stale-lock" {
		t.Fatal("failed open changed stale lock")
	}
}

func TestOwnedSettingsStrictBoundedTOML(t *testing.T) {
	valid := "model = 'magpie/test/one'\nmodel_reasoning_effort = 'none'\n"
	for _, data := range []string{
		"model = 'partial'", "model_reasoning_effort = 'none'", valid + "extra = 'private-canary'\n",
		valid + "model = 'duplicate'\n", "[model]\nname='nested'\n", "model = [1]\nmodel_reasoning_effort = 'none'\n",
		"model = ''\nmodel_reasoning_effort = 'none'\n", "model = 'x'\nmodel_reasoning_effort = 'high'\n",
		"model = 1\nmodel_reasoning_effort = 'none'\n", "model = '\xff'\nmodel_reasoning_effort = 'none'\n",
		valid + strings.Repeat(" ", ownedSettingsLimit+1), "invalid = [",
	} {
		if _, _, err := parseOwnedSettings([]byte(data)); err != errOwnedSettingsUnavailable || strings.Contains(err.Error(), "private-canary") {
			t.Fatal("unsupported settings or private parse error escaped")
		}
	}
	boundary := valid + strings.Repeat(" ", ownedSettingsLimit-len(valid))
	if _, _, err := parseOwnedSettings([]byte(boundary)); err != nil {
		t.Fatal("exact byte boundary rejected")
	}
	for _, next := range []object{{}, {"model": raw("partial")}, {"model": raw("x"), "model_reasoning_effort": raw("high")}, {"model": raw(nil), "model_reasoning_effort": raw("none")}} {
		s := ownedSettingsTestOpen(t, ownedSettingsTestHome(t))
		if _, err := s.commit(nil, next); err != errOwnedSettingsUnavailable {
			t.Fatal("invalid replacement accepted")
		}
		if _, err := os.Lstat(s.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid replacement created a config file")
		}
	}
}

func TestOwnedSettingsRejectsSymlinkFilesAndHome(t *testing.T) {
	for _, name := range []string{ownedSettingsFilename, ownedSettingsLockname, "home"} {
		t.Run(name, func(t *testing.T) {
			home := ownedSettingsTestHome(t)
			target := filepath.Join(ownedSettingsTestHome(t), "synthetic-target")
			if err := os.WriteFile(target, nil, 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, name)
			if name == "home" {
				target = ownedSettingsTestHome(t)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if name == "home" {
				home = link
			}
			if s, err := openOwnedSettingsStore(home); err != errOwnedSettingsUnavailable || s != nil {
				t.Fatal("symlink was followed")
			}
		})
	}
}

func TestOwnedSettingsReplacementFailurePoisonsWithoutRollbackClaim(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-replace", true: "after-replace"}[changed], func(t *testing.T) {
			s := ownedSettingsTestOpen(t, ownedSettingsTestHome(t))
			if _, err := s.commit(nil, ownedSettingsTestValue("magpie/test/one")); err != nil {
				t.Fatal(err)
			}
			s.replace = func(home, temp string, initial bool) error {
				if changed {
					if err := ownedSettingsReplace(home, temp, initial); err != nil {
						return err
					}
				}
				return errors.New("synthetic-private-filesystem-error")
			}
			if version, err := s.commit(nil, ownedSettingsTestValue("magpie/test/two")); version != "" || err != errOwnedSettingsUnavailable {
				t.Fatal("uncertain replacement returned success or a private error")
			}
			if _, _, err := s.snapshot(); err != errOwnedSettingsUnavailable {
				t.Fatal("uncertain store continued serving")
			}
			data, err := os.ReadFile(s.path)
			if err != nil {
				t.Fatal(err)
			}
			value, _, err := parseOwnedSettings(data)
			want := "magpie/test/one"
			if changed {
				want = "magpie/test/two"
			}
			if err != nil || text(value, "model") != want {
				t.Fatal("failure performed an unrequested rollback or corrupted TOML")
			}
			entries, err := os.ReadDir(s.home)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".tmp") {
					t.Fatal("staged file was not cleaned up")
				}
			}
		})
	}
}

func TestOwnedSettingsCloseNeverRemovesReplacedLock(t *testing.T) {
	s := ownedSettingsTestOpen(t, ownedSettingsTestHome(t))
	// Close the handle to allow the synthetic replacement on Windows too.
	// Retain the old file under a second name to prevent identity reuse.
	if err := s.lock.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.home, ownedSettingsLockname)
	if err := os.Rename(path, filepath.Join(s.home, "synthetic-old-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("another-owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != errOwnedSettingsUnavailable {
		t.Fatal("close hid lost lock ownership")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "another-owner" {
		t.Fatal("close removed another owner's lock")
	}
}

func TestOwnedSettingsInvalidExistingFileIsNotRepaired(t *testing.T) {
	home := ownedSettingsTestHome(t)
	path := filepath.Join(home, ownedSettingsFilename)
	invalid := []byte("unreviewed = 'synthetic-private-value'\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := openOwnedSettingsStore(home); s != nil || err != errOwnedSettingsUnavailable {
		t.Fatal("invalid existing file accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, invalid) {
		t.Fatal("invalid existing file was rewritten")
	}
	if _, err := os.Lstat(filepath.Join(home, ownedSettingsLockname)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed open retained a lock it could release")
	}
}
