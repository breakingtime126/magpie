package appserverbridge

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func ownedRefreshEnableReads(r *Router) {
	r.auth.authStatusNoAuth = true
	r.experimentalFeatureList167 = true
	r.configRequirementsRead = true
	r.configRequirementsNull167 = true
}

func ownedRefreshReadResult(method string) json.RawMessage {
	switch method {
	case "getAuthStatus":
		return json.RawMessage(`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}`)
	case "experimentalFeature/list":
		return json.RawMessage(`{"data":[],"nextCursor":null}`)
	default:
		return json.RawMessage(`{"requirements":null,"supportsIndependentSpeedModes":true}`)
	}
}

func ownedRefreshReadReply(t *testing.T, r *Router, requests []Delivery, result json.RawMessage) []Delivery {
	t.Helper()
	var replies []Delivery
	for _, d := range requests {
		m, _ := parseObject(d.Message)
		out, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": result}))
		if err != nil {
			t.Fatal(err)
		}
		replies = append(replies, out...)
	}
	return replies
}

func TestOwnedCatalogIndependentReadsRemainAvailableDuringRefresh(t *testing.T) {
	for _, method := range []string{"experimentalFeature/list", "configRequirements/read"} {
		t.Run(method, func(t *testing.T) {
			r, _, refresh := ownedAuthRefreshStart(t)
			ownedRefreshEnableReads(r)
			params := json.RawMessage(`{}`)
			if method == "experimentalFeature/list" {
				params = json.RawMessage(`{"cursor":null,"limit":0,"threadId":null}`)
			}
			out, err := r.Client(raw(object{"id": raw(2), "method": raw(method), "params": params}))
			if err != nil || len(out) != 2 {
				t.Fatal("enabled catalog-independent read must reach both children during refresh")
			}
			for _, d := range out {
				m, _ := parseObject(d.Message)
				expected := params
				if method == "configRequirements/read" {
					expected = raw(nil)
				}
				if text(m, "method") != method || !sameJSON(m["params"], expected) {
					t.Fatal("existing native method or params changed")
				}
			}
			result := ownedRefreshReadResult(method)
			replies := ownedRefreshReadReply(t, r, out, result)
			if len(replies) != 1 || replies[0].Backend != "" {
				t.Fatal("paired native result was not returned")
			}
			m, _ := parseObject(replies[0].Message)
			if !sameJSON(m["result"], result) || !r.ownedCatalog.refreshing || len(r.models) != 0 || r.controlSession167 != nil {
				t.Fatal("independent read changed the catalog publication barrier")
			}
			native := ownedTestNative(t)
			for _, d := range refresh {
				if _, err := ownedTestReply(t, r, d, native[d.Backend], nil); err != nil {
					t.Fatal(err)
				}
			}
			if r.ownedCatalog.refreshing || len(r.models) != 2 || r.controlSession167 == nil {
				t.Fatal("independent read disturbed atomic catalog completion")
			}
		})
	}
}

