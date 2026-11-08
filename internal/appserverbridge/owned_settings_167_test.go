package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ownedSettingsTestRouter(t *testing.T) *Router {
	t.Helper()
	r, catalog, _ := ownedTestSetup(t)
	store, err := openOwnedSettingsStore(filepath.Dir(catalog))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	r.ownedSettings = store
	return r
}

func ownedSettingsTestPolicy(t *testing.T, r *Router, deliveries []Delivery) []Delivery {
	t.Helper()
	if len(deliveries) != 2 {
		t.Fatal("settings operation must check both native policies")
	}
	var final []Delivery
	for _, d := range deliveries {
		m, err := parseObject(d.Message)
		if err != nil || text(m, "method") != "configRequirements/read" || !requirementsNull(m["params"]) || d.Backend == "" {
			t.Fatal("settings sent an unauthorized native write")
		}
		out, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)})}))
		if err != nil {
			t.Fatal(err)
		}
		final = append(final, out...)
	}
	return final
}

func ownedSettingsTestCatalog(t *testing.T, r *Router, id int) {
	t.Helper()
	out, err := r.Client(raw(object{"id": raw(id), "method": raw("model/list"), "params": raw(object{})}))
	if err != nil {
		t.Fatal(err)
	}
	out = ownedSettingsTestPolicy(t, r, out)
	if len(out) != 2 {
		t.Fatal("settings catalog did not wait for both policies")
	}
	native := ownedTestNative(t)
	for _, d := range out {
		if _, err := ownedTestReply(t, r, d, native[d.Backend], nil); err != nil {
			t.Fatal(err)
		}
	}
}

func ownedSettingsTestRequest(id int, model, version string) []byte {
	p := object{"edits": raw([]object{
		{"keyPath": raw("model"), "value": raw(model), "mergeStrategy": raw("replace")},
		{"keyPath": raw("model_reasoning_effort"), "value": raw("none"), "mergeStrategy": raw("replace")},
	})}
	if version != "" {
		p["expectedVersion"] = raw(version)
	}
	return raw(object{"id": raw(id), "method": raw("config/batchWrite"), "params": raw(p)})
}

