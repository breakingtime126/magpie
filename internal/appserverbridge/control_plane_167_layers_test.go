package appserverbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func controlPlaneLayersAsk167(t *testing.T, r *Router, params string) []Delivery {
	t.Helper()
	out := client(t, r, `{"id":"owned-layer","method":"config/read","params":`+params+`,"trace":{"traceparent":"PRIVATE_TRACE"}}`)
	if len(out) != 2 || out[0].Backend == "" || out[0].Backend == out[1].Backend {
		t.Fatal("layer read must request two fresh independent policy views")
	}
	for _, delivery := range out {
		message, _ := parseObject(delivery.Message)
		if len(message) != 3 || text(message, "method") != "configRequirements/read" || string(message["params"]) != "null" {
			t.Fatal("layer read forwarded config, params or trace to a native backend")
		}
	}
	return out
}

func controlPlaneLayersDone167(t *testing.T, r *Router, requests []Delivery) object {
	t.Helper()
	if len(reply(t, r, requests[1], controlPlaneNullRequirements)) != 0 {
		t.Fatal("layer read returned before both policy views")
	}
	done := reply(t, r, requests[0], controlPlaneNullRequirements)
	if len(done) != 1 || done[0].Backend != "" {
		t.Fatal("layer read did not produce one frontend response")
	}
	message, _ := parseObject(done[0].Message)
	if text(message, "id") != "owned-layer" || exists(message, "error") {
		t.Fatal("layer read changed the original ID or failed")
	}
	result, _ := child(message, "result")
	key, _ := idKey(raw("owned-layer"))
	if r.active[key] {
		t.Fatal("completed layer read retained active state")
	}
	return result
}

// Independent fixed-schema oracle. The order and escaping here follow pinned
// serde_json sorted compact JSON, not the production serializer or snapshot.
// modelJSON is already quoted JSON so Unicode/control fixtures remain explicit.
func controlPlaneLayerOracle167(modelJSON string) []byte {
	model, provenance := "", ""
	if modelJSON != "" {
		model = `"model":` + modelJSON + ","
		provenance = `"model":"bridge-catalog-default",`
	}
	return []byte(`{"magpie_bridge":{"backends":[{"catalog_source":"native-model-list","id":"openai","model_prefix":"magpie/openai/","provider":"openai"},{"catalog_source":"native-model-list","id":"aihub","model_prefix":"magpie/aihub/","provider":"aihub"}],"execution_policy":"backend-owned-per-thread","model_catalog":"model/list","provenance":{` +
		provenance + `"model_provider":"bridge-routing"},"provider_routing":"fixed-per-thread","supports":{"config_writes":false,"cwd_reads":false,"layer_reads":true},"version":1,"view":"routing-defaults"},` +
		model + `"model_provider":"magpie_bridge"}`)
}

func controlPlaneCheckLayer167(t *testing.T, result object, modelJSON string, included bool) string {
	t.Helper()
	wantConfig := controlPlaneLayerOracle167(modelJSON)
	if !sameJSON(result["config"], wantConfig) {
		t.Fatalf("owned config differs from independent routing oracle: %s", result["config"])
	}
	digest := sha256.Sum256(wantConfig)
	version := "sha256:" + hex.EncodeToString(digest[:])
	wantMetadata := raw(object{"name": raw(object{"type": raw("sessionFlags")}), "version": raw(version)})
	keys := []string{
		"model_provider", "magpie_bridge.version", "magpie_bridge.view",
		"magpie_bridge.provider_routing", "magpie_bridge.execution_policy", "magpie_bridge.model_catalog",
		"magpie_bridge.supports.config_writes", "magpie_bridge.supports.layer_reads", "magpie_bridge.supports.cwd_reads",
		"magpie_bridge.provenance.model_provider",
	}
	for index := 0; index < 2; index++ {
		for _, field := range []string{"id", "provider", "model_prefix", "catalog_source"} {
			keys = append(keys, fmt.Sprintf("magpie_bridge.backends.%d.%s", index, field))
		}
	}
	if modelJSON != "" {
		keys = append(keys, "model", "magpie_bridge.provenance.model")
	}
	origins, err := child(result, "origins")
	if err != nil || len(origins) != len(keys) {
		t.Fatal("session origins omitted leaves or advertised container/native origins")
	}
	for _, key := range keys {
		if !sameJSON(origins[key], wantMetadata) {
			t.Fatalf("origin %s does not identify the actual session layer and version", key)
		}
	}
	if !included {
		if len(result) != 2 || exists(result, "layers") {
			t.Fatal("unrequested layers must be omitted, not null or empty")
		}
		return version
	}
	var layers []object
	if len(result) != 3 || json.Unmarshal(result["layers"], &layers) != nil || len(layers) != 1 {
		t.Fatal("includeLayers:true requires exactly one genuine session layer")
	}
	layer := layers[0]
	if len(layer) != 3 || !sameJSON(layer["config"], result["config"]) ||
		text(layer, "version") != version || !sameJSON(layer["name"], raw(object{"type": raw("sessionFlags")})) {
		t.Fatal("layer config, source or native content version differs from the owned result")
	}
	return version
}

