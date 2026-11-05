package appserverbridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixtureModel(id string, def bool) json.RawMessage {
	return raw(map[string]any{"id": id, "model": id, "displayName": id, "description": "fixture, not a live capability claim", "hidden": false, "isDefault": def, "supportsPersonality": false, "supportedReasoningEfforts": []any{}, "defaultReasoningEffort": "medium", "inputModalities": []string{"text"}, "extraFutureMetadata": map[string]any{"preserved": true}})
}
func testRouter(t *testing.T, static bool) *Router {
	t.Helper()
	b := []Backend{{ID: "openai", Provider: "openai"}, {ID: "aihub", Provider: "aihub"}}
	if static {
		b[0].Models = []json.RawMessage{fixtureModel("gpt", true)}
		b[1].Models = []json.RawMessage{fixtureModel("deepseek-flash-ft", true)}
	}
	r, e := NewRouter(b)
	if e != nil {
		t.Fatal(e)
	}
	init := client(t, r, `{"id":9007199254740993,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"0"},"capabilities":{"experimentalApi":false}}}`)
	if len(init) != 2 {
		t.Fatalf("initialize deliveries: %v", init)
	}
	if out := reply(t, r, init[1], `{"userAgent":"second"}`); len(out) != 0 {
		t.Fatal("initialized before both backends")
	}
	out := reply(t, r, init[0], `{"userAgent":"first"}`)
	if !strings.Contains(string(out[0].Message), `9007199254740993`) || !strings.Contains(string(out[0].Message), `first`) {
		t.Fatalf("initialize identity or id lost: %s", out[0].Message)
	}
	if n := len(client(t, r, `{"method":"initialized"}`)); n != 2 {
		t.Fatalf("initialized deliveries=%d", n)
	}
	if static {
		client(t, r, `{"id":1,"method":"model/list"}`)
	}
	return r
}
func client(t *testing.T, r *Router, s string) []Delivery {
	t.Helper()
	out, e := r.Client([]byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func server(t *testing.T, r *Router, b, s string) []Delivery {
	t.Helper()
	out, e := r.Server(b, []byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func reply(t *testing.T, r *Router, d Delivery, result string) []Delivery {
	t.Helper()
	m, _ := parseObject(d.Message)
	return server(t, r, d.Backend, string(raw(object{"id": m["id"], "result": json.RawMessage(result)})))
}
func expectError(t *testing.T, out []Delivery) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "" || !strings.Contains(string(out[0].Message), `"error"`) {
		t.Fatalf("expected local error, got %v", out)
	}
}
func start(t *testing.T, r *Router, backend, id, model string) {
	t.Helper()
	out := client(t, r, `{"id":"start-`+id+`","method":"thread/start","params":{"model":"magpie/`+backend+`/`+model+`"}}`)
	if len(out) != 1 || out[0].Backend != backend {
		t.Fatalf("wrong start routing: %v", out)
	}
	reply(t, r, out[0], `{"thread":{"id":"`+id+`","modelProvider":"`+backend+`","model":"`+model+`","turns":[]},"model":"`+model+`","modelProvider":"`+backend+`"}`)
}

func TestMergedModelsPreserveMetadataAndOneDefault(t *testing.T) {
	r := testRouter(t, true)
	out := client(t, r, `{"id":2,"method":"model/list"}`)
	var m struct {
		Result struct{ Data []map[string]any }
	}
	if e := json.Unmarshal(out[0].Message, &m); e != nil {
		t.Fatal(e)
	}
	if len(m.Result.Data) != 2 {
		t.Fatal(m)
	}
	defaults := 0
	for _, model := range m.Result.Data {
		if model["isDefault"] == true {
			defaults++
		}
		if model["id"] != model["model"] || model["extraFutureMetadata"] == nil {
			t.Fatal(model)
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults=%d", defaults)
	}
}
func TestModelPaginationAcrossBothBackends(t *testing.T) {
	r := testRouter(t, false)
	out := client(t, r, `{"id":"list","method":"model/list","params":{"limit":1}}`)
	first := reply(t, r, out[0], string(raw(map[string]any{"data": []json.RawMessage{fixtureModel("a", true)}, "nextCursor": "upstream-2"})))
	if len(first) != 1 || first[0].Backend != "openai" {
		t.Fatal(first)
	}
	if out := reply(t, r, out[1], string(raw(map[string]any{"data": []json.RawMessage{fixtureModel("c", true)}, "nextCursor": nil}))); len(out) != 0 {
		t.Fatal(out)
	}
	merged := reply(t, r, first[0], string(raw(map[string]any{"data": []json.RawMessage{fixtureModel("b", false)}, "nextCursor": nil})))
	m, _ := parseObject(merged[0].Message)
	result, _ := child(m, "result")
	cursor := text(result, "nextCursor")
	if cursor == "" || strings.Contains(cursor, "upstream") {
		t.Fatal(string(merged[0].Message))
	}
	for i := 0; i < 2; i++ {
		page := client(t, r, `{"id":3,"method":"model/list","params":{"cursor":"`+cursor+`"}}`)
		p, _ := parseObject(page[0].Message)
		q, _ := child(p, "result")
		cursor = text(q, "nextCursor")
	}
	if cursor != "" {
		t.Fatal(cursor)
	}
	expectError(t, client(t, r, `{"id":4,"method":"model/list","params":{"cursor":"upstream-2"}}`))
}
func TestThreadProviderFixedAndPayloadUnchanged(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "thread-a", "gpt")
	start(t, r, "aihub", "thread-b", "deepseek-flash-ft")
	input := `[{"type":"text","text":"literal magpie/aihub/deepseek-flash-ft is user text","text_elements":[]}]`
	out := client(t, r, `{"id":"turn-a","method":"turn/start","params":{"threadId":"thread-a","input":`+input+`}}`)
	if len(out) != 1 || out[0].Backend != "openai" || !strings.Contains(string(out[0].Message), "literal magpie/aihub/") {
		t.Fatal(out)
	}
	expectError(t, client(t, r, `{"id":"cross","method":"turn/start","params":{"threadId":"thread-a","model":"magpie/aihub/deepseek-flash-ft","input":[]}}`))
	expectError(t, client(t, r, `{"id":"unknown","method":"turn/start","params":{"threadId":"unknown","model":"magpie/aihub/deepseek-flash-ft","input":[]}}`))
	out = client(t, r, `{"id":"turn-b","method":"turn/start","params":{"threadId":"thread-b","input":[]}}`)
	if len(out) != 1 || out[0].Backend != "aihub" {
		t.Fatal(out)
	}
}
func TestOverridesAndUnknownMethodsFailClosed(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "thread-a", "gpt")
	cases := []string{
		`{"method":"thread/start","params":{"model":"gpt"}}`,
		`{"method":"thread/start","params":{"model":"magpie/openai/gpt","modelProvider":"aihub"}}`,
		`{"method":"thread/start","params":{"model":"magpie/openai/gpt","config":{"model_provider":"aihub"}}}`,
		`{"method":"thread/start","params":{"model":"magpie/openai/gpt","allowProviderModelFallback":true}}`,
		`{"method":"thread/resume","params":{"threadId":"thread-a","history":[]}}`,
		`{"method":"thread/resume","params":{"threadId":"thread-a","path":"/foreign"}}`,
		`{"method":"turn/start","params":{"threadId":"thread-a","input":[],"collaborationMode":{"settings":{"model":"foreign"}}}}`,
		`{"method":"unknown/send","params":{"threadId":"thread-a","input":"secret"}}`,
	}
	for _, s := range cases {
		m, _ := parseObject([]byte(s))
		m["id"] = raw("reject")
		expectError(t, client(t, r, string(raw(m))))
	}
}
func TestStartedNotificationBeforeResponseAndTurnStop(t *testing.T) {
	r := testRouter(t, true)
	req := client(t, r, `{"id":20,"method":"thread/start","params":{"model":"magpie/aihub/deepseek-flash-ft"}}`)[0]
	notice := server(t, r, "aihub", `{"method":"thread/started","params":{"thread":{"id":"thread-b","model":"deepseek-flash-ft","modelProvider":"aihub","turns":[]}}}`)
	if !strings.Contains(string(notice[0].Message), "magpie/aihub/deepseek-flash-ft") {
		t.Fatal(notice)
	}
	reply(t, r, req, `{"thread":{"id":"thread-b","modelProvider":"aihub","turns":[]},"model":"deepseek-flash-ft","modelProvider":"aihub"}`)
	server(t, r, "aihub", `{"method":"turn/started","params":{"threadId":"thread-b","turn":{"id":"turn-b","items":[]}}}`)
	out := client(t, r, `{"id":21,"method":"turn/interrupt","params":{"threadId":"thread-b","turnId":"turn-b"}}`)
	if len(out) != 1 || out[0].Backend != "aihub" {
		t.Fatal(out)
	}
	expectError(t, client(t, r, `{"id":22,"method":"turn/interrupt","params":{"threadId":"thread-b","turnId":"foreign-turn"}}`))
}
func TestBackendRequestIDsAreScopedAndLateResponsesIgnored(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "a", "gpt")
	start(t, r, "aihub", "b", "deepseek-flash-ft")
	a := server(t, r, "openai", `{"id":7,"method":"item/commandExecution/requestApproval","params":{"threadId":"a","command":"echo a"}}`)[0]
	b := server(t, r, "aihub", `{"id":7,"method":"item/commandExecution/requestApproval","params":{"threadId":"b","command":"echo b"}}`)[0]
	am, _ := parseObject(a.Message)
	bm, _ := parseObject(b.Message)
	if string(am["id"]) == string(bm["id"]) {
		t.Fatal("server ID collision")
	}
	response := object{"id": bm["id"], "result": raw(map[string]string{"decision": "accept"})}
	out := client(t, r, string(raw(response)))
	if out[0].Backend != "aihub" || !strings.Contains(string(out[0].Message), `"id":7`) {
		t.Fatal(out)
	}
	resolved := server(t, r, "openai", `{"method":"serverRequest/resolved","params":{"threadId":"a","requestId":7}}`)
	rm, _ := parseObject(resolved[0].Message)
	rp, _ := child(rm, "params")
	if string(rp["requestId"]) != string(am["id"]) {
		t.Fatal(resolved)
	}
	response["id"] = am["id"]
	if out := client(t, r, string(raw(response))); len(out) != 0 {
		t.Fatal("late cancelled response was forwarded")
	}
}
func TestWrongBackendResponseAndNotificationAreFatal(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "a", "gpt")
	req := client(t, r, `{"id":40,"method":"thread/read","params":{"threadId":"a"}}`)[0]
	m, _ := parseObject(req.Message)
	if _, e := r.Server("aihub", raw(object{"id": m["id"], "result": raw(object{})})); e == nil {
		t.Fatal("foreign response accepted")
	}
	if _, e := r.Server("aihub", []byte(`{"method":"item/agentMessage/delta","params":{"threadId":"a","delta":"wrong provider"}}`)); e == nil {
		t.Fatal("foreign notification accepted")
	}
}
func TestThreadCollisionAndUnknownOwnershipFailClosed(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "same", "gpt")
	req := client(t, r, `{"id":41,"method":"thread/start","params":{"model":"magpie/aihub/deepseek-flash-ft"}}`)[0]
	m, _ := parseObject(req.Message)
	_, e := r.Server("aihub", raw(object{"id": m["id"], "result": raw(map[string]any{"thread": map[string]any{"id": "same", "modelProvider": "aihub"}})}))
	if e == nil {
		t.Fatal("collision accepted")
	}
	expectError(t, client(t, r, `{"id":42,"method":"thread/read","params":{"threadId":"same"}}`))
	if _, e := r.Server("openai", []byte(`{"method":"thread/started","params":{"thread":{"id":"unasked","modelProvider":"openai"}}}`)); e == nil {
		t.Fatal("unknown start accepted")
	}
}
func TestForkHistoryAndOutOfOrderDiscovery(t *testing.T) {
	r := testRouter(t, true)
	req := client(t, r, `{"id":50,"method":"thread/list","params":{"excludedThreadIds":["excluded"]}}`)
	thread := func(id, parent string, created int) map[string]any {
		return map[string]any{"id": id, "modelProvider": "openai", "forkedFromId": parent, "createdAt": created, "turns": []any{map[string]any{"id": "shared-historical-turn"}}}
	}
	reply(t, r, req[1], `{"data":[],"nextCursor":null}`)
	out := reply(t, r, req[0], string(raw(map[string]any{"data": []any{thread("fork", "parent", 3), thread("parent", "", 2), thread("excluded", "", 1)}, "nextCursor": nil})))
	if strings.Contains(string(out[0].Message), `"id":"excluded"`) {
		t.Fatal("unknown exclusion ignored")
	}
	for _, id := range []string{"fork", "parent"} {
		req := client(t, r, `{"id":"items-`+id+`","method":"thread/items/list","params":{"threadId":"`+id+`","turnId":"shared-historical-turn"}}`)
		if req[0].Backend != "openai" {
			t.Fatal(req)
		}
	}
}
func TestCatalogRefreshDropsRemovedAndHiddenDefault(t *testing.T) {
	r := testRouter(t, true)
	hidden, _ := parseObject(fixtureModel("hidden", true))
	hidden["hidden"] = raw(true)
	r.backends[0].Models = []json.RawMessage{raw(hidden)}
	r.backends[1].Models = []json.RawMessage{}
	client(t, r, `{"id":60,"method":"model/list"}`)
	expectError(t, client(t, r, `{"id":61,"method":"thread/start","params":{"model":"magpie/openai/gpt"}}`))
	expectError(t, client(t, r, `{"id":62,"method":"thread/start","params":{}}`))
}
func TestMalformedAndUninitializedInput(t *testing.T) {
	r, _ := NewRouter([]Backend{{ID: "a", Provider: "openai"}, {ID: "b", Provider: "aihub"}})
	expectError(t, client(t, r, `garbage`))
	expectError(t, client(t, r, `{"id":1,"method":"thread/start"}`))
	expectError(t, client(t, r, `{"id":true,"method":"initialize"}`))
	if _, e := r.Server("a", []byte(`[]`)); e == nil {
		t.Fatal("array accepted")
	}
}
func TestBackendFailureNeverRetriesOtherProvider(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "aihub", "a", "deepseek-flash-ft")
	req := client(t, r, `{"id":"failure","method":"turn/start","params":{"threadId":"a","input":[]}}`)[0]
	m, _ := parseObject(req.Message)
	out := server(t, r, "aihub", string(raw(object{"id": m["id"], "error": raw(map[string]any{"code": -32000, "message": "fixture denied"})})))
	expectError(t, out)
	if !strings.Contains(string(out[0].Message), "fixture denied") {
		t.Fatal(out)
	}
}