func TestOwnedSettingsAdmissionIsIndependentAndDefaultOff(t *testing.T) {
	r, _, _ := ownedTestSetup(t)
	out, err := r.Client(ownedSettingsTestRequest(1, "magpie/two/second", ""))
	if err != nil || len(out) != 1 || out[0].Backend != "" || !strings.Contains(string(out[0].Message), "-32601") {
		t.Fatal("default-off settings method unexpectedly enabled")
	}
	if (Config{BridgeModelSettings: "unknown"}).Validate() == nil || (Config{BridgeModelSettings: BridgeModelSettingsOwned167}).Validate() == nil {
		t.Fatal("unknown or unpaired settings mode accepted")
	}
	r, catalog, _ := ownedTestSetup(t)
	command := filepath.Join(t.TempDir(), "inert.exe")
	if err := os.WriteFile(command, []byte("MZ inert nonexecuted fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := range r.backends {
		r.backends[i].Command = command
		r.backends[i].Home = t.TempDir()
		r.backends[i].CodexHome = t.TempDir()
		r.backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	cfg := Config{Backends: r.backends, ControlHome: filepath.Dir(catalog), ControlPlane: ControlPlaneRouting167, BridgeModelCatalogJSON: catalog, BridgeModelSettings: BridgeModelSettingsOwned167}
	if _, err := LoadConfig(bytes.NewReader(raw(cfg))); err != nil {
		t.Fatal("explicit owned settings mode rejected")
	}
	if _, err := os.Stat(filepath.Join(cfg.ControlHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("configuration validation created a settings file")
	}
}

func TestOwnedSettingsPersistsRealUserLayerAndPreservesThreadOwner(t *testing.T) {
	r := ownedSettingsTestRouter(t)
	if _, err := os.Stat(r.ownedSettings.path); !os.IsNotExist(err) {
		t.Fatal("settings file exists before complete policy/catalog validation")
	}
	ownedSettingsTestCatalog(t, r, 1)
	_, initial, err := r.ownedSettings.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	r.threads["existing"] = "one"
	out, err := r.Client(ownedSettingsTestRequest(2, "magpie/two/second", initial))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(r.ownedSettings.path)
	if bytes.Contains(before, []byte("magpie/two/second")) {
		t.Fatal("settings wrote before policy precondition")
	}
	out = ownedSettingsTestPolicy(t, r, out)
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatal("persisted settings did not produce one frontend response")
	}
	m, _ := parseObject(out[0].Message)
	result, _ := child(m, "result")
	user, version, err := r.ownedSettings.snapshot()
	if err != nil || text(user, "model") != "magpie/two/second" || version == initial || text(result, "status") != "ok" || text(result, "version") != version || text(result, "filePath") != r.ownedSettings.path || !requirementsNull(result["overriddenMetadata"]) {
		t.Fatal("successful write does not describe actual persisted file/version")
	}
	out, err = r.Client([]byte(`{"id":3,"method":"config/read","params":{"includeLayers":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	out = ownedSettingsTestPolicy(t, r, out)
	m, _ = parseObject(out[0].Message)
	read, _ := child(m, "result")
	config, _ := child(read, "config")
	origins, _ := child(read, "origins")
	origin, _ := child(origins, "model")
	name, _ := child(origin, "name")
	var layers []object
	if json.Unmarshal(read["layers"], &layers) != nil || len(layers) != 2 || text(config, "model") != "magpie/two/second" || text(config, "model_reasoning_effort") != "none" || text(origin, "version") != version || text(name, "type") != "user" || text(name, "file") != r.ownedSettings.path || !requirementsNull(name["profile"]) {
		t.Fatal("config/read is not the real user-file layer")
	}
	session, _ := child(layers[1], "config")
	if exists(session, "model") || exists(session, "model_reasoning_effort") {
		t.Fatal("session flags still override the persisted defaults")
	}
	ownedSettingsTestCatalog(t, r, 4)
	if r.controlSession167.model != "magpie/two/second" || r.defaultModel != "magpie/one/first" {
		t.Fatal("catalog refresh reset user choice or changed catalog priority")
	}
	out, err = r.Client([]byte(`{"id":5,"method":"thread/start","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "two" {
		t.Fatal("new thread did not use persisted default owner")
	}
	m, _ = parseObject(out[0].Message)
	params, _ := child(m, "params")
	if text(params, "model") != "second" || text(params, "modelProvider") != "native_two" || exists(params, "config") {
		t.Fatal("new thread changed native identity or injected config")
	}
	out, err = r.Client([]byte(`{"id":6,"method":"thread/read","params":{"threadId":"existing"}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "one" || r.threads["existing"] != "one" {
		t.Fatal("settings write moved an existing thread")
	}
}

func TestOwnedSettingsRejectsUnknownOrConflictingBatchAtomically(t *testing.T) {
	for _, mutate := range []func(object){
		func(p object) { p["profile"] = raw(nil) },
		func(p object) { p["reloadUserConfig"] = raw(nil) },
		func(p object) { p["reloadUserConfig"] = raw("true") },
		func(p object) { p["reloadUserConfig"] = raw(1) },
		func(p object) { p["filePath"] = raw(filepath.Join(t.TempDir(), "foreign.toml")) },
		func(p object) { p["edits"] = raw([]object{{"keyPath": raw("model"), "value": raw("magpie/two/second"), "mergeStrategy": raw("replace")}}) },
		func(p object) { p["edits"] = raw([]object{{"keyPath": raw("model"), "value": raw("foreign"), "mergeStrategy": raw("replace")}, {"keyPath": raw("model_reasoning_effort"), "value": raw("none"), "mergeStrategy": raw("replace")}}) },
		func(p object) { p["edits"] = raw([]object{{"keyPath": raw("model"), "value": raw("magpie/two/second"), "mergeStrategy": raw("unknown")}, {"keyPath": raw("model_reasoning_effort"), "value": raw("none"), "mergeStrategy": raw("replace")}}) },
		func(p object) { p["edits"] = raw([]object{{"keyPath": raw("model"), "value": raw("magpie/two/second"), "mergeStrategy": raw("replace")}, {"keyPath": raw("model"), "value": raw("magpie/one/first"), "mergeStrategy": raw("replace")}}) },
	} {
		r := ownedSettingsTestRouter(t)
		ownedSettingsTestCatalog(t, r, 1)
		before, _ := os.ReadFile(r.ownedSettings.path)
		m, _ := parseObject(ownedSettingsTestRequest(2, "magpie/two/second", ""))
		p, _ := child(m, "params")
		mutate(p)
		m["params"] = raw(p)
		out, err := r.Client(raw(m))
		after, _ := os.ReadFile(r.ownedSettings.path)
		if err != nil || len(out) != 1 || out[0].Backend != "" || !bytes.Equal(before, after) || !strings.Contains(string(out[0].Message), "configValidationError") {
			t.Fatal("invalid batch reached native or changed persisted settings")
		}
	}
	r := ownedSettingsTestRouter(t)
	ownedSettingsTestCatalog(t, r, 1)
	before, _ := os.ReadFile(r.ownedSettings.path)
	out, err := r.Client(ownedSettingsTestRequest(2, "magpie/two/second", "sha256:stale"))
	if err != nil {
		t.Fatal(err)
	}
	out = ownedSettingsTestPolicy(t, r, out)
	after, _ := os.ReadFile(r.ownedSettings.path)
	if len(out) != 1 || !strings.Contains(string(out[0].Message), "configVersionConflict") || !bytes.Equal(before, after) || r.controlSession167.model != "magpie/one/first" {
		t.Fatal("version conflict changed effective or persisted defaults")
	}
}

func TestOwnedSettingsPolicyAndRefreshNeverFakePersistence(t *testing.T) {
	r := ownedSettingsTestRouter(t)
	out, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`))
	if err != nil || len(out) != 2 {
		t.Fatal("initial settings policy precondition missing")
	}
	m, _ := parseObject(out[0].Message)
	_, _ = r.Server(out[0].Backend, raw(object{"id": m["id"], "result": raw(object{"requirements": raw(object{})})}))
	if _, err := os.Stat(r.ownedSettings.path); !os.IsNotExist(err) || !r.ownedCatalog.failed {
		t.Fatal("non-null initial policy created settings or remained ready")
	}
	r = ownedSettingsTestRouter(t)
	ownedSettingsTestCatalog(t, r, 1)
	before, _ := os.ReadFile(r.ownedSettings.path)
	out, err = r.Client(ownedSettingsTestRequest(2, "magpie/two/second", ""))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = parseObject(out[0].Message)
	failed, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("SYNTHETIC_CANARY")})}))
	after, _ := os.ReadFile(r.ownedSettings.path)
	if err != nil || len(failed) != 1 || strings.Contains(string(failed[0].Message), "SYNTHETIC_CANARY") || !bytes.Equal(before, after) {
		t.Fatal("failed policy wrote settings or leaked native error")
	}
	_, err = r.Client([]byte(`{"id":3,"method":"model/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err = r.Client(ownedSettingsTestRequest(4, "magpie/two/second", ""))
	if err != nil || len(out) != 1 || out[0].Backend != "" {
		t.Fatal("settings write escaped catalog refresh barrier")
	}
	if err := os.WriteFile(r.ownedSettings.path, []byte("model = \"foreign\"\nmodel_reasoning_effort = \"none\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Client([]byte(`{"id":5,"method":"getAuthStatus","params":{}}`)); err == nil || !r.ownedCatalog.failed {
		t.Fatal("foreign settings mutation did not terminate connection")
	}
}

func TestOwnedSettingsRestartKeepsPreferenceWithoutChangingCatalog(t *testing.T) {
	r := ownedSettingsTestRouter(t)
	ownedSettingsTestCatalog(t, r, 1)
	catalogPath, catalogDigest := r.ownedCatalog.path, r.ownedCatalog.digest
	out, err := r.Client(ownedSettingsTestRequest(2, "magpie/two/second", ""))
	if err != nil {
		t.Fatal(err)
	}
	ownedSettingsTestPolicy(t, r, out)
	if err := r.ownedSettings.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewRouter(r.backends)
	if err != nil {
		t.Fatal(err)
	}
	next.ownedCatalog, err = loadOwnedCatalog(filepath.Dir(catalogPath), catalogPath, r.backends)
	if err != nil {
		t.Fatal(err)
	}
	next.ownedSettings, err = openOwnedSettingsStore(filepath.Dir(catalogPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.ownedSettings.Close() })
	if err := next.enableControlPlane167(); err != nil {
		t.Fatal(err)
	}
	next.phase = "ready"
	ownedSettingsTestCatalog(t, next, 1)
	if next.controlSession167.model != "magpie/two/second" || next.defaultModel != "magpie/one/first" || next.ownedCatalog.digest != catalogDigest {
		t.Fatal("restart reset persisted preference or mutated immutable catalog default")
	}
	out, err = next.Client([]byte(`{"id":2,"method":"thread/start","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "two" {
		t.Fatal("new thread after restart ignored persisted preference")
	}
}

func TestOwnedSettingsUncertainPersistenceCannotReturnSuccess(t *testing.T) {
	for _, afterPublication := range []bool{false, true} {
		r := ownedSettingsTestRouter(t)
		ownedSettingsTestCatalog(t, r, 1)
		r.ownedSettings.replace = func(home, temp string, initial bool) error {
			if afterPublication {
				if err := ownedSettingsReplace(home, temp, initial); err != nil {
					return err
				}
			}
			return errors.New("synthetic persistence uncertainty")
		}
		out, err := r.Client(ownedSettingsTestRequest(2, "magpie/two/second", ""))
		if err != nil || len(out) != 2 {
			t.Fatal("write policy fanout missing")
		}
		var failure error
		for _, d := range out {
			m, _ := parseObject(d.Message)
			response, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)})}))
			if len(response) != 0 {
				t.Fatal("uncertain persistence returned an RPC success")
			}
			failure = err
		}
		if failure == nil || !r.ownedCatalog.failed || r.controlSession167 != nil || len(r.models) != 0 {
			t.Fatal("uncertain persistence did not close routing authority")
		}
		data, err := os.ReadFile(r.ownedSettings.path)
		if err != nil || bytes.Contains(data, []byte("magpie/two/second")) != afterPublication {
			t.Fatal("fixture did not distinguish pre-write failure from post-publication uncertainty")
		}
	}
}

