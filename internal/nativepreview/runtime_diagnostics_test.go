package nativepreview

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

func TestRuntimePreparationDiagnosticsObserveOriginalCalls(t *testing.T) {
	for _, failure := range []string{"", "homes", "discovery", "hash", "directory", "version", "rediscovery", "cleanup", "version-cleanup"} {
		t.Run(failure, func(t *testing.T) {
			base := t.TempDir()
			home := filepath.Join(base, "home")
			codex := filepath.Join(home, "codex")
			scratch := filepath.Join(base, "scratch")
			exe := filepath.Join(base, "inert.exe")
			for _, dir := range []string{home, codex, scratch} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(exe, []byte("inert, never executed"), 0600); err != nil {
				t.Fatal(err)
			}
			environment := []string{"HOME=" + home, "USERPROFILE=" + home, "CODEX_HOME=" + codex}
			if failure == "homes" {
				environment = append(environment, "CODEX_SQLITE_HOME=SECRET-CANARY")
			}
			var calls []string
			located := 0
			ops := runtimePreparationOps{
				locate: func(context.Context) (string, error) {
					calls = append(calls, "locate")
					located++
					if failure == "discovery" || failure == "rediscovery" && located == 2 {
						return "", errors.New("SECRET-CANARY")
					}
					return exe, nil
				},
				digest: func(string) ([32]byte, error) {
					calls = append(calls, "hash")
					if failure == "hash" {
						return [32]byte{}, errors.New("SECRET-CANARY")
					}
					return [32]byte{1}, nil
				},
				mktemp: func(root, prefix string) (string, error) {
					calls = append(calls, "directory")
					if failure == "directory" {
						return "", errors.New("SECRET-CANARY")
					}
					return os.MkdirTemp(root, prefix)
				},
				version: func(context.Context, peerSpec) (string, bool) {
					calls = append(calls, "version")
					return SharedRuntimeVersion, failure != "version" && failure != "version-cleanup"
				},
				cleanup: func(path string) error {
					calls = append(calls, "cleanup")
					err := os.RemoveAll(path)
					if failure == "cleanup" || failure == "version-cleanup" {
						return errors.New("SECRET-CANARY")
					}
					return err
				},
			}
			var lines []string
			var replay bytes.Buffer
			logger := log.New(&replay, "", log.LstdFlags)
			ctx, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s); logger.Print(s) })
			_, err := prepareInstalledRuntime(ctx, environment, scratch, "windows", ops)
			finish(err == nil)
			if failure == "version-cleanup" {
				// Fixed-field records from the real observer can be replayed by
				// the offline acceptance parser; no operation data is logged.
				t.Log("\n" + replay.String())
			}
			wantCalls := map[string]string{"": "locate,hash,directory,version,locate,hash,cleanup", "homes": "", "discovery": "locate", "hash": "locate,hash", "directory": "locate,hash,directory", "version": "locate,hash,directory,version,cleanup", "rediscovery": "locate,hash,directory,version,locate,cleanup", "cleanup": "locate,hash,directory,version,locate,hash,cleanup", "version-cleanup": "locate,hash,directory,version,cleanup"}[failure]
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("operation order/count changed: %v", calls)
			}
			if (err == nil) != (failure == "") {
				t.Fatal("result changed", err)
			}
			output := strings.Join(lines, "\n")
			stage := map[string]string{"homes": "runtime-homes", "discovery": "runtime-discovery", "hash": "runtime-executable", "directory": "probe-directory", "version": "runtime-version", "rediscovery": "runtime-rediscovery", "cleanup": "probe-cleanup", "version-cleanup": "runtime-version"}[failure]
			if failure != "" && !strings.Contains(output, "stage="+stage+" outcome=failed") {
				t.Fatal("missing actual failure stage", output)
			}
			if failure == "version-cleanup" {
				primary := strings.Index(output, "stage=runtime-version outcome=failed")
				cleanup := strings.Index(output, "stage=probe-cleanup outcome=failed")
				if primary < 0 || cleanup <= primary {
					t.Fatal("cleanup replaced primary failure", output)
				}
			}
			if strings.Contains(output, "SECRET") || strings.Contains(output, base) {
				t.Fatal("diagnostic leaked operation data")
			}
			if failure == "" && strings.Contains(output, "outcome=failed") {
				t.Fatal("false failure", output)
			}
		})
	}
}
