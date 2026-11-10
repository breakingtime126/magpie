package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
func startActor(t *testing.T, root, label, path string, arguments, env []string) *actor {
	t.Helper()
	return startActorAt(t, root, root, label, path, arguments, env)
}
func startActorAt(t *testing.T, root, directory, label, path string, arguments, env []string) *actor {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(filepath.Join(root, label+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		read.Close()
		write.Close()
		t.Fatal(err)
	}
	cmd := proc.Command(path, arguments...)
	cmd.Dir = directory
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
func TestComposedRelistenTakeoverAndRecovery(t *testing.T) {
	packageRoot := os.Getenv("MAGPIE_RELISTEN_FIXTURE_PACKAGE")
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
	if report := os.Getenv("MAGPIE_RELISTEN_REPORT"); report != "" {
		t.Cleanup(func() {
			out := filepath.Join(filepath.Dir(report), "actor-evidence")
			if err := os.Mkdir(out, 0700); err != nil {
				t.Error("cannot preserve actor evidence", err)
				return
			}
			// Preserve the independent, credential-free process-entry ledger.
			ledger, err := backendStartSnapshot(root)
			if err != nil {
				t.Error("cannot preserve backend ledger", err)
			} else if err := writeNew(filepath.Join(out, "backend-starts.json"), ledger); err != nil {
				t.Error(err)
			}
			for _, name := range []string{"host.log", "bridge-first.log", "bridge-stale.log", "bridge-second.log", "stale-backend-control.log", "takeover.log", "host-ready-first.json", "host-ready-second.json", "http-delivered-first.json", "http-delivered-second.json", "primary-started-first.json", "secondary-started-first.json", "primary-started-second.json", "secondary-started-second.json", "selection-restored.json", "relistened.json", "old-challenge-rejected.json", "host-revoked.json", "takeover-ready.json", "takeover-zero.json", "takeover-control.json"} {
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
	for _, p := range []string{"pkg", "control", "work/first", "work/second", "publication", "native", "backend-starts"} {
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
	if err := writeNew(filepath.Join(root, "composed-guard.json"), guard{root, id, time.Now().Add(115 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnv(t, root)
	host := startActor(t, root, "host", filepath.Join(root, "pkg", "Magpie-Native-Codex-Experimental.exe"), []string{"--composed-fixture", id}, env)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	var first ready
	waitFile(t, ctx, root, "host-ready-first.json", &first)
	checkReady(t, root, first)
	// Report fields locate authored fixture evidence only. The actual adapter
	// obtains authority from kernel-verified bootstrap and exact profile bytes.
	launch := func(label string, r ready) *actor {
		return startActor(t, root, label, filepath.Join(root, "pkg", "magpie-codex-bridge.exe"), []string{"app-server", "--stdio"}, append(append([]string{}, env...), "MAGPIE_APP_SERVER_BRIDGE_CONFIG="+r.Profile))
	}
	bridge := launch("bridge-first", first)
	firstDelivery := awaitDelivery(t, ctx, root, "first")
	actorLive(t, bridge)
	command(t, host, "restore")
	var restored map[string]bool
	waitFile(t, ctx, root, "selection-restored.json", &restored)
	if !restored["owner_active"] {
		t.Fatal("selection restoration changed active lifetime")
	}
	actorLive(t, bridge)
	if status, body, err := fixtureHTTP(ctx, firstDelivery.URL, "magpie-codex", `{}`); err != nil || status != 400 || !strings.Contains(body, "invalid request: model must be a nonempty string") {
		t.Fatal("selection restore broke active HTTP route", status, err)
	}

	// A partial HTTP request is demonstrably pending on the actual owned
	// frontend before Relisten. This does not claim it already reached the
	// genuine gateway's body reader; no production instrumentation is injected.
	pending, err := net.DialTimeout("tcp4", endpointAddress(t, firstDelivery.URL), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pending.Close() })
	if err := pending.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(pending, "POST /v1/responses HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer magpie-codex\r\nContent-Type: application/json\r\nContent-Length: 4096\r\n\r\n{", endpointAddress(t, firstDelivery.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := pending.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if n, err := pending.Read(one[:]); n != 0 || err == nil {
		t.Fatal("partial request unexpectedly answered")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal("partial request not pending", err)
	}
	actorLive(t, bridge)
	command(t, host, "relisten")
	var changed struct {
		Invalid    bool  `json:"old_capability_invalid_immediately"`
		Unread     bool  `json:"old_request_rejected_without_body_read"`
		CanceledMS int64 `json:"host_cancel_observed_ms"`
	}
	waitFile(t, ctx, root, "relistened.json", &changed)
	if !changed.Invalid || !changed.Unread || changed.CanceledMS < 0 || changed.CanceledMS > 1500 {
		t.Fatal("old generation not invalidated on genuine Relisten", changed)
	}
	takeover := startActor(t, root, "takeover", filepath.Join(root, "pkg", "synthetic-backend.exe"), []string{"--takeover", id}, env)
	var occupied map[string]string
	waitFile(t, ctx, root, "takeover-ready.json", &occupied)
	if occupied["url"] != first.GatewayURL || occupied["claims_generation"] != first.Generation {
		t.Fatal("takeover did not claim exact old address/generation")
	}
	command(t, host, "challenge-old")
	var rejected map[string]bool
	waitFile(t, ctx, root, "old-challenge-rejected.json", &rejected)
	if !rejected["rejected_without_body_read"] {
		t.Fatal("old captured capability followed takeover")
	}
	// Reuse of the old frontend's locator cannot recover the revoked route.
	// A refusal/closed socket is acceptable; a deadline is not cleanup proof.
	status, _, requestErr := fixtureHTTP(ctx, firstDelivery.URL, "synthetic-private-"+id, `{"marker":"synthetic-private-`+id+`"}`)
	if requestErr == nil && status != 502 && status != 503 {
		t.Fatal("revoked frontend accepted a request", status)
	}
	if errors.Is(requestErr, context.DeadlineExceeded) {
		t.Fatal("revoked frontend request did not finish", requestErr)
	}
	waitActor(t, ctx, bridge, 1)
	checkAdapterDiagnostic(t, root, "bridge-first")
	if err := pending.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response, readErr := http.ReadResponse(bufio.NewReader(pending), nil)
	if response != nil {
		response.Body.Close()
		if response.StatusCode < 400 || response.StatusCode >= 600 {
			t.Fatal("unexpected incomplete request response", response.StatusCode)
		}
	}
	if readErr == nil && response == nil {
		t.Fatal("pending request termination unconfirmed")
	}
	if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
		t.Fatal("pending request remained open after outer Job drain")
	}
	pending.Close()
	assertReusable(t, endpointAddress(t, firstDelivery.URL))

	command(t, host, "recover")
	var second ready
	waitFile(t, ctx, root, "host-ready-second.json", &second)
	checkReady(t, root, second)
	if second.Generation == first.Generation || second.ProfileSHA256 == first.ProfileSHA256 || second.Profile == first.Profile || second.GatewayURL == first.GatewayURL {
		t.Fatal("recovery reused old generation")
	}
	// The first bridge Job is already drained. Snapshot a ledger independent of
	// phase markers, which duplicate old starts could otherwise fail to replace.
	beforeStale, err := backendStartSnapshot(root)
	if err != nil || len(beforeStale) != 2 {
		t.Fatal("first bridge backend entry evidence invalid", beforeStale, err)
	}
	// A fresh host exists, but cannot authorize the old immutable profile.
	stale := launch("bridge-stale", first)
	waitActor(t, ctx, stale, 1)
	checkAdapterDiagnostic(t, root, "bridge-stale")
	afterStale, err := backendStartSnapshot(root)
	if err != nil || !slices.Equal(beforeStale, afterStale) {
		t.Fatal("stale profile accessed a backend before rejection", beforeStale, afterStale, err)
	}
	for _, name := range []string{"primary-started-second.json", "secondary-started-second.json"} {
		if _, e := os.Stat(filepath.Join(root, name)); !os.IsNotExist(e) {
			t.Fatal("stale profile started fresh backends", name, e)
		}
	}
	// Real process negative control: deliberately start the old primary helper.
	// It records entry, then exits 1 on the existing first-phase marker, exactly
	// the failure that used to look like valid stale-profile rejection.
	phasePath := filepath.Join(root, "primary-started-first.json")
	phaseBefore, err := os.ReadFile(phasePath)
	if err != nil {
		t.Fatal(err)
	}
	controlBackend := startActorAt(t, root, filepath.Join(root, "work", "first"), "stale-backend-control", filepath.Join(root, "pkg", "synthetic-backend.exe"), []string{"app-server"}, env)
	waitActor(t, ctx, controlBackend, 1)
	controlLog, logErr := os.ReadFile(filepath.Join(root, "stale-backend-control.log"))
	phaseAfter, phaseErr := os.ReadFile(phasePath)
	afterControl, ledgerErr := backendStartSnapshot(root)
	if logErr != nil || strings.TrimSpace(string(controlLog)) != "synthetic backend phase marker already exists" || phaseErr != nil || string(phaseAfter) != string(phaseBefore) || ledgerErr != nil || len(afterControl) != len(afterStale)+1 || slices.Equal(afterStale, afterControl) {
		t.Fatal("duplicate-phase backend negative control was not detected", logErr, phaseErr, ledgerErr)
	}
	for _, name := range afterStale {
		if !slices.Contains(afterControl, name) {
			t.Fatal("backend ledger lost prior evidence")
		}
	}
	freshBridge := launch("bridge-second", second)
	secondDelivery := awaitDelivery(t, ctx, root, "second")
	actorLive(t, freshBridge)
	command(t, host, "revoke")
	var revoked map[string]bool
	waitFile(t, ctx, root, "host-revoked.json", &revoked)
	if !revoked["cleanup_confirmed"] {
		t.Fatal("fresh host pipe/channel cleanup incomplete")
	}
	waitActor(t, ctx, freshBridge, 1)
	checkAdapterDiagnostic(t, root, "bridge-second")
	waitActor(t, ctx, host, 0)
	finalStarts, err := backendStartSnapshot(root)
	if err != nil || len(finalStarts) != len(afterControl)+2 {
		t.Fatal("fresh bridge backend entry evidence invalid", finalStarts, err)
	}
	for _, name := range afterControl {
		if !slices.Contains(finalStarts, name) {
			t.Fatal("backend ledger lost prior evidence")
		}
	}
	assertReusable(t, endpointAddress(t, secondDelivery.URL))
	command(t, takeover, "check-zero")
	var zero takeoverReport
	waitFile(t, ctx, root, "takeover-zero.json", &zero)
	if zero.Requests != 0 || zero.CredentialMarkers != 0 || zero.BodyMarkers != 0 || zero.ClaimedGeneration != first.Generation {
		t.Fatal("old authority reached the impersonator", zero)
	}

	// A deliberately unsafe fixed-URL request, only after both bridge Jobs
	// drain and the zero report is sealed, calibrates the takeover detector.
	// This sends synthetic markers, never a real private credential.
	marker := "synthetic-private-" + id
	status, body, err := fixtureHTTP(ctx, first.GatewayURL, marker, `{"marker":"`+marker+`"}`)
	if err != nil || status != 200 {
		t.Fatal("negative-control request did not reach takeover", status, err)
	}
	var claim map[string]string
	if json.Unmarshal([]byte(body), &claim) != nil || claim["name"] != "magpie" || claim["generation"] != first.Generation || claim["profile_sha256"] != first.ProfileSHA256 {
		t.Fatal("takeover did not impersonate saved identity")
	}
	command(t, takeover, "finish-control")
	var control takeoverReport
	waitFile(t, ctx, root, "takeover-control.json", &control)
	if control.Requests != 1 || control.CredentialMarkers != 1 || control.BodyMarkers != 1 {
		t.Fatal("negative control not detected exactly once", control)
	}
	waitActor(t, ctx, takeover, 0)
	assertReusable(t, endpointAddress(t, first.GatewayURL))
	report := map[string]any{
		"first_http_status": firstDelivery.Status, "recovered_http_status": secondDelivery.Status,
		"genuine_relisten_changed_port": true, "old_capability_invalid_immediately": true,
		"old_request_rejected_without_body_read": true, "host_cancel_observed_ms": changed.CanceledMS,
		"selection_restore_kept_bridge_alive": true, "pending_frontend_request_terminated": true,
		"takeover_protected_requests": zero.Requests, "takeover_protected_credential_markers": zero.CredentialMarkers,
		"takeover_protected_body_markers": zero.BodyMarkers, "fixed_url_negative_control_detected": true,
		"old_profile_relaunch_rejected": true, "fresh_generation_verified_via_real_named_pipe": true,
		"stale_profile_backend_starts": len(afterStale) - len(beforeStale), "duplicate_phase_backend_negative_control_detected": true,
		"backend_start_count": len(finalStarts), "backend_control_outer_job_drained": true,
		"actual_adapter_exit_codes": []int{1, 1, 1}, "retained_outer_jobs_drained": true,
		"endpoint_addresses_reusable_after_drain": true, "runner_cleanup_error_distinguished": false,
		"vendor_native_protocol_verified": false, "real_accounts_used": false, "zero_egress_proven": false,
	}
	if out := os.Getenv("MAGPIE_RELISTEN_REPORT"); out != "" {
		if !filepath.IsAbs(out) {
			t.Fatal("report path must be absolute")
		}
		if err := writeNew(out, report); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("Genuine Relisten revoked old authority; owned old-port impersonator saw zero protected traffic; fresh generation delivered real HTTP400; fixed-URL negative control detected. Outer Job drain does not distinguish inner runner cleanup errors; no vendor-native/account acceptance.")
}

func command(t *testing.T, a *actor, text string) {
	t.Helper()
	if _, e := fmt.Fprintln(a.input, text); e != nil {
		t.Fatal(e)
	}
}
func checkReady(t *testing.T, root string, r ready) {
	t.Helper()
	if !inside(root, r.Profile) || len(r.Generation) != 32 || len(r.ProfileSHA256) != 64 {
		t.Fatal("bad host profile record")
	}
	endpointAddress(t, r.GatewayURL)
}
func awaitDelivery(t *testing.T, ctx context.Context, root, generation string) delivery {
	t.Helper()
	var delivered delivery
	waitFile(t, ctx, root, "http-delivered-"+generation+".json", &delivered)
	if delivered.Status != 400 || !strings.Contains(delivered.Body, "invalid request: model must be a nonempty string") {
		t.Fatal("no real gateway missing-model result", generation)
	}
	for _, name := range []string{"primary", "secondary"} {
		var started map[string]bool
		waitFile(t, ctx, root, name+"-started-"+generation+".json", &started)
		if !started["authored_backend"] {
			t.Fatal("backend evidence absent")
		}
	}
	endpointAddress(t, delivered.URL)
	return delivered
}
func endpointAddress(t *testing.T, base string) string {
	t.Helper()
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/v1" || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		t.Fatal("unexpected fixture loopback locator", base, e)
	}
	return u.Host
}
func assertReusable(t *testing.T, address string) {
	t.Helper()
	l, e := net.Listen("tcp4", address)
	if e != nil {
		t.Fatal("owned endpoint not released after whole Job drain", e)
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
}
func checkAdapterDiagnostic(t *testing.T, root, label string) {
	t.Helper()
	data, e := os.ReadFile(filepath.Join(root, label+".log"))
	if e != nil || strings.TrimSpace(string(data)) != "magpie-codex-bridge: trusted native launch failed" {
		t.Fatal("unexpected actual adapter diagnostic", label, e, string(data))
	}
}
func fixtureHTTP(ctx context.Context, base, token, body string) (int, string, error) {
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", strings.NewReader(body))
	if e != nil {
		return 0, "", e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(r)
	if e != nil {
		return 0, "", e
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 8193))
	if len(data) > 8192 {
		return 0, "", errors.New("fixture response oversized")
	}
	return response.StatusCode, string(data), e
}
