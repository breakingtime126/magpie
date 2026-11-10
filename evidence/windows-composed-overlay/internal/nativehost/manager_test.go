package nativehost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativeconnect"
)

type managerGeneration struct {
	plan      nativeconnect.Plan
	ctx       context.Context
	cancel    context.CancelFunc
	stale     bool
	closes    int
	closeErr  error
	checkPlan func()
}

func (g *managerGeneration) Plan() (nativeconnect.Plan, error) {
	if g.checkPlan != nil {
		g.checkPlan()
	}
	if g.stale || g.ctx.Err() != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	return g.plan, nil
}

func (g *managerGeneration) Close(ctx context.Context) error {
	g.closes++
	g.cancel()
	if g.closeErr != nil {
		err := g.closeErr
		g.closeErr = nil
		return err
	}
	return ctx.Err()
}

type managerTransaction struct {
	prepare    nativeconnect.Prepare
	status     nativeconnect.Status
	connectErr error
	inspectErr error
	prepareErr error
	connects   int
}

func (c *managerTransaction) Inspect() (nativeconnect.Status, error) {
	return c.status, c.inspectErr
}

func (c *managerTransaction) Connect(ctx context.Context) (nativeconnect.Status, error) {
	c.connects++
	if _, err := c.prepare(ctx); err != nil {
		c.prepareErr = err
		return nativeconnect.Status{}, err
	}
	if c.connectErr != nil {
		if errors.Is(c.connectErr, nativeconnect.ErrUncertain) {
			c.status = nativeconnect.Status{JournalPresent: true, RecoveryRequired: true}
		}
		return c.status, c.connectErr
	}
	c.status = nativeconnect.Status{JournalPresent: true, SelectionConfigured: true}
	return c.status, nil
}

func (c *managerTransaction) Disconnect() (nativeconnect.Status, error) {
	c.status = nativeconnect.Status{}
	return c.status, nil
}

func managerFixture(t *testing.T) (*Manager, *managerTransaction, *[]*managerGeneration) {
	t.Helper()
	window, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &managerTransaction{}
	var generations []*managerGeneration
	var m *Manager
	m = newManager(window, func(context.Context) (appserverbridge.Config, error) {
		return appserverbridge.Config{}, nil
	}, func(ctx context.Context, _ appserverbridge.Config) (generation, error) {
		child, stop := context.WithCancel(ctx)
		g := &managerGeneration{ctx: child, cancel: stop}
		g.checkPlan = func() {
			if len(m.owners) == 0 {
				t.Error("Plan called before registering its owner")
			}
		}
		generations = append(generations, g)
		return g, nil
	}, func(p nativeconnect.Prepare) transaction {
		c.prepare = p
		return c
	})
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m, c, &generations
}

func TestManagerMissingCatalogWritesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	store := &managerStore{}
	m := NewManager(context.Background(), nil, root, store, nil)
	m.start = func(context.Context, appserverbridge.Config) (generation, error) {
		t.Fatal("missing catalog invoked Start")
		return nil, ErrUnavailable
	}
	defer m.Close(context.Background())
	if _, err := m.Connect(context.Background()); !errors.Is(err, nativeconnect.ErrMissingCatalog) {
		t.Fatalf("Connect = %v", err)
	}
	if store.reads != 0 || store.writes != 0 {
		t.Fatal("missing catalog touched selection store")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing catalog created state: %v", err)
	}
}

func TestManagerRequestCancellationAndRepeatedConnect(t *testing.T) {
	m, c, generations := managerFixture(t)
	request, cancel := context.WithCancel(context.Background())
	if _, err := m.Connect(request); err != nil {
		t.Fatal(err)
	}
	cancel()
	g := (*generations)[0]
	if g.ctx.Err() != nil {
		t.Fatal("request cancellation revoked window generation")
	}
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*generations) != 1 || c.connects != 1 {
		t.Fatal("repeated Connect allocated another generation/transaction")
	}
}

