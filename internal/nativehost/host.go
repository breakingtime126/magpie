// Package nativehost owns one inactive-by-default native launch generation.
// It never discovers a gateway through a URL and never starts a native process.
package nativehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativeprofile"
)

var ErrUnavailable = errors.New("native host generation unavailable")

const cleanupLimit = 3 * time.Second

type capability interface {
	http.Handler
	Valid() bool
}
type ownerLease interface {
	Context() context.Context
	Close(context.Context) error
}
type pipeOwner interface {
	Await(context.Context) (ownerLease, error)
	Close() error
}
type dependencies struct {
	resolve func() (nativeconnect.PackagedLauncher, error)
	listen  func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error)
}

// Start requires already provenance/freshness-validated runtime/catalog inputs.
// Config.Validate is not supplier authentication. root must be an existing,
// separate caller-owned publication directory; no directory is discovered or
// created. No native executable, model catalog parser or account worker runs.
//
// The caller owns any nonnil Host, including on partial preparation failure,
// and must retain it independently of the Controller transaction. Obtain Plan
// only after registering the owner in the host lifetime.
func Start(parent context.Context, server *gateway.Server, cfg appserverbridge.Config, root string) (*Host, error) {
	return startServer(parent, server, cfg, root, nil)
}

// StartWithAdmission revalidates supplier evidence before allocation, when a
// Plan is requested, and on both bootstrap peer checks surrounding disclosure.
// The callback must honor cancellation, be concurrency-safe and disclose no
// secrets in errors. The parent lifetime must independently revoke on supplier
// route changes or expiry; admission is not polled by the host.
func StartWithAdmission(parent context.Context, server *gateway.Server, cfg appserverbridge.Config, root string, admission func(context.Context) error) (*Host, error) {
	if admission == nil {
		return nil, ErrUnavailable
	}
	return startServer(parent, server, cfg, root, admission)
}

func startServer(parent context.Context, server *gateway.Server, cfg appserverbridge.Config, root string, admission func(context.Context) error) (*Host, error) {
	if parent == nil || server == nil {
		return nil, ErrUnavailable
	}
	captured, ok := server.OwnedConnection()
	if !ok {
		return nil, ErrUnavailable
	}
	return startWithAdmission(parent, captured, cfg, root, dependencies{nativeconnect.ResolvePackagedLauncher, listenOwner}, admission)
}

type Host struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{} // first bounded cleanup attempt finished; not a drain claim
	session   *nativechannel.Session
	owner     pipeOwner
	lease     ownerLease
	baseErr   error
	ownerOnce sync.Once
	ownerErr  error
	captured  capability
	plan      nativeconnect.Plan
	consumed  atomic.Bool
	admission func(context.Context) error
}

func (h *Host) Context() context.Context { return h.ctx }
func (h *Host) Consumed() bool           { return h.consumed.Load() }

