package appserverbridge

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

func ownedAuthRefreshStart(t *testing.T) (*Router, string, []Delivery) {
	t.Helper()
	r, path, _ := ownedTestSetup(t)
	r.auth.authStatusNoAuth = true
	out, err := r.Client([]byte(`{"id":1,"method":"model/list","params":{}}`))
	if err != nil || len(out) != 2 || !r.ownedCatalog.refreshing {
		t.Fatal("owned catalog refresh did not start")
	}
	return r, path, out
}

func ownedAuthRefreshRequest(t *testing.T, r *Router) Delivery {
	t.Helper()
	out, err := r.Client([]byte(`{"id":2,"method":"getAuthStatus","params":{"includeToken":true,"refreshToken":false}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "one" {
		t.Fatal("existing primary no-auth read must remain available during owned catalog refresh")
	}
	m, _ := parseObject(out[0].Message)
	if text(m, "method") != "getAuthStatus" || !sameJSON(m["params"], json.RawMessage(`{"includeToken":true,"refreshToken":false}`)) {
		t.Fatal("primary no-auth request intent changed")
	}
	return out[0]
}

func TestOwnedCatalogRefreshKeepsExistingPrimaryNoAuthRead(t *testing.T) {
	r, _, refresh := ownedAuthRefreshStart(t)
	d := ownedAuthRefreshRequest(t, r)
	m, _ := parseObject(d.Message)
	result := json.RawMessage(`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}`)
	out, err := r.Server("one", raw(object{"id": m["id"], "result": result}))
	if err != nil || len(out) != 1 || out[0].Backend != "" {
		t.Fatal("genuine primary no-auth response was not returned")
	}
	reply, _ := parseObject(out[0].Message)
	if !sameJSON(reply["id"], raw(2)) || !sameJSON(reply["result"], result) || !r.ownedCatalog.refreshing || len(r.models) != 0 || r.controlSession167 != nil {
		t.Fatal("auth read bypassed catalog publication barrier")
	}
	native := ownedTestNative(t)
	for _, delivery := range refresh {
		if _, err := ownedTestReply(t, r, delivery, native[delivery.Backend], nil); err != nil {
			t.Fatal(err)
		}
	}
	if r.ownedCatalog.refreshing || len(r.models) != 2 || r.controlSession167 == nil {
		t.Fatal("auth read disturbed atomic catalog completion")
	}
}

func TestOwnedCatalogRefreshAuthExceptionsStayNarrow(t *testing.T) {
	for _, tc := range []struct {
		name, request string
		mode          bool
		code          int
	}{
		{"mode-off", `{"id":2,"method":"getAuthStatus","params":{"includeToken":true}}`, false, -32000},
		{"wrong-case", `{"id":2,"method":"getauthstatus","params":{}}`, true, -32000},
		{"refresh-denied", `{"id":2,"method":"getAuthStatus","params":{"refreshToken":true}}`, true, -32602},
		{"extra-params", `{"id":2,"method":"getAuthStatus","params":{"unknown":true}}`, true, -32602},
		{"requirements", `{"id":2,"method":"configRequirements/read","params":{}}`, true, -32000},
		{"thread", `{"id":2,"method":"thread/start","params":{}}`, true, -32000},
		{"turn", `{"id":2,"method":"turn/start","params":{}}`, true, -32000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := ownedAuthRefreshStart(t)
			r.auth.authStatusNoAuth = tc.mode
			before := len(r.pending)
			out, err := r.Client([]byte(tc.request))
			if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.pending) != before {
				t.Fatal("prohibited read escaped to a native child")
			}
			m, _ := parseObject(out[0].Message)
			failure, _ := child(m, "error")
			if !sameJSON(failure["code"], raw(tc.code)) || !r.ownedCatalog.refreshing {
				t.Fatal("existing mode, parameter or refresh gate changed")
			}
		})
	}
	for _, bytesLimit := range []bool{false, true} {
		r, _, _ := ownedAuthRefreshStart(t)
		if bytesLimit {
			r.activeBytes = 32 << 20
		} else {
			for i := 0; i < 128; i++ {
				r.active["inert-limit-"+strconv.Itoa(i)] = true
			}
		}
		out, err := r.Client([]byte(`{"id":2,"method":"getAuthStatus","params":{}}`))
		if err != nil || len(out) != 1 || out[0].Backend != "" || !strings.Contains(string(out[0].Message), "bridge request limit reached") {
			t.Fatal("auth exception bypassed the existing request limit")
		}
	}
}

func TestOwnedCatalogRefreshAuthResultCannotBecomeFalseSuccess(t *testing.T) {
	for _, payload := range []string{
		`"result":{"authMethod":null,"authToken":"SYNTHETIC_CANARY","requiresOpenaiAuth":false}`,
		`"result":{"authMethod":"apikey","authToken":null,"requiresOpenaiAuth":false}`,
		`"result":{"authMethod":null,"authToken":null,"requiresOpenaiAuth":true}`,
		`"error":{"code":-32000,"message":"SYNTHETIC_CANARY","data":"SYNTHETIC_CANARY"}`,
	} {
		r, _, _ := ownedAuthRefreshStart(t)
		d := ownedAuthRefreshRequest(t, r)
		m, _ := parseObject(d.Message)
		out, err := r.Server("one", []byte(`{"id":`+string(m["id"])+`,`+payload+`}`))
		if err != nil || len(out) != 1 || out[0].Backend != "" || strings.Contains(string(out[0].Message), "SYNTHETIC_CANARY") {
			t.Fatal("auth failure leaked or was lost")
		}
		reply, _ := parseObject(out[0].Message)
		failure, _ := child(reply, "error")
		if exists(reply, "result") || !sameJSON(failure["code"], raw(-32000)) || text(failure, "message") != authStatusUnavailable {
			t.Fatal("invalid native auth result became a successful no-auth result")
		}
	}
}

func TestOwnedCatalogRefreshAuthCannotSurviveCatalogFailure(t *testing.T) {
	for _, fileChanged := range []bool{false, true} {
		r, path, refresh := ownedAuthRefreshStart(t)
		d := ownedAuthRefreshRequest(t, r)
		if fileChanged {
			if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			m, _ := parseObject(refresh[0].Message)
			_, _ = r.Server(refresh[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("synthetic failure")})}))
		}
		m, _ := parseObject(d.Message)
		out, err := r.Server("one", raw(object{"id": m["id"], "result": raw(object{"authMethod": raw(nil), "authToken": raw(nil), "requiresOpenaiAuth": raw(false)})}))
		if err == nil || len(out) != 0 || !r.ownedCatalog.failed || len(r.models) != 0 || r.defaultModel != "" || r.controlSession167 != nil {
			t.Fatal("auth result survived failed immutable catalog boundary")
		}
	}
}