func TestControlPlane167LayersShapesAndFreshPolicy(t *testing.T) {
	r := controlPlaneRouter(t)
	var expectedConfig, expectedOrigins json.RawMessage
	for _, params := range []string{
		`{}`, `{"includeLayers":false}`, `{"cwd":null}`, `{"includeLayers":false,"cwd":null}`,
		`{"includeLayers":true}`, `{"includeLayers":true,"cwd":null}`,
		`{"include\u004cayers": true, "c\u0077d":null}`,
	} {
		var values struct {
			IncludeLayers bool
		}
		if err := json.Unmarshal([]byte(params), &values); err != nil {
			t.Fatal(err)
		}
		result := controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, params))
		controlPlaneCheckLayer167(t, result, "", values.IncludeLayers)
		if expectedConfig == nil {
			expectedConfig, expectedOrigins = result["config"], result["origins"]
		} else if !sameJSON(expectedConfig, result["config"]) || !sameJSON(expectedOrigins, result["origins"]) {
			t.Fatal("includeLayers changed the effective session settings or origins")
		}
		requests := controlPlaneLayersAsk167(t, r, params)
		reply(t, r, requests[0], controlPlaneNullRequirements)
		controlPlaneError(t, reply(t, r, requests[1], `{"requirements":{},"supportsIndependentSpeedModes":true}`))
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("failed layer policy read retained state")
		}
	}
}

func TestControlPlane167LayerCatalogPublicationAndRouting(t *testing.T) {
	r := controlPlaneRouter(t)
	read := func(modelJSON string) string {
		t.Helper()
		result := controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`))
		return controlPlaneCheckLayer167(t, result, modelJSON, true)
	}
	initial := read("")
	controlPlaneCatalog(t, r, true)
	first := read(`"magpie/openai/gpt"`)
	if first == initial || read(`"magpie/openai/gpt"`) != first {
		t.Fatal("default publication did not change the version or identical reread was unstable")
	}
	// One native page cannot change the published authority.
	refresh := client(t, r, `{"id":"refresh","method":"model/list","params":{}}`)
	firstPage := raw(object{"data": raw([]json.RawMessage{fixtureModel("next", true)}), "nextCursor": raw(nil)})
	if len(reply(t, r, refresh[0], string(firstPage))) != 0 || read(`"magpie/openai/gpt"`) != first {
		t.Fatal("incomplete catalog refresh changed the session layer")
	}
	message, _ := parseObject(refresh[1].Message)
	expectError(t, server(t, r, refresh[1].Backend, string(raw(object{
		"id": message["id"], "error": raw(object{"code": raw(-32000), "message": raw("catalog unavailable")}),
	}))))
	if read(`"magpie/openai/gpt"`) != first {
		t.Fatal("failed catalog refresh changed the session layer")
	}
	// A read linearizes after its second policy reply. A complete catalog
	// published while the policy pair is pending must be reflected coherently.
	requests := controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`)
	reply(t, r, requests[0], controlPlaneNullRequirements)
	refresh = client(t, r, `{"id":"refresh-next","method":"model/list","params":{}}`)
	reply(t, r, refresh[0], string(firstPage))
	reply(t, r, refresh[1], `{"data":[],"nextCursor":null}`)
	done := reply(t, r, requests[1], controlPlaneNullRequirements)
	message, _ = parseObject(done[0].Message)
	result, _ := child(message, "result")
	next := controlPlaneCheckLayer167(t, result, `"magpie/openai/next"`, true)
	if next == first {
		t.Fatal("new default did not receive a new content version")
	}
	start := client(t, r, `{"id":"start-default","method":"thread/start","params":{}}`)
	if len(start) != 1 || start[0].Backend != "openai" {
		t.Fatal("published session default did not choose the real thread owner")
	}
	native, _ := parseObject(start[0].Message)
	params, _ := child(native, "params")
	if text(params, "model") != "next" || text(params, "modelProvider") != "openai" {
		t.Fatal("omitted-model thread/start did not consume the published session default")
	}
	reply(t, r, start[0], `{"thread":{"id":"session-thread","modelProvider":"openai","model":"next","turns":[]},"model":"next","modelProvider":"openai"}`)
	controlPlaneCatalog(t, r, false)
	if read("") != initial {
		t.Fatal("removing the default did not restore the original content version")
	}
	expectError(t, client(t, r, `{"id":"no-default","method":"thread/start","params":{}}`))
}

