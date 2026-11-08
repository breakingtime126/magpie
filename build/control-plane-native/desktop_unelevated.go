package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const desktopUnelevatedProfile = "unelevated-read-only-4239"

// This audit records the one explicitly selected disposable-VM batch. It does
// not authorize a retry or claim that any failed operation rolled itself back.
type desktopSandboxSetupAudit struct {
	Schema               int    `json:"schema"`
	Profile              string `json:"profile"`
	Stage                string `json:"stage"`
	PreflightPassed      int    `json:"preflight_passed"`
	SetupAttempted       int    `json:"setup_attempted"`
	SetupStarted         int    `json:"setup_started"`
	SetupCompleted       int    `json:"setup_completed"`
	Postverified         int    `json:"postverified"`
	PeersClosed          int    `json:"peers_closed"`
	PartialStatePossible bool   `json:"partial_state_possible"`
	NoRetry              bool   `json:"no_retry"`
}

func newDesktopSandboxSetupAudit() *desktopSandboxSetupAudit {
	return &desktopSandboxSetupAudit{Schema: 1, Profile: desktopUnelevatedProfile, Stage: "preflight", NoRetry: true}
}

func validateDesktopSandboxSetupProfile(setup, fixtureProfile, build string) error {
	if setup == "" {
		return nil
	}
	if setup != desktopUnelevatedProfile || fixtureProfile != desktopOwnedCatalogProfile || build != "desktop-4239" {
		return failure("sandbox_setup_profile_rejected")
	}
	return nil
}

func desktopUnelevatedConfig(config []byte) ([]byte, error) {
	old := []byte("sandbox_mode = \"read-only\"\n")
	if bytes.Count(config, old) != 1 || bytes.Contains(config, []byte("default_permissions")) || bytes.Contains(config, []byte("[windows]")) {
		return nil, failure("sandbox_setup_config_rejected")
	}
	return bytes.Replace(config, old, []byte("default_permissions = \":read-only\"\n"), 1), nil
}

type desktopSetupPeer interface {
	rpc(string, any) (json.RawMessage, error)
	send(any) error
	wait(func(message) bool) (message, error)
	close(bool) error
}

type desktopSetupTarget struct {
	home, codexHome, cwd string
	provider, model      string
	expected             []byte
	version              string
}

type desktopSetupFactory func(desktopSetupTarget) (desktopSetupPeer, error)

