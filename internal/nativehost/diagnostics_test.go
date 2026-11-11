package nativehost

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativeprepdiag"
)

func TestHostDiagnosticsReportOriginalFailureWithoutLeak(t *testing.T) {
	for _, failure := range []string{"", "resolve", "verify", "admission", "listen"} {
		t.Run(failure, func(t *testing.T) {
			cfg, root, pinned, cap, owner := fixture(t)
			sentinel := errors.New("SECRET-CANARY")
			resolves, listens := 0, 0
			deps := dependencies{resolve: func() (nativeconnect.PackagedLauncher, error) {
				resolves++
				if failure == "resolve" {
					return pinned, sentinel
				}
				if failure == "verify" {
					pinned.SHA256 = strings.Repeat("0", 64)
				}
				return pinned, nil
			}, listen: func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) {
				listens++
				if failure == "listen" {
					return nil, sentinel
				}
				return owner, nil
			}}
			admission := func(context.Context) error {
				if failure == "admission" {
					return sentinel
				}
				return nil
			}
			var lines []string
			ctx, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s) })
			h, err := startWithAdmission(ctx, cap, cfg, root, deps, admission)
			finish(err == nil)
			if h != nil {
				defer closeHost(t, h)
			}
			if resolves != 1 || (failure == "" || failure == "listen") && listens != 1 || (failure != "" && failure != "listen") && listens != 0 {
				t.Fatal("calls changed", resolves, listens)
			}
			if (err == nil) != (failure == "") {
				t.Fatal("result changed", err)
			}
			if failure == "resolve" || failure == "admission" || failure == "listen" {
				if !errors.Is(err, sentinel) {
					t.Fatal("original cause lost", err)
				}
			}
			output := strings.Join(lines, "\n")
			stage := map[string]string{"resolve": "launcher-resolution", "verify": "launcher-verification", "admission": "host-admission", "listen": "bootstrap-listeners"}[failure]
			if stage != "" && !strings.Contains(output, "stage="+stage+" outcome=failed") {
				t.Fatal("missing stage", output)
			}
			if strings.Contains(output, "SECRET") || strings.Contains(output, root) {
				t.Fatal("leak")
			}
		})
	}
}
