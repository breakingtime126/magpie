package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type desktopSetupFake struct {
	target      desktopSetupTarget
	log         *[]string
	name        string
	configured  bool
	fail        string
	afterThread func()
}

func (p *desktopSetupFake) rpc(method string, params any) (json.RawMessage, error) {
	*p.log = append(*p.log, p.name+":"+method)
	if p.fail == method {
		return nil, failure("synthetic_native_failure")
	}
	switch method {
	case "initialize":
		return jsonBytes(map[string]any{"codexHome": p.target.codexHome}), nil
	case "configRequirements/read":
		if p.fail == "non-null-requirements" {
			return jsonBytes(map[string]any{"requirements": map[string]any{}}), nil
		}
		return jsonBytes(map[string]any{"requirements": nil}), nil
	case "config/read":
		if !reflect.DeepEqual(params, map[string]any{"includeLayers": true, "cwd": p.target.cwd}) {
			return nil, failure("wrong_params")
		}
		return setupFakeLayers(p.target, p.configured), nil
	case "thread/start":
		if !reflect.DeepEqual(params, map[string]any{"ephemeral": true}) {
			return nil, failure("overridden_thread")
		}
		if p.afterThread != nil {
			p.afterThread()
		}
		if p.fail == "writable-sandbox" {
			var result map[string]any
			_ = json.Unmarshal(setupFakeThread(p.target), &result)
			result["sandbox"] = map[string]any{"type": "workspaceWrite", "writableRoots": []string{p.target.cwd}, "networkAccess": false}
			return jsonBytes(result), nil
		}
		return setupFakeThread(p.target), nil
	case "windowsSandbox/setupStart":
		if !reflect.DeepEqual(params, map[string]any{"mode": "unelevated"}) {
			return nil, failure("wrong_setup_params")
		}
		p.configured = true
		content := append(append([]byte{}, p.target.expected...), []byte("[windows]\nsandbox = \"unelevated\"\n")...)
		if err := os.WriteFile(filepath.Join(p.target.codexHome, "config.toml"), content, 0600); err != nil {
			return nil, err
		}
		return jsonBytes(map[string]any{"started": true}), nil
	case "windowsSandbox/readiness":
		if p.fail == "not-ready" {
			return jsonBytes(map[string]any{"status": "notConfigured"}), nil
		}
		return jsonBytes(map[string]any{"status": "ready"}), nil
	}
	return nil, failure("unexpected_method")
}

func (p *desktopSetupFake) send(any) error { return nil }

func (p *desktopSetupFake) wait(match func(message) bool) (message, error) {
	*p.log = append(*p.log, p.name+":completed")
	if p.fail == "completion" {
		return message{}, failure("synthetic_timeout")
	}
	m := message{Method: "windowsSandbox/setupCompleted", Params: jsonBytes(map[string]any{"mode": "unelevated", "success": true, "error": nil})}
	if p.fail == "unsuccessful-completion" {
		m.Params = jsonBytes(map[string]any{"mode": "unelevated", "success": false, "error": "synthetic"})
	}
	if !match(m) {
		return message{}, failure("unexpected_match")
	}
	return m, nil
}

func (p *desktopSetupFake) close(normal bool) error {
	if normal {
		*p.log = append(*p.log, p.name+":close-normal")
	} else {
		*p.log = append(*p.log, p.name+":close-abort")
	}
	if p.fail == "close" {
		return failure("shutdown_failed")
	}
	return nil
}

func setupTestTargets(t *testing.T) [2]desktopSetupTarget {
	t.Helper()
	root := t.TempDir()
	var targets [2]desktopSetupTarget
	for i, name := range []string{"alpha", "beta"} {
		ch := filepath.Join(root, name)
		if err := os.Mkdir(ch, 0700); err != nil {
			t.Fatal(err)
		}
		cfg := []byte("default_permissions = \":read-only\"\napproval_policy = \"never\"\n")
		if err := os.WriteFile(filepath.Join(ch, "config.toml"), cfg, 0600); err != nil {
			t.Fatal(err)
		}
		targets[i] = desktopSetupTarget{home: ch, codexHome: ch, cwd: root, provider: name + "_provider", model: "test-" + name, expected: cfg}
	}
	return targets
}