func prepareDesktopUnelevated(codex, root string, audit *desktopSandboxSetupAudit, fixtures ...*fixture) error {
	if audit == nil || len(fixtures) != 2 || fixtures[0].name != "alpha" || fixtures[1].name != "beta" {
		return failure("sandbox_setup_config_rejected")
	}
	if err := checkEnvironment(os.Environ()); err != nil {
		return err
	}
	if err := verifyBinaryHash(codex, native4239SHA); err != nil {
		return err
	}
	var targets [2]desktopSetupTarget
	for i, f := range fixtures {
		config, err := nativeConfigForModel(f.name+"_provider", f.server.URL, filepath.Join(root, f.name+"-self-authored-catalog.json"), "test-"+f.name)
		if err != nil {
			return err
		}
		config, err = desktopUnelevatedConfig(config)
		if err != nil {
			return err
		}
		targets[i] = desktopSetupTarget{home: filepath.Join(root, f.name+"-home"), codexHome: filepath.Join(root, f.name+"-codex"), cwd: filepath.Join(root, "workspace"), provider: f.name + "_provider", model: "test-" + f.name, expected: config}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	factory := func(t desktopSetupTarget) (desktopSetupPeer, error) {
		// The existing whitelist excludes credentials and Registered Core from
		// the first instruction of this process. No -c/profile/defaults override.
		if err := verifyBinaryHash(codex, native4239SHA); err != nil {
			return nil, err
		}
		return startPeer(ctx, codex, childEnvironment(t.home, t.codexHome), t.cwd)
	}
	return runDesktopUnelevated(targets, factory, audit)
}

func runDesktopUnelevated(targets [2]desktopSetupTarget, factory desktopSetupFactory, audit *desktopSandboxSetupAudit) (runErr error) {
	if audit == nil || *audit != *newDesktopSandboxSetupAudit() {
		return failure("sandbox_setup_already_used")
	}
	var peers [2]desktopSetupPeer
	defer func() {
		for i, p := range peers {
			if p != nil {
				if err := p.close(false); err != nil && runErr == nil {
					runErr = err
				}
				peers[i] = nil
			}
		}
		if runErr != nil && audit.SetupAttempted != 0 {
			audit.PartialStatePossible = true
		}
	}()
	// Both complete checks precede the first security-setting request.
	for i := range targets {
		t := &targets[i]
		if err := checkDesktopSetupFile(*t, false); err != nil {
			return failure("sandbox_setup_initial_file_rejected")
		}
		p, err := factory(*t)
		if err != nil {
			return failure("sandbox_setup_peer_start_failed")
		}
		peers[i] = p
		if err := initializeDesktopSetupPeer(p, *t); err != nil {
			return err
		}
		version, err := checkDesktopSetupPolicy(p, *t, false)
		if err != nil {
			return err
		}
		t.version = version
		if err := checkDesktopSetupThread(p, *t); err != nil {
			return err
		}
		audit.PreflightPassed++
	}
	audit.Stage = "setup"
	for i, p := range peers {
		t := targets[i]
		if err := checkDesktopSetupFile(t, false); err != nil {
			return err
		}
		version, err := checkDesktopSetupPolicy(p, t, false)
		if err != nil || version != t.version {
			return failure("sandbox_setup_policy_changed")
		}
		audit.SetupAttempted++
		audit.PartialStatePossible = true
		// Omit cwd: the reviewed official handler then sets workspace_roots=[]
		// before resolving this exact built-in read-only profile's preflight.
		raw, err := p.rpc("windowsSandbox/setupStart", map[string]any{"mode": "unelevated"})
		if err != nil {
			return err
		}
		if !desktopSetupJSONEqual(raw, map[string]any{"started": true}) {
			return failure("sandbox_setup_start_rejected")
		}
		audit.SetupStarted++
		n, err := p.wait(func(m message) bool { return m.Method == "windowsSandbox/setupCompleted" })
		if err != nil {
			return err
		}
		if !desktopSetupJSONEqual(n.Params, map[string]any{"mode": "unelevated", "success": true, "error": nil}) {
			return failure("sandbox_setup_completion_rejected")
		}
		audit.SetupCompleted++
		if err := checkDesktopSetupFile(t, true); err != nil {
			return err
		}
	}
	// Readiness uses the startup config in this official version. A new peer
	// must really load the persisted selection; old notConfigured is not ready.
	for i, p := range peers {
		err := p.close(true)
		peers[i] = nil
		if err != nil {
			return err
		}
		audit.PeersClosed++
	}
	audit.Stage = "restart-verify"
	for i, t := range targets {
		p, err := factory(t)
		if err != nil {
			return err
		}
		peers[i] = p
		if err := initializeDesktopSetupPeer(p, t); err != nil {
			return err
		}
		if _, err := checkDesktopSetupPolicy(p, t, true); err != nil {
			return err
		}
		if err := checkDesktopSetupThread(p, t); err != nil {
			return err
		}
		raw, err := p.rpc("windowsSandbox/readiness", map[string]any{})
		if err != nil {
			return err
		}
		if !desktopSetupJSONEqual(raw, map[string]any{"status": "ready"}) {
			return failure("sandbox_setup_readiness_rejected")
		}
		if err := checkDesktopSetupFile(t, true); err != nil {
			return err
		}
		audit.Postverified++
		err = p.close(true)
		peers[i] = nil
		if err != nil {
			return err
		}
		audit.PeersClosed++
	}
	audit.Stage = "complete"
	audit.PartialStatePossible = false
	return nil
}

func initializeDesktopSetupPeer(p desktopSetupPeer, t desktopSetupTarget) error {
	raw, err := p.rpc("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "magpie_synthetic_ci", "title": "Synthetic isolated CI", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return failure("sandbox_setup_initialize_rpc_failed")
	}
	obj, err := object(raw)
	if err != nil || stringField(obj, "codexHome") != t.codexHome {
		return failure("sandbox_setup_home_mismatch")
	}
	if err := p.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return failure("sandbox_setup_initialized_send_failed")
	}
	return nil
}