func TestManagerFailureCleanupAndUncertainRetention(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "uncertain"}[uncertain], func(t *testing.T) {
			m, c, generations := managerFixture(t)
			c.connectErr = nativeconnect.ErrUnavailable
			if uncertain {
				c.connectErr = nativeconnect.ErrUncertain
			}
			if _, err := m.Connect(context.Background()); err == nil {
				t.Fatal("failed transaction succeeded")
			}
			g := (*generations)[0]
			if uncertain {
				if g.closes != 0 || len(m.owners) != 1 || g.ctx.Err() != nil {
					t.Fatal("uncertain transaction revoked its generation")
				}
			} else if g.closes != 1 || len(m.owners) != 0 {
				t.Fatal("known failure did not drain unused generation")
			}
		})
	}
}

func TestManagerUnknownInspectionRetainsFailedStartOwner(t *testing.T) {
	m, c, generations := managerFixture(t)
	start := m.start
	m.start = func(ctx context.Context, cfg appserverbridge.Config) (generation, error) {
		g, _ := start(ctx, cfg)
		c.inspectErr = errors.New("unreadable journal")
		return g, ErrUnavailable
	}
	if _, err := m.Connect(context.Background()); err == nil {
		t.Fatal("partial start succeeded")
	}
	if len(m.owners) != 1 || (*generations)[0].closes != 0 {
		t.Fatal("unknown transaction discarded partial-start owner")
	}
}

func TestManagerPartialStartCleanupRequiresSuccessfulClose(t *testing.T) {
	m, _, generations := managerFixture(t)
	start := m.start
	m.start = func(ctx context.Context, cfg appserverbridge.Config) (generation, error) {
		g, _ := start(ctx, cfg)
		g.(*managerGeneration).closeErr = context.DeadlineExceeded
		return g, ErrUnavailable
	}
	if _, err := m.Connect(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup error lost: %v", err)
	}
	if len(m.owners) != 1 || (*generations)[0].closes != 1 {
		t.Fatal("unverified cleanup discarded owner")
	}
	if err := m.Close(context.Background()); err != nil || len(m.owners) != 0 {
		t.Fatalf("retry did not drain: %v", err)
	}
}

func TestManagerDisconnectRetainsConsumedOwnerAndStartsFresh(t *testing.T) {
	m, _, generations := managerFixture(t)
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := (*generations)[0]
	old.stale = true // models a consumed generation; Plan is no longer reusable.
	if _, err := m.Connect(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale generation reused: %v", err)
	}
	if len(*generations) != 1 {
		t.Fatal("stale selection silently replaced")
	}
	if _, err := m.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if old.closes != 0 || old.ctx.Err() != nil {
		t.Fatal("Disconnect killed a running owner")
	}
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*generations) != 2 || len(m.owners) != 2 {
		t.Fatal("reconnect did not register a fresh independent owner")
	}
}

func TestManagerCloseRetriesDrainAndRejectsConnect(t *testing.T) {
	m, _, generations := managerFixture(t)
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	g := (*generations)[0]
	g.closeErr = context.DeadlineExceeded
	if err := m.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) || len(m.owners) != 1 {
		t.Fatalf("timeout discarded owner: %v", err)
	}
	if g.ctx.Err() == nil {
		t.Fatal("Close did not cancel owner")
	}
	if _, err := m.Connect(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed window accepted Connect: %v", err)
	}
	if err := m.Close(context.Background()); err != nil || len(m.owners) != 0 || g.closes != 2 {
		t.Fatalf("retry did not verify drain: %v", err)
	}
}

