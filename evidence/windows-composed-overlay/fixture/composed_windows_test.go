package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type packageManifest struct {
	SourceRevision string            `json:"source_revision"`
	BridgeMode     string            `json:"bridge_mode"`
	SHA256         map[string]string `json:"sha256"`
}
type actor struct {
	command *exec.Cmd
	tree    *proc.DrainedTree
	input   *os.File
}

func isolatedEnv(t *testing.T, root string) []string {
	t.Helper()
	values := map[string]string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "ComSpec", "PATHEXT", "PATH"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	for key, relative := range map[string]string{"HOME": ".", "USERPROFILE": ".", "APPDATA": "AppData/Roaming", "LOCALAPPDATA": "AppData/Local", "XDG_CONFIG_HOME": ".config", "XDG_CONFIG_DIRS": "etc/xdg", "XDG_CACHE_HOME": ".cache", "XDG_DATA_HOME": ".local/share", "XDG_STATE_HOME": ".local/state", "XDG_RUNTIME_DIR": "runtime", "CODEX_HOME": "native", "TEMP": "tmp", "TMP": "tmp", "TMPDIR": "tmp"} {
		p := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		values[key] = p
	}
	values[testenv.Marker] = root
	values["MAGPIE_NO_ACCOUNTS"] = "1"
	values["MAGPIE_PARTNERS"] = "off"
	values["MAGPIE_ADDR"] = "127.0.0.1:0"
	var env []string
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}
func startActor(t *testing.T, root, path string, arguments, env []string) *actor {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(filepath.Join(root, filepath.Base(path)+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		read.Close()
		write.Close()
		t.Fatal(err)
	}
	cmd := proc.Command(path, arguments...)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdin = read
	cmd.Stdout = log
	cmd.Stderr = log
	tree, err := proc.StartTreeDrained(cmd)
	read.Close()
	log.Close()
	a := &actor{cmd, tree, write}
	if tree != nil {
		t.Cleanup(func() {
			write.Close()
			if err := tree.Stop(); err != nil {
				t.Error("owned outer Job stop", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tree.Wait(ctx); err != nil {
				t.Error("owned outer Job drain", err)
			}
		})
	} else {
		write.Close()
	}
	if err != nil {
		t.Fatal("fixture start", err)
	}
	return a
}
func waitFile(t *testing.T, ctx context.Context, root, name string, target any) {
	t.Helper()
	for {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err == nil {
			if len(data) > 16384 || json.Unmarshal(data, target) != nil {
				t.Fatal("invalid fixture report", name)
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture report timeout", name, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func waitActor(t *testing.T, ctx context.Context, a *actor, expectedExit int) {
	t.Helper()
	if err := a.tree.Wait(ctx); err != nil {
		t.Fatal("outer root/descendant drain failed", err)
	}
	if a.command.ProcessState == nil || !a.command.ProcessState.Exited() || a.command.ProcessState.ExitCode() != expectedExit {
		t.Fatal("fixture exited unsuccessfully", a.command.ProcessState)
	}
}
func actorLive(t *testing.T, a *actor) {
	t.Helper()
	var state uint32
	var operation error
	e := a.command.Process.WithHandle(func(h uintptr) { state, operation = windows.WaitForSingleObject(windows.Handle(h), 0) })
	if e != nil || operation != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("owned bridge no longer live", e, operation, state)
	}
}
func TestComposedHTTPDeliveryAndLeaseDrain(t *testing.T) {
	packageRoot := os.Getenv("MAGPIE_COMPOSED_FIXTURE_PACKAGE")
	if packageRoot == "" {
		t.Skip("requires the explicit two-phase authored fixture package after gateway audit")
	}
	if !filepath.IsAbs(packageRoot) {
		t.Fatal("fixture package must be absolute")
	}
	data, err := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
	if err != nil || len(data) > 16384 {
		t.Fatal("fixture package manifest unavailable", err)
	}
	var manifest packageManifest
	if json.Unmarshal(data, &manifest) != nil || len(manifest.SourceRevision) != 40 || len(manifest.SHA256) != 3 || manifest.BridgeMode != "actual-adapter" {
		t.Fatal("invalid fixture package manifest")
	}
	root := os.Getenv(testenv.Marker)
	if root == "" || root != os.Getenv("HOME") {
		t.Fatal("testenv isolation missing")
	}
	if report := os.Getenv("MAGPIE_COMPOSED_REPORT"); report != "" {
		t.Cleanup(func() {
			out := filepath.Join(filepath.Dir(report), "actor-evidence")
			if err := os.Mkdir(out, 0700); err != nil {
				t.Error("cannot preserve actor evidence", err)
				return
			}
			for _, name := range []string{"Magpie-Native-Codex-Experimental.exe.log", "magpie-codex-bridge.exe.log", "host-ready.json", "http-delivered.json", "primary-started.json", "secondary-started.json", "selection-restored.json", "host-revoked.json"} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					t.Error(err)
					continue
				}
				if len(data) > 1<<20 {
					t.Error("fixture evidence oversized", name)
					continue
				}
				if err := os.WriteFile(filepath.Join(out, name), data, 0600); err != nil {
					t.Error(err)
				}
			}
		})
	}
	// This package has exactly one composed test. Its TestMain supplies a fresh
	// home plus inert CLI/keychain tools before any child starts.
	for _, p := range []string{"pkg", "control", "work", "publication", "native"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"magpie-codex-bridge.exe", "Magpie-Native-Codex-Experimental.exe", "synthetic-backend.exe"} {
		source := filepath.Join(packageRoot, name)
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
			t.Fatal("invalid fixture artifact", name, err)
		}
		bytes, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(bytes)
		if hex.EncodeToString(sum[:]) != manifest.SHA256[name] {
			t.Fatal("fixture artifact changed", name)
		}
		if err := os.WriteFile(filepath.Join(root, "pkg", name), bytes, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	if err := writeNew(filepath.Join(root, "composed-guard.json"), guard{root, id, time.Now().Add(90 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnv(t, root)
	host := startActor(t, root, filepath.Join(root, "pkg", "Magpie-Native-Codex-Experimental.exe"), []string{"--composed-fixture", id}, env)
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	var ready ready
	waitFile(t, ctx, root, "host-ready.json", &ready)
	if !inside(root, ready.Profile) || len(ready.Generation) != 32 || len(ready.ProfileSHA256) != 64 {
		t.Fatal("bad host profile record")
	}
	// The ready record supplies only a file locator. The real launcher obtains
	// profile trust from kernel-verified live bootstrap, not this test record.
	bridgeEnv := append(append([]string{}, env...), "MAGPIE_APP_SERVER_BRIDGE_CONFIG="+ready.Profile)
	bridge := startActor(t, root, filepath.Join(root, "pkg", "magpie-codex-bridge.exe"), []string{"app-server", "--stdio"}, bridgeEnv)
	var delivered delivery
	waitFile(t, ctx, root, "http-delivered.json", &delivered)
	if delivered.Status != 400 || !strings.Contains(delivered.Body, "invalid request: model must be a nonempty string") {
		t.Fatal("no real gateway malformed-request result")
	}
	var started map[string]bool
	waitFile(t, ctx, root, "primary-started.json", &started)
	waitFile(t, ctx, root, "secondary-started.json", &started)
	actorLive(t, bridge)
	if _, err := fmt.Fprintln(host.input, "restore"); err != nil {
		t.Fatal(err)
	}
	var restored map[string]bool
	waitFile(t, ctx, root, "selection-restored.json", &restored)
	if !restored["owner_active"] {
		t.Fatal("selection restoration changed active lifetime")
	}
	actorLive(t, bridge)
	if _, err := fmt.Fprintln(host.input, "revoke"); err != nil {
		t.Fatal(err)
	}
	var revoked map[string]bool
	waitFile(t, ctx, root, "host-revoked.json", &revoked)
	if !revoked["cleanup_confirmed"] {
		t.Fatal("host pipe/channel cleanup incomplete")
	}
	// The real adapter intentionally makes cancellation and cleanup failures
	// indistinguishable to callers. Require its generic failure exit, but do not
	// turn that into a claim that internal runner cleanup returned no error.
	waitActor(t, ctx, bridge, 1)
	waitActor(t, ctx, host, 0)
	diagnostic, err := os.ReadFile(filepath.Join(root, "magpie-codex-bridge.exe.log"))
	if err != nil || strings.TrimSpace(string(diagnostic)) != "magpie-codex-bridge: trusted native launch failed" {
		t.Fatal("unexpected actual adapter diagnostic", err, string(diagnostic))
	}
	// Endpoint address is test evidence only. It is never reused as launch trust.
	address := strings.TrimSuffix(strings.TrimPrefix(delivered.URL, "http://"), "/v1")
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("exclusive endpoint not released after whole owned Job drain", err)
	}
	listener.Close()
	report := map[string]any{"http_status": delivered.Status, "profile_verified_via_real_named_pipe": true, "selection_restore_kept_bridge_alive": true, "retained_outer_jobs_drained": true, "actual_adapter_exit_code": 1, "runner_cleanup_error_distinguished": false, "endpoint_address_reusable_after_drain": true, "vendor_native_protocol_verified": false, "real_accounts_used": false, "zero_egress_proven": false}
	if out := os.Getenv("MAGPIE_COMPOSED_REPORT"); out != "" {
		if !filepath.IsAbs(out) {
			t.Fatal("report path must be absolute")
		}
		if err := writeNew(out, report); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("Actual adapter HTTP delivery and outer-Job cleanup proved; generic exit does not distinguish internal runner cleanup errors; no vendor-native protocol/model/account acceptance")
}
