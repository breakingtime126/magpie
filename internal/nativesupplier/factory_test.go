package nativesupplier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativepreview"
	"github.com/yetone/magpie/internal/nativeprofile"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type fixtureStore struct {
	value  nativeconnect.Selection
	writes int
}

func (s *fixtureStore) Read() (nativeconnect.Selection, error) { return s.value, nil }
func (s *fixtureStore) Replace(before, after nativeconnect.Selection) error {
	if s.value != before {
		return nativeconnect.ErrConflict
	}
	s.value = after
	s.writes++
	return nil
}

type fixtureHost struct {
	plan      nativeconnect.Plan
	lifetime  context.Context
	closed    int
	closeErr  error
	admission func(context.Context) error
}

func (h *fixtureHost) Plan() (nativeconnect.Plan, error) {
	if h.lifetime.Err() != nil || h.admission(context.Background()) != nil || h.plan.PreparedProfile.ValidateFor(h.plan.Config) != nil {
		return nativeconnect.Plan{}, nativeconnect.ErrUnavailable
	}
	return h.plan, nil
}
func (h *fixtureHost) Close(context.Context) error { h.closed++; return h.closeErr }

func fixture(t *testing.T) (string, dependencies, *snapshot, **fixtureHost) {
	t.Helper()
	base := t.TempDir()
	parent, home := filepath.Join(base, "magpie"), filepath.Join(base, "native-home")
	codex := filepath.Join(home, "codex")
	for _, path := range []string{parent, home, codex} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command, launcher := filepath.Join(base, "inert-native.exe"), filepath.Join(base, "magpie-codex-bridge.exe")
	for _, path := range []string{command, launcher} {
		if err := os.WriteFile(path, []byte("nonexecuted authored fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	launcherSum := sha256.Sum256([]byte("nonexecuted authored fixture"))
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	// Intentionally not a native model record. This fixture exercises the
	// preparation contract and exact-byte preservation, never native parsing.
	data := []byte(`{"synthetic_supplier_pipeline_fixture":true}`)
	sum := sha256.Sum256(data)
	s := &snapshot{nativeVersion: nativepreview.SharedRuntimeVersion, catalog: data, digest: hex.EncodeToString(sum[:]), source: "authored-test-only", observed: now.Add(-time.Minute), expires: now.Add(time.Hour), validity: context.Background(), current: func(context.Context) bool { return true }}
	var host *fixtureHost
	d := dependencies{
		catalog: func(context.Context) (snapshot, error) { return *s, nil },
		now:     func() time.Time { return now }, ready: func() bool { return true },
		runtime: func(context.Context, []string, string) ([]appserverbridge.Backend, error) {
			return []appserverbridge.Backend{
				{ID: "openai", Provider: "openai", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("a", 64)},
				{ID: "aihub", Provider: "magpie_shared_aihub", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("a", 64)},
			}, nil
		},
		start: func(ctx context.Context, cfg appserverbridge.Config, root string, admission func(context.Context) error) (nativeconnect.Generation, error) {
			prepared, err := nativeprofile.New(strings.Repeat("1", 32), cfg)
			if err != nil {
				t.Fatal(err)
			}
			host = &fixtureHost{lifetime: ctx, admission: admission, plan: nativeconnect.Plan{Launcher: launcher, LauncherIdentity: nativeconnect.TrustedLauncherIdentity{Filename: filepath.Base(launcher), SHA256: hex.EncodeToString(launcherSum[:]), SourceRevision: strings.Repeat("b", 40)}, Config: cfg, PreparedProfile: prepared}}
			return host, nil
		},
	}
	return filepath.Join(parent, "connection"), d, s, &host
}

func TestProductionFactoryMissingMetadataIsZeroWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "connection")
	store := &fixtureStore{}
	m := nativeconnect.NewManager(context.Background(), root, store, NewFactory(nil, root, nil))
	defer m.Close(context.Background())
	_, err := m.Connect(context.Background())
	if !errors.Is(err, nativeconnect.ErrMissingCatalog) || !strings.Contains(err.Error(), "independently validated native catalog") {
		t.Fatal("missing requirement not explained", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) || store.writes != 0 {
		t.Fatal("missing supplier metadata created state")
	}
}

func TestFactoryRejectsUntrustedStaleOrChangedEvidenceBeforeEffects(t *testing.T) {
	for _, kind := range []string{"metadata", "digest", "expired", "future", "route", "gateway"} {
		t.Run(kind, func(t *testing.T) {
			root, d, s, _ := fixture(t)
			switch kind {
			case "metadata":
				s.nativeVersion = "unknown"
			case "digest":
				s.digest = strings.Repeat("0", 64)
			case "expired":
				s.expires = d.now()
			case "future":
				s.observed = d.now().Add(time.Minute)
			case "route":
				s.current = func(context.Context) bool { return false }
			case "gateway":
				d.ready = func() bool { return false }
			}
			d.runtime = func(context.Context, []string, string) ([]appserverbridge.Backend, error) {
				t.Fatal("invalid evidence reached runtime")
				return nil, nil
			}
			if owner, err := newFactory(root, nil, d)(context.Background(), context.Background()); err == nil || owner != nil {
				t.Fatal("invalid evidence accepted")
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid evidence created files")
			}
		})
	}
}

func TestFactoryRechecksRouteAfterRuntimeBeforeWrites(t *testing.T) {
	root, d, s, _ := fixture(t)
	original := d.runtime
	// The captured source closure checks the same underlying route generation.
	valid := true
	s.current = func(context.Context) bool { return valid }
	d.runtime = func(ctx context.Context, env []string, scratch string) ([]appserverbridge.Backend, error) {
		valid = false
		return original(ctx, env, scratch)
	}
	if owner, err := newFactory(root, nil, d)(context.Background(), context.Background()); !errors.Is(err, nativeconnect.ErrMissingCatalog) || owner != nil {
		t.Fatal("changed supplier route accepted", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("changed route created state")
	}
}

func TestFactoryConnectOwnsPinnedSupplierAndPreservesRequestLifetime(t *testing.T) {
	root, d, s, host := fixture(t)
	store := &fixtureStore{}
	m := nativeconnect.NewManager(context.Background(), root, store, newFactory(root, nil, d))
	defer m.Close(context.Background())
	request, cancel := context.WithCancel(context.Background())
	status, err := m.Connect(request)
	if err != nil || !status.SelectionConfigured || status.ChatVerified {
		t.Fatal(status, err)
	}
	cancel()
	if (*host).lifetime.Err() != nil {
		t.Fatal("HTTP cancellation revoked host")
	}
	cfg := (*host).plan.Config
	if len(cfg.Backends[0].InheritEnv) != 0 || len(cfg.Backends[1].InheritEnv) != 0 || cfg.Backends[0].Provider != "openai" || cfg.Backends[1].Provider != "magpie_shared_aihub" || cfg.SharedNativeRoute.SecondaryCatalogSHA256 != s.digest {
		t.Fatal("supplier preparation changed route or inherited credentials")
	}
	if _, err = m.Connect(context.Background()); err != nil || store.writes != 1 {
		t.Fatal("repeated connect reconfigured", err)
	}
	path := cfg.SharedNativeRoute.SecondaryCatalog
	if err := os.WriteFile(path, []byte(`{"changed":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Connect(context.Background()); err == nil || store.writes != 1 {
		t.Fatal("modified supplier bytes reused")
	}
	if _, err = m.Disconnect(context.Background()); err != nil || (*host).closed != 0 {
		t.Fatal("disconnect revoked owner", err)
	}
	if err := m.Close(context.Background()); err != nil || (*host).closed != 1 {
		t.Fatal("host not drained", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("drained supplier resources remain")
	}
}

func TestFactoryRetainsPartialHostAndFilesUntilCleanupSucceeds(t *testing.T) {
	root, d, _, host := fixture(t)
	start := d.start
	d.start = func(ctx context.Context, cfg appserverbridge.Config, path string, admission func(context.Context) error) (nativeconnect.Generation, error) {
		h, _ := start(ctx, cfg, path, admission)
		(*host).closeErr = errors.New("synthetic incomplete drain")
		return h, errors.New("synthetic partial startup")
	}
	m := nativeconnect.NewManager(context.Background(), root, &fixtureStore{}, newFactory(root, nil, d))
	if _, err := m.Connect(context.Background()); err == nil || (*host).closed != 1 {
		t.Fatal("partial owner lost", err)
	}
	path := (*host).plan.Config.SharedNativeRoute.SecondaryCatalog
	if _, err := os.Stat(path); err != nil {
		t.Fatal("removed files before verified drain")
	}
	(*host).closeErr = nil
	if err := m.Close(context.Background()); err != nil || (*host).closed != 2 {
		t.Fatal("cleanup retry failed", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retry did not remove owned directory")
	}
}

func TestFactoryRejectsNativeStateOverlapBeforeCreatingConnection(t *testing.T) {
	_, d, _, _ := fixture(t)
	backends, _ := d.runtime(context.Background(), nil, "")
	root := filepath.Join(backends[0].CodexHome, "must-not-create")
	if g, err := newFactory(root, nil, d)(context.Background(), context.Background()); err == nil || g != nil {
		t.Fatal("native state overlap accepted")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote inside native state")
	}
}

func TestSnapshotFreezesBytesBeforeSourceCallback(t *testing.T) {
	_, d, s, _ := fixture(t)
	want := string(s.catalog)
	s.current = func(context.Context) bool {
		copy(s.catalog, strings.Repeat("x", len(s.catalog)))
		return true
	}
	frozen, err := s.freeze(context.Background(), d.now())
	if err != nil || string(frozen.catalog) != want {
		t.Fatal("source mutation escaped immutable snapshot", err)
	}
}

func TestSnapshotRechecksCancellationAfterSourceCallback(t *testing.T) {
	_, d, s, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.current = func(context.Context) bool { cancel(); return true }
	if _, err := s.freeze(ctx, d.now()); err == nil {
		t.Fatal("cancelled source callback admitted snapshot")
	}
}

func TestFactoryRevocationCancelsGenerationWithoutAnotherConnect(t *testing.T) {
	root, d, s, host := fixture(t)
	validity, revoke := context.WithCancel(context.Background())
	defer revoke()
	s.validity = validity
	m := nativeconnect.NewManager(context.Background(), root, &fixtureStore{}, newFactory(root, nil, d))
	defer m.Close(context.Background())
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	revoke()
	select {
	case <-(*host).lifetime.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("revoked supplier generation stayed live")
	}
	if (*host).admission(context.Background()) == nil {
		t.Fatal("revoked generation still admitted launches")
	}
}

func TestFactoryDelayedAdmissionRejectsExpiredEvidence(t *testing.T) {
	root, d, s, host := fixture(t)
	now := d.now()
	d.now = func() time.Time { return now }
	m := nativeconnect.NewManager(context.Background(), root, &fixtureStore{}, newFactory(root, nil, d))
	defer m.Close(context.Background())
	if _, err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = s.expires
	if err := (*host).admission(context.Background()); !errors.Is(err, nativeconnect.ErrMissingCatalog) {
		t.Fatal("expired evidence admitted a delayed launch", err)
	}
}
