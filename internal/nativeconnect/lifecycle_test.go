package nativeconnect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fixtureGeneration struct {
	plan     Plan
	closed   int
	closeErr error
	consumed bool
	lifetime context.Context
}

func (g *fixtureGeneration) Plan() (Plan, error) {
	if g.consumed {
		return Plan{}, ErrUnavailable
	}
	return g.plan, nil
}
func (g *fixtureGeneration) Close(context.Context) error { g.closed++; return g.closeErr }

func TestManagerMissingCatalogAndStatusWriteNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	store := &fakeSelection{}
	m := NewManager(context.Background(), root, store, nil)
	defer m.Close(context.Background())
	if _, err := m.Connect(context.Background()); !errors.Is(err, ErrMissingCatalog) {
		t.Fatal(err)
	}
	if s, err := m.Status(context.Background()); err != nil || s != (Status{}) {
		t.Fatal(s, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) || store.writes != 0 {
		t.Fatal("missing catalog/status wrote state")
	}
}
func TestManagerRepeatedConnectAndDisconnectRetainOwner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "selection")
	store := &fakeSelection{}
	g := &fixtureGeneration{plan: fixturePlan(t)}
	calls := 0
	m := NewManager(context.Background(), root, store, func(request, lifetime context.Context) (Generation, error) {
		calls++
		g.lifetime = lifetime
		return g, nil
	})
	request, cancel := context.WithCancel(context.Background())
	s, err := m.Connect(request)
	cancel()
	if err != nil || !s.SelectionConfigured || !s.JournalPresent || s.ChatVerified || s.BackendHandshakeVerified {
		t.Fatal(s, err)
	}
	if g.lifetime.Err() != nil {
		t.Fatal("request cancellation revoked launch lifetime")
	}
	if _, err = m.Connect(context.Background()); err != nil || calls != 1 {
		t.Fatal("repeat prepared a new generation", err, calls)
	}
	g.consumed = true
	if _, err = m.Connect(context.Background()); err == nil || calls != 1 {
		t.Fatal("consumed generation reused")
	}
	if _, err = m.Disconnect(context.Background()); err != nil || g.closed != 0 {
		t.Fatal("disconnect revoked launch owner", err)
	}
	if err = m.Close(context.Background()); err != nil || g.closed != 1 || g.lifetime.Err() == nil {
		t.Fatal("shutdown failed to revoke/drain", err)
	}
	if _, err = m.Connect(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("connected after shutdown")
	}
}
func TestManagerPreexistingJournalNeverPrepares(t *testing.T) {
	root := filepath.Join(t.TempDir(), "selection")
	store := &fakeSelection{}
	plan := fixturePlan(t)
	c := New(root, store, func(context.Context) (Plan, error) { return plan, nil })
	if _, err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m := NewManager(context.Background(), root, store, func(context.Context, context.Context) (Generation, error) { calls++; return nil, ErrUnavailable })
	defer m.Close(context.Background())
	if _, err := m.Connect(context.Background()); !errors.Is(err, ErrConflict) || calls != 0 {
		t.Fatal("adopted stale journal", err, calls)
	}
	if _, err := m.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestManagerUncertainSelectionRetainsOwnerAndCloseRetries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "selection")
	store := &fakeSelection{partial: true}
	g := &fixtureGeneration{plan: fixturePlan(t), closeErr: errors.New("not drained")}
	m := NewManager(context.Background(), root, store, func(context.Context, context.Context) (Generation, error) { return g, nil })
	if s, err := m.Connect(context.Background()); !errors.Is(err, ErrUncertain) || !s.RecoveryRequired || g.closed != 0 {
		t.Fatal(s, err)
	}
	if err := m.Close(context.Background()); err == nil || g.closed != 1 {
		t.Fatal("cleanup error hidden")
	}
	g.closeErr = nil
	if err := m.Close(context.Background()); err != nil || g.closed != 2 {
		t.Fatal("cleanup not retried", err)
	}
	if _, err := os.Stat(filepath.Join(root, "selection.json")); err != nil {
		t.Fatal("shutdown restored selection", err)
	}
}
func TestManagerPartialPreparationCleanup(t *testing.T) {
	g := &fixtureGeneration{closeErr: errors.New("blocked")}
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(context.Context, context.Context) (Generation, error) { return g, ErrUnavailable })
	if _, err := m.Connect(context.Background()); err == nil || g.closed != 1 {
		t.Fatal("partial ownership lost")
	}
	g.closeErr = nil
	if err := m.Close(context.Background()); err != nil || g.closed != 2 {
		t.Fatal("partial cleanup not retried")
	}
}
func TestManagerCloseCancelsInFlightPreparation(t *testing.T) {
	entered := make(chan struct{})
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(request, lifetime context.Context) (Generation, error) {
		close(entered)
		<-request.Done()
		return nil, request.Err()
	})
	done := make(chan error, 1)
	go func() { _, err := m.Connect(context.Background()); done <- err }()
	<-entered
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled preparation succeeded")
	}
}
func TestManagerConcurrentConnectSerializesPreparation(t *testing.T) {
	g := &fixtureGeneration{plan: fixturePlan(t)}
	calls := 0
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(context.Context, context.Context) (Generation, error) { calls++; return g, nil })
	defer m.Close(context.Background())
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := m.Connect(context.Background()); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("concurrent prepares", calls)
	}
}
func TestManagerCancelledRequestDoesNotPrepare(t *testing.T) {
	calls := 0
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(context.Context, context.Context) (Generation, error) { calls++; return nil, nil })
	defer m.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Connect(ctx); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled request prepared", err)
	}
}

