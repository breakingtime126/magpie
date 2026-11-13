package nativepreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

// These are inert files, never executed. The paths include a WindowsApps-like
// layout; they do not claim to reproduce the user's package ACLs.
func TestRuntimeExecutableFailureDetails(t *testing.T) {
	for _, failure := range []string{"", "missing", "directory", "read", "cancelled", "linked"} {
		t.Run(failure, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(base, "Program Files", "WindowsApps", "OpenAI.Codex_test", "app", "resources")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			exe := filepath.Join(dir, "codex.exe")
			if failure == "directory" {
				if err := os.Mkdir(exe, 0700); err != nil {
					t.Fatal(err)
				}
			} else if failure != "missing" {
				if err := os.WriteFile(exe, []byte("inert"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "linked" {
				link := filepath.Join(dir, "linked.exe")
				if err := os.Symlink(exe, link); err != nil {
					if runtime.GOOS == "windows" {
						t.Skip("symlink creation unavailable")
					}
					t.Fatal(err)
				}
				exe = link
			}
			var lines []string
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, finish := nativeprepdiag.Begin(parent, func(s string) { lines = append(lines, s) })
			calls := 0
			_, _, err = inspectInstalledRuntime(ctx, runtimePreparationOps{
				locate: func(context.Context) (string, error) { return exe, nil },
				digest: func(p string) ([32]byte, error) {
					calls++
					if failure == "read" {
						return [32]byte{}, errors.New("SECRET-CANARY: permission denied")
					}
					if failure == "cancelled" {
						cancel()
					}
					return executableDigest(p)
				},
			})
			finish(err == nil)
			if (err == nil) != (failure == "") {
				t.Fatal("admission changed", err)
			}
			wantCalls := 0
			if failure == "" || failure == "read" || failure == "cancelled" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatal("unexpected executable reads", calls)
			}
			output := strings.Join(lines, "\n")
			want := map[string]string{"missing": "runtime-path-resolution", "directory": "runtime-file-info", "read": "runtime-digest", "cancelled": "runtime-cancelled", "linked": "runtime-path-identity"}[failure]
			if want != "" && !strings.Contains(output, "stage="+want+" outcome=failed") {
				t.Fatal("missing specific failure", output)
			}
			if strings.Contains(output, base) || strings.Contains(output, "SECRET") || strings.Contains(output, "permission denied") {
				t.Fatal("diagnostic leaked operation data")
			}
			if failure == "" && strings.Contains(output, "outcome=failed") {
				t.Fatal("success mislabeled", output)
			}
		})
	}
}
