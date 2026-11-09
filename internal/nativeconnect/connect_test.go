package nativeconnect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

type fakeSelection struct {
	value   Selection
	partial bool
	writes  int
}

func (s *fakeSelection) Read() (Selection, error) { return s.value, nil }

func (s *fakeSelection) Replace(before, after Selection) error {
	if s.value != before {
		return ErrConflict
	}
	s.writes++
	s.value.CLI = after.CLI
	if s.partial {
		return ErrUncertain
	}
	s.value = after
	return nil
}

// These bytes are inert fixtures, never executed or parsed as a model catalog.
func fixturePlan(t *testing.T) Plan {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	codex, control, work := filepath.Join(home, "codex"), filepath.Join(home, "control"), filepath.Join(home, "work")
	for _, dir := range []string{codex, control, work} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command, catalog := filepath.Join(home, "inert.exe"), filepath.Join(home, "synthetic-catalog.json")
	for _, path := range []string{command, catalog} {
		if err := os.WriteFile(path, []byte("nonexecuted fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := appserverbridge.Config{
		ControlPlane: appserverbridge.ControlPlaneRouting167, ControlHome: control, WorkingDirectory: work,
		SharedNativeRoute: &appserverbridge.SharedNativeRoute167{Mode: appserverbridge.SharedNativeHome167, GatewayURL: "http://127.0.0.1:19431/v1", GatewayEnvKey: "MAGPIE_SHARED_TEST_KEY", SecondaryCatalog: catalog},
		Backends: []appserverbridge.Backend{
			{ID: "openai", Provider: "openai", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("a", 64)},
			{ID: "aihub", Provider: "magpie_shared_aihub", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("b", 64), InheritEnv: []string{"MAGPIE_SHARED_TEST_KEY"}},
		},
	}
	return Plan{Launcher: command, Config: cfg}
}

func TestNativeConnectMissingCatalogHasNoWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	store := &fakeSelection{}
	c := New(root, store, nil)
	if _, err := c.Connect(context.Background()); !errors.Is(err, ErrMissingCatalog) {
		t.Fatal("missing catalog was not reported")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) || store.writes != 0 {
		t.Fatal("missing input changed files or selection")
	}
}

func TestNativeConnectRestoreAndCrashRecovery(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[partial], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "connection")
			plan := fixturePlan(t)
			original := Selection{CLI: Value{true, 2, "%LOCALAPPDATA%\\original.exe"}, Profile: Value{true, 1, ""}}
			store := &fakeSelection{value: original, partial: partial}
			prepare := func(context.Context) (Plan, error) { return plan, nil }
			c := New(root, store, prepare)
			status, err := c.Connect(context.Background())
			if partial && !errors.Is(err, ErrUncertain) || !partial && err != nil {
				t.Fatal("incorrect selection result")
			}
			if status.BackendHandshakeVerified || status.ChatVerified || status.SelectionConfigured == partial {
				t.Fatal("selection confused with handshake")
			}
			beforeRepeat := store.value
			if _, err := c.Connect(context.Background()); !errors.Is(err, ErrConflict) || store.value != beforeRepeat {
				t.Fatal("repeated connect replaced original journal")
			}
			store.partial = false
			// Loss of the own profile must not strand the selected launch paths.
			j, err := c.readJournal()
			if err != nil || os.Remove(j.Installed.Profile.Text) != nil {
				t.Fatal("fixture profile removal failed")
			}
			// A newly constructed controller recovers using only its journal.
			c = New(root, store, nil)
			if status, err := c.Disconnect(); err != nil || status.SelectionConfigured || store.value != original {
				t.Fatal("exact original presence/type/value not restored")
			}
		})
	}
}

func TestNativeConnectExternalChangeNeverRestored(t *testing.T) {
	plan := fixturePlan(t)
	root := filepath.Join(t.TempDir(), "connection")
	store := &fakeSelection{}
	c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
	if _, err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.value.Profile = Value{true, 1, "external-choice"}
	before, writes := store.value, store.writes
	if _, err := c.Disconnect(); !errors.Is(err, ErrConflict) || store.value != before || store.writes != writes {
		t.Fatal("external modification was overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "selection.json")); err != nil {
		t.Fatal("conflict removed recovery journal")
	}
}

func TestNativeConnectRejectsInvalidProfileBeforeSelection(t *testing.T) {
	plan := fixturePlan(t)
	plan.Config.SharedNativeRoute.SecondaryCatalog = "missing"
	root := filepath.Join(t.TempDir(), "not-created")
	store := &fakeSelection{}
	c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
	if _, err := c.Connect(context.Background()); err == nil || store.writes != 0 {
		t.Fatal("invalid profile selected")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid profile wrote state")
	}
}
