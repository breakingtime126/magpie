package appserverbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativehandshake"
	"github.com/yetone/magpie/internal/proc"
)

type syntheticProbeEndpoint struct {
	net.Listener
	tree        *proc.Tree
	waited      chan struct{}
	waitErr     error
	once        sync.Once
	starts      int
	beforeStart func()
	afterWait   func()
}

func (e *syntheticProbeEndpoint) Start(c *exec.Cmd) error {
	e.starts++
	if e.beforeStart != nil {
		e.beforeStart()
	}
	tree, err := proc.StartTreeStrict(c)
	if err != nil {
		return err
	}
	e.tree = tree
	go func() {
		e.waitErr = tree.Wait()
		if e.afterWait != nil {
			e.afterWait()
		}
		close(e.waited)
		e.Listener.Close()
	}()
	return nil
}
func (e *syntheticProbeEndpoint) WaitForExit(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.waited:
		return e.waitErr
	}
}
func (e *syntheticProbeEndpoint) Shutdown(ctx context.Context) error {
	e.once.Do(func() {
		e.Listener.Close()
		if e.tree != nil {
			e.tree.Stop(0)
		}
	})
	if e.tree == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.waited:
		return nil
	}
}
func TestOwnedStartupProbeUsesGenuinePairAndRejectsMutation(t *testing.T) {
	for _, mode := range []string{"success", "success-isolated-codex", "home-mismatch", "profile-mismatch", "bad-secondary", "changed-catalog", "changed-digest", "wrong-reference"} {
		t.Run(mode, func(t *testing.T) {
			cfg := sharedNativeConfigFixture167(t)
			cfg.StartupProbe = StartupProbeCompareUnrestricted167
			cfg.SharedNativeRoute.GatewayAuth = SharedNativeLocalGateway167
			cfg.Backends[1].InheritEnv = nil
			if mode == "success-isolated-codex" {
				// Model normal Desktop activation's session identity separately
				// from the fixture's still-fresh pinned native state directory.
				sessionHome, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				for i := range cfg.Backends {
					cfg.Backends[i].Home = sessionHome
				}
			}
			t.Setenv("HOME", cfg.Backends[0].Home)
			t.Setenv("USERPROFILE", cfg.Backends[0].Home)
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			// This copied test executable dispatches the package's authored probe fixture.
			command := filepath.Join(t.TempDir(), "synthetic-probe-test.exe")
			if err = os.WriteFile(command, data, 0700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			for i := range cfg.Backends {
				cfg.Backends[i].Command = command
				cfg.Backends[i].StartupProbeSHA256 = hex.EncodeToString(digest[:])
			}
			catalog, err := os.ReadFile(cfg.SharedNativeRoute.SecondaryCatalog)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(catalog)
			cfg.SharedNativeRoute.SecondaryCatalogSHA256 = hex.EncodeToString(sum[:])
			modePath := filepath.Join(cfg.Backends[0].Home, "startup-probe-mode")
			if err = os.WriteFile(modePath, []byte("success-167"), 0600); err != nil {
				t.Fatal(err)
			}
			session, err := nativechannel.Start(context.Background(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("probe reached supplier") }))
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(context.Background())
			reference, err := nativechannel.Publish(filepath.Join(t.TempDir(), "channel.json"), session.Descriptor())
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ep := &syntheticProbeEndpoint{Listener: listener, waited: make(chan struct{})}
			switch mode {
			case "home-mismatch":
				t.Setenv("HOME", t.TempDir())
			case "profile-mismatch":
				t.Setenv("USERPROFILE", t.TempDir())
			case "bad-secondary":
				ep.beforeStart = func() {
					if err := os.WriteFile(modePath, []byte("malformed"), 0600); err != nil {
						t.Error(err)
					}
				}
			case "changed-catalog":
				ep.afterWait = func() {
					if err := os.WriteFile(cfg.SharedNativeRoute.SecondaryCatalog, []byte("changed"), 0600); err != nil {
						t.Error(err)
					}
				}
			case "changed-digest":
				ep.afterWait = func() {
					replacement := command + ".replacement"
					if err := os.WriteFile(replacement, []byte("changed"), 0700); err != nil {
						t.Error(err)
						return
					}
					if err := os.Rename(replacement, command); err != nil {
						t.Error(err)
					}
				}
			case "wrong-reference":
				reference.SHA256 = hex.EncodeToString(make([]byte, 32))
			}
			var stages []nativehandshake.Stage
			ctx := nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) { stages = append(stages, s) })
			out, err := probeWithNativeEndpoint(ctx, cfg, LaunchOptions{}, StartupClientInfo{Name: "synthetic", Version: "1"}, ep, reference)
			if mode == "success" || mode == "success-isolated-codex" {
				if err != nil || len(out) == 0 {
					t.Fatal("valid native pair failed", err)
				}
			} else if err == nil || len(out) != 0 {
				t.Fatal("untrusted success", mode, err)
			}
			if mode == "wrong-reference" && ep.starts != 0 {
				t.Fatal("untrusted reference started process")
			}
			want := nativehandshake.ProbeEnvironmentAdmitted
			if mode == "home-mismatch" || mode == "profile-mismatch" {
				want = nativehandshake.ProbeEnvironmentRejected
				if ep.starts != 0 {
					t.Fatal("mismatched identity started secondary")
				}
				for _, home := range []string{cfg.Backends[0].Home, os.Getenv("HOME"), os.Getenv("USERPROFILE")} {
					if _, err := os.Stat(filepath.Join(home, "startup-probe-report")); !os.IsNotExist(err) {
						t.Fatal("mismatched identity reached a native fixture or report absence is unknown")
					}
				}
			}
			if mode == "wrong-reference" {
				if len(stages) != 0 {
					t.Fatal("untrusted reference claimed environment admission")
				}
			} else if len(stages) != 1 || stages[0] != want {
				t.Fatal("actual probe environment boundary was not observed")
			}
			if err := ep.Shutdown(context.Background()); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}