func TestManagerConcurrentConnectSerializesPreparation(t *testing.T) {
	m, c, generations := managerFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Connect(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(*generations) != 1 || c.connects != 1 {
		t.Fatal("concurrent Connect allocated multiple generations")
	}
}

func TestManagerCloseCancelsBeforeJoiningPreparation(t *testing.T) {
	m, _, _ := managerFixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.catalog = func(context.Context) (appserverbridge.Config, error) {
		close(entered)
		<-release
		return appserverbridge.Config{}, nil
	}
	go func() {
		defer close(done)
		_, _ = m.Connect(context.Background())
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.Close(ctx)
	close(release)
	<-done
	if !errors.Is(err, context.Canceled) || m.ctx.Err() == nil {
		t.Fatalf("Close did not cancel before bounded join: %v", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// managerStore is an in-memory selection pair. The real Controller still owns
// profile validation, launcher verification, journal publication and restoration.
type managerStore struct {
	selection nativeconnect.Selection
	readErr   error
	writeErr  error
	reads     int
	writes    int
}

func (s *managerStore) Read() (nativeconnect.Selection, error) {
	s.reads++
	return s.selection, s.readErr
}

func (s *managerStore) Replace(before, after nativeconnect.Selection) error {
	s.writes++
	if before != s.selection {
		return nativeconnect.ErrConflict
	}
	s.selection = after
	return s.writeErr
}

func TestManagerRealControllerOwnsSelectionNotGeneration(t *testing.T) {
	for _, outcome := range []string{"success", "known failure", "uncertain write"} {
		t.Run(outcome, func(t *testing.T) {
			cfg, root, launcher, _, _ := fixture(t)
			store := &managerStore{}
			if outcome == "known failure" {
				store.readErr = errors.New("selection unavailable")
			}
			if outcome == "uncertain write" {
				store.writeErr = errors.New("notification failed after write")
			}
			var m *Manager
			var owner *managerGeneration
			starts := 0
			m = newManager(context.Background(), func(context.Context) (appserverbridge.Config, error) {
				return cfg, nil
			}, func(ctx context.Context, config appserverbridge.Config) (generation, error) {
				starts++
				child, cancel := context.WithCancel(ctx)
				owner = &managerGeneration{ctx: child, cancel: cancel, plan: nativeconnect.Plan{
					Launcher: launcher.Path, Config: config,
					LauncherIdentity: nativeconnect.TrustedLauncherIdentity{
						Filename: filepath.Base(launcher.Path), SHA256: launcher.SHA256, SourceRevision: launcher.SourceRevision,
					},
				}}
				owner.checkPlan = func() {
					if len(m.owners) != 1 || m.owners[0] != owner {
						t.Error("real transaction received Plan before lifetime registration")
					}
				}
				return owner, nil
			}, func(prepare nativeconnect.Prepare) transaction {
				return nativeconnect.New(root, store, prepare)
			})
			t.Cleanup(func() {
				if err := m.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			status, err := m.Connect(context.Background())
			if owner == nil {
				t.Fatalf("transaction never prepared owner: %v", err)
			}
			switch outcome {
			case "known failure":
				if err == nil || owner.closes != 1 || len(m.owners) != 0 || store.writes != 0 {
					t.Fatalf("known failure did not clean unused owner: %+v, %v", status, err)
				}
			case "uncertain write":
				if !errors.Is(err, nativeconnect.ErrUncertain) || !status.JournalPresent || owner.closes != 0 || len(m.owners) != 1 {
					t.Fatalf("uncertain selection lost owner/journal: %+v, %v", status, err)
				}
			case "success":
				if err != nil || !status.SelectionConfigured || owner.closes != 0 {
					t.Fatalf("Connect = %+v, %v", status, err)
				}
				if _, err := m.Connect(context.Background()); err != nil || starts != 1 {
					t.Fatalf("repeated Connect allocated another owner: %v", err)
				}
				if _, err := m.Disconnect(context.Background()); err != nil {
					t.Fatal(err)
				}
				if store.selection != (nativeconnect.Selection{}) || owner.closes != 0 || owner.ctx.Err() != nil {
					t.Fatal("Disconnect failed restoration or revoked independent owner")
				}
				if _, err := os.Stat(filepath.Join(root, "selection.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("successful restoration kept journal: %v", err)
				}
			}
		})
	}
}