func TestOfficialUnauthenticatedModelCatalogFixture(t *testing.T) {
	data, e := os.ReadFile("testdata/codex-0.167.0-alpha.4-model-list.json")
	if e != nil {
		t.Fatal(e)
	}
	r := testRouter(t, false)
	req := client(t, r, `{"id":80,"method":"model/list"}`)
	reply(t, r, req[1], `{"data":[],"nextCursor":null}`)
	out := reply(t, r, req[0], string(data))
	if len(out) != 1 || !strings.Contains(string(out[0].Message), "magpie/openai/") {
		t.Fatal(out)
	}
}
func TestUnsupportedCapabilitiesAndSortFailBeforeSending(t *testing.T) {
	for _, capability := range []string{"requestAttestation", "explicitGatewayOauth"} {
		r, _ := NewRouter([]Backend{{ID: "a", Provider: "openai"}, {ID: "b", Provider: "aihub"}})
		expectError(t, client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{"`+capability+`":true}}}`))
	}
	r := testRouter(t, true)
	expectError(t, client(t, r, `{"id":2,"method":"thread/list","params":{"sortKey":"section_position"}}`))
}
func TestRequestBudgetAndConcurrentListFailLocally(t *testing.T) {
	r := testRouter(t, false)
	req := client(t, r, `{"id":90,"method":"model/list"}`)
	if len(req) != 2 {
		t.Fatal(req)
	}
	expectError(t, client(t, r, `{"id":91,"method":"model/list"}`))
	r.activeBytes = 32 << 20
	expectError(t, client(t, r, `{"id":92,"method":"account/read"}`))
}

func TestFailedFanoutReleasesPayloadAndBoundsLateResponses(t *testing.T) {
	r := testRouter(t, false)
	var stalled Delivery
	for i := 0; i < 300; i++ {
		req := client(t, r, `{"id":"list","method":"model/list","params":{"includeHidden":true}}`)
		stalled = req[1]
		first, _ := parseObject(req[0].Message)
		expectError(t, server(t, r, req[0].Backend, string(raw(object{"id": first["id"], "error": raw(map[string]any{"code": -32000, "message": "fixture failure"})}))))
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.staleResponses) > 256 {
			t.Fatal("failed list retained request state")
		}
	}
	if out := reply(t, r, stalled, `{"data":[],"nextCursor":null}`); len(out) != 0 {
		t.Fatal("late failed fanout response reached client")
	}
}