func TestControlPlane167LayersFailuresNeverExposeNativeData(t *testing.T) {
	for _, bad := range []string{
		`null`, `{}`, `{"requirements":{},"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":false}`,
		`{"requirements":null,"supportsIndependentSpeedModes":true,"config":{"token":"PRIVATE_SECRET"}}`,
		`{"requirements":{},"require\u006dents":null,"supportsIndependentSpeedModes":true}`,
	} {
		for _, first := range []int{0, 1} {
			r := controlPlaneRouter(t)
			requests := controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`)
			reply(t, r, requests[first], controlPlaneNullRequirements)
			controlPlaneError(t, reply(t, r, requests[1-first], bad))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("rejected layer read retained state")
			}
		}
	}
	r := controlPlaneRouter(t)
	requests := controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`)
	if out, err := r.Client([]byte(`{"id":"owned-layer","method":"config/read","params":{"includeLayers":true}}`)); err == nil || len(out) != 0 {
		t.Fatal("duplicate active layer request was accepted")
	}
	message, _ := parseObject(requests[0].Message)
	controlPlaneError(t, server(t, r, requests[0].Backend, string(raw(object{
		"id": message["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE_ERROR"), "data": raw("PRIVATE_SECRET")}),
	}))))
	if len(reply(t, r, requests[1], controlPlaneNullRequirements)) != 0 {
		t.Fatal("late requirements reply completed a rejected layer read")
	}
	result := controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`))
	controlPlaneCheckLayer167(t, result, "", true)
	for _, private := range []string{"PRIVATE_", "auth", "approval_policy", "sandbox_mode", "model_providers", "file", "disabledReason"} {
		if strings.Contains(string(raw(result)), private) {
			t.Fatalf("layer response exposed native/private/writable metadata %q", private)
		}
	}
}

func TestControlPlane167LayersStrictBounds(t *testing.T) {
	for _, params := range []string{
		`{"includeLayers":true,"include\u004cayers":false}`,
		`{"includeLayers":true,"cwd":null,"c\u0077d":null}`,
		`{"includeLayers":true,"cwd":""}`, `{"includeLayers":true,"cwd":"PRIVATE_PATH"}`,
		`{"includeLayers":true,"other":"PRIVATE_FIELD"}`, `{"includeLayers":"true"}`,
		`{"includeLayers":true,"cwd":[]}`,
	} {
		r := controlPlaneRouter(t)
		expectError(t, client(t, r, `{"id":1,"method":"config/read","params":`+params+`}`))
		if len(r.pending) != 0 || len(r.active) != 0 {
			t.Fatal("invalid layered request reached a native backend")
		}
	}
	base := `{"id":1,"method":"config/read","params":{"includeLayers":true}}`
	atLimit := base + strings.Repeat(" ", requirementsJSONLimit-len(base))
	if !validateControlPlaneRead167([]byte(atLimit)) || validateControlPlaneRead167([]byte(atLimit+" ")) {
		t.Fatal("layered request did not preserve the exact strict JSON byte bound")
	}
	r := testRouter(t, false)
	expectError(t, client(t, r, base))
	if len(r.pending) != 0 || len(r.active) != 0 {
		t.Fatal("layer request enabled the default-off control plane")
	}
}

func TestControlPlane167NativeFingerprintEscaping(t *testing.T) {
	value := map[string]any{
		"z": "quote\" backslash\\ literal\\u2028 <>& 中文 😀 \u2028\u2029\b\f\n\r\t\x00\x01\x1f",
		"a": []any{true, json.Number("1"), map[string]any{"x": false}},
	}
	want := []byte("{\"a\":[true,1,{\"x\":false}],\"z\":\"quote\\\" backslash\\\\ literal\\\\u2028 <>& 中文 😀 \u2028\u2029\\b\\f\\n\\r\\t\\u0000\\u0001\\u001f\"}")
	got, err := appendControlPlaneJSON167(nil, value)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("canonical bytes differ from native UTF-8 JSON oracle: %q", got)
	}
	for _, bad := range []any{nil, json.Number("1.5"), json.Number("9223372036854775808"), string([]byte{0xff})} {
		if _, err := appendControlPlaneJSON167(nil, bad); err == nil {
			t.Fatal("unsupported or invalid owned JSON value was silently fingerprinted")
		}
	}
	r := controlPlaneRouter(t)
	model := "<>&中文😀\u2028\u2029\\u2028"
	requests := client(t, r, `{"id":"unicode-catalog","method":"model/list","params":{}}`)
	reply(t, r, requests[0], string(raw(object{"data": raw([]json.RawMessage{fixtureModel(model, true)}), "nextCursor": raw(nil)})))
	reply(t, r, requests[1], `{"data":[],"nextCursor":null}`)
	result := controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`))
	controlPlaneCheckLayer167(t, result, "\"magpie/openai/<>&中文😀\u2028\u2029\\\\u2028\"", true)
	// A response is read-only: constructing another response cannot mutate the
	// already published settings or return a newly generated version.
	first := r.controlSession167
	before := append([]byte(nil), first.config...)
	controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, `{"includeLayers":false}`))
	if !bytes.Equal(before, r.controlSession167.config) || first != r.controlSession167 {
		t.Fatal("config/read republished the session authority")
	}
}