func setupFakeLayers(target desktopSetupTarget, configured bool) json.RawMessage {
	user, _ := desktopSetupExpected(target, configured)
	return jsonBytes(map[string]any{"config": map[string]any{"default_permissions": ":read-only", "approval_policy": "never"}, "layers": []any{
		map[string]any{"name": map[string]any{"type": "system", "file": filepath.Join(target.cwd, "system.toml")}, "version": "empty-system", "config": map[string]any{}},
		map[string]any{"name": map[string]any{"type": "user", "file": filepath.Join(target.codexHome, "config.toml"), "profile": nil}, "version": "owned-user", "config": user},
		map[string]any{"name": map[string]any{"type": "sessionFlags"}, "version": "empty-flags", "config": map[string]any{}},
	}})
}

func setupFakeThread(target desktopSetupTarget) json.RawMessage {
	return jsonBytes(map[string]any{"cwd": target.cwd, "approvalPolicy": "never", "modelProvider": target.provider, "model": target.model, "sandbox": map[string]any{"type": "readOnly", "networkAccess": false}, "activePermissionProfile": map[string]any{"id": ":read-only", "extends": nil}, "runtimeWorkspaceRoots": []string{target.cwd}})
}

func setupTestFactory(log *[]string, failIndex int, fail string, callback func(int, *desktopSetupFake)) desktopSetupFactory {
	count := 0
	return func(target desktopSetupTarget) (desktopSetupPeer, error) {
		index := count
		count++
		p := &desktopSetupFake{target: target, log: log, name: target.provider, configured: index >= 2}
		if index == failIndex {
			p.fail = fail
		}
		if callback != nil {
			callback(index, p)
		}
		return p, nil
	}
}

func TestDesktopUnelevatedBatchPreflightsBothBeforeSetupAndRestarts(t *testing.T) {
	targets := setupTestTargets(t)
	var log []string
	audit := newDesktopSandboxSetupAudit()
	if err := runDesktopUnelevated(targets, setupTestFactory(&log, -1, "", nil), audit); err != nil {
		t.Fatal(err, log)
	}
	want := &desktopSandboxSetupAudit{Schema: 1, Profile: desktopUnelevatedProfile, Stage: "complete", PreflightPassed: 2, SetupAttempted: 2, SetupStarted: 2, SetupCompleted: 2, Postverified: 2, PeersClosed: 4, NoRetry: true}
	if !reflect.DeepEqual(audit, want) {
		t.Fatalf("audit %#v", audit)
	}
	joined := strings.Join(log, "\n")
	if strings.Index(joined, "beta_provider:thread/start") > strings.Index(joined, "alpha_provider:windowsSandbox/setupStart") {
		t.Fatal("setup before both preflights")
	}
	if strings.Count(joined, "windowsSandbox/setupStart") != 2 || strings.Count(joined, "windowsSandbox/readiness") != 2 || strings.Count(joined, "close-normal") != 4 {
		t.Fatal(joined)
	}
	if err := runDesktopUnelevated(targets, setupTestFactory(&log, -1, "", nil), audit); err != failure("sandbox_setup_already_used") {
		t.Fatal("reused batch accepted", err)
	}
}

