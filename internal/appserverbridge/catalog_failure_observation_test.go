package appserverbridge

import (
	"encoding/json"
	"testing"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// Authored synthetic records only; no native record is stored or printed.
func TestNativeCatalogFixedFailureReasons(t *testing.T) {
	cases := []struct {
		name   string
		reason nativehandshake.Stage
	}{
		{"missing-field", nativehandshake.CatalogReasonModelSchema},
		{"unknown-effort", nativehandshake.CatalogReasonEffortUnknown},
		{"duplicate-effort", nativehandshake.CatalogReasonDuplicateEffort},
		{"duplicate-model", nativehandshake.CatalogReasonDuplicateModel},
		{"default-unsupported", nativehandshake.CatalogReasonDefaultUnsupported},
		{"empty-list", nativehandshake.CatalogReasonEmptyList},
		{"backend-count", nativehandshake.CatalogReasonBackendCount},
		{"no-visible-default", nativehandshake.CatalogReasonNoVisibleDefault},
		{"snapshot", nativehandshake.CatalogReasonSnapshot},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := nativeSettingsRouter167(t)
			rows := nativeSettingsRows167(t)
			m, _ := parseObject(rows["one"][0])
			switch c.name {
			case "missing-field":
				delete(m, "displayName")
			case "unknown-effort":
				m["defaultReasoningEffort"] = raw("private-custom-canary")
			case "duplicate-effort":
				m["supportedReasoningEfforts"] = raw([]object{{"reasoningEffort": raw("medium"), "description": raw("a")}, {"reasoningEffort": raw("medium"), "description": raw("b")}})
			case "default-unsupported":
				m["defaultReasoningEffort"] = raw("none")
			case "snapshot":
				if r.ownedCatalog.validateNative(rows, r.backends) != nil {
					t.Fatal("synthetic baseline failed")
				}
				m["description"] = raw("authored changed")
			}
			rows["one"] = []json.RawMessage{raw(m)}
			switch c.name {
			case "duplicate-model":
				rows["one"] = append(rows["one"], rows["one"][0])
			case "empty-list":
				rows["one"] = nil
			case "backend-count":
				delete(rows, "one")
			case "no-visible-default":
				for backend, list := range rows {
					item, _ := parseObject(list[0])
					item["isDefault"] = raw(false)
					rows[backend] = []json.RawMessage{raw(item)}
				}
			}
			if r.ownedCatalog.validateNative(rows, r.backends) == nil || r.ownedCatalog.failureStage != c.reason {
				t.Fatal("fixed native catalog rejection branch missing")
			}
			r.ownedCatalog.failure(nativehandshake.CatalogReasonRouter)
			if r.ownedCatalog.failureStage != c.reason {
				t.Fatal("cleanup overwrote first cause")
			}
		})
	}
}

func TestProtocolPendingOwnedSettingsPolicy(t *testing.T) {
	g := &request{method: "model/list", settingsPolicyStage: true}
	if protocolPendingStage(g) != nativehandshake.ProtocolPendingPolicy {
		t.Fatal("internal policy response misclassified as model list")
	}
}

func TestOwnedSettingsPolicyFailureKeepsFixedReason(t *testing.T) {
	for _, kind := range []string{"schema", "nonnull", "size", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			r := nativeSettingsRouter167(t)
			g := &request{id: raw(1), method: "model/list", remaining: 1, results: map[string][]json.RawMessage{}, settingsPolicyStage: true}
			line := raw(object{"id": raw("synthetic"), "result": json.RawMessage(controlPlaneNullRequirements)})
			want := nativehandshake.CatalogReasonPolicySchema
			switch kind {
			case "schema":
				line = []byte(`{"id":"synthetic","result":{"private":"canary"}}`)
			case "nonnull":
				line = []byte(`{"id":"synthetic","result":{"requirements":{},"supportsIndependentSpeedModes":true}}`)
				want = nativehandshake.CatalogReasonPolicyNonNull
			case "size":
				g.bytes = requirementsJSONLimit
				want = nativehandshake.CatalogReasonPolicySize
			case "mismatch":
				g.results[r.backends[1].ID] = []json.RawMessage{raw(object{})}
				want = nativehandshake.CatalogReasonPolicyMismatch
			}
			out, err := r.ownedSettingsPolicyResponse167(g, r.backends[0].ID, line)
			if err != nil || len(out) != 1 || r.ownedCatalog.failureStage != want {
				t.Fatal("policy rejection reason missing")
			}
		})
	}
}
