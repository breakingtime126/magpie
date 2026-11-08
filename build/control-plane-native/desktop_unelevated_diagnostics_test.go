package main

import (
	"encoding/json"
	"testing"
)

// Synthetic failures never copy a native error, path or value into diagnostics.
type setupDiagnosticPeer struct {
	desktopSetupPeer
	method   string
	result   json.RawMessage
	sendFail bool
}

func (p setupDiagnosticPeer) rpc(method string, params any) (json.RawMessage, error) {
	if method == p.method {
		if p.result == nil {
			return nil, failure("synthetic-untrusted-error-with-private-data")
		}
		return p.result, nil
	}
	return p.desktopSetupPeer.rpc(method, params)
}

func (p setupDiagnosticPeer) send(v any) error {
	if p.sendFail {
		return failure("synthetic-untrusted-send-error")
	}
	return p.desktopSetupPeer.send(v)
}

func TestDesktopUnelevatedDiagnosticRPCStagesAreFixed(t *testing.T) {
	target := setupTestTargets(t)[0]
	var log []string
	base := &desktopSetupFake{target: target, log: &log}
	for _, tc := range []struct {
		method string
		want   failure
	}{
		{"initialize", "sandbox_setup_initialize_rpc_failed"},
		{"configRequirements/read", "sandbox_setup_requirements_rpc_failed"},
		{"config/read", "sandbox_setup_config_rpc_failed"},
		{"thread/start", "sandbox_setup_thread_rpc_failed"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			p := setupDiagnosticPeer{desktopSetupPeer: base, method: tc.method}
			var err error
			switch tc.method {
			case "initialize":
				err = initializeDesktopSetupPeer(p, target)
			case "thread/start":
				err = checkDesktopSetupThread(p, target)
			default:
				_, err = checkDesktopSetupPolicy(p, target, false)
			}
			if err != tc.want {
				t.Fatal(err, tc.want)
			}
		})
	}
	if err := initializeDesktopSetupPeer(setupDiagnosticPeer{desktopSetupPeer: base, sendFail: true}, target); err != failure("sandbox_setup_initialized_send_failed") {
		t.Fatal(err)
	}
	var neverCalled bool
	targets := setupTestTargets(t)
	targets[0].expected = []byte("invalid")
	err := runDesktopUnelevated(targets, func(desktopSetupTarget) (desktopSetupPeer, error) {
		neverCalled = true
		return nil, failure("private")
	}, newDesktopSandboxSetupAudit())
	if neverCalled || err != failure("sandbox_setup_initial_file_rejected") {
		t.Fatal(err, neverCalled)
	}
	err = runDesktopUnelevated(setupTestTargets(t), func(desktopSetupTarget) (desktopSetupPeer, error) {
		return nil, failure("private")
	}, newDesktopSandboxSetupAudit())
	if err != failure("sandbox_setup_peer_start_failed") {
		t.Fatal(err)
	}
}

func TestDesktopUnelevatedDiagnosticLayerReasonsPreserveRejection(t *testing.T) {
	target := setupTestTargets(t)[0]
	for _, tc := range []struct {
		want   failure
		change func(map[string]any)
	}{
		{"sandbox_setup_default_permissions_rejected", func(m map[string]any) { m["config"].(map[string]any)["default_permissions"] = "other" }},
		{"sandbox_setup_approval_rejected", func(m map[string]any) { m["config"].(map[string]any)["approval_policy"] = "other" }},
		{"sandbox_setup_layer_count_rejected", func(m map[string]any) { m["layers"] = []any{} }},
		{"sandbox_setup_layer_disabled", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["disabledReason"] = "private" }},
		{"sandbox_setup_layer_metadata_rejected", func(m map[string]any) { m["layers"].([]any)[1].(map[string]any)["version"] = "" }},
		{"sandbox_setup_layer_source_rejected", func(m map[string]any) {
			m["layers"].([]any)[0].(map[string]any)["name"] = map[string]any{"type": "unknown"}
		}},
		{"sandbox_setup_user_identity_rejected", func(m map[string]any) {
			m["layers"].([]any)[1].(map[string]any)["name"].(map[string]any)["file"] = "private"
		}},
		{"sandbox_setup_user_content_rejected", func(m map[string]any) {
			m["layers"].([]any)[1].(map[string]any)["config"].(map[string]any)["unknown"] = "private"
		}},
		{"sandbox_setup_system_layer_rejected", func(m map[string]any) {
			m["layers"].([]any)[0].(map[string]any)["config"] = map[string]any{"unknown": "private"}
		}},
		{"sandbox_setup_session_layer_rejected", func(m map[string]any) {
			m["layers"].([]any)[2].(map[string]any)["config"] = map[string]any{"unknown": "private"}
		}},
		{"sandbox_setup_user_layer_missing", func(m map[string]any) { m["layers"] = []any{m["layers"].([]any)[0]} }},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(setupFakeLayers(target, false), &m); err != nil {
				t.Fatal(err)
			}
			tc.change(m)
			if _, err := checkDesktopSetupLayers(jsonBytes(m), target, false); err != tc.want {
				t.Fatal(err, tc.want)
			}
		})
	}
}

func TestDesktopUnelevatedDiagnosticThreadReasonsPreserveRejection(t *testing.T) {
	target := setupTestTargets(t)[0]
	for _, tc := range []struct {
		field string
		value any
		want  failure
	}{
		{"cwd", "private", "sandbox_setup_thread_identity_rejected"},
		{"modelProvider", "other", "sandbox_setup_thread_identity_rejected"},
		{"model", "other", "sandbox_setup_thread_identity_rejected"},
		{"approvalPolicy", "other", "sandbox_setup_approval_rejected"},
		{"sandbox", map[string]any{"type": "workspaceWrite"}, "sandbox_setup_readonly_rejected"},
		{"activePermissionProfile", map[string]any{"id": "other"}, "sandbox_setup_permission_profile_rejected"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(setupFakeThread(target), &m); err != nil {
				t.Fatal(err)
			}
			m[tc.field] = tc.value
			p := setupDiagnosticPeer{method: "thread/start", result: jsonBytes(m)}
			if err := checkDesktopSetupThread(p, target); err != tc.want {
				t.Fatal(err, tc.want)
			}
		})
	}
}
