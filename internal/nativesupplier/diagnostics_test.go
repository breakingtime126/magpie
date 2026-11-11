package nativesupplier

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativeprepdiag"
)

func TestFactoryDiagnosticsPreserveTransactionAndRedact(t *testing.T) {
	for _, failure := range []string{"", "metadata", "freshness", "gateway", "runtime", "host", "sink"} {
		t.Run(failure, func(t *testing.T) {
			root, d, s, host := fixture(t)
			calls := []string{}
			catalog, runtime, start := d.catalog, d.runtime, d.start
			d.catalog = func(ctx context.Context) (snapshot, error) {
				calls = append(calls, "catalog")
				if failure == "metadata" {
					return snapshot{}, errors.New("SECRET-CANARY")
				}
				return catalog(ctx)
			}
			if failure == "freshness" {
				s.current = func(context.Context) bool { return false }
			}
			if failure == "gateway" {
				d.ready = func() bool { return false }
			}
			d.runtime = func(ctx context.Context, env []string, path string) ([]appserverbridge.Backend, error) {
				calls = append(calls, "runtime")
				if failure == "runtime" {
					return nil, errors.New("SECRET-CANARY")
				}
				return runtime(ctx, env, path)
			}
			d.start = func(ctx context.Context, cfg appserverbridge.Config, path string, admission func(context.Context) error) (nativeconnect.Generation, error) {
				calls = append(calls, "host")
				if failure == "host" {
					return nil, errors.New("SECRET-CANARY")
				}
				return start(ctx, cfg, path, admission)
			}
			store := &fixtureStore{}
			m := nativeconnect.NewManager(context.Background(), root, store, newFactory(root, nil, d))
			defer m.Close(context.Background())
			var lines []string
			request, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, finish := nativeprepdiag.Begin(request, func(s string) {
				if failure == "sink" {
					panic("SECRET-CANARY")
				}
				lines = append(lines, s)
			})
			status, err := m.Connect(ctx)
			if len(lines) != 0 {
				t.Fatal("log I/O during locked transaction")
			}
			finish(err == nil)
			success := failure == "" || failure == "sink"
			if success {
				if err != nil || !status.SelectionConfigured || status.ChatVerified || store.writes != 1 || *host == nil {
					t.Fatal("changed success", status, err)
				}
				cancel()
				if (*host).lifetime.Err() != nil {
					t.Fatal("request cancelled host lifetime")
				}
			} else {
				wantError := nativeconnect.ErrUnavailable
				if failure == "freshness" {
					wantError = nativeconnect.ErrMissingCatalog
				}
				if !errors.Is(err, wantError) || store.writes != 0 || status.JournalPresent {
					t.Fatal("changed refusal", status, err)
				}
				if failure != "host" {
					if _, e := os.Stat(root); !os.IsNotExist(e) {
						t.Fatal("early failure wrote state")
					}
				}
			}
			want := "catalog,runtime,host"
			if failure == "metadata" || failure == "freshness" || failure == "gateway" {
				want = "catalog"
			}
			if failure == "runtime" {
				want = "catalog,runtime"
			}
			if strings.Join(calls, ",") != want {
				t.Fatal("operation count/order", calls)
			}
			output := strings.Join(lines, "\n")
			if strings.Contains(output, "SECRET") || strings.Contains(output, root) {
				t.Fatal("leaked data")
			}
			stages := map[string]string{"metadata": "supplier-metadata", "freshness": "supplier-freshness", "gateway": "gateway-ownership", "runtime": "runtime-preparation", "host": "host-preparation"}
			if stage := stages[failure]; stage != "" && !strings.Contains(output, "stage="+stage+" outcome=failed") {
				t.Fatal("missing stage", output)
			}
			if failure == "sink" {
				return
			}
			if len(lines) >= nativeprepdiag.MaxEvents {
				t.Fatal("normal attempt exceeded bound")
			}
			var stack []string
			for _, line := range lines {
				f := strings.Fields(line)
				stage := strings.TrimPrefix(f[3], "stage=")
				outcome := strings.TrimPrefix(f[4], "outcome=")
				if outcome == "entered" {
					stack = append(stack, stage)
				} else {
					if len(stack) == 0 || stack[len(stack)-1] != stage {
						t.Fatal("not nested", lines)
					}
					stack = stack[:len(stack)-1]
				}
			}
			if len(stack) != 0 {
				t.Fatal("incomplete trace", lines)
			}
			if success && strings.Contains(output, "outcome=failed") {
				t.Fatal("false failure", output)
			}
		})
	}
}
