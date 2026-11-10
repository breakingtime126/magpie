package nativesupplier

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativehost"
)

type dependencies struct {
	catalog func(context.Context) (snapshot, error)
	runtime func(context.Context, []string, string) ([]appserverbridge.Backend, error)
	ready   func() bool
	start   func(context.Context, appserverbridge.Config, string, func(context.Context) error) (nativeconnect.Generation, error)
	now     func() time.Time
}

// NewFactory binds the actual desktop-owned server, not a URL found in saved
// settings. Construction is side-effect free. Preparation reads the trusted
// supplier source before runtime probing or creating transaction resources.
// Real preparation remains unavailable until configuredCatalog is implemented.
func NewFactory(server *gateway.Server, root string, environment []string) nativeconnect.PrepareGeneration {
	environment = append([]string(nil), environment...)
	return newFactory(root, environment, dependencies{
		catalog: configuredCatalog, runtime: nativeconnect.PrepareRuntimeBackends,
		ready: func() bool {
			if server == nil {
				return false
			}
			owned, ok := server.OwnedConnection()
			return ok && owned.Valid()
		},
		start: func(ctx context.Context, cfg appserverbridge.Config, publicationRoot string, admission func(context.Context) error) (nativeconnect.Generation, error) {
			host, err := nativehost.StartWithAdmission(ctx, server, cfg, publicationRoot, admission)
			if host == nil {
				return nil, err
			}
			return host, err
		},
		now: time.Now,
	})
}

func newFactory(root string, environment []string, d dependencies) nativeconnect.PrepareGeneration {
	environment = append([]string(nil), environment...)
	return func(request, lifetime context.Context) (nativeconnect.Generation, error) {
		if request == nil || lifetime == nil || request.Err() != nil || lifetime.Err() != nil || d.catalog == nil {
			return nil, nativeconnect.ErrUnavailable
		}
		catalog, err := d.catalog(request)
		if err != nil {
			return nil, err
		}
		if d.now == nil || d.ready == nil || d.runtime == nil || d.start == nil {
			return nil, nativeconnect.ErrUnavailable
		}
		catalog, err = catalog.freeze(request, d.now())
		if err != nil {
			return nil, err
		}
		if !d.ready() || !safePath(root) {
			return nil, nativeconnect.ErrUnavailable
		}
		// Runtime preparation validates the existing parent against native homes
		// before creating its own disposable --version probe directory.
		backends, err := d.runtime(request, environment, filepath.Dir(root))
		if err != nil || request.Err() != nil || lifetime.Err() != nil || len(backends) != 2 {
			return nil, nativeconnect.ErrUnavailable
		}
		if !separate(root, backends) {
			return nil, nativeconnect.ErrUnavailable
		}
		// Recheck supplier identity/freshness and ownership after runtime work.
		catalog, err = catalog.freeze(request, d.now())
		if err != nil {
			return nil, err
		}
		if !d.ready() {
			return nil, nativeconnect.ErrUnavailable
		}
		if err := os.Mkdir(root, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, nativeconnect.ErrUnavailable
		}
		if !safePath(root) {
			return nil, nativeconnect.ErrUnavailable
		}
		work, err := os.MkdirTemp(root, "generation-")
		if err != nil {
			return nil, nativeconnect.ErrUnavailable
		}
		generationContext, cancel := context.WithTimeout(lifetime, catalog.expires.Sub(d.now()))
		stop := context.AfterFunc(catalog.validity, cancel)
		g := &generation{root: work, catalog: catalog, now: d.now, cancel: cancel, stop: stop}
		control, cwd := filepath.Join(work, "control"), filepath.Join(work, "work")
		if os.Mkdir(control, 0700) != nil || os.Mkdir(cwd, 0700) != nil {
			return g, nativeconnect.ErrUnavailable
		}
		g.catalogPath = filepath.Join(work, "supplier-catalog.json")
		if writeCatalog(g.catalogPath, catalog.catalog) != nil {
			return g, nativeconnect.ErrUnavailable
		}
		cfg := appserverbridge.Config{
			Backends: backends, ControlPlane: appserverbridge.ControlPlaneRouting167,
			ControlHome: control, WorkingDirectory: cwd,
			SharedNativeRoute: &appserverbridge.SharedNativeRoute167{
				Mode: appserverbridge.SharedNativeHome167,
				// Replaced only by the retained native endpoint, never dialed by
				// the host. No gateway address or secret is discovered here.
				GatewayURL: "http://127.0.0.1:1/v1", GatewayEnvKey: "MAGPIE_NATIVE_GATEWAY_CALLER",
				GatewayAuth:      appserverbridge.SharedNativeLocalGateway167,
				SecondaryCatalog: g.catalogPath, SecondaryCatalogSHA256: catalog.digest,
			},
		}
		if request.Err() != nil || lifetime.Err() != nil || !d.ready() || cfg.Validate() != nil {
			return g, nativeconnect.ErrUnavailable
		}
		// Return partial host ownership too. Manager owns cleanup independently
		// of the request and cannot mistake failed preparation for no resources.
		g.host, err = d.start(generationContext, cfg, work, func(ctx context.Context) error {
			_, err := catalog.freeze(ctx, d.now())
			return err
		})
		return g, err
	}
}

