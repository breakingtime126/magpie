package appserverbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are independent test definitions, not native or bundled records.
func ownedTestModel(owner, model string, priority int) object {
	return object{
		"slug": raw(alias(owner, model)), "display_name": raw("Local " + model + " [" + owner + "]"),
		"description": raw("Independent text-only test definition."), "default_reasoning_level": raw("none"),
		"supported_reasoning_levels": raw([]object{{"effort": raw("none"), "description": raw("No reasoning in this test.")}}),
		"shell_type":                 raw("disabled"), "visibility": raw("list"), "supported_in_api": raw(true), "priority": raw(priority),
		"support_verbosity": raw(false), "default_verbosity": raw(nil),
		"truncation_policy":            raw(object{"mode": raw("tokens"), "limit": raw(1024)}),
		"experimental_supported_tools": raw([]any{}), "input_modalities": raw([]string{"text"}),
		"include_apps_usage_instructions": raw(false), "include_skills_usage_instructions": raw(false),
		"include_plugin_usage_instructions": raw(false), "supports_reasoning_summary_parameter": raw(false),
		"default_reasoning_summary": raw("none"), "node_repl_disabled": raw(true), "supports_search_tool": raw(false),
		"apply_patch_tool_type": raw(nil), "upgrade": raw(nil), "availability_nux": raw(nil),
		"additional_speed_tiers": raw([]any{}), "service_tiers": raw([]any{}),
		"model_messages": raw(object{"instructions_template": raw("Emit the local test response.")}),
	}
}

func ownedTestSetup(t *testing.T) (*Router, string, []object) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "owned-test-catalog.json")
	models := []object{ownedTestModel("one", "first", 0), ownedTestModel("two", "second", 1)}
	if err := os.WriteFile(path, raw(object{"models": raw(models)}), 0600); err != nil {
		t.Fatal(err)
	}
	created, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows may expand a short temp-directory name or normalize its case.
	// Derive the expected published spelling independently of the loader.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(parent, filepath.Base(path))
	backends := []Backend{{ID: "one", Provider: "native_one"}, {ID: "two", Provider: "native_two"}}
	r, err := NewRouter(backends)
	if err != nil {
		t.Fatal(err)
	}
	r.ownedCatalog, err = loadOwnedCatalog(home, path, backends)
	if err != nil {
		t.Fatal(err)
	}
	if r.ownedCatalog.path != expectedPath || !os.SameFile(created, r.ownedCatalog.identity) {
		t.Fatal("loaded owned catalog does not name the original fixture file")
	}
	if err := r.enableControlPlane167(); err != nil {
		t.Fatal(err)
	}
	r.phase = "ready"
	return r, expectedPath, models
}

// These fixed wire fixtures are independently authored from the public c407
// app-server Model schema, not captured responses or upstream catalog records.
// Do not generate them using ownedModelProjection: they are its test oracle.
func ownedTestNative(t *testing.T) map[string][]json.RawMessage {
	t.Helper()
	out := map[string][]json.RawMessage{
		"one": {json.RawMessage(`{
			"id":"first","model":"first","displayName":"Local first",
			"description":"Independent text-only test definition.",
			"upgrade":null,"upgradeInfo":null,"availabilityNux":null,
			"modelSpecialty":null,"hidden":false,
			"supportedReasoningEfforts":[{"reasoningEffort":"none","description":"No reasoning in this test."}],
			"defaultReasoningEffort":"none","inputModalities":["text"],
			"supportsPersonality":false,"multiAgentVersion":null,
			"additionalSpeedTiers":[],"serviceTiers":[],"defaultServiceTier":null,
			"availableAccessPrograms":null,"isDefault":true
		}`)},
		"two": {json.RawMessage(`{
			"id":"second","model":"second","displayName":"Local second",
			"description":"Independent text-only test definition.",
			"upgrade":null,"upgradeInfo":null,"availabilityNux":null,
			"modelSpecialty":null,"hidden":false,
			"supportedReasoningEfforts":[{"reasoningEffort":"none","description":"No reasoning in this test."}],
			"defaultReasoningEffort":"none","inputModalities":["text"],
			"supportsPersonality":false,"multiAgentVersion":null,
			"additionalSpeedTiers":[],"serviceTiers":[],"defaultServiceTier":null,
			"availableAccessPrograms":null,"isDefault":true
		}`)},
	}
	for _, rows := range out {
		m, err := parseObject(rows[0])
		if err != nil || len(m) != 19 {
			t.Fatal("independent wire fixture must contain exactly 19 Model fields")
		}
	}
	return out
}