func TestDesktopUnelevatedFailureNeverRetriesOrStartsSiblingEarly(t *testing.T) {
	for _, tc := range []struct {
		name                string
		peer                int
		fail                string
		attempts, completed int
	}{
		{"alpha-preflight", 0, "thread/start", 0, 0},
		{"beta-preflight", 1, "config/read", 0, 0},
		{"beta-policy", 1, "non-null-requirements", 0, 0},
		{"beta-writable", 1, "writable-sandbox", 0, 0},
		{"alpha-request-uncertain", 0, "windowsSandbox/setupStart", 1, 0},
		{"alpha-completion-timeout", 0, "completion", 1, 0},
		{"alpha-completion-failure", 0, "unsuccessful-completion", 1, 0},
		{"beta-request-uncertain", 1, "windowsSandbox/setupStart", 2, 1},
		{"alpha-close", 0, "close", 2, 2},
		{"post-restart-policy", 2, "config/read", 2, 2},
		{"post-restart-readiness", 3, "windowsSandbox/readiness", 2, 2},
		{"post-restart-not-ready", 3, "not-ready", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			audit := newDesktopSandboxSetupAudit()
			err := runDesktopUnelevated(setupTestTargets(t), setupTestFactory(&log, tc.peer, tc.fail, nil), audit)
			if err == nil || audit.SetupAttempted != tc.attempts || audit.SetupCompleted != tc.completed || audit.PartialStatePossible != (tc.attempts > 0) || audit.Stage == "complete" {
				t.Fatal(err, audit, log)
			}
			if strings.Count(strings.Join(log, "\n"), "windowsSandbox/setupStart") != tc.attempts {
				t.Fatal("retry", log)
			}
		})
	}
}

