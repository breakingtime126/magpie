package appserverbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeendpoint"
	"github.com/yetone/magpie/internal/proc"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Authored Go helper only. It never runs an installed native executable.
func init() {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "production-owned-probe-fixture") && len(os.Args) > 3 && os.Args[1] == "app-server" {
		mode := "success"
		for _, arg := range os.Args[1:] {
			if arg == `model_provider="magpie_shared_aihub"` {
				data, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "secondary-mode"))
				mode = string(data)
			}
		}
		if mode == "descendant" {
			c := proc.Command(os.Args[0], "synthetic-production-probe", "leaf")
			c.Stdout = os.Stdout
			if c.Start() != nil {
				os.Exit(9)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "leaf-pid")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(9)
				}
				time.Sleep(time.Millisecond)
			}
		}
		fmt.Print(probe167SuccessFixture)
		if mode == "nonzero" {
			os.Exit(7)
		}
		os.Exit(0)
	}

	if len(os.Args) != 3 || os.Args[1] != "synthetic-production-probe" {
		return
	}
	home := os.Getenv("HOME")
	mode := os.Args[2]
	if mode == "leaf" {
		_ = os.WriteFile(filepath.Join(home, "leaf-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "descendant" {
		c := proc.Command(os.Args[0], "synthetic-production-probe", "leaf")
		c.Stdout = os.Stdout
		if c.Start() != nil {
			os.Exit(9)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(home, "leaf-pid")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(9)
			}
			time.Sleep(time.Millisecond)
		}
	}
	fmt.Print(probe167SuccessFixture)
	if mode == "nonzero" {
		os.Exit(7)
	}
	if mode == "closed-output-hang" {
		os.Stdout.Close()
		_ = os.WriteFile(filepath.Join(home, "closed-output"), []byte("ready"), 0600)
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestWindowsProductionPrimaryProbeRequiresSuccessfulDrainedExit(t *testing.T) {
	for _, mode := range []string{"success", "nonzero", "descendant", "closed-output-hang"} {
		t.Run(mode, func(t *testing.T) {
			cfg := startupProbeFixtureConfig(t)
			b := cfg.Backends[0]
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			type result struct {
				data []byte
				err  error
			}
			done := make(chan result, 1)
			go func() {
				data, err := runProductionPrimaryProbe(ctx, b, b.Home, []string{"synthetic-production-probe", mode}, backendEnvironment(b))
				done <- result{data, err}
			}()
			if mode == "closed-output-hang" {
				ready := filepath.Join(b.Home, "closed-output")
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					select {
					case r := <-done:
						t.Fatalf("returned before cancellation: %v", r.err)
					case <-ctx.Done():
						t.Fatal("fixture did not start")
					case <-time.After(time.Millisecond):
					}
				}
				select {
				case <-done:
					t.Fatal("stdout EOF was accepted before native exit")
				default:
				}
				cancel()
			}
			r := <-done
			failed := mode == "nonzero" || mode == "closed-output-hang"
			if (r.err != nil) != failed || (failed && len(r.data) != 0) {
				t.Fatal("wrong exit/drain result", mode, r.err)
			}
			if !failed && string(r.data) != probe167SuccessFixture {
				t.Fatal("genuine bytes changed")
			}
			if mode == "descendant" {
				pidBytes, err := os.ReadFile(filepath.Join(b.Home, "leaf-pid"))
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(string(pidBytes))
				if err != nil {
					t.Fatal(err)
				}
				assertStartupProbeProcessExited(t, pid)
			}
		})
	}
}

func TestWindowsOwnedProbeUsesActualEndpointExitAndDrain(t *testing.T) {
	for _, mode := range []string{"success", "nonzero", "descendant"} {
		t.Run(mode, func(t *testing.T) {
			cfg := sharedNativeConfigFixture167(t)
			cfg.StartupProbe = StartupProbeCompareUnrestricted167
			cfg.SharedNativeRoute.GatewayAuth = SharedNativeLocalGateway167
			cfg.Backends[1].InheritEnv = nil
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
			path := filepath.Join(t.TempDir(), "production-owned-probe-fixture.exe")
			if err = os.WriteFile(path, data, 0700); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(data)
			for i := range cfg.Backends {
				cfg.Backends[i].Command = path
				cfg.Backends[i].StartupProbeSHA256 = hex.EncodeToString(hash[:])
			}
			if err = os.WriteFile(filepath.Join(cfg.Backends[0].Home, "secondary-mode"), []byte(mode), 0600); err != nil {
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
			endpoint, err := nativeendpoint.Listen()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := ProbeStartupWithNativeEndpoint(ctx, cfg, LaunchOptions{}, StartupClientInfo{Name: "synthetic", Version: "1"}, endpoint, reference)
			if mode == "nonzero" {
				if err == nil || len(result) != 0 {
					t.Fatal("native nonzero exit accepted")
				}
			} else if err != nil || len(result) == 0 {
				t.Fatal("genuine pair failed", err)
			}
			if mode == "descendant" {
				data, err := os.ReadFile(filepath.Join(cfg.Backends[0].Home, "leaf-pid"))
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					t.Fatal(err)
				}
				assertStartupProbeProcessExited(t, pid)
			}
		})
	}
}