func TestOwnedCatalogProjectionMatchesIndependentWire(t *testing.T) {
	r, _, models := ownedTestSetup(t)
	native := ownedTestNative(t)
	for i, tc := range []struct{ owner, alias, display string }{
		{"one", "magpie/one/first", "Local first [one]"},
		{"two", "magpie/two/second", "Local second [two]"},
	} {
		want, err := parseObject(native[tc.owner][0])
		if err != nil {
			t.Fatal("independent wire fixture is invalid")
		}
		want["id"], want["model"], want["displayName"] = raw(tc.alias), raw(tc.alias), raw(tc.display)
		// Projection has no aggregate default until the loader binds owners.
		want["isDefault"] = raw(false)
		got, priority, err := ownedModelProjection(models[i])
		if err != nil || priority != int32(i) || !sameJSON(raw(got), raw(want)) {
			t.Fatal("owned projection differs from independent 19-field wire fixture")
		}
	}
	if err := r.ownedCatalog.validateNative(native, r.backends); err != nil {
		t.Fatal("independent complete native wire fixtures were rejected")
	}
}

func ownedTestReply(t *testing.T, r *Router, delivery Delivery, rows []json.RawMessage, cursor any) ([]Delivery, error) {
	t.Helper()
	m, err := parseObject(delivery.Message)
	if err != nil {
		t.Fatal(err)
	}
	return r.Server(delivery.Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw(rows), "nextCursor": raw(cursor)})}))
}

func TestOwnedCatalogPublishesOnlyCompleteNativeProjection(t *testing.T) {
	r, path, _ := ownedTestSetup(t)
	if exists(r.controlPlaneSessionConfig167(""), "model_catalog_json") {
		t.Fatal("catalog published before native validation")
	}
	out, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{"limit":1}}`))
	if err != nil || len(out) != 2 {
		t.Fatal("native catalog fanout missing")
	}
	for _, delivery := range out {
		m, _ := parseObject(delivery.Message)
		p, _ := child(m, "params")
		if !sameJSON(p["includeHidden"], raw(true)) {
			t.Fatal("native catalog was filtered before validation")
		}
	}
	native := ownedTestNative(t)
	if done, err := ownedTestReply(t, r, out[0], nil, "page-2"); err != nil || len(done) != 1 {
		t.Fatal("native pagination missing")
	} else {
		if result, err := ownedTestReply(t, r, done[0], native[out[0].Backend], nil); err != nil || len(result) != 0 {
			t.Fatal("catalog published before second native authority")
		}
	}
	if r.controlSession167 != nil || len(r.models) != 0 {
		t.Fatal("partial catalog escaped")
	}
	if done, err := ownedTestReply(t, r, out[1], native[out[1].Backend], nil); err != nil || len(done) != 1 {
		t.Fatal("complete native catalog was rejected")
	}
	if r.defaultModel != "magpie/one/first" || len(r.models) != 2 || r.ownedCatalog.refreshing {
		t.Fatal("catalog state not atomic")
	}
	cfg, err := parseObject(r.controlSession167.config)
	if err != nil || text(cfg, "model_catalog_json") != path {
		t.Fatal("actual owned path not published")
	}
	ext, _ := child(cfg, "magpie_bridge")
	catalog, _ := child(ext, "catalog")
	if text(catalog, "sha256") != r.ownedCatalog.digest || text(catalog, "source") != "bridge-owned-alias-catalog" {
		t.Fatal("catalog provenance not digest bound")
	}
	origins, _ := parseObject(r.controlSession167.origins)
	if !exists(origins, "model_catalog_json") {
		t.Fatal("catalog lacks owned session lineage")
	}
}

func TestOwnedCatalogRefreshBlocksRequestsAndFailureIsSticky(t *testing.T) {
	r, _, _ := ownedTestSetup(t)
	native := ownedTestNative(t)
	out, _ := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`))
	for _, d := range out {
		if _, err := ownedTestReply(t, r, d, native[d.Backend], nil); err != nil {
			t.Fatal(err)
		}
	}
	r.threads["existing-owned-thread"] = "two"
	out, err := r.Client([]byte(`{"id":2,"method":"model/list","params":{}}`))
	if err != nil || len(out) != 2 || r.controlSession167 != nil || len(r.models) != 0 || r.defaultModel != "" {
		t.Fatal("refresh retained stale public state")
	}
	for _, method := range []string{"model/list", "thread/start", "turn/start"} {
		done, err := r.Client(raw(object{"id": raw("blocked-" + method), "method": raw(method), "params": raw(object{})}))
		if err != nil || len(done) != 1 || done[0].Backend != "" || !strings.Contains(string(done[0].Message), `"error"`) {
			t.Fatal("request used an in-progress catalog")
		}
	}
	m, _ := parseObject(out[0].Message)
	if _, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("private native error")})})); err == nil || err.Error() != ownedCatalogUnavailable {
		t.Fatal("native refresh error did not close the owned contract")
	}
	if r.threads["existing-owned-thread"] != "two" {
		t.Fatal("refresh rebound existing thread ownership")
	}
	if done, err := r.Client([]byte(`{"id":3,"method":"thread/start","params":{}}`)); err == nil || len(done) != 0 {
		t.Fatal("failed catalog permitted stale routing")
	}
}