type generation struct {
	host        nativeconnect.Generation
	root        string
	catalogPath string
	catalog     snapshot
	now         func() time.Time
	cancel      context.CancelFunc
	stop        func() bool
}

func (g *generation) Plan() (nativeconnect.Plan, error) {
	if g == nil || g.host == nil || g.root == "" {
		return nativeconnect.Plan{}, nativeconnect.ErrUnavailable
	}
	// A completed HTTP request is not a launch lifetime. Revalidation gets a
	// bounded independent context and cannot accidentally revoke an owner.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := g.catalog.freeze(ctx, g.now()); err != nil {
		return nativeconnect.Plan{}, err
	}
	// Host.Plan revalidates the exact profile and its pinned catalog bytes.
	return g.host.Plan()
}

func (g *generation) Close(ctx context.Context) error {
	if g == nil || ctx == nil {
		return nativeconnect.ErrUnavailable
	}
	if g.cancel != nil {
		g.cancel()
	}
	if g.stop != nil {
		g.stop()
	}
	if g.host != nil {
		if err := g.host.Close(ctx); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Only this call's fresh directory is removed, after verified host drain.
	// Never remove native state or the separate selection journal/profile.
	if g.root != "" {
		if err := os.RemoveAll(g.root); err != nil {
			return err
		}
		g.root = ""
	}
	return nil
}

func writeCatalog(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func safePath(root string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return false
	}
	for p := root; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) || err != nil && !errors.Is(err, os.ErrNotExist) {
			return false
		}
		if p == filepath.Dir(p) {
			return true
		}
	}
}

func separate(root string, backends []appserverbridge.Backend) bool {
	inside := func(parent, child string) bool {
		if runtime.GOOS == "windows" {
			parent, child = strings.ToLower(parent), strings.ToLower(child)
		}
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	ancestor := func(parent, child string) bool {
		p, err := os.Stat(parent)
		if err != nil {
			return false
		}
		for {
			c, err := os.Stat(child)
			if err == nil && os.SameFile(p, c) {
				return true
			}
			if filepath.Dir(child) == child {
				return false
			}
			child = filepath.Dir(child)
		}
	}
	for _, b := range backends {
		rootInfo, rootErr := os.Stat(root)
		homeInfo, homeErr := os.Stat(b.Home)
		sameHome := rootErr == nil && homeErr == nil && os.SameFile(rootInfo, homeInfo)
		if root == b.Home || sameHome || inside(root, b.CodexHome) || inside(b.CodexHome, root) || ancestor(root, b.CodexHome) || ancestor(b.CodexHome, root) {
			return false
		}
	}
	return true
}