func ownedSettingsTestInitializePolicies(t *testing.T) (*Router, []Delivery) {
	t.Helper()
	r := ownedSettingsTestRouter(t)
	r.phase = "cold"
	r.controlHome167 = filepath.Dir(r.ownedSettings.path)
	r.controlNativeHomes167 = map[string]string{"one": t.TempDir(), "two": t.TempDir()}
	out, err := r.Client(raw(object{"id": raw(91), "method": raw("initialize"), "params": raw(object{"clientInfo": raw(object{"name": raw("owned-settings-test"), "version": raw("1")}), "capabilities": raw(object{})})}))
	if err != nil || len(out) != 2 {
		t.Fatal("initialize did not fan out")
	}
	var policies []Delivery
	for _, d := range out {
		m, _ := parseObject(d.Message)
		next, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"userAgent": raw("synthetic-test"), "codexHome": raw(r.controlNativeHomes167[d.Backend]), "platformFamily": raw("test"), "platformOs": raw("test")})}))
		if err != nil {
			t.Fatal(err)
		}
		policies = append(policies, next...)
	}
	if r.phase != "initializing" || r.controlSession167 != nil {
		t.Fatal("initialize published readiness before policy/catalog")
	}
	return r, policies
}

func TestOwnedSettingsInitializeWaitsForPolicyCatalogAndPersistence(t *testing.T) {
	r, policies := ownedSettingsTestInitializePolicies(t)
	out := ownedSettingsTestPolicy(t, r, policies)
	if _, err := os.Stat(r.ownedSettings.path); !os.IsNotExist(err) {
		t.Fatal("policy alone created settings before native catalog validation")
	}
	native := ownedTestNative(t)
	var responses []Delivery
	for _, d := range out {
		next, err := ownedTestReply(t, r, d, native[d.Backend], nil)
		if err != nil {
			t.Fatal(err)
		}
		responses = append(responses, next...)
	}
	if len(responses) != 1 || responses[0].Backend != "" || r.phase != "ready" || r.controlSession167 == nil {
		t.Fatal("complete startup did not publish one ready response")
	}
	m, _ := parseObject(responses[0].Message)
	result, _ := child(m, "result")
	if !sameJSON(m["id"], raw(91)) || text(result, "codexHome") != r.controlHome167 {
		t.Fatal("startup response lost its original correlation or owned home")
	}
	if _, err := os.Stat(r.ownedSettings.path); err != nil {
		t.Fatal("startup acknowledged readiness without actual persisted defaults")
	}
}

