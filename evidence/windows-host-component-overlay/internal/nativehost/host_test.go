package nativehost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type fakeCapability struct {
	valid atomic.Bool
	calls atomic.Int32
}

func (c *fakeCapability) Valid() bool { return c.valid.Load() }
func (c *fakeCapability) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.Valid() {
		http.Error(w, "expired", 503)
		return
	}
	c.calls.Add(1)
	io.Copy(w, r.Body)
}

type fakeLease struct {
	ctx    context.Context
	cancel context.CancelFunc
	closes atomic.Int32
	err    error
}

func (l *fakeLease) Context() context.Context    { return l.ctx }
func (l *fakeLease) Close(context.Context) error { l.cancel(); l.closes.Add(1); return l.err }

type fakeOwner struct {
	ready  chan struct{}
	lease  *fakeLease
	closes atomic.Int32
	err    error
}

func (o *fakeOwner) Await(ctx context.Context) (ownerLease, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.ready:
		return o.lease, nil
	}
}
func (o *fakeOwner) Close() error { o.closes.Add(1); return o.err }
func fixture(t *testing.T) (appserverbridge.Config, string, nativeconnect.PackagedLauncher, *fakeCapability, *fakeOwner) {
	t.Helper()
	home := t.TempDir()
	codex := filepath.Join(home, "codex")
	control := filepath.Join(home, "control")
	work := filepath.Join(home, "work")
	root := filepath.Join(home, "publication")
	for _, p := range []string{codex, control, work, root} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	command, catalog, launcher := filepath.Join(home, "inert-native.exe"), filepath.Join(home, "inert-catalog.json"), filepath.Join(home, "magpie-codex-bridge.exe")
	for _, p := range []string{command, catalog, launcher} {
		if e := os.WriteFile(p, []byte("synthetic inert fixture never executed or used as model metadata"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	cfg := appserverbridge.Config{ControlPlane: appserverbridge.ControlPlaneRouting167, ControlHome: control, WorkingDirectory: work,
		SharedNativeRoute: &appserverbridge.SharedNativeRoute167{Mode: appserverbridge.SharedNativeHome167, GatewayURL: "http://127.0.0.1:19431/v1", GatewayEnvKey: "MAGPIE_SYNTHETIC_LABEL", GatewayAuth: appserverbridge.SharedNativeLocalGateway167, SecondaryCatalog: catalog},
		Backends:          []appserverbridge.Backend{{ID: "primary", Provider: "openai", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("a", 64)}, {ID: "secondary", Provider: "magpie_shared_aihub", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("b", 64)}}}
	bytes, e := os.ReadFile(launcher)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(bytes)
	pinned := nativeconnect.PackagedLauncher{Path: launcher, SHA256: hex.EncodeToString(sum[:]), SourceRevision: strings.Repeat("c", 40)}
	cap := &fakeCapability{}
	cap.valid.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return cfg, root, pinned, cap, &fakeOwner{ready: make(chan struct{}), lease: &fakeLease{ctx: ctx, cancel: cancel}}
}
func closeHost(t *testing.T, h *Host) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.Close(ctx)
}
func TestHostCapturesChannelProfileAndSingleUseLease(t *testing.T) {
	cfg, root, pinned, cap, owner := fixture(t)
	var record nativebootstrap.Record
	var peer nativebootstrap.Peer
	deps := dependencies{resolve: func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, listen: func(r nativebootstrap.Record, p nativebootstrap.Peer) (pipeOwner, error) {
		record, peer = r, p
		return owner, nil
	}}
	h, err := start(context.Background(), cap, cfg, root, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost(t, h)
	plan, err := h.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if record.ProfileSHA256 != plan.PreparedProfile.SHA256() || record.Generation != plan.PreparedProfile.Generation() || record.VerifyProfile(plan.PreparedProfile.Bytes()) != nil {
		t.Fatal("owner/profile identity mismatch")
	}
	if peer.Path != pinned.Path || peer.Check(context.Background(), nativebootstrap.Process{}) != nil {
		t.Fatal("lost compiled launcher policy")
	}
	// Caller mutations cannot change the captured Plan or profile.
	plan.Config.Backends[0].Provider = "changed"
	again, err := h.Plan()
	if err != nil || again.Config.Backends[0].Provider != "openai" {
		t.Fatal("mutable Plan escaped", err)
	}
	descriptor, err := nativechannel.ReadPublication(record.Reference)
	if err != nil {
		t.Fatal(err)
	}
	client, err := nativechannel.NewClient(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	req, _ := http.NewRequest("POST", descriptor.URL+"/v1/responses", strings.NewReader("synthetic-only"))
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "synthetic-only" || cap.calls.Load() != 1 {
		t.Fatal("channel did not use captured capability")
	}
	close(owner.ready)
	until := time.After(time.Second)
	for !h.Consumed() {
		select {
		case <-until:
			t.Fatal("lease not consumed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := h.Plan(); err == nil {
		t.Fatal("consumed generation offered another Plan")
	}
	// Restore-selection semantics: no Controller/Disconnect coupling closes this.
	if h.Context().Err() != nil {
		t.Fatal("active lease was silently revoked")
	}
	owner.lease.cancel()
	select {
	case <-h.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("lease end did not close owner")
	}
	if err := closeHost(t, h); err != nil {
		t.Fatal(err)
	}
	if _, err := nativechannel.ReadPublication(record.Reference); err != nil {
		t.Fatal("immutable recovery artifact removed")
	}
}
func TestHostGatewayLossAndHostCancellationRevokeWaitingOwner(t *testing.T) {
	for _, mode := range []string{"gateway", "parent"} {
		t.Run(mode, func(t *testing.T) {
			cfg, root, pinned, cap, owner := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h, err := start(ctx, cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return owner, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "gateway" {
				cap.valid.Store(false)
			} else {
				cancel()
			}
			select {
			case <-h.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("owner not revoked")
			}
			if _, err := h.Plan(); err == nil {
				t.Fatal("expired Plan accepted")
			}
			if err := closeHost(t, h); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestHostRootSeparationAndMissingPinAreZeroPublication(t *testing.T) {
	for _, mode := range []string{"home", "codex", "below-codex", "above-codex", "relative", "missing-pin"} {
		t.Run(mode, func(t *testing.T) {
			cfg, root, pinned, cap, _ := fixture(t)
			switch mode {
			case "home":
				root = cfg.Backends[0].Home
			case "codex":
				root = cfg.Backends[0].CodexHome
			case "below-codex":
				root = filepath.Join(cfg.Backends[0].CodexHome, "nested")
				os.Mkdir(root, 0700)
			case "above-codex":
				root = filepath.Dir(cfg.Backends[0].Home)
			case "relative":
				root = "relative"
			case "missing-pin":
				pinned.SHA256 = ""
			}
			listens := 0
			h, err := start(context.Background(), cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { listens++; return nil, nil }})
			if err == nil || h != nil || listens != 0 {
				t.Fatal("invalid preparation allocated owner", err)
			}
			if filepath.IsAbs(root) {
				matches, _ := filepath.Glob(filepath.Join(root, "channel-*.json"))
				if len(matches) != 0 {
					t.Fatal("invalid preparation published descriptor")
				}
			}
		})
	}
}
func TestHostFailedPreparationRetainsArtifactAndCleanupErrors(t *testing.T) {
	cfg, root, pinned, cap, owner := fixture(t)
	failure := errors.New("synthetic listener unavailable")
	h, err := start(context.Background(), cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return nil, failure }})
	if !errors.Is(err, failure) || h == nil {
		t.Fatal("lost partial owner", err)
	}
	if !errors.Is(closeHost(t, h), failure) {
		t.Fatal("lost preparation failure")
	}
	artifacts, _ := filepath.Glob(filepath.Join(root, "channel-*.json"))
	if len(artifacts) != 1 {
		t.Fatal("expected immutable artifact retained")
	}
	cfg, root, pinned, cap, owner = fixture(t)
	owner.err = failure
	h, err = start(context.Background(), cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return owner, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(closeHost(t, h), failure) || !errors.Is(closeHost(t, h), failure) {
		t.Fatal("owner close failure disappeared on retry")
	}
}

type selectionStore struct{ value nativeconnect.Selection }

func (s *selectionStore) Read() (nativeconnect.Selection, error) { return s.value, nil }
func (s *selectionStore) Replace(before, after nativeconnect.Selection) error {
	if s.value != before {
		return nativeconnect.ErrConflict
	}
	s.value = after
	return nil
}
func TestHostSelectionRestoreDoesNotCancelActiveLease(t *testing.T) {
	cfg, root, pinned, cap, owner := fixture(t)
	h, err := start(context.Background(), cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return owner, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost(t, h)
	store := &selectionStore{}
	controller := nativeconnect.New(filepath.Join(t.TempDir(), "selection"), store, func(context.Context) (nativeconnect.Plan, error) { return h.Plan() })
	if _, err := controller.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(owner.ready)
	limit := time.After(time.Second)
	for !h.Consumed() {
		select {
		case <-limit:
			t.Fatal("lease not admitted")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := controller.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if h.Context().Err() != nil || owner.lease.Context().Err() != nil {
		t.Fatal("selection restoration killed active lease")
	}
	if err := closeHost(t, h); err != nil {
		t.Fatal(err)
	}
	if owner.lease.Context().Err() == nil {
		t.Fatal("explicit owner shutdown failed to revoke lease")
	}
}

func TestHostSessionRevocationCancelsLeaseBeforeChannelDrain(t *testing.T) {
	cfg, root, pinned, _, owner := fixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	cap := &blockedCapability{entered: entered, release: release}
	h, err := start(context.Background(), cap, cfg, root, dependencies{func() (nativeconnect.PackagedLauncher, error) { return pinned, nil }, func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return owner, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(release)
		if err := closeHost(t, h); err != nil {
			t.Error(err)
		}
	}()
	client, err := nativechannel.NewClient(h.session.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	req, _ := http.NewRequest("POST", h.session.Descriptor().URL+"/v1/responses", nil)
	go func() {
		response, _ := client.Do(req)
		if response != nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not admitted")
	}
	close(owner.ready)
	until := time.After(time.Second)
	for !h.Consumed() {
		select {
		case <-until:
			t.Fatal("lease not admitted")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.session.Close(ctx); err == nil {
		t.Fatal("blocked handler reported drained")
	}
	select {
	case <-h.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("host waited for channel drain before revocation")
	}
	if _, err := h.Plan(); err == nil {
		t.Fatal("revoked channel still offered Plan")
	}
	select {
	case <-owner.lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("active lease waited for blocked channel drain")
	}

}

type blockedCapability struct{ entered, release chan struct{} }

func (*blockedCapability) Valid() bool { return true }
func (c *blockedCapability) ServeHTTP(http.ResponseWriter, *http.Request) {
	close(c.entered)
	<-c.release
}

func TestHostPlanRejectsRevocationBeforeConsumptionOrSupervisorCancel(t *testing.T) {
	cfg, _, _, cap, _ := fixture(t)
	session, err := nativechannel.Start(context.Background(), cap)
	if err != nil {
		t.Fatal(err)
	}
	// No host supervisor is started. Thus only the synchronous session-revocation
	// branch can invalidate Plan; context and consumed remain unchanged.
	h := &Host{ctx: context.Background(), session: session, captured: cap, plan: nativeconnect.Plan{Config: cfg}}
	if _, err := h.Plan(); err != nil {
		t.Fatal("live isolated plan unavailable", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if h.Context().Err() != nil || h.Consumed() {
		t.Fatal("test failed to isolate revocation guard")
	}
	if _, err := h.Plan(); err == nil {
		t.Fatal("revoked session passed synchronous Plan guard")
	}
}
