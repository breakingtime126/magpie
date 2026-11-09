package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/buildvariant"
)

func TestNativeCodexGuardFamily(t *testing.T) {
	for _, id := range []string{"codex", "codex@wsl:Ubuntu", "codex#work", "codex#work@wsl:Ubuntu"} {
		if got := (&Agent{ID: id}).LegacyCodexWritesBlocked(); got != buildvariant.NativeCodex {
			t.Fatalf("%s blocked=%v", id, got)
		}
	}
	for _, id := range []string{"claude", "codex-other", "t3code", "omp#codex@wsl:Ubuntu"} {
		if (&Agent{ID: id}).LegacyCodexWritesBlocked() {
			t.Fatalf("unrelated agent blocked: %s", id)
		}
	}
}

func TestNativeCodexGuardDirectHooks(t *testing.T) {
	for _, id := range []string{"codex", "codex@wsl:Ubuntu", "codex#work", "claude"} {
		t.Run(id, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.toml")
			before := []byte("model_provider = \"original\"\n")
			if err := os.WriteFile(p, before, 0o600); err != nil {
				t.Fatal(err)
			}
			writes := 0
			change := func() error {
				writes++
				return os.WriteFile(p, []byte("changed\n"), 0o600)
			}
			a := protectNativeCodex(&Agent{ID: id,
				Fields: []Field{{Key: "model", Set: func(string) error { return change() }}},
				Sync: change, Unwire: change, Restore: change, Follow: change, EndFailover: change,
				Join: func() (bool, error) { return true, change() },
				RenameRefs: func(string, string) (bool, error) { return true, change() },
				move: func(string, string) error { return change() },
			})
			calls := []func() error{func() error { return a.Fields[0].Set("x") }, a.Sync, a.Unwire, a.Restore, a.Follow, a.EndFailover,
				func() error { _, err := a.Join(); return err },
				func() error { _, err := a.RenameRefs("a", "b"); return err },
				func() error { return a.move("a", "b") },
			}
			blocked := a.LegacyCodexWritesBlocked()
			for _, call := range calls {
				err := call()
				if blocked && !errors.Is(err, ErrNativeCodexLegacyWrite) || !blocked && err != nil {
					t.Fatalf("blocked=%v err=%v", blocked, err)
				}
			}
			after, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if blocked && (writes != 0 || !bytes.Equal(before, after)) {
				t.Fatal("blocked hooks changed the fixture")
			}
			if !blocked && writes != len(calls) {
				t.Fatalf("ordinary hooks called %d times, want %d", writes, len(calls))
			}
		})
	}
}

func TestNativeCodexGuardEntryBeforeReads(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	for _, id := range []string{"codex", "codex@wsl:Ubuntu", "codex#work"} {
		a := &Agent{ID: id, Fields: []Field{{Key: "model", Get: func() string { t.Fatal("read before rejection"); return "" }, Set: func(string) error { t.Fatal("write before rejection"); return nil }}}}
		calls := []func() error{
			func() error { return a.Apply("model", "x") }, func() error { return a.Pick("model", "x") },
			a.Reapply, a.Connect, a.Disconnect, a.DisconnectOffline, a.Keep,
			func() error { return reset(a) },
		}
		for _, call := range calls {
			if err := call(); !errors.Is(err, ErrNativeCodexLegacyWrite) {
				t.Fatalf("%s: %v", id, err)
			}
		}
	}
}

func TestNativeCodexGuardRealConstructor(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	h := t.TempDir()
	d := filepath.Join(h, ".codex")
	t.Setenv("CODEX_HOME", d)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "config.toml")
	before := []byte("model_provider = \"custom\"\n[profiles.work]\nmodel_provider = \"custom\"\n")
	if err := os.WriteFile(p, before, 0o600); err != nil {
		t.Fatal(err)
	}
	a := codex(h)
	for _, call := range []func() error{a.Sync, a.Unwire, a.EndFailover, func() error { return a.Field("model").Set("anything") }} {
		if err := call(); !errors.Is(err, ErrNativeCodexLegacyWrite) {
			t.Fatalf("constructor hook: %v", err)
		}
	}
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("constructor hooks changed fixture")
	}
}

func TestNativeCodexGuardSleepingWSLDoesNotStart(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	live := &Agent{ID: "codex@wsl:Test", Fields: []Field{{Key: "model", Label: "model"}}}
	a := asleep(live, wslKind{id: "codex", in: func(place) *Agent { t.Fatal("started WSL constructor"); return nil }}, distro{Name: "Test"})
	if err := a.Field("model").Set("anything"); !errors.Is(err, ErrNativeCodexLegacyWrite) {
		t.Fatalf("sleeping setter: %v", err)
	}
}