// Plan returns references and immutable profile bytes only. The host, not the
// Controller, retains resources. A consumed/expired generation cannot be reused.
// Selection restoration alone does not close this owner or a running lease.
func (h *Host) Plan() (nativeconnect.Plan, error) {
	if h == nil || h.ctx.Err() != nil || h.consumed.Load() || !h.captured.Valid() {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	select {
	case <-h.session.Revoked():
		return nativeconnect.Plan{}, ErrUnavailable
	default:
	}
	if err := checkAdmission(h.ctx, h.admission); err != nil {
		return nativeconnect.Plan{}, err
	}
	p := h.plan
	if p.PreparedProfile == nil || p.PreparedProfile.ValidateFor(p.Config) != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	// Return a private Config copy, preserving the immutable prepared snapshot.
	data, err := json.Marshal(p.Config)
	if err != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	p.Config = appserverbridge.Config{}
	if json.Unmarshal(data, &p.Config) != nil {
		return nativeconnect.Plan{}, ErrUnavailable
	}
	return p, nil
}
func start(parent context.Context, captured capability, cfg appserverbridge.Config, root string, deps dependencies) (*Host, error) {
	return startWithAdmission(parent, captured, cfg, root, deps, nil)
}

func startWithAdmission(parent context.Context, captured capability, cfg appserverbridge.Config, root string, deps dependencies, admission func(context.Context) error) (*Host, error) {
	if parent == nil || parent.Err() != nil || captured == nil || !captured.Valid() {
		return nil, ErrUnavailable
	}
	if cfg.SharedNativeRoute == nil || cfg.SharedNativeRoute.GatewayAuth != appserverbridge.SharedNativeLocalGateway167 || cfg.Validate() != nil || separateRoot(root, cfg) != nil {
		return nil, ErrUnavailable
	}
	launcher, err := deps.resolve()
	if err != nil {
		return nil, err
	}
	identity := nativeconnect.TrustedLauncherIdentity{Filename: filepath.Base(launcher.Path), SHA256: launcher.SHA256, SourceRevision: launcher.SourceRevision}
	verified, err := nativeconnect.VerifyPackagedLauncher(filepath.Dir(launcher.Path), identity)
	if err != nil || verified != launcher {
		return nil, ErrUnavailable
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	prepared, err := nativeprofile.New(hex.EncodeToString(random[:]), cfg)
	if err != nil {
		return nil, err
	}
	// Snapshot the caller's possibly aliased Config before resources are created.
	frozen, err := nativeprofile.LoadConfig(prepared.Bytes())
	if err != nil {
		return nil, err
	}
	if parent.Err() != nil || !captured.Valid() {
		return nil, ErrUnavailable
	}
	if err := checkAdmission(parent, admission); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	h := &Host{admission: admission, ctx: ctx, cancel: cancel, done: make(chan struct{}), captured: captured, plan: nativeconnect.Plan{Launcher: launcher.Path, LauncherIdentity: identity, Config: frozen, PreparedProfile: prepared}}
	fail := func(cause error) (*Host, error) {
		cancel()
		h.baseErr = cause
		closeContext, stop := context.WithTimeout(context.Background(), cleanupLimit)
		defer stop()
		cleanupErr := h.cleanup(closeContext)
		close(h.done)
		return h, errors.Join(cause, cleanupErr)
	}
	var handler http.Handler = captured
	if admission != nil {
		handler = admittedCapability{captured: captured, admission: admission}
	}
	h.session, err = nativechannel.Start(ctx, handler)
	if err != nil {
		return fail(err)
	}
	publication := filepath.Join(root, "channel-"+prepared.Generation()+".json")
	reference, err := nativechannel.Publish(publication, h.session.Descriptor())
	if err != nil {
		return fail(err)
	}
	record := nativebootstrap.Record{Version: 1, Generation: prepared.Generation(), Reference: reference, ProfileSHA256: prepared.SHA256()}
	if record.VerifyProfile(prepared.Bytes()) != nil {
		return fail(ErrUnavailable)
	}
	peer := nativebootstrap.Peer{Path: launcher.Path, Check: func(ctx context.Context, _ nativebootstrap.Process) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if h.ctx.Err() != nil {
			return ErrUnavailable
		}
		if err := checkAdmission(ctx, h.admission); err != nil {
			return err
		}
		current, err := nativeconnect.VerifyPackagedLauncher(filepath.Dir(launcher.Path), identity)
		if err != nil || current != launcher || ctx.Err() != nil || h.ctx.Err() != nil {
			return ErrUnavailable
		}
		return nil
	}}
	h.owner, err = deps.listen(record, peer)
	if err != nil {
		return fail(err)
	}
	if h.owner == nil || ctx.Err() != nil || !captured.Valid() {
		return fail(ErrUnavailable)
	}
	go h.serve(captured)
	return h, nil
}
func (h *Host) serve(captured capability) {
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-h.session.Revoked():
				h.cancel()
				return
			case <-ticker.C:
				if !captured.Valid() {
					h.cancel()
					return
				}
			}
		}
	}()
	lease, err := h.owner.Await(h.ctx)
	h.lease = lease
	if err != nil && h.ctx.Err() == nil {
		h.baseErr = err
	}
	if err == nil && lease == nil {
		h.baseErr = ErrUnavailable
	}
	if err == nil && lease != nil {
		h.consumed.Store(true)
		select {
		case <-lease.Context().Done():
		case <-h.ctx.Done():
		}
	}
	h.cancel()
	<-watchDone
	closeContext, stop := context.WithTimeout(context.Background(), cleanupLimit)
	_ = h.cleanup(closeContext) // Close retries actual cleanup and returns its result.
	stop()
	close(h.done)
}
func (h *Host) cleanup(ctx context.Context) error {
	var errs []error
	if h.owner != nil {
		h.ownerOnce.Do(func() { h.ownerErr = h.owner.Close() })
		errs = append(errs, h.ownerErr)
	}
	if h.lease != nil {
		errs = append(errs, h.lease.Close(ctx))
	}
	if h.session != nil {
		errs = append(errs, h.session.Close(ctx))
	}
	return errors.Join(errs...)
}

// Outcome reports preparation/serving failure after the owner has finished.
// It is separate from Close: a failed launch can still have fully drained resources.
func (h *Host) Outcome() (error, bool) {
	if h == nil {
		return nil, true
	}
	select {
	case <-h.done:
		return h.baseErr, true
	default:
		return nil, false
	}
}

// Close explicitly revokes this generation, then checks actual resource cleanup.
// Its result describes drainage only; Outcome retains any earlier launch failure.
// A timeout never means the lease/channel drained. Retain Host and retry Close
// with a new context if a cooperative handler finishes later. This method is not
// automatically called by nativeconnect.Disconnect: existing running windows
// remain untouched by selection restoration alone.
func (h *Host) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		return ErrUnavailable
	}
	h.cancel()
	select {
	case <-h.done:
		return h.cleanup(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A timeout bounds cooperative adapters; callbacks must honor their context.
func checkAdmission(parent context.Context, admission func(context.Context) error) error {
	if parent == nil || parent.Err() != nil {
		return ErrUnavailable
	}
	if admission == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, cleanupLimit)
	defer cancel()
	if err := admission(ctx); err != nil {
		return err
	}
	return ctx.Err()
}