func desktopSetupExpected(t desktopSetupTarget, configured bool) (map[string]any, error) {
	var expected map[string]any
	if toml.Unmarshal(t.expected, &expected) != nil || expected["default_permissions"] != ":read-only" || expected["approval_policy"] != "never" {
		return nil, failure("sandbox_setup_config_rejected")
	}
	if _, exists := expected["sandbox_mode"]; exists {
		return nil, failure("sandbox_setup_config_rejected")
	}
	if _, exists := expected["windows"]; exists {
		return nil, failure("sandbox_setup_config_rejected")
	}
	if configured {
		expected["windows"] = map[string]any{"sandbox": "unelevated"}
	}
	return expected, nil
}

func checkDesktopSetupFile(t desktopSetupTarget, configured bool) error {
	expected, err := desktopSetupExpected(t, configured)
	if err != nil {
		return err
	}
	raw, err := readBounded(filepath.Join(t.codexHome, "config.toml"), 64<<10)
	if err != nil {
		return err
	}
	if !configured && sha256.Sum256(raw) != sha256.Sum256(t.expected) {
		return failure("sandbox_setup_file_changed")
	}
	var actual map[string]any
	if toml.Unmarshal(raw, &actual) != nil || !reflect.DeepEqual(actual, expected) {
		return failure("sandbox_setup_file_changed")
	}
	return nil
}

func checkDesktopSetupPolicy(p desktopSetupPeer, t desktopSetupTarget, configured bool) (string, error) {
	raw, err := p.rpc("configRequirements/read", map[string]any{})
	if err != nil {
		return "", failure("sandbox_setup_requirements_rpc_failed")
	}
	if !desktopSetupJSONEqual(raw, map[string]any{"requirements": nil}) {
		return "", failure("sandbox_setup_requirements_rejected")
	}
	raw, err = p.rpc("config/read", map[string]any{"includeLayers": true, "cwd": t.cwd})
	if err != nil {
		return "", failure("sandbox_setup_config_rpc_failed")
	}
	return checkDesktopSetupLayers(raw, t, configured)
}

func checkDesktopSetupLayers(raw json.RawMessage, t desktopSetupTarget, configured bool) (string, error) {
	if _, err := decodeConfigJSON(raw); err != nil {
		return "", failure("sandbox_setup_layers_rejected")
	}
	obj, err := object(raw)
	if err != nil {
		return "", err
	}
	cfg, err := object(obj["config"])
	if err != nil {
		return "", failure("sandbox_setup_layers_rejected")
	}
	if stringField(cfg, "default_permissions") != ":read-only" {
		return "", failure("sandbox_setup_default_permissions_rejected")
	}
	if stringField(cfg, "approval_policy") != "never" {
		return "", failure("sandbox_setup_approval_rejected")
	}
	expected, err := desktopSetupExpected(t, configured)
	if err != nil {
		return "", err
	}
	var layers []map[string]json.RawMessage
	if json.Unmarshal(obj["layers"], &layers) != nil || len(layers) < 1 || len(layers) > 3 {
		return "", failure("sandbox_setup_layer_count_rejected")
	}
	seen := map[string]bool{}
	version := ""
	for _, layer := range layers {
		for key := range layer {
			if key != "name" && key != "version" && key != "config" && key != "disabledReason" {
				return "", failure("sandbox_setup_layers_rejected")
			}
		}
		if reason, ok := layer["disabledReason"]; ok && !bytes.Equal(reason, []byte("null")) {
			return "", failure("sandbox_setup_layer_disabled")
		}
		name, err := object(layer["name"])
		if err != nil {
			return "", err
		}
		kind := stringField(name, "type")
		if seen[kind] || stringField(layer, "version") == "" || len(stringField(layer, "version")) > 256 {
			return "", failure("sandbox_setup_layer_metadata_rejected")
		}
		seen[kind] = true
		switch kind {
		case "user":
			if !desktopSetupJSONEqual(layer["name"], map[string]any{"type": "user", "file": filepath.Join(t.codexHome, "config.toml"), "profile": nil}) {
				return "", failure("sandbox_setup_user_identity_rejected")
			}
			if !desktopSetupJSONEqual(layer["config"], expected) {
				return "", failure("sandbox_setup_user_content_rejected")
			}
			version = stringField(layer, "version")
		case "system":
			if len(name) != 2 || stringField(name, "file") == "" || !filepath.IsAbs(stringField(name, "file")) || !desktopSetupJSONEqual(layer["config"], map[string]any{}) {
				return "", failure("sandbox_setup_system_layer_rejected")
			}
		case "sessionFlags":
			if len(name) != 1 || !desktopSetupJSONEqual(layer["config"], map[string]any{}) {
				return "", failure("sandbox_setup_session_layer_rejected")
			}
		default:
			return "", failure("sandbox_setup_layer_source_rejected")
		}
	}
	if version == "" {
		return "", failure("sandbox_setup_user_layer_missing")
	}
	return version, nil
}