func TestManagerDamagedJournalBlocksPreparation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "selection.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m := NewManager(context.Background(), root, &fakeSelection{}, func(context.Context, context.Context) (Generation, error) { calls++; return nil, nil })
	defer m.Close(context.Background())
	if s, err := m.Connect(context.Background()); err == nil || !s.RecoveryRequired || calls != 0 {
		t.Fatal("damaged journal treated as absence", s, err)
	}
}
func TestManagerCloseCancelledWaitRetainsOwner(t *testing.T) {
	g := &fixtureGeneration{plan: fixturePlan(t)}
	m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(context.Context, context.Context) (Generation, error) { return g, nil })
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Close(ctx); !errors.Is(err, context.Canceled) || g.closed != 0 {
		t.Fatal("expired cleanup falsely drained", err)
	}
	if err := m.Close(context.Background()); err != nil || g.closed != 1 {
		t.Fatal("retained owner not retried", err)
	}
}

func TestInspectRejectsLinkedRootAndJournal(t *testing.T) {
	for _, kind := range []string{"root", "journal"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			target := filepath.Join(base, "target")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "connection")
			if kind == "root" {
				if err := os.Symlink(target, root); err != nil {
					t.Skip("symlinks unavailable:", err)
				}
			} else {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(target, "journal.json")
				if err := os.WriteFile(file, []byte("unread target"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(file, filepath.Join(root, "selection.json")); err != nil {
					t.Skip("symlinks unavailable:", err)
				}
			}
			c := New(root, &fakeSelection{}, nil)
			if s, err := c.Inspect(); !errors.Is(err, ErrUnavailable) || !s.RecoveryRequired {
				t.Fatal("followed linked state", s, err)
			}
			if _, err := os.Stat(filepath.Join(target, "selection.lock")); !os.IsNotExist(err) {
				t.Fatal("inspection wrote lock")
			}
		})
	}
}

func TestManagerRepeatedConnectRejectsChangedProfileButRestores(t *testing.T) {
	for _, change := range []string{"missing", "tampered", "linked"} {
		t.Run(change, func(t *testing.T) {
			store := &fakeSelection{}
			g := &fixtureGeneration{plan: fixturePlan(t)}
			calls := 0
			m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), store, func(context.Context, context.Context) (Generation, error) { calls++; return g, nil })
			defer m.Close(context.Background())
			if _, err := m.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			profile := store.value.Profile.Text
			switch change {
			case "missing":
				if err := os.Remove(profile); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				if err := os.WriteFile(profile, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "linked":
				original, err := os.ReadFile(profile)
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "profile.json")
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(profile); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, profile); err != nil {
					t.Skip("symlinks unavailable:", err)
				}
			}
			if s, err := m.Connect(context.Background()); err == nil || calls != 1 || !s.RecoveryRequired || s.SelectionConfigured {
				t.Fatal("changed profile reported reusable", s, err, calls)
			}
			if _, err := m.Disconnect(context.Background()); err != nil || g.closed != 0 {
				t.Fatal("damaged profile prevented restore", err)
			}
		})
	}
}
