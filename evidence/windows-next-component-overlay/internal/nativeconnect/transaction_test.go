package nativeconnect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type faultyJournalFile struct {
	*os.File
	fault string
}

func (f *faultyJournalFile) Write(data []byte) (int, error) {
	if f.fault == "short" {
		return f.File.Write(data[:len(data)/2])
	}
	return f.File.Write(data)
}
func (f *faultyJournalFile) Sync() error {
	if f.fault == "sync" {
		return errors.New("injected sync failure")
	}
	return f.File.Sync()
}
func (f *faultyJournalFile) Close() error {
	err := f.File.Close()
	if f.fault == "close" {
		return errors.Join(err, errors.New("injected close failure"))
	}
	return err
}

func TestNativeConnectJournalPublicationFailuresRemainRecoverable(t *testing.T) {
	for _, fault := range []string{"short", "sync", "close", "link", "cleanup"} {
		t.Run(fault, func(t *testing.T) {
			plan := fixturePlan(t)
			root := filepath.Join(t.TempDir(), "connection")
			store := &fakeSelection{}
			c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
			c.journalOps.create = func(dir string) (journalFile, error) {
				f, err := os.CreateTemp(dir, ".selection-*")
				if err != nil {
					return nil, err
				}
				return &faultyJournalFile{File: f, fault: fault}, nil
			}
			if fault == "link" {
				c.journalOps.link = func(string, string) error { return errors.New("injected link failure") }
			}
			if fault == "cleanup" {
				c.journalOps.remove = func(string) error { return errors.New("injected cleanup failure") }
			}
			status, err := c.Connect(context.Background())
			if err == nil || store.writes != 0 {
				t.Fatal("failed publication changed external selection")
			}
			c.journalOps = journalPublishOps{}
			if fault == "cleanup" {
				if !status.JournalPresent || !status.RestoreAvailable || !status.RecoveryRequired {
					t.Fatal("published journal lost its recovery state")
				}
				inspected, err := c.Inspect()
				if err != nil || !inspected.RestoreAvailable || inspected.SelectionConfigured {
					t.Fatal("cleanup failure stranded an unreadable journal")
				}
				if _, err := c.Disconnect(); err != nil {
					t.Fatal("complete publication could not restore", err)
				}
				return
			}
			if _, err := os.Lstat(filepath.Join(root, "selection.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial final journal exposed")
			}
			if _, err := c.Connect(context.Background()); err != nil {
				t.Fatal("failed unpublished journal blocked a new attempt", err)
			}
		})
	}
}
func TestJournalPublicationNeverOverwritesExistingFinal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	before := []byte("existing external bytes")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	published, err := publishJournal(path, []byte("new bytes"), journalPublishOps{})
	if err == nil || published {
		t.Fatal("existing final journal was replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(before) {
		t.Fatal("existing final bytes changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("unpublished temporary journal leaked")
	}
}
func TestNativeConnectRejectsNativeRootAliasesAndOverlap(t *testing.T) {
	for _, kind := range []string{"home", "codex", "ancestor", "alias"} {
		t.Run(kind, func(t *testing.T) {
			plan := fixturePlan(t)
			root := plan.Config.Backends[0].Home
			switch kind {
			case "codex":
				root = plan.Config.Backends[0].CodexHome
			case "ancestor":
				root = t.TempDir()
				codex := filepath.Join(root, "native")
				if err := os.Mkdir(codex, 0700); err != nil {
					t.Fatal(err)
				}
				for i := range plan.Config.Backends {
					plan.Config.Backends[i].CodexHome = codex
				}
			case "alias":
				root = t.TempDir()
				link := filepath.Join(t.TempDir(), "native-link")
				if err := os.Symlink(root, link); err != nil {
					t.Skip("symlink unavailable", err)
				}
				for i := range plan.Config.Backends {
					plan.Config.Backends[i].CodexHome = link
				}
			}
			before, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			store := &fakeSelection{}
			c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
			if _, err := c.Connect(context.Background()); err == nil || store.writes != 0 {
				t.Fatal("native state directory accepted")
			}
			after, err := os.ReadDir(root)
			if err != nil || len(before) != len(after) {
				t.Fatal("native directory received connection files")
			}
		})
	}
}
func TestNativeConnectAllowsSeparateMagpieDirectoryUnderHome(t *testing.T) {
	plan := fixturePlan(t)
	root := filepath.Join(plan.Config.Backends[0].Home, "magpie-connection")
	store := &fakeSelection{}
	c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
	if _, err := c.Connect(context.Background()); err != nil {
		t.Fatal("ordinary Magpie HOME descendant rejected", err)
	}
}

type readingSelection struct {
	fakeSelection
	onRead func()
}

func (s *readingSelection) Read() (Selection, error) {
	if s.onRead != nil {
		s.onRead()
		s.onRead = nil
	}
	return s.fakeSelection.Read()
}
func TestNativeConnectRechecksTrustedLauncherIdentity(t *testing.T) {
	for _, kind := range []string{"missing", "changed-before", "changed-during"} {
		t.Run(kind, func(t *testing.T) {
			plan := fixturePlan(t)
			root := filepath.Join(t.TempDir(), "connection")
			store := &readingSelection{}
			change := func() {
				if err := os.WriteFile(plan.Launcher, []byte("replacement launcher bytes"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "missing":
				plan.LauncherIdentity = TrustedLauncherIdentity{}
			case "changed-before":
				change()
			case "changed-during":
				store.onRead = change
			}
			c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
			status, err := c.Connect(context.Background())
			if err == nil || store.writes != 0 {
				t.Fatal("unchecked launcher selected")
			}
			if kind == "changed-during" {
				if !status.JournalPresent || !status.RestoreAvailable || !status.RecoveryRequired {
					t.Fatal("late pin failure lost valid recovery journal")
				}
				if _, err := c.Disconnect(); err != nil {
					t.Fatal("late pin failure blocked recovery", err)
				}
			} else if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid launcher created profile")
			}
		})
	}
}
