package appserverbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const controlPlaneNullRequirements = `{"requirements":null,"supportsIndependentSpeedModes":true}`

func controlPlaneRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.controlPlaneRouting167 = true
	r.auth = (Config{ControlPlane: ControlPlaneRouting167}).authCompatibility()
	return r
}

func controlPlaneAsk(t *testing.T, r *Router) []Delivery {
	t.Helper()
	out := client(t, r, `{"id":9007199254740993,"method":"config/read","params":{"includeLayers":false},"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
	if len(out) != 2 {
		t.Fatalf("routing read did not request both policy authorities: %v", out)
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if d.Backend == "" || len(m) != 3 || text(m, "method") != "configRequirements/read" || string(m["params"]) != "null" {
			t.Fatal("routing read fetched native config, propagated trace or changed the native policy call")
		}
	}
	return out
}

func controlPlaneRead(t *testing.T, r *Router) object {
	t.Helper()
	out := controlPlaneAsk(t, r)
	if len(reply(t, r, out[1], controlPlaneNullRequirements)) != 0 {
		t.Fatal("routing view returned before both genuine policy responses")
	}
	done := reply(t, r, out[0], controlPlaneNullRequirements)
	if len(done) != 1 || done[0].Backend != "" {
		t.Fatal("routing view did not return a single owned result")
	}
	m, _ := parseObject(done[0].Message)
	if string(m["id"]) != "9007199254740993" || exists(m, "error") {
		t.Fatal("routing view lost the original client ID")
	}
	result, _ := child(m, "result")
	if len(result) != 2 || string(result["origins"]) != "{}" {
		t.Fatal("routing view invented native origins or layers")
	}
	config, _ := child(result, "config")
	return config
}

func controlPlaneCatalog(t *testing.T, r *Router, def bool) {
	t.Helper()
	out := client(t, r, `{"id":"catalog","method":"model/list","params":{}}`)
	if len(out) != 2 {
		t.Fatal("expected two genuine catalog reads")
	}
	for _, d := range out {
		model := "gpt"
		if d.Backend == "aihub" {
			model = "deepseek-flash-ft"
		}
		reply(t, r, d, string(raw(object{"data": raw([]json.RawMessage{fixtureModel(model, def)}), "nextCursor": raw(nil)})))
	}
}

func controlPlaneError(t *testing.T, out []Delivery) {
	t.Helper()
	expectError(t, out)
	m, _ := parseObject(out[0].Message)
	e, _ := child(m, "error")
	if len(e) != 2 || text(e, "message") != controlPlaneUnavailable167 || string(e["code"]) != "-32000" ||
		strings.Contains(string(out[0].Message), "PRIVATE_") {
		t.Fatal("native failure escaped the owned config gate")
	}
}

func TestControlPlane167OwnedRoutingViewAndRealDefault(t *testing.T) {
	r := controlPlaneRouter(t)
	for i := range r.backends {
		r.backends[i].Home = "PRIVATE_HOME"
		r.backends[i].CodexHome = "PRIVATE_CODEX_HOME"
		r.backends[i].Command = "PRIVATE_BINARY"
		r.backends[i].InheritEnv = []string{"PRIVATE_ENV"}
	}
	config := controlPlaneRead(t, r)
	if len(config) != 2 || text(config, "model_provider") != controlPlaneProvider167 {
		t.Fatal("routing-only view invented model, policy or native configuration fields")
	}
	extension, _ := child(config, "magpie_bridge")
	if text(extension, "view") != "routing-defaults" || text(extension, "execution_policy") != "backend-owned-per-thread" ||
		text(extension, "provider_routing") != "fixed-per-thread" || text(extension, "model_catalog") != "model/list" {
		t.Fatal("owned view did not declare its limited authority")
	}
	if strings.Contains(string(raw(config)), "PRIVATE_") {
		t.Fatal("bridge paths or environment metadata leaked")
	}
	var entries []object
	if json.Unmarshal(extension["backends"], &entries) != nil || len(entries) != 2 {
		t.Fatal("missing routing authorities")
	}
	for i, entry := range entries {
		b := r.backends[i]
		if len(entry) != 4 || text(entry, "id") != b.ID || text(entry, "provider") != b.Provider ||
			text(entry, "model_prefix") != alias(b.ID, "") || text(entry, "catalog_source") != "native-model-list" {
			t.Fatal("incorrect or excessive backend metadata")
		}
	}
	supports, _ := child(extension, "supports")
	if len(supports) != 3 || string(supports["config_writes"]) != "false" || string(supports["cwd_reads"]) != "false" ||
		string(supports["layer_reads"]) != "false" {
		t.Fatal("unsupported config operations advertised")
	}
	controlPlaneCatalog(t, r, true)
	config = controlPlaneRead(t, r)
	if len(config) != 3 || text(config, "model") != "magpie/openai/gpt" {
		t.Fatal("owned view did not reuse the current merged catalog default")
	}
	controlPlaneCatalog(t, r, false)
	if _, present := controlPlaneRead(t, r)["model"]; present {
		t.Fatal("a newer catalog with no default retained the old model")
	}
	// Static descriptors are declared metadata, not native entitlement.
	r = controlPlaneRouter(t)
	r.backends[0].Models = []json.RawMessage{fixtureModel("declared", true)}
	r.backends[1].Models = []json.RawMessage{}
	if _, present := controlPlaneRead(t, r)["model"]; present {
		t.Fatal("configured descriptors invented a default before model/list")
	}
	client(t, r, `{"id":"static","method":"model/list","params":{}}`)
	config = controlPlaneRead(t, r)
	extension, _ = child(config, "magpie_bridge")
	json.Unmarshal(extension["backends"], &entries)
	if text(config, "model") != "magpie/openai/declared" || text(entries[0], "catalog_source") != "configured-descriptors" {
		t.Fatal("declared catalog provenance was lost")
	}
}

func TestControlPlane167StrictRequests(t *testing.T) {
	base := `{"id":1,"method":"config/read","params":{"includeLayers":false}}`
	cases := []string{
		strings.Replace(base, `,"params":{"includeLayers":false}`, "", 1),
		strings.Replace(base, `"params":{"includeLayers":false}`, `"params":null`, 1),
		strings.Replace(base, `"params":{"includeLayers":false}`, `"params":[]`, 1),
		strings.Replace(base, `"id":1`, `"id":1,"\u0069d":2`, 1),
		strings.Replace(base, `"method":"config/read"`, `"method":"config/read","method":"account/read"`, 1),
		strings.Replace(base, `"method":"config/read"`, `"method":"account/read","method":"config/read"`, 1),
		base + "{}",
		string([]byte{'{', '"', 0xff, '"', ':', '0', '}'}),
	}
	for _, params := range []string{
		`{}`, `{"includeLayers":true}`, `{"includeLayers":null}`, `{"includeLayers":0}`, `{"includeLayers":"false"}`,
		`{"includeLayers":false,"include\u004cayers":false}`, `{"includeLayers":false,"cwd":null}`,
		`{"includeLayers":false,"cwd":"PRIVATE_PATH"}`, `{"includeLayers":false,"other":"PRIVATE_PARAM"}`,
	} {
		cases = append(cases, strings.Replace(base, `{"includeLayers":false}`, params, 1))
	}
	for _, id := range []string{"null", "true", "1.5", "1e0", "9223372036854775808", "-9223372036854775809", `"\ud800"`, string(raw(strings.Repeat("a", 257)))} {
		cases = append(cases, strings.Replace(base, `"id":1`, `"id":`+id, 1))
	}
	for _, field := range []string{
		`"jsonrpc":"2.0"`, `"result":null`, `"trace":true`, `"trace":{"traceparent":"\ud800"}`,
		`"trace":{"unknown":"PRIVATE_TRACE"}`, `"trace":{"traceparent":"\u0000"}`, `"trace":null,"tr\u0061ce":null`,
	} {
		cases = append(cases, base[:len(base)-1]+","+field+"}")
	}
	cases = append(cases, base+strings.Repeat(" ", requirementsJSONLimit-len(base)+1))
	for i, line := range cases {
		if validateControlPlaneRead167([]byte(line)) {
			t.Fatalf("malformed routing read %d accepted", i)
		}
		r := controlPlaneRouter(t)
		out, err := r.Client([]byte(line))
		if err == nil {
			expectError(t, out)
		} else if len(out) != 0 {
			t.Fatal("fatal request produced deliveries")
		}
		for _, d := range out {
			if d.Backend != "" {
				t.Fatalf("malformed request %d reached a child", i)
			}
		}
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("malformed routing read retained state")
		}
	}
	for _, line := range []string{
		base, `{"id":"","method":"config/read","params":{"include\u004cayers": false },"trace":null}`,
		base + strings.Repeat(" ", requirementsJSONLimit-len(base)),
	} {
		if !validateControlPlaneRead167([]byte(line)) {
			t.Fatal("valid bounded observed routing request rejected")
		}
	}
}

func TestControlPlane167FreshPolicyFailuresAndCleanup(t *testing.T) {
	for _, bad := range []string{
		`null`, `{}`, `{"requirements":{},"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":false}`,
		`{"requirements":null,"supportsIndependentSpeedModes":true,"extra":"PRIVATE_CONFIG"}`,
		`{"requirements":{},"require\u006dents":null,"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":true,"x":"\ud800"}`,
	} {
		for _, first := range []int{0, 1} {
			r := controlPlaneRouter(t)
			controlPlaneRead(t, r)
			out := controlPlaneAsk(t, r)
			reply(t, r, out[first], controlPlaneNullRequirements)
			controlPlaneError(t, reply(t, r, out[1-first], bad))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("failed routing read retained payload")
			}
			controlPlaneRead(t, r)
		}
	}
	r := controlPlaneRouter(t)
	for i := 0; i < 300; i++ {
		out := controlPlaneAsk(t, r)
		m, _ := parseObject(out[0].Message)
		g := r.pending["s:"+text(m, "id")].group
		controlPlaneError(t, server(t, r, out[0].Backend, string(raw(object{
			"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE_ERROR"), "data": raw("PRIVATE_TOKEN")}),
		}))))
		if g.params != nil || g.results != nil || g.seen != nil || !g.failed {
			t.Fatal("abandoned routing read retained inputs")
		}
		if i == 0 && len(reply(t, r, out[1], controlPlaneNullRequirements)) != 0 {
			t.Fatal("late policy sibling became a config success")
		}
	}
	if len(r.staleResponses) > 256 || len(r.staleResponseOrder) > 256 {
		t.Fatal("routing read tombstones grew without bound")
	}
	out := controlPlaneAsk(t, r)
	expectError(t, client(t, r, `{"id":"overlap","method":"config/read","params":{"includeLayers":false}}`))
	reply(t, r, out[0], controlPlaneNullRequirements)
	reply(t, r, out[1], controlPlaneNullRequirements)
	controlPlaneRead(t, r)
}

func TestControlPlane167ProviderProjectionAndPayloadPreservation(t *testing.T) {
	r := controlPlaneRouter(t)
	controlPlaneCatalog(t, r, true)
	for _, b := range r.backends {
		model := "gpt"
		if b.ID == "aihub" {
			model = "deepseek-flash-ft"
		}
		params := object{
			"model": raw(alias(b.ID, model)), "modelProvider": raw(controlPlaneProvider167),
			"permissions": raw("backend-owned-profile"), "approvalPolicy": raw("on-request"),
			"approvalsReviewer": raw("user"), "runtimeWorkspaceRoots": raw([]string{"/fixture/root"}),
		}
		out := client(t, r, string(raw(object{"id": raw("start-"+b.ID), "method": raw("thread/start"), "params": raw(params)})))
		if len(out) != 1 || out[0].Backend != b.ID {
			t.Fatal("logical provider overrode the model's fixed owner")
		}
		request, _ := parseObject(out[0].Message)
		actual, _ := child(request, "params")
		for _, key := range []string{"permissions", "approvalPolicy", "approvalsReviewer", "runtimeWorkspaceRoots"} {
			if !bytes.Equal(actual[key], params[key]) {
				t.Fatalf("routing mode rewrote native permission field %s", key)
			}
		}
		if text(actual, "modelProvider") != b.Provider || text(actual, "model") != model {
			t.Fatal("logical model/provider reached native child")
		}
		thread := object{"id": raw(b.ID+"-thread"), "modelProvider": raw(b.Provider), "model": raw(model), "turns": raw([]any{}),
			"preview": raw(`literal {"modelProvider":"openai","model":"gpt"}`)}
		policy := json.RawMessage(`{"type":"workspaceWrite","writableRoots":["/fixture/root"],"networkAccess":false,"excludeTmpdirEnvVar":true,"excludeSlashTmp":true}`)
		result := reply(t, r, out[0], string(raw(object{"thread": raw(thread), "model": raw(model), "modelProvider": raw(b.Provider),
			"approvalPolicy": raw("on-request"), "sandbox": policy})))
		m, _ := parseObject(result[0].Message)
		value, _ := child(m, "result")
		frontThread, _ := child(value, "thread")
		if text(value, "modelProvider") != controlPlaneProvider167 || text(frontThread, "modelProvider") != controlPlaneProvider167 ||
			text(value, "model") != alias(b.ID, model) || !bytes.Equal(value["sandbox"], policy) || !bytes.Equal(frontThread["preview"], thread["preview"]) {
			t.Fatal("provider projection changed payloads or permissions")
		}
		for _, method := range []string{"thread/resume", "thread/fork"} {
			p := object{"threadId": raw(b.ID+"-thread"), "modelProvider": raw(controlPlaneProvider167)}
			out := client(t, r, string(raw(object{"id": raw(method+b.ID), "method": raw(method), "params": raw(p)})))
			msg, _ := parseObject(out[0].Message)
			p, _ = child(msg, "params")
			if text(p, "modelProvider") != b.Provider || out[0].Backend != b.ID {
				t.Fatal("resume/fork used logical provider as native authority")
			}
			reply(t, r, out[0], string(raw(object{"thread": raw(thread), "model": raw(model), "modelProvider": raw(b.Provider)})))
		}
		schema := json.RawMessage(`{"modelProvider":"literal-provider","nested":{"model":"literal-model"},"same":1,"same":2}`)
		out = client(t, r, string(raw(object{"id": raw("turn-"+b.ID), "method": raw("turn/start"), "params": raw(object{
			"threadId": raw(b.ID+"-thread"), "input": raw([]object{{"type": raw("text"), "text": raw("literal "+controlPlaneProvider167), "text_elements": raw([]any{})}}),
			"outputSchema": schema, "permissions": raw("backend-owned-profile"),
		})})))
		msg, _ := parseObject(out[0].Message)
		p, _ := child(msg, "params")
		if out[0].Backend != b.ID || !bytes.Equal(p["outputSchema"], schema) || text(p, "permissions") != "backend-owned-profile" {
			t.Fatal("arbitrary nested JSON or native permissions were rewritten")
		}
		reply(t, r, out[0], `{"turn":{"id":"turn-`+b.ID+`","items":[],"status":"inProgress","error":null}}`)
		settings := object{"model": raw(model), "modelProvider": raw(b.Provider), "sandboxPolicy": policy}
		out = server(t, r, b.ID, string(raw(object{"method": raw("thread/settings/updated"), "params": raw(object{
			"threadId": raw(b.ID+"-thread"), "threadSettings": raw(settings),
		})})))
		msg, _ = parseObject(out[0].Message)
		p, _ = child(msg, "params")
		settings, _ = child(p, "threadSettings")
		if text(settings, "modelProvider") != controlPlaneProvider167 || !bytes.Equal(settings["sandboxPolicy"], policy) {
			t.Fatal("thread settings provider projection changed execution policy")
		}
	}
	expectError(t, client(t, r, `{"id":"cross","method":"turn/start","params":{"threadId":"openai-thread","model":"magpie/aihub/deepseek-flash-ft","input":[]}}`))
	for _, value := range []string{`"openai"`, `"aihub"`, `"foreign"`, `""`, `true`} {
		expectError(t, client(t, r, `{"id":"foreign","method":"thread/start","params":{"model":"magpie/openai/gpt","modelProvider":`+value+`}}`))
	}
}

func TestControlPlane167ThreadFilterEveryPage(t *testing.T) {
	r := controlPlaneRouter(t)
	out := client(t, r, `{"id":"threads","method":"thread/list","params":{"modelProviders":["magpie_bridge"],"limit":1}}`)
	check := func(d Delivery) {
		t.Helper()
		m, _ := parseObject(d.Message)
		p, _ := child(m, "params")
		if string(p["modelProviders"]) != string(raw([]string{r.byID[d.Backend].Provider})) {
			t.Fatal("logical filter reached a native page")
		}
	}
	for _, d := range out {
		check(d)
	}
	more := reply(t, r, out[0], `{"data":[],"nextCursor":"native-next"}`)
	check(more[0])
	reply(t, r, out[1], `{"data":[{"id":"b","modelProvider":"aihub","model":"deepseek-flash-ft","turns":[],"createdAt":1}],"nextCursor":null}`)
	done := reply(t, r, more[0], `{"data":[{"id":"a","modelProvider":"openai","model":"gpt","turns":[],"createdAt":2}],"nextCursor":null}`)
	if len(done) != 1 || strings.Contains(string(done[0].Message), `"modelProvider":"openai"`) || strings.Contains(string(done[0].Message), `"modelProvider":"aihub"`) {
		t.Fatal("native provider escaped merged frontend list")
	}
	for _, filter := range []string{`["openai"]`, `["magpie_bridge","magpie_bridge"]`, `[null]`, `"magpie_bridge"`, `{}`} {
		expectError(t, client(t, r, `{"id":"filter","method":"thread/list","params":{"modelProviders":`+filter+`}}`))
	}
	for _, filter := range []string{`[]`, `null`} {
		r := controlPlaneRouter(t)
		out := client(t, r, `{"id":"filter","method":"thread/list","params":{"modelProviders":`+filter+`}}`)
		for _, d := range out {
			m, _ := parseObject(d.Message)
			p, _ := child(m, "params")
			if string(p["modelProviders"]) != filter {
				t.Fatal("empty/null native filter semantics changed")
			}
		}
	}
}

func TestControlPlane167NativeProviderAndDuplicateChecks(t *testing.T) {
	for _, method := range []string{"thread/start", "thread/resume", "thread/fork", "thread/read", "thread/unarchive", "thread/revert", "thread/rollback"} {
		for _, body := range []string{
			`{"thread":{"id":"a","modelProvider":"aihub","model":"gpt","turns":[]},"modelProvider":"openai"}`,
			`{"thread":{"id":"a","modelProvider":null,"model":"gpt","turns":[]},"modelProvider":"openai"}`,
			`{"thread":{"id":"a","modelProvider":"aihub","modelProvider":"openai","model":"gpt","turns":[]},"modelProvider":"openai"}`,
			`{"thread":{},"thread":{"id":"a","modelProvider":"openai","model":"gpt","turns":[]},"modelProvider":"openai"}`,
		} {
			r := controlPlaneRouter(t)
			controlPlaneCatalog(t, r, true)
			r.threads["a"] = "openai"
			params := object{"threadId": raw("a")}
			if method == "thread/start" {
				params = object{"model": raw("magpie/openai/gpt")}
			}
			out := client(t, r, string(raw(object{"id": raw("owned"), "method": raw(method), "params": raw(params)})))
			msg, _ := parseObject(out[0].Message)
			response, err := r.Server("openai", []byte(`{"id":`+string(msg["id"])+`,"result":`+body+"}"))
			if err == nil || len(response) != 0 {
				t.Fatalf("malformed native Thread accepted on %s", method)
			}
		}
	}
	for _, bad := range []string{`null`, `"magpie_bridge"`, `"aihub"`} {
		r := controlPlaneRouter(t)
		controlPlaneCatalog(t, r, true)
		out := client(t, r, `{"id":"start","method":"thread/start","params":{"model":"magpie/openai/gpt"}}`)
		msg, _ := parseObject(out[0].Message)
		payload := `{"id":`+string(msg["id"])+`,"result":{"thread":{"id":"a","modelProvider":"openai","model":"gpt","turns":[]},"model":"gpt","modelProvider":`+bad+`}}`
		if result, err := r.Server("openai", []byte(payload)); err == nil || len(result) != 0 {
			t.Fatal("unexpected native top-level provider accepted")
		}
		r.threads["a"] = "openai"
		payload = `{"method":"thread/settings/updated","params":{"threadId":"a","threadSettings":{"model":"gpt","modelProvider":`+bad+`}}}`
		if result, err := r.Server("openai", []byte(payload)); err == nil || len(result) != 0 {
			t.Fatal("unexpected native settings provider accepted")
		}
	}
	r := controlPlaneRouter(t)
	controlPlaneCatalog(t, r, true)
	expectError(t, client(t, r, `{"id":1,"method":"thread/start","params":{"model":"magpie/openai/gpt","modelProvider":"openai","modelProvider":"magpie_bridge"}}`))
}

func TestControlPlane167IDsReadinessAndIndependentMode(t *testing.T) {
	for _, id := range []string{`""`, `"7"`, "7", "-0", "-9223372036854775808", "9223372036854775807"} {
		r := controlPlaneRouter(t)
		out := client(t, r, `{"id":`+id+`,"method":"config/read","params":{"includeLayers":false}}`)
		reply(t, r, out[0], controlPlaneNullRequirements)
		m, _ := parseObject(reply(t, r, out[1], controlPlaneNullRequirements)[0].Message)
		if string(m["id"]) != id {
			t.Fatal("original native request ID changed")
		}
	}
	for _, pair := range [][2]string{{"account/read", "config/read"}, {"config/read", "account/read"}} {
		r := controlPlaneRouter(t)
		params := func(method string) string {
			if method == "config/read" {
				return `{"includeLayers":false}`
			}
			return "{}"
		}
		client(t, r, fmt.Sprintf(`{"id":-0,"method":%q,"params":%s}`, pair[0], params(pair[0])))
		if out, err := r.Client([]byte(fmt.Sprintf(`{"id":0,"method":%q,"params":%s}`, pair[1], params(pair[1])))); err == nil || len(out) != 0 {
			t.Fatal("native integer ID aliases overlapped")
		}
	}
	for _, phase := range []string{"cold", "initializing", "failed"} {
		r := controlPlaneRouter(t)
		r.phase = phase
		expectError(t, client(t, r, `{"id":1,"method":"config/read","params":{"includeLayers":false}}`))
		if len(r.pending) != 0 {
			t.Fatal("routing read bypassed native readiness")
		}
	}
	r := testRouter(t, false)
	out := client(t, r, `{"id":1,"method":"config/read","params":{"includeLayers":false}}`)
	m, _ := parseObject(out[0].Message)
	failure, _ := child(m, "error")
	if string(failure["code"]) != "-32601" {
		t.Fatal("routing view is not default-off")
	}
	r = controlPlaneRouter(t)
	for _, method := range []string{"config/value/write", "config/batchWrite", "configRequirements/read", "getAuthStatus", "account/gatewayOAuth/read", "account/login/start", "fs/readFile", "project/list"} {
		expectError(t, client(t, r, fmt.Sprintf(`{"id":1,"method":%q,"params":{}}`, method)))
	}
	for _, capability := range []string{"requiredHumanApproval", "requestAttestation", "explicitGatewayOauth"} {
		a := (Config{ControlPlane: ControlPlaneRouting167}).authCompatibility()
		if a.negotiate(object{capability: raw(true)}) == nil {
			t.Fatal("routing-only mode enabled an unreviewed capability")
		}
	}
}

func TestControlPlane167ExactConfigAndVersionGates(t *testing.T) {
	base := fixtureConfig(t)
	base.ControlPlane = ControlPlaneRouting167
	base.ControlHome = t.TempDir()
	for i := range base.Backends {
		base.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	if err := base.Validate(); err != nil {
		t.Fatal("sole routing mode rejected", err)
	}
	for name, change := range map[string]func(*Config){
		"missing control home": func(c *Config) { c.ControlHome = "" },
		"relative control home": func(c *Config) { c.ControlHome = "relative-control" },
		"overlapping control home": func(c *Config) { c.ControlHome = c.Backends[1].CodexHome },
		"orphan control home": func(c *Config) { c.ControlPlane = "" },
		"partial": func(c *Config) { c.ControlPlane = "routing-v0.167" },
		"future": func(c *Config) { c.ControlPlane = "routing-v0.167.0-alpha.3.3" },
		"legacy requirements": func(c *Config) { c.RequirementsRead = ConfigRequirementsCompareSafe },
		"legacy startup": func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted },
		"legacy attestation": func(c *Config) { c.Attestation = AttestationUnavailable },
		"legacy gateway": func(c *Config) { c.GatewayOAuth = GatewayOAuthPrimaryRead; c.GatewayOAuthProviderName = "Fixture" },
		"first pin": func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		"second pin": func(c *Config) { c.Backends[1].StartupProbeSHA256 = "" },
		"reserved native provider": func(c *Config) { c.Backends[1].Provider = controlPlaneProvider167 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			change(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("invalid routing mode or version/pin/provider combination accepted")
			}
		})
	}
	base.RequirementsRead = ConfigRequirementsCompareNull167
	base.StartupProbe = StartupProbeCompareUnrestricted167
	base.Attestation = AttestationUnavailable167
	base.GatewayOAuth = GatewayOAuthPrimaryRead167
	base.GatewayOAuthProviderName = "Fixture"
	base.AuthStatus = AuthStatusPrimaryNoAuth167
	base.DesktopPluginOverride = DesktopPluginCodexAppTools167
	base.ExperimentalFeatureList = ExperimentalFeatureListCompare167
	if err := base.Validate(); err != nil {
		t.Fatal("compatible independently selected 0.167 modes rejected", err)
	}
}

func TestControlPlane167InitializeOwnsFrontendHome(t *testing.T) {
	newRouter := func() *Router {
		r, err := NewRouter([]Backend{
			{ID: "openai", Provider: "openai", CodexHome: t.TempDir()},
			{ID: "aihub", Provider: "aihub", CodexHome: t.TempDir()},
		})
		if err != nil {
			t.Fatal(err)
		}
		r.controlPlaneRouting167 = true
		r.controlHome167 = t.TempDir()
		return r
	}
	native := func(r *Router, backend string) object {
		return object{"userAgent": raw("official-fixture"), "codexHome": raw(r.byID[backend].CodexHome),
			"platformFamily": raw("fixture"), "platformOs": raw("fixture")}
	}
	r := newRouter()
	out := client(t, r, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`)
	if len(reply(t, r, out[0], string(raw(native(r, out[0].Backend))))) != 0 || r.phase == "ready" {
		t.Fatal("control frontend initialized before both native homes were checked")
	}
	done := reply(t, r, out[1], string(raw(native(r, out[1].Backend))))
	msg, _ := parseObject(done[0].Message)
	value, _ := child(msg, "result")
	if len(value) != 4 || text(value, "codexHome") != r.controlHome167 || r.phase != "ready" ||
		text(value, "userAgent") != "official-fixture" {
		t.Fatal("frontend initialize did not use its genuine control home")
	}
	for _, b := range r.backends {
		if strings.Contains(string(done[0].Message), b.CodexHome) {
			t.Fatal("native private config home leaked into frontend initialize")
		}
	}
	for _, change := range []func(object){
		func(v object) { v["codexHome"] = raw("PRIVATE_FOREIGN_PATH") },
		func(v object) { v["codexHome"] = raw(nil) },
		func(v object) { v["platformOs"] = raw("different") },
		func(v object) { v["platformFamily"] = raw("different") },
		func(v object) { v["userAgent"] = raw("different") },
		func(v object) { v["unknown"] = raw("PRIVATE_EXTRA") },
		func(v object) { delete(v, "platformOs") },
	} {
		r := newRouter()
		out := client(t, r, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`)
		reply(t, r, out[0], string(raw(native(r, out[0].Backend))))
		other := native(r, out[1].Backend)
		change(other)
		done := reply(t, r, out[1], string(raw(other)))
		expectError(t, done)
		if r.phase != "failed" || strings.Contains(string(done[0].Message), "PRIVATE_") {
			t.Fatal("incoherent native initialize became ready or disclosed data")
		}
	}
}

func TestControlPlane167ControlHomeIsExplicitAndIsolated(t *testing.T) {
	cfg := fixtureConfig(t)
	cfg.ControlPlane = ControlPlaneRouting167
	cfg.ControlHome = t.TempDir()
	for i := range cfg.Backends {
		cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	check := func(path string) {
		t.Helper()
		c := cfg
		c.ControlHome = path
		if c.Validate() == nil {
			t.Fatal("control home overlapped native state or was not a directory")
		}
	}
	for _, b := range cfg.Backends {
		check(b.Home)
		check(b.CodexHome)
		check(filepath.Dir(b.Home))
		nested := filepath.Join(b.Home, "control")
		if err := os.Mkdir(nested, 0700); err != nil {
			t.Fatal(err)
		}
		check(nested)
	}
	check(filepath.Join(cfg.ControlHome, "missing"))
	file := filepath.Join(cfg.ControlHome, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	check(file)
	link := filepath.Join(t.TempDir(), "native-alias")
	if err := os.Symlink(cfg.Backends[0].CodexHome, link); err == nil {
		check(link)
	}
	r := &Router{byID: map[string]Backend{"native": {CodexHome: cfg.Backends[0].CodexHome}},
		controlNativeHomes167: map[string]string{"native": cfg.Backends[1].CodexHome}}
	if !r.controlPlaneNativeHome167("native", raw(cfg.Backends[1].CodexHome)) ||
		r.controlPlaneNativeHome167("native", raw(cfg.ControlHome)) {
		t.Fatal("configured canonical home matching changed authority")
	}
	if runtime.GOOS == "windows" {
		for _, pair := range [][2]string{
			{`C:\Fixture\Codex`, `\\?\c:\fixture\codex`},
			{`\\server\share\Codex`, `\\?\UNC\SERVER\SHARE\codex`},
		} {
			if controlPlanePath167(pair[0]) != controlPlanePath167(pair[1]) {
				t.Fatal("native Windows canonical home spelling rejected")
			}
		}
		if controlPlanePath167(`\\?\GLOBALROOT\device\harddiskvolume1\Codex`) == controlPlanePath167(`C:\Codex`) {
			t.Fatal("device namespace was mistaken for a configured directory")
		}
	}
}