func TestControlPlane167SessionSnapshotOwnsItsBytes(t *testing.T) {
	r := controlPlaneRouter(t)
	config := r.controlPlaneSessionConfig167("")
	session, err := newControlPlaneSession167(config)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), session.config...)
	config["model_provider"][0] = '!'
	result := session.read(true)
	result["config"][0] = '!'
	result["origins"][0] = '!'
	result["layers"][0] = '!'
	if !bytes.Equal(session.config, before) {
		t.Fatal("source or returned response mutated the published session bytes")
	}
	controlPlaneCheckLayer167(t, session.read(true), "", true)
}

func TestControlPlane167LayerWireBudgetAndCatalogRollback(t *testing.T) {
	// A fixed source/version length makes this result grow by two bytes per
	// ASCII byte: one in effective config and one in its sole layer.
	key := "x"
	small, err := newControlPlaneSession167(object{key: raw("")})
	if err != nil {
		t.Fatal(err)
	}
	base := len(raw(small.read(true)))
	if (controlPlaneResultLimit167-base)%2 != 0 {
		key = "xx"
		small, err = newControlPlaneSession167(object{key: raw("")})
		if err != nil {
			t.Fatal(err)
		}
		base = len(raw(small.read(true)))
	}
	size := (controlPlaneResultLimit167 - base) / 2
	atLimit, err := newControlPlaneSession167(object{key: raw(strings.Repeat("a", size))})
	if err != nil || len(raw(atLimit.read(true))) != controlPlaneResultLimit167 {
		t.Fatal("owned result at the exact wire budget must be accepted")
	}
	if _, err := newControlPlaneSession167(object{key: raw(strings.Repeat("a", size+1))}); err == nil {
		t.Fatal("owned result exceeding the wire budget must be rejected")
	}
	// Native fingerprint JSON leaves '<' literal. Wire JSON escapes it as
	// six bytes, twice, so canonical length is not the output budget.
	htmlSize := (controlPlaneResultLimit167 - base) / 12
	if _, err := newControlPlaneSession167(object{key: raw(strings.Repeat("<", htmlSize))}); err != nil {
		t.Fatal("bounded HTML-escaped result was rejected")
	}
	if _, err := newControlPlaneSession167(object{key: raw(strings.Repeat("<", htmlSize+1))}); err == nil {
		t.Fatal("HTML wire amplification bypassed the layered result budget")
	}
	r := controlPlaneRouter(t)
	controlPlaneCatalog(t, r, true)
	before := r.controlSession167
	requests := client(t, r, `{"id":"oversize-refresh","method":"model/list","params":{}}`)
	large := fixtureModel(strings.Repeat("<", controlPlaneResultLimit167/4), true)
	reply(t, r, requests[0], string(raw(object{"data": raw([]json.RawMessage{large}), "nextCursor": raw(nil)})))
	controlPlaneError(t, reply(t, r, requests[1], `{"data":[],"nextCursor":null}`))
	if r.controlSession167 != before || r.defaultModel != "magpie/openai/gpt" || len(r.models) != 2 ||
		len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
		t.Fatal("oversized layer published partial catalog state or retained the failed refresh")
	}
	result := controlPlaneLayersDone167(t, r, controlPlaneLayersAsk167(t, r, `{"includeLayers":true}`))
	controlPlaneCheckLayer167(t, result, `"magpie/openai/gpt"`, true)
	start := client(t, r, `{"id":"after-oversize","method":"thread/start","params":{}}`)
	if len(start) != 1 || start[0].Backend != "openai" {
		t.Fatal("oversized refresh lost the previous usable routing default")
	}
	message, _ := parseObject(start[0].Message)
	params, _ := child(message, "params")
	if text(params, "model") != "gpt" {
		t.Fatal("oversized refresh changed actual omitted-model routing")
	}
	reply(t, r, start[0], `{"thread":{"id":"after-oversize","modelProvider":"openai","model":"gpt","turns":[]},"model":"gpt","modelProvider":"openai"}`)
}
