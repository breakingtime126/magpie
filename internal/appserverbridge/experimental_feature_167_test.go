package appserverbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const experimentalFeatureTestRow = `{"name":"network_proxy","stage":"beta","displayName":"Network proxy","description":"Apply network restrictions.","announcement":"","enabled":false,"defaultEnabled":false}`
const experimentalFeatureTestPage = `{"data":[` + experimentalFeatureTestRow + `],"nextCursor":"1"}`
const experimentalFeatureTestLast = `{"data":[{"name":"shell_tool","stage":"stable","displayName":null,"description":null,"announcement":null,"enabled":true,"defaultEnabled":true}],"nextCursor":null}`

func experimentalFeatureRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.experimentalFeatureList167 = true
	r.auth = (Config{ExperimentalFeatureList: ExperimentalFeatureListCompare167}).authCompatibility()
	return r
}

func experimentalFeatureAsk(t *testing.T, r *Router, params string) []Delivery {
	t.Helper()
	out := client(t, r, `{"id":9007199254740993,"method":"experimentalFeature/list","params":`+params+`,"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
	if len(out) != 2 || out[0].Backend != "openai" || out[1].Backend != "aihub" {
		t.Fatalf("native feature page did not reach exactly two children: %v", out)
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if len(m) != 3 || string(m["params"]) != authStatusTestCompact(t, params) || strings.Contains(string(d.Message), "PRIVATE_") {
			t.Fatal("native params changed or trace was forwarded")
		}
	}
	return out
}

func experimentalFeatureError(t *testing.T, out []Delivery) {
	t.Helper()
	expectError(t, out)
	m, _ := parseObject(out[0].Message)
	failure, _ := child(m, "error")
	if len(m) != 2 || len(failure) != 2 || string(failure["code"]) != "-32000" ||
		text(failure, "message") != experimentalFeatureUnavailable || strings.Contains(string(out[0].Message), "PRIVATE_") {
		t.Fatal("feature failure exposed backend content or changed shape")
	}
}

func TestExperimentalFeature167PreservesNativePagesAndFreshReads(t *testing.T) {
	for _, params := range []string{
		`{}`, `{"cursor":null,"limit":null,"threadId":null}`,
		`{"limit":0}`, `{"limit":1}`, `{"limit":4294967295}`,
		`{ "cu\u0072sor" : "1", "limit" : 17 }`,
		`{"cursor":"0001","limit":2}`,
	} {
		r := experimentalFeatureRouter(t)
		var previous string
		for round := 0; round < 2; round++ {
			out := experimentalFeatureAsk(t, r, params)
			m, _ := parseObject(out[0].Message)
			if string(m["id"]) == previous {
				t.Fatal("feature read reused transport request")
			}
			previous = string(m["id"])
			source := `{ "data": [` + strings.Replace(experimentalFeatureTestRow, `"enabled"`, `"enab\u006ced"`, 1) + `], "nextCursor": "1" }`
			if result := reply(t, r, out[1], experimentalFeatureTestPage); len(result) != 0 {
				t.Fatal("feature page succeeded before both replies")
			}
			result := reply(t, r, out[0], source)
			if len(result) != 1 || result[0].Backend != "" {
				t.Fatal("equal complete native pages did not succeed")
			}
			m, _ = parseObject(result[0].Message)
			if string(m["id"]) != "9007199254740993" || string(m["result"]) != authStatusTestCompact(t, source) {
				t.Fatal("genuine source bytes or client ID were reconstructed")
			}
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.pages) != 0 {
				t.Fatal("native page retained request state or created a bridge snapshot")
			}
		}
	}
	r := experimentalFeatureRouter(t)
	for _, tc := range []struct{ params, page string }{
		{`{"limit":1}`, experimentalFeatureTestPage},
		{`{"cursor":"1","limit":0,"threadId":null}`, experimentalFeatureTestLast},
		{`{"cursor":"2","limit":4294967295}`, `{"data":[],"nextCursor":null}`},
	} {
		out := experimentalFeatureAsk(t, r, tc.params)
		reply(t, r, out[0], tc.page)
		result := reply(t, r, out[1], tc.page)
		m, _ := parseObject(result[0].Message)
		if string(m["result"]) != tc.page {
			t.Fatal("native pagination was merged or replaced")
		}
	}
}

func TestExperimentalFeature167StrictRequests(t *testing.T) {
	base := `{"id":1,"method":"experimentalFeature/list","params":{}}`
	cases := []string{
		strings.Replace(base, `,"params":{}`, "", 1),
		strings.Replace(base, `"params":{}`, `"params":null`, 1),
		strings.Replace(base, `"params":{}`, `"params":[]`, 1),
		strings.Replace(base, `"id":1`, `"id":1,"\u0069d":2`, 1),
		strings.Replace(base, `"method":"experimentalFeature/list"`, `"method":"experimentalFeature/list","method":"account/read"`, 1),
		strings.Replace(base, `"method":"experimentalFeature/list"`, `"method":"account/read","method":"experimentalFeature/list"`, 1),
		strings.Replace(base, `"params":{}`, `"params":{},"params":{}`, 1),
		base + "{}",
	}
	for _, fields := range []string{
		`"threadId":"PRIVATE_THREAD"`, `"threadId":""`, `"threadId":false`, `"threadId":[]`,
		`"limit":-1`, `"limit":4294967296`, `"limit":1.0`, `"limit":1e0`, `"limit":"1"`, `"limit":true`,
		`"cursor":1`, `"cursor":false`, `"cursor":[]`, `"cursor":"\ud800"`, `"cursor":"\u0000"`,
		`"limit":1,"li\u006dit":2`, `"unknown":"PRIVATE_PARAM"`,
		`"cursor":` + string(raw(strings.Repeat("a", 257))),
	} {
		cases = append(cases, strings.Replace(base, `"params":{}`, `"params":{`+fields+"}", 1))
	}
	for _, fields := range []string{
		`"jsonrpc":"2.0"`, `"result":null`, `"error":null`, `"unknown":"PRIVATE_FIELD"`,
		`"trace":true`, `"trace":{"traceparent":1}`, `"trace":{"unknown":"PRIVATE_TRACE"}`,
		`"trace":null,"tr\u0061ce":null`, `"trace":{"traceparent":"\ud800"}`,
	} {
		cases = append(cases, base[:len(base)-1]+","+fields+"}")
	}
	for _, id := range []string{"null", "false", "1.5", "1e0", "9223372036854775808", "-9223372036854775809", `"\ud800"`, string(raw(strings.Repeat("a", 257)))} {
		cases = append(cases, strings.Replace(base, `"id":1`, `"id":`+id, 1))
	}
	for i, line := range cases {
		r := experimentalFeatureRouter(t)
		if validateExperimentalFeatureRequest167([]byte(line)) {
			t.Fatalf("malformed request %d accepted by validator", i)
		}
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
			t.Fatal("malformed request retained active state")
		}
	}
}

func TestExperimentalFeature167CompleteComparisonAndCleanup(t *testing.T) {
	var altered []string
	for key, value := range map[string]json.RawMessage{
		"name":           raw("other"),
		"stage":          raw("underDevelopment"),
		"displayName":    raw("Other"),
		"description":    raw("Other"),
		"announcement":   raw("Other"),
		"enabled":        raw(true),
		"defaultEnabled": raw(true),
	} {
		row, _ := parseObject([]byte(experimentalFeatureTestRow))
		row[key] = value
		altered = append(altered, string(raw(object{"data": raw([]object{row}), "nextCursor": raw("1")})))
	}
	altered = append(altered,
		strings.Replace(experimentalFeatureTestPage, `"nextCursor":"1"`, `"nextCursor":"2"`, 1),
		`{"data":[],"nextCursor":null}`,
	)
	// Same complete rows in the opposite order must not compare equal.
	first, _ := parseObject([]byte(experimentalFeatureTestPage))
	last, _ := parseObject([]byte(experimentalFeatureTestLast))
	var a, b []json.RawMessage
	json.Unmarshal(first["data"], &a)
	json.Unmarshal(last["data"], &b)
	rOrder := experimentalFeatureRouter(t)
	orderRequests := experimentalFeatureAsk(t, rOrder, "{}")
	reply(t, rOrder, orderRequests[0], string(raw(object{"data": raw([]json.RawMessage{a[0], b[0]}), "nextCursor": raw(nil)})))
	experimentalFeatureError(t, reply(t, rOrder, orderRequests[1], string(raw(object{"data": raw([]json.RawMessage{b[0], a[0]}), "nextCursor": raw(nil)}))))
	rStage := experimentalFeatureRouter(t)
	stageRequests := experimentalFeatureAsk(t, rStage, "{}")
	reply(t, rStage, stageRequests[0], experimentalFeatureTestLast)
	experimentalFeatureError(t, reply(t, rStage, stageRequests[1], strings.Replace(experimentalFeatureTestLast, `"stage":"stable"`, `"stage":"deprecated"`, 1)))
	for _, changed := range altered {
		for _, order := range []int{0, 1} {
			r := experimentalFeatureRouter(t)
			out := experimentalFeatureAsk(t, r, `{"limit":1}`)
			reply(t, r, out[order], experimentalFeatureTestPage)
			experimentalFeatureError(t, reply(t, r, out[1-order], changed))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("disagreement retained pending state")
			}
		}
	}
	r := experimentalFeatureRouter(t)
	for i := 0; i < 300; i++ {
		out := experimentalFeatureAsk(t, r, "{}")
		m, _ := parseObject(out[0].Message)
		g := r.pending["s:"+text(m, "id")].group
		failure := object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE_ERROR"), "data": raw("PRIVATE_DATA")})}
		experimentalFeatureError(t, server(t, r, out[0].Backend, string(raw(failure))))
		if g.params != nil || g.results != nil || g.seen != nil || !g.failed || len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("failed fanout retained payload")
		}
		if i == 0 && len(reply(t, r, out[1], experimentalFeatureTestPage)) != 0 {
			t.Fatal("late sibling became success after failure")
		}
	}
	if len(r.staleResponses) > 256 || len(r.staleResponseOrder) > 256 {
		t.Fatal("late-response tombstones are unbounded")
	}
	// Reject overlap, but an abandoned read must not prevent a fresh recovery.
	out := experimentalFeatureAsk(t, r, "{}")
	expectError(t, client(t, r, `{"id":"overlap","method":"experimentalFeature/list","params":{}}`))
	reply(t, r, out[0], experimentalFeatureTestLast)
	result := reply(t, r, out[1], experimentalFeatureTestLast)
	if !strings.Contains(string(result[0].Message), `"result"`) {
		t.Fatal("fresh recovery did not succeed")
	}
}

func TestExperimentalFeature167RejectsMalformedResponses(t *testing.T) {
	cases := []string{"null", "[]", "{}", `{"data":null,"nextCursor":null}`, `{"data":[]}`,
		`{"data":[],"nextCursor":""}`, `{"data":[],"nextCursor":"1"}`, `{"data":[],"nextCursor":null,"unknown":"PRIVATE_EXTRA"}`,
		strings.Replace(experimentalFeatureTestPage, `"enabled":false`, `"enabled":null`, 1),
		strings.Replace(experimentalFeatureTestPage, `"defaultEnabled":false`, `"defaultEnabled":0`, 1),
		strings.Replace(experimentalFeatureTestPage, `"stage":"beta"`, `"stage":"future"`, 1),
		strings.Replace(experimentalFeatureTestPage, `"announcement":""`, `"announcement":null`, 1),
		strings.Replace(experimentalFeatureTestPage, `"description":"Apply network restrictions."`, `"description":"\ud800"`, 1),
		strings.Replace(experimentalFeatureTestPage, `"enabled":false`, `"enabled":true,"enab\u006ced":false`, 1),
		strings.Replace(experimentalFeatureTestPage, `"name":"network_proxy"`, `"name":"network_proxy","unknown":"PRIVATE_ROW"`, 1),
		`{"data":[` + experimentalFeatureTestRow + "," + experimentalFeatureTestRow + `],"nextCursor":null}`,
	}
	for _, field := range []string{"name", "stage", "displayName", "description", "announcement", "enabled", "defaultEnabled"} {
		row, _ := parseObject([]byte(experimentalFeatureTestRow))
		delete(row, field)
		cases = append(cases, string(raw(object{"data": raw([]object{row}), "nextCursor": raw(nil)})))
	}
	for _, result := range cases {
		r := experimentalFeatureRouter(t)
		out := experimentalFeatureAsk(t, r, "{}")
		experimentalFeatureError(t, reply(t, r, out[0], result))
		if len(reply(t, r, out[1], experimentalFeatureTestPage)) != 0 {
			t.Fatal("malformed result allowed late success")
		}
	}
	for _, kind := range []string{"duplicate id", "duplicate result", "error and result", "method", "unknown", "wrong backend"} {
		r := experimentalFeatureRouter(t)
		out := experimentalFeatureAsk(t, r, "{}")
		m, _ := parseObject(out[0].Message)
		line := `{"id":` + string(m["id"]) + `,"result":` + experimentalFeatureTestPage
		switch kind {
		case "duplicate id":
			line += `,"\u0069d":` + string(m["id"])
		case "duplicate result":
			line += `,"result":` + experimentalFeatureTestPage
		case "error and result":
			line += `,"error":null`
		case "method":
			line += `,"method":"account/updated","params":{}`
		case "unknown":
			line += `,"unknown":"PRIVATE_ENVELOPE"`
		}
		backend := out[0].Backend
		if kind == "wrong backend" {
			backend = out[1].Backend
		}
		response, err := r.Server(backend, []byte(line+"}"))
		if err == nil {
			experimentalFeatureError(t, response)
		} else if len(response) != 0 || strings.Contains(err.Error(), "PRIVATE_") {
			t.Fatal("fatal envelope exposed data")
		}
	}
}

func TestExperimentalFeature167CatalogAndBounds(t *testing.T) {
	// Reconstructed from the pinned FEATURES registry for a Windows platform,
	// with enabled equal to its default; not an observed runtime policy.
	data, err := os.ReadFile("testdata/codex-0.167.0-alpha.3.2-feature-source.json")
	if err != nil || !validateExperimentalFeaturePage167(data) {
		t.Fatal("complete pinned 201-row source catalog rejected", err)
	}
	r := experimentalFeatureRouter(t)
	out := experimentalFeatureAsk(t, r, `{"limit":4294967295}`)
	reply(t, r, out[0], string(data))
	response := reply(t, r, out[1], string(data))
	if len(response) != 1 || !strings.Contains(string(response[0].Message), `"result"`) {
		t.Fatal("complete pinned catalog did not survive full comparison")
	}
	page, _ := parseObject([]byte(experimentalFeatureTestPage))
	row, _ := parseObject([]byte(experimentalFeatureTestRow))
	for _, n := range []int{4096, 4097} {
		row["description"] = raw(strings.Repeat("x", n))
		page["data"] = raw([]object{row})
		if validateExperimentalFeaturePage167(raw(page)) != (n == 4096) {
			t.Fatal("metadata string bound changed")
		}
	}
	for _, n := range []int{256, 257} {
		var rows []object
		for i := 0; i < n; i++ {
			row, _ := parseObject([]byte(experimentalFeatureTestRow))
			row["name"] = raw(fmt.Sprintf("feature_%d", i))
			rows = append(rows, row)
		}
		page["data"] = raw(rows)
		if validateExperimentalFeaturePage167(raw(page)) != (n == 256) {
			t.Fatal("feature row bound changed")
		}
	}
	at := experimentalFeatureTestPage + strings.Repeat(" ", experimentalFeatureJSONLimit-len(experimentalFeatureTestPage))
	if !validateExperimentalFeaturePage167([]byte(at)) || validateExperimentalFeaturePage167([]byte(at+" ")) {
		t.Fatal("feature byte boundary changed")
	}
	// Route a complete page larger than policy's 64 KiB reader budget.
	var largeRows []object
	for i := 0; i < 20; i++ {
		row, _ := parseObject([]byte(experimentalFeatureTestRow))
		row["name"] = raw(fmt.Sprintf("feature_%d", i))
		row["description"] = raw(strings.Repeat("m", 4000))
		largeRows = append(largeRows, row)
	}
	large := raw(object{"data": raw(largeRows), "nextCursor": raw(nil)})
	if len(large) <= 64<<10 || !validateExperimentalFeaturePage167(large) {
		t.Fatal("large bounded page rejected by a smaller policy reader")
	}
	r = experimentalFeatureRouter(t)
	out = experimentalFeatureAsk(t, r, "{}")
	reply(t, r, out[0], string(large))
	if result := reply(t, r, out[1], string(large)); len(result) != 1 || !strings.Contains(string(result[0].Message), `"result"`) {
		t.Fatal("large bounded page failed routed comparison")
	}
	for _, extra := range []int{0, 1} {
		r = experimentalFeatureRouter(t)
		out = experimentalFeatureAsk(t, r, "{}")
		msg, _ := parseObject(out[0].Message)
		envelope := string(raw(object{"id": msg["id"], "result": json.RawMessage(experimentalFeatureTestPage)}))
		envelope += strings.Repeat(" ", experimentalFeatureJSONLimit-len(envelope)+extra)
		response := server(t, r, out[0].Backend, envelope)
		if extra == 1 {
			experimentalFeatureError(t, response)
			reply(t, r, out[1], experimentalFeatureTestPage)
		} else {
			if len(response) != 0 {
				t.Fatal("envelope at its byte cap rejected")
			}
			result := reply(t, r, out[1], experimentalFeatureTestPage)
			if !strings.Contains(string(result[0].Message), `"result"`) {
				t.Fatal("envelope at its byte cap did not complete")
			}
		}
	}
	row, _ = parseObject([]byte(experimentalFeatureTestRow))
	row["announcement"] = raw("line\n\t�")
	page["data"] = raw([]object{row})
	if !validateExperimentalFeaturePage167(raw(page)) {
		t.Fatal("valid native metadata was forced through the policy text profile")
	}
}

func TestExperimentalFeature167IDsReadinessAndIndependentMode(t *testing.T) {
	for _, id := range []string{`""`, `"7"`, "7", "-0", "-9223372036854775808", "9223372036854775807"} {
		r := experimentalFeatureRouter(t)
		out := client(t, r, `{"id":`+id+`,"method":"experimentalFeature/list","params":{},"trace":null}`)
		if len(out) != 2 {
			t.Fatal("native ID rejected")
		}
		reply(t, r, out[0], experimentalFeatureTestPage)
		m, _ := parseObject(reply(t, r, out[1], experimentalFeatureTestPage)[0].Message)
		if string(m["id"]) != id {
			t.Fatal("native ID bytes changed")
		}
	}
	for _, pair := range [][2]string{{"account/read", "experimentalFeature/list"}, {"experimentalFeature/list", "account/read"}} {
		r := experimentalFeatureRouter(t)
		client(t, r, fmt.Sprintf(`{"id":-0,"method":%q,"params":{}}`, pair[0]))
		if out, err := r.Client([]byte(fmt.Sprintf(`{"id":0,"method":%q,"params":{}}`, pair[1]))); err == nil || len(out) != 0 {
			t.Fatal("native zero aliases overlapped")
		}
	}
	for _, phase := range []string{"cold", "initializing", "failed"} {
		r := experimentalFeatureRouter(t)
		r.phase = phase
		expectError(t, client(t, r, `{"id":1,"method":"experimentalFeature/list","params":{}}`))
		if len(r.pending) != 0 {
			t.Fatal("feature read bypassed readiness")
		}
	}
	r := testRouter(t, false)
	out := client(t, r, `{"id":1,"method":"experimentalFeature/list","params":{"limit":1}}`)
	m, _ := parseObject(out[0].Message)
	failure, _ := child(m, "error")
	if string(failure["code"]) != "-32601" {
		t.Fatal("default-off route changed")
	}
	r = experimentalFeatureRouter(t)
	for _, method := range []string{"experimentalFeature/enablement/set", "config/read", "configRequirements/read", "getAuthStatus", "account/gatewayOAuth/read", "account/login/start"} {
		expectError(t, client(t, r, fmt.Sprintf(`{"id":1,"method":%q,"params":{}}`, method)))
	}
	for _, capability := range []string{"requiredHumanApproval", "requestAttestation", "explicitGatewayOauth"} {
		a := (Config{ExperimentalFeatureList: ExperimentalFeatureListCompare167}).authCompatibility()
		if a.negotiate(object{capability: raw(true)}) == nil {
			t.Fatal("feature-only mode enabled unreviewed capability")
		}
	}
	caps := object{"requiredHumanApproval": raw(false), "experimentalApi": raw(true), "optOutNotificationMethods": raw([]string{"fixture/notification"})}
	before := raw(caps)
	a := (Config{ExperimentalFeatureList: ExperimentalFeatureListCompare167}).authCompatibility()
	if a.negotiate(caps) != nil || !bytes.Equal(before, raw(caps)) {
		t.Fatal("feature mode rewrote capabilities")
	}
}

func TestExperimentalFeature167ExactConfigAndVersionGates(t *testing.T) {
	base := fixtureConfig(t)
	base.ExperimentalFeatureList = ExperimentalFeatureListCompare167
	for i := range base.Backends {
		base.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	if err := base.Validate(); err != nil {
		t.Fatal("feature-only mode rejected", err)
	}
	for name, change := range map[string]func(*Config){
		"partial":             func(c *Config) { c.ExperimentalFeatureList = "compare-v0.167" },
		"future":              func(c *Config) { c.ExperimentalFeatureList = "compare-v0.167.0-alpha.3.3" },
		"legacy requirements": func(c *Config) { c.RequirementsRead = ConfigRequirementsCompareSafe },
		"legacy startup":      func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted },
		"legacy attestation":  func(c *Config) { c.Attestation = AttestationUnavailable },
		"legacy gateway": func(c *Config) {
			c.GatewayOAuth = GatewayOAuthPrimaryRead
			c.GatewayOAuthProviderName = "Fixture"
		},
		"first pin":  func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		"second pin": func(c *Config) { c.Backends[1].StartupProbeSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			change(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("invalid feature mode or version/pin combination accepted")
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
	if err := base.Validate(); err != nil {
		t.Fatal("same-version independent modes rejected", err)
	}
}