func TestOwnedCatalogFeaturePagesAndGroupLimitStayNativeDuringRefresh(t *testing.T) {
	r, _, _ := ownedAuthRefreshStart(t)
	ownedRefreshEnableReads(r)
	for page := 0; page < 2; page++ {
		params := json.RawMessage(`{"cursor":null,"limit":1,"threadId":null}`)
		result := json.RawMessage(`{"data":[{"name":"local_test_feature","stage":"stable","displayName":null,"description":null,"announcement":null,"enabled":false,"defaultEnabled":false}],"nextCursor":"1"}`)
		if page == 1 {
			params = json.RawMessage(`{"cursor":"1","limit":1,"threadId":null}`)
			result = ownedRefreshReadResult("experimentalFeature/list")
		}
		out, err := r.Client(raw(object{"id": raw(10 + page), "method": raw("experimentalFeature/list"), "params": params}))
		if err != nil || len(out) != 2 {
			t.Fatal("native feature continuation blocked by unrelated catalog refresh")
		}
		for _, d := range out {
			m, _ := parseObject(d.Message)
			if !sameJSON(m["params"], params) {
				t.Fatal("native feature cursor or limit was rewritten")
			}
		}
		replies := ownedRefreshReadReply(t, r, out, result)
		if len(replies) != 1 {
			t.Fatal("native feature page unavailable")
		}
		m, _ := parseObject(replies[0].Message)
		if !sameJSON(m["result"], result) || len(r.pages) != 0 {
			t.Fatal("feature page became a bridge snapshot")
		}
	}
	for id := 20; id < 24; id++ {
		out, err := r.Client(raw(object{"id": raw(id), "method": raw("experimentalFeature/list"), "params": raw(object{})}))
		if err != nil || len(out) != 2 {
			t.Fatal("feature logical group was not admitted within its limit")
		}
		if got := ownedRefreshReadReply(t, r, out[:1], ownedRefreshReadResult("experimentalFeature/list")); len(got) != 0 {
			t.Fatal("half of a feature pair released its logical slot")
		}
	}
	out, err := r.Client([]byte(`{"id":24,"method":"experimentalFeature/list","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "" {
		t.Fatal("feature logical group cap bypassed during refresh")
	}
	m, _ := parseObject(out[0].Message)
	failure, _ := child(m, "error")
	if text(failure, "message") != "experimental feature list request limit reached" {
		t.Fatal("feature limit was replaced by the catalog barrier")
	}
}

func TestOwnedCatalogIndependentReadAdmissionRemainsNarrow(t *testing.T) {
	for _, method := range []string{"getAuthStatus", "experimentalFeature/list", "configRequirements/read"} {
		for _, gate := range []string{"mode-off", "params", "phase", "count", "bytes"} {
			t.Run(method+"/"+gate, func(t *testing.T) {
				r, _, _ := ownedAuthRefreshStart(t)
				ownedRefreshEnableReads(r)
				params := raw(object{})
				code := -32000
				switch gate {
				case "mode-off":
					r.auth.authStatusNoAuth = false
					r.experimentalFeatureList167 = false
					r.configRequirementsRead = false
				case "params":
					params = json.RawMessage(`{"unreviewed":true}`)
					code = -32602
				case "phase":
					r.phase = "initializing"
					code = -32600
				case "count":
					for i := 0; i < 128; i++ {
						r.active["inert-limit-"+strconv.Itoa(i)] = true
					}
				case "bytes":
					r.activeBytes = 32 << 20
				}
				before := len(r.pending)
				out, err := r.Client(raw(object{"id": raw(2), "method": raw(method), "params": params}))
				if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.pending) != before {
					t.Fatal("independent read bypassed an existing admission guard")
				}
				m, _ := parseObject(out[0].Message)
				failure, _ := child(m, "error")
				if !sameJSON(failure["code"], raw(code)) {
					t.Fatal("independent read changed its admission failure")
				}
			})
		}
	}
	// config/read is catalog-dependent and waits with its own policy pair;
	// TestOwnedConfigReadWaitBeforeAndDuringRefresh verifies that distinction.
	for _, method := range []string{"model/list", "thread/start", "turn/start", "account/read", "account/gatewayOAuth/read", "windowsSandbox/readiness", "environment/info", "thread/read", "experimentalFeature/enablement/set", "unreviewed/read"} {
		r, _, _ := ownedAuthRefreshStart(t)
		ownedRefreshEnableReads(r)
		before := len(r.pending)
		out, err := r.Client(raw(object{"id": raw(2), "method": raw(method), "params": raw(object{})}))
		if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.pending) != before {
			t.Fatal("unlisted method escaped the catalog refresh barrier")
		}
		m, _ := parseObject(out[0].Message)
		failure, _ := child(m, "error")
		if !sameJSON(failure["code"], raw(-32000)) {
			t.Fatal("unlisted method did not retain the catalog refresh barrier")
		}
	}
}

func TestOwnedCatalogIndependentReadsDoNotSynthesizeOrSurviveFailure(t *testing.T) {
	for _, method := range []string{"experimentalFeature/list", "configRequirements/read"} {
		t.Run(method+"/unequal", func(t *testing.T) {
			r, _, _ := ownedAuthRefreshStart(t)
			ownedRefreshEnableReads(r)
			out, err := r.Client(raw(object{"id": raw(2), "method": raw(method), "params": raw(object{})}))
			if err != nil || len(out) != 2 {
				t.Fatal("independent read unavailable")
			}
			if got := ownedRefreshReadReply(t, r, out[:1], ownedRefreshReadResult(method)); len(got) != 0 {
				t.Fatal("one native result became shared success")
			}
			bad := json.RawMessage(`{"data":[{"name":"different_local_feature","stage":"stable","displayName":null,"description":null,"announcement":null,"enabled":true,"defaultEnabled":false}],"nextCursor":null}`)
			if method == "configRequirements/read" {
				bad = json.RawMessage(`{"requirements":{},"supportsIndependentSpeedModes":true}`)
			}
			replies := ownedRefreshReadReply(t, r, out[1:], bad)
			if len(replies) != 1 || !r.ownedCatalog.refreshing || r.ownedCatalog.failed {
				t.Fatal("native disagreement changed catalog state")
			}
			m, _ := parseObject(replies[0].Message)
			if !exists(m, "error") || exists(m, "result") {
				t.Fatal("native disagreement became synthetic success")
			}
		})
		for _, failure := range []string{"catalog", "file"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				r, path, refresh := ownedAuthRefreshStart(t)
				ownedRefreshEnableReads(r)
				out, err := r.Client(raw(object{"id": raw(2), "method": raw(method), "params": raw(object{})}))
				if err != nil || len(out) != 2 {
					t.Fatal("independent read unavailable")
				}
				if failure == "file" {
					if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					m, _ := parseObject(refresh[0].Message)
					got, err := r.Server(refresh[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("local fixture failure")})}))
					if err == nil || len(got) != 0 {
						t.Fatal("catalog failure did not close the owned connection")
					}
				}
				m, _ := parseObject(out[0].Message)
				got, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "result": ownedRefreshReadResult(method)}))
				if err == nil || len(got) != 0 || !r.ownedCatalog.failed {
					t.Fatal("late independent result survived catalog failure")
				}
				got, err = r.Client(raw(object{"id": raw(3), "method": raw(method), "params": raw(object{})}))
				if err == nil || len(got) != 0 || len(r.models) != 0 || r.controlSession167 != nil {
					t.Fatal("failed owned catalog reopened through an independent read")
				}
			})
		}
	}
}