func checkDesktopSetupThread(p desktopSetupPeer, t desktopSetupTarget) error {
	raw, err := p.rpc("thread/start", map[string]any{"ephemeral": true})
	if err != nil {
		return failure("sandbox_setup_thread_rpc_failed")
	}
	if _, err := decodeConfigJSON(raw); err != nil {
		return failure("sandbox_setup_permissions_rejected")
	}
	obj, err := object(raw)
	if err != nil {
		return failure("sandbox_setup_permissions_rejected")
	}
	if stringField(obj, "cwd") != t.cwd || stringField(obj, "modelProvider") != t.provider || stringField(obj, "model") != t.model {
		return failure("sandbox_setup_thread_identity_rejected")
	}
	if stringField(obj, "approvalPolicy") != "never" {
		return failure("sandbox_setup_approval_rejected")
	}
	if !desktopSetupJSONEqual(obj["sandbox"], map[string]any{"type": "readOnly", "networkAccess": false}) {
		return failure("sandbox_setup_readonly_rejected")
	}
	if !desktopSetupJSONEqual(obj["activePermissionProfile"], map[string]any{"id": ":read-only", "extends": nil}) {
		return failure("sandbox_setup_permission_profile_rejected")
	}
	return nil
}

func desktopSetupJSONEqual(raw json.RawMessage, expected any) bool {
	actual, err := decodeConfigJSON(raw)
	if err != nil {
		return false
	}
	want, err := decodeConfigJSON(jsonBytes(expected))
	return err == nil && reflect.DeepEqual(actual, want)
}

func writeDesktopFixtureReadyWithSetup(out io.Writer, pinnedSHA, profile, root string, audit *desktopSandboxSetupAudit) error {
	if audit == nil {
		return writeDesktopFixtureReadyFromRoot(out, pinnedSHA, profile, root)
	}
	if audit.Schema != 1 || audit.Profile != desktopUnelevatedProfile || audit.Stage != "complete" || audit.PreflightPassed != 2 || audit.SetupAttempted != 2 || audit.SetupStarted != 2 || audit.SetupCompleted != 2 || audit.Postverified != 2 || audit.PeersClosed != 4 || audit.PartialStatePossible || !audit.NoRetry {
		return failure("sandbox_setup_not_complete")
	}
	var original bytes.Buffer
	if err := writeDesktopFixtureReadyFromRoot(&original, pinnedSHA, profile, root); err != nil {
		return err
	}
	var ready map[string]any
	if json.Unmarshal(original.Bytes(), &ready) != nil {
		return failure("sandbox_setup_not_complete")
	}
	ready["sandbox_setup"] = audit
	ready["lifetime_seconds"] = 360
	if err := json.NewEncoder(out).Encode(ready); err != nil {
		return failure("output_failed")
	}
	return nil
}