func TestDesktopUnelevatedFileChangeAfterPreflightPreventsFirstSetup(t *testing.T) {
	targets := setupTestTargets(t)
	var log []string
	factory := setupTestFactory(&log, -1, "", func(i int, p *desktopSetupFake) {
		if i == 1 {
			p.afterThread = func() {
				if err := os.WriteFile(filepath.Join(targets[0].codexHome, "config.toml"), append(targets[0].expected, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
	audit := newDesktopSandboxSetupAudit()
	if err := runDesktopUnelevated(targets, factory, audit); err != failure("sandbox_setup_file_changed") || audit.SetupAttempted != 0 {
		t.Fatal(err, audit)
	}
}

func TestDesktopUnelevatedLayersRejectAmbientOrChangedMeaning(t *testing.T) {
	target := setupTestTargets(t)[0]
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"managed", func(m map[string]any) { m["layers"].([]any)[0].(map[string]any)["name"] = map[string]any{"type": "mdm"} }},
		{"nonempty-system", func(m map[string]any) { m["layers"].([]any)[0].(map[string]any)["config"] = map[string]any{"hooks": map[string]any{}} }},
		{"nonempty-flags", func(m map[string]any) { m["layers"].([]any)[2].(map[string]any)["config"] = map[string]any{"sandbox_mode": "danger-full-access"} }},
		{"extra-user-key", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["config"].(map[string]any)["hooks"] = map[string]any{} }},
		{"wrong-file", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["name"].(map[string]any)["file"] = "other" }},
		{"profile", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["name"].(map[string]any)["profile"] = "other" }},
		{"disabled", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["disabledReason"] = "disabled" }},
		{"default-override", func(m map[string]any) { m["config"].(map[string]any)["default_permissions"] = "other" }},
		{"approval-override", func(m map[string]any) { m["config"].(map[string]any)["approval_policy"] = "on-request" }},
		{"no-layer", func(m map[string]any) { delete(m, "layers") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(setupFakeLayers(target, false), &raw); err != nil {
				t.Fatal(err)
			}
			tc.change(raw)
			if _, err := checkDesktopSetupLayers(jsonBytes(raw), target, false); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if _, err := checkDesktopSetupLayers(setupFakeLayers(target, false), target, false); err != nil {
		t.Fatal(err)
	}
	if _, err := checkDesktopSetupLayers(setupFakeLayers(target, true), target, false); err == nil {
		t.Fatal("preexisting setup accepted")
	}
}

func TestDesktopUnelevatedConfigAndProfileAreExplicit(t *testing.T) {
	for _, s := range []string{"other", "unelevated", "elevated"} {
		if validateDesktopSandboxSetupProfile(s, desktopOwnedCatalogProfile, "desktop-4239") == nil {
			t.Fatal(s)
		}
	}
	if validateDesktopSandboxSetupProfile(desktopUnelevatedProfile, acceptanceSynthetic4239, "desktop-4239") == nil || validateDesktopSandboxSetupProfile(desktopUnelevatedProfile, desktopOwnedCatalogProfile, "") == nil {
		t.Fatal("broad profile")
	}
	if err := validateDesktopSandboxSetupProfile(desktopUnelevatedProfile, desktopOwnedCatalogProfile, "desktop-4239"); err != nil {
		t.Fatal(err)
	}
	if err := validateDesktopSandboxSetupProfile("", "", ""); err != nil {
		t.Fatal(err)
	}
	config := []byte("sandbox_mode = \"read-only\"\n")
	got, err := desktopUnelevatedConfig(config)
	if err != nil || string(got) != "default_permissions = \":read-only\"\n" || string(config) != "sandbox_mode = \"read-only\"\n" {
		t.Fatal(err, string(got))
	}
	for _, s := range []string{"", "sandbox_mode = \"danger-full-access\"\n", string(config) + string(config), string(config) + "[windows]\n", string(config) + "default_permissions=\"x\"\n"} {
		if _, err := desktopUnelevatedConfig([]byte(s)); err == nil {
			t.Fatal("config accepted", s)
		}
	}
}

func TestDesktopUnelevatedReadyRequiresAllActualMilestones(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "control-home"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog, err := bridgeOwnedSelfAuthoredCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control-home", "bridge-model-catalog.json"), catalog, 0600); err != nil {
		t.Fatal(err)
	}
	complete := desktopSandboxSetupAudit{Schema: 1, Profile: desktopUnelevatedProfile, Stage: "complete", PreflightPassed: 2, SetupAttempted: 2, SetupStarted: 2, SetupCompleted: 2, Postverified: 2, PeersClosed: 4, NoRetry: true}
	for _, alter := range []func(*desktopSandboxSetupAudit){
		func(a *desktopSandboxSetupAudit) { a.PreflightPassed = 1 },
		func(a *desktopSandboxSetupAudit) { a.SetupAttempted = 1 },
		func(a *desktopSandboxSetupAudit) { a.SetupStarted = 1 },
		func(a *desktopSandboxSetupAudit) { a.SetupCompleted = 1 },
		func(a *desktopSandboxSetupAudit) { a.Postverified = 1 },
		func(a *desktopSandboxSetupAudit) { a.PeersClosed = 3 },
		func(a *desktopSandboxSetupAudit) { a.PartialStatePossible = true },
		func(a *desktopSandboxSetupAudit) { a.NoRetry = false },
		func(a *desktopSandboxSetupAudit) { a.Stage = "setup" },
	} {
		bad := complete
		alter(&bad)
		var out strings.Builder
		if err := writeDesktopFixtureReadyWithSetup(&out, native4239SHA, desktopOwnedCatalogProfile, root, &bad); err == nil || out.Len() != 0 {
			t.Fatal("premature ready", err, out.String())
		}
	}
	for _, audit := range []*desktopSandboxSetupAudit{nil, &complete} {
		var out strings.Builder
		if err := writeDesktopFixtureReadyWithSetup(&out, native4239SHA, desktopOwnedCatalogProfile, root, audit); err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if err := json.Unmarshal([]byte(out.String()), &actual); err != nil {
			t.Fatal(err)
		}
		if audit == nil {
			if actual["sandbox_setup"] != nil || actual["lifetime_seconds"] != float64(180) {
				t.Fatal("old profile changed", actual)
			}
		} else if actual["sandbox_setup"] == nil || actual["lifetime_seconds"] != float64(360) {
			t.Fatal("new profile missing evidence", actual)
		}
	}
}