func TestOwnedSettingsColdPolicyFailureNeverSeedsOrResumes(t *testing.T) {
	for _, kind := range []string{"missing-field", "non-null", "rpc-error"} {
		t.Run(kind, func(t *testing.T) {
			r, policies := ownedSettingsTestInitializePolicies(t)
			if len(policies) != 2 {
				t.Fatal("cold initialization did not issue the policy pair")
			}
			m, _ := parseObject(policies[0].Message)
			response := object{"id": m["id"], "result": raw(object{"requirements": raw(nil)})}
			if kind == "non-null" {
				response["result"] = raw(object{"requirements": raw(object{}), "supportsIndependentSpeedModes": raw(true)})
			}
			if kind == "rpc-error" {
				delete(response, "result")
				response["error"] = raw(object{"code": raw(-32000), "message": raw("synthetic policy failure")})
			}
			out, err := r.Server(policies[0].Backend, raw(response))
			if err == nil || len(out) != 0 || r.phase != "failed" || !r.ownedCatalog.failed || r.controlSession167 != nil {
				t.Fatal("cold policy failure did not close initialization")
			}
			late, _ := parseObject(policies[1].Message)
			out, err = r.Server(policies[1].Backend, raw(object{"id": late["id"], "result": raw(object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)})}))
			if err == nil || len(out) != 0 || r.phase != "failed" || r.controlSession167 != nil || len(r.models) != 0 {
				t.Fatal("late policy sibling resumed failed initialization")
			}
			if _, err := os.Stat(r.ownedSettings.path); !os.IsNotExist(err) {
				t.Fatal("failed initialization created persistent defaults")
			}
		})
	}
}

