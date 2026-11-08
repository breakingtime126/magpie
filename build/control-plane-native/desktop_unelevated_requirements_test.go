package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDesktopUnelevatedRequirementsRequireExactPinnedEnvelope(t *testing.T) {
	// Public c407 config.rs:838-847 defines the complete response and
	// config_processor.rs:153-156 emits supportsIndependentSpeedModes=true.
	// This fixture does not assert that a live machine's requirements are null.
	target := setupTestTargets(t)[0]
	for _, tc := range []struct {
		name string
		raw  string
		pass bool
	}{
		{"complete", `{"supportsIndependentSpeedModes":true,"requirements":null}`, true},
		{"reordered", `{"requirements":null,"supportsIndependentSpeedModes":true}`, true},
		{"missing-speed", `{"requirements":null}`, false},
		{"false-speed", `{"supportsIndependentSpeedModes":false,"requirements":null}`, false},
		{"null-speed", `{"supportsIndependentSpeedModes":null,"requirements":null}`, false},
		{"string-speed", `{"supportsIndependentSpeedModes":"true","requirements":null}`, false},
		{"number-speed", `{"supportsIndependentSpeedModes":1,"requirements":null}`, false},
		{"unknown-field", `{"supportsIndependentSpeedModes":true,"requirements":null,"extra":true}`, false},
		{"missing-requirements", `{"supportsIndependentSpeedModes":true}`, false},
		{"nonnull-empty", `{"supportsIndependentSpeedModes":true,"requirements":{}}`, false},
		{"nonnull-policy", `{"supportsIndependentSpeedModes":true,"requirements":{"modelProvider":"managed"}}`, false},
		{"wrong-policy-type", `{"supportsIndependentSpeedModes":true,"requirements":false}`, false},
		{"duplicate-policy", `{"supportsIndependentSpeedModes":true,"requirements":{},"requirements":null}`, false},
		{"duplicate-speed", `{"supportsIndependentSpeedModes":false,"supportsIndependentSpeedModes":true,"requirements":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			base := &desktopSetupFake{target: target, log: &log}
			p := setupDiagnosticPeer{desktopSetupPeer: base, method: "configRequirements/read", result: json.RawMessage(tc.raw)}
			version, err := checkDesktopSetupPolicy(p, target, false)
			if tc.pass {
				if err != nil || version == "" || !strings.Contains(strings.Join(log, "\n"), ":config/read") {
					t.Fatal("complete response rejected", err, version, log)
				}
			} else if err != failure("sandbox_setup_requirements_rejected") || version != "" || len(log) != 0 {
				t.Fatal("invalid response advanced to config", err, version, log)
			}
		})
	}
}