func TestOwnedCatalogRejectsEveryNativeProjectionMismatch(t *testing.T) {
	for _, field := range []string{"id", "model", "displayName", "description", "hidden", "isDefault", "defaultReasoningEffort", "supportedReasoningEfforts", "inputModalities", "supportsPersonality", "upgrade", "upgradeInfo", "availabilityNux", "modelSpecialty", "multiAgentVersion", "additionalSpeedTiers", "serviceTiers", "defaultServiceTier", "availableAccessPrograms"} {
		t.Run(field, func(t *testing.T) {
			r, _, _ := ownedTestSetup(t)
			native := ownedTestNative(t)
			m, _ := parseObject(native["two"][0])
			m[field] = raw("unreviewed mismatch")
			native["two"][0] = raw(m)
			if r.ownedCatalog.validateNative(native, r.backends) == nil {
				t.Fatal("native projection mismatch accepted")
			}
		})
	}
}

func TestOwnedCatalogRejectsChangedFileAndInvalidDeclarations(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		r, path, _ := ownedTestSetup(t)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if replacement {
			temp := path + ".replacement"
			if err := os.WriteFile(temp, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(temp, path); err != nil {
				t.Fatal(err)
			}
		} else {
			data = append(data, '\n')
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`)); err == nil {
			t.Fatal("changed immutable catalog accepted")
		}
	}
	for _, mutate := range []func([]object){
		func(m []object) { m[1]["slug"] = raw("magpie/unknown/model") },
		func(m []object) { m[1]["slug"] = m[0]["slug"] },
		func(m []object) { m[1]["priority"] = raw(-1) },
		func(m []object) { m[0]["priority"] = raw(nil) },
		func(m []object) { delete(m[0], "upgrade"); m[0]["unknown_nullable"] = raw(nil) },
		func(m []object) { m[0]["guardian"] = raw(object{}) },
		func(m []object) { m[0]["visibility"] = raw("hide") },
		func(m []object) { m[0]["supported_in_api"] = raw(false) },
		func(m []object) { m[0]["shell_type"] = raw("shell_command") },
	} {
		r, path, models := ownedTestSetup(t)
		mutate(models)
		if err := os.WriteFile(path, raw(object{"models": raw(models)}), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadOwnedCatalog(filepath.Dir(path), path, r.backends); err == nil {
			t.Fatal("unknown mapping or unsupported declaration accepted")
		}
	}
	if ownedCatalogJSON([]byte(`{"models":[],"models":[]}`)) || ownedCatalogJSON([]byte(`{"models":[{"slug":"x","slug":"y"}]}`)) {
		t.Fatal("duplicate catalog keys accepted")
	}
	r, _, _ := ownedTestSetup(t)
	outside := filepath.Join(t.TempDir(), "external.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOwnedCatalog(t.TempDir(), outside, r.backends); err == nil {
		t.Fatal("catalog outside actual control home accepted")
	}
}

func TestOwnedCatalogConfigReadStillRequiresBothFreshPolicies(t *testing.T) {
	r, path, _ := ownedTestSetup(t)
	native := ownedTestNative(t)
	out, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range out {
		if _, err := ownedTestReply(t, r, d, native[d.Backend], nil); err != nil {
			t.Fatal(err)
		}
	}
	out, err = r.Client([]byte(`{"id":2,"method":"config/read","params":{"includeLayers":true}}`))
	if err != nil || len(out) != 2 {
		t.Fatal("owned catalog bypassed fresh policy fanout")
	}
	for i, d := range out {
		m, _ := parseObject(d.Message)
		if text(m, "method") != "configRequirements/read" || string(m["params"]) != "null" {
			t.Fatal("owned catalog fetched native config")
		}
		done, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)})}))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && len(done) != 0 {
			t.Fatal("owned config published before both policies")
		}
		if i == 1 {
			if len(done) != 1 {
				t.Fatal("owned config response missing")
			}
			envelope, _ := parseObject(done[0].Message)
			result, _ := child(envelope, "result")
			cfg, _ := child(result, "config")
			if text(cfg, "model_catalog_json") != path || !exists(result, "layers") {
				t.Fatal("owned catalog or session lineage omitted")
			}
		}
	}
}

func TestOwnedCatalogRejectsValidlyTypedMismatchBeforePublication(t *testing.T) {
	r, _, _ := ownedTestSetup(t)
	native := ownedTestNative(t)
	m, _ := parseObject(native["two"][0])
	m["description"] = raw("A different but well-typed native description.")
	native["two"][0] = raw(m)
	out, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`))
	if err != nil || len(out) != 2 {
		t.Fatal("native catalog fanout missing")
	}
	for _, d := range out {
		_, err = ownedTestReply(t, r, d, native[d.Backend], nil)
	}
	if err == nil || !r.ownedCatalog.failed || r.controlSession167 != nil || len(r.models) != 0 {
		t.Fatal("mismatched native description published an owned catalog")
	}
}