func TestOwnedSettingsStaticScalarStrategiesAndReloadBoolean(t *testing.T) {
	for _, modelStrategy := range []string{"replace", "upsert"} {
		for _, effortStrategy := range []string{"replace", "upsert"} {
			for _, reload := range []bool{false, true} {
				r := ownedSettingsTestRouter(t)
				ownedSettingsTestCatalog(t, r, 1)
				r.threads["existing"] = "one"
				m, _ := parseObject(ownedSettingsTestRequest(2, "magpie/two/second", ""))
				p, _ := child(m, "params")
				var edits []object
				if json.Unmarshal(p["edits"], &edits) != nil {
					t.Fatal("invalid synthetic edits")
				}
				edits[0]["mergeStrategy"] = raw(modelStrategy)
				edits[1]["mergeStrategy"] = raw(effortStrategy)
				p["edits"] = raw(edits)
				p["reloadUserConfig"] = raw(reload)
				m["params"] = raw(p)
				out, err := r.Client(raw(m))
				if err != nil {
					t.Fatal(err)
				}
				out = ownedSettingsTestPolicy(t, r, out)
				if len(out) != 1 || out[0].Backend != "" {
					t.Fatal("static default edit did not complete locally")
				}
				response, _ := parseObject(out[0].Message)
				result, _ := child(response, "result")
				user, _, err := r.ownedSettings.snapshot()
				if err != nil || text(result, "status") != "ok" || text(user, "model") != "magpie/two/second" || text(user, "model_reasoning_effort") != "none" || r.controlSession167.model != "magpie/two/second" || r.threads["existing"] != "one" || len(r.pending) != 0 {
					t.Fatal("static scalar write changed owner, failed persistence or forwarded extra operations")
				}
			}
		}
	}
}

func TestOwnedSettingsUpsertCannotExpandScalarContract(t *testing.T) {
	for _, value := range []json.RawMessage{raw(nil), raw(object{"nested": raw("value")}), raw([]string{"value"})} {
		r := ownedSettingsTestRouter(t)
		ownedSettingsTestCatalog(t, r, 1)
		before, _ := os.ReadFile(r.ownedSettings.path)
		m, _ := parseObject(ownedSettingsTestRequest(2, "magpie/two/second", ""))
		p, _ := child(m, "params")
		p["edits"] = raw([]object{{"keyPath": raw("model"), "value": value, "mergeStrategy": raw("upsert")}, {"keyPath": raw("model_reasoning_effort"), "value": raw("none"), "mergeStrategy": raw("upsert")}})
		m["params"] = raw(p)
		out, err := r.Client(raw(m))
		after, _ := os.ReadFile(r.ownedSettings.path)
		if err != nil || len(out) != 1 || out[0].Backend != "" || !strings.Contains(string(out[0].Message), "configValidationError") || !bytes.Equal(before, after) {
			t.Fatal("upsert accepted non-scalar value or changed storage")
		}
	}
}
