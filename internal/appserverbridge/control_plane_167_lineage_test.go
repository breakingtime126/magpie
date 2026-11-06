package appserverbridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func lineageList167(t *testing.T, r *Router, params string) []Delivery {
	t.Helper()
	return client(t, r, `{"id":"lineage","method":"thread/list","params":`+params+`}`)
}

func TestControlPlane167NullLineageOnlyInExactMode(t *testing.T) {
	for _, key := range []string{"parentThreadId", "ancestorThreadId", "attachedToThreadId"} {
		t.Run(key, func(t *testing.T) {
			r := testRouter(t, false)
			expectError(t, lineageList167(t, r, `{"`+key+`":null}`))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("mode-off lineage rejection retained or sent a request")
			}
			// The global validator stays closed even when a control router exists.
			if validateParams("thread/list", object{key: raw(nil)}) == nil {
				t.Fatal("null lineage widened the ordinary protocol allowlist")
			}
		})
	}
	for _, params := range []string{`{}`, `null`} {
		r := testRouter(t, false)
		out := lineageList167(t, r, params)
		if len(out) != 2 || out[0].Backend == "" || out[1].Backend == "" {
			t.Fatal("ordinary absent lineage behavior changed")
		}
	}
}

func TestControlPlane167LineageRejectsBeforeSend(t *testing.T) {
	for _, key := range []string{"parentThreadId", "ancestorThreadId", "attachedToThreadId"} {
		for _, value := range []string{`""`, `"null"`, `"unknown"`, `"owned"`, `"foreign"`, `0`, `1`, `false`, `true`, `[]`, `[null]`, `{}`, `{"value":null}`} {
			t.Run(key+"/"+value, func(t *testing.T) {
				r := controlPlaneRouter(t)
				r.threads["owned"] = "openai"
				r.threads["foreign"] = "aihub"
				expectError(t, lineageList167(t, r, `{"`+key+`":`+value+`}`))
				if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.threads) != 2 {
					t.Fatal("non-null lineage sent a request or inferred ownership")
				}
			})
		}
		for _, fields := range []string{
			`"` + key + `":null,"` + key + `":null`,
			`"` + key + `":"unknown","` + key + `":null`,
			`"` + key + `":null,"` + key + `":"unknown"`,
			`"` + key + `":null,"` + strings.Replace(key, "T", `\u0054`, 1) + `":null`,
		} {
			r := controlPlaneRouter(t)
			expectError(t, lineageList167(t, r, "{"+fields+"}"))
			if len(r.pending) != 0 {
				t.Fatal("duplicate lineage keys reached a backend")
			}
		}
	}
	for _, params := range []string{
		`[]`, `"null"`, `{"parentThreadId":nul}`, `{"parentThreadId":null} {}`,
		`{"parentThreadId":null,"projectId":null}`,
		`{"parentThreadId":null,"projectId":"unknown"}`,
		`{"parentThreadId":null,"futureFilter":null}`,
		`{"parentThreadId":null,"sortKey":"section_position"}`,
		`{"parentThreadId":null,"sortKey":"section_position","sectionId":null}`,
		`{"parentThreadId":null,"sortKey":"section_position","sectionId":"native-section"}`,
		`{"parentThreadId":null,"sortKey":"unknown"}`,
		`{"parentThreadId":null,"modelProviders":["openai"]}`,
	} {
		r := controlPlaneRouter(t)
		expectError(t, lineageList167(t, r, params))
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("unsupported listing scope reached a backend")
		}
	}
}

func TestControlPlane167LineagePreservesEveryNativePage(t *testing.T) {
	keys := []string{"parentThreadId", "ancestorThreadId", "attachedToThreadId"}
	for mask := 0; mask < 1<<len(keys); mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			r := controlPlaneRouter(t)
			r.threads["openai-excluded"] = "openai"
			r.threads["aihub-excluded"] = "aihub"
			params := object{
				"modelProviders": raw([]string{"magpie_bridge"}), "limit": raw(1),
				"excludedThreadIds": raw([]string{"openai-excluded", "aihub-excluded", "unknown-excluded"}),
				"archived": raw(false), "sectionId": raw(nil), "cwd": raw(`C:\native\workspace`),
				"sourceKinds": raw([]string{"vscode"}), "currentAccountOnly": raw(false),
			}
			for i, key := range keys {
				if mask&(1<<i) != 0 {
					params[key] = json.RawMessage(" null ")
				}
			}
			check := func(d Delivery, cursor string) {
				t.Helper()
				m, _ := parseObject(d.Message)
				p, _ := child(m, "params")
				for i, key := range keys {
					value, present := p[key]
					if present != (mask&(1<<i) != 0) || (present && string(value) != "null") {
						t.Fatal("explicit null or absent lineage changed on a native page")
					}
				}
				for _, key := range []string{"archived", "sectionId", "cwd", "sourceKinds", "currentAccountOnly"} {
					if string(p[key]) != string(params[key]) {
						t.Fatal("null lineage changed a neighboring native filter")
					}
				}
				if text(p, "cursor") != cursor || string(p["limit"]) != "100" ||
					string(p["modelProviders"]) != string(raw([]string{r.byID[d.Backend].Provider})) ||
					string(p["excludedThreadIds"]) != string(raw([]string{d.Backend + "-excluded"})) {
					t.Fatal("native lineage pagination changed cursor, cap, provider or exclusion ownership")
				}
			}
			out := lineageList167(t, r, string(raw(params)))
			if len(out) != 2 || out[0].Backend == "" || out[1].Backend == "" {
				t.Fatal("a reviewed null lineage combination was rejected")
			}
			for _, d := range out {
				check(d, "")
				more := reply(t, r, d, `{"data":[],"nextCursor":"native-next"}`)
				if len(more) != 1 || more[0].Backend != d.Backend {
					t.Fatal("native pagination crossed backend owners")
				}
				check(more[0], "native-next")
				row := object{"id": raw("unknown-excluded"), "modelProvider": raw(r.byID[d.Backend].Provider), "createdAt": raw(1)}
				data := []json.RawMessage{}
				if d.Backend == "openai" {
					data = append(data, raw(row))
				}
				done := reply(t, r, more[0], string(raw(object{"data": raw(data), "nextCursor": raw(nil)})))
				if d.Backend == out[1].Backend {
					if len(done) != 1 || !strings.Contains(string(done[0].Message), `"data":[]`) {
						t.Fatal("unknown excluded thread leaked through null lineage listing")
					}
				}
			}
		})
	}
}

func TestControlPlane167LineageKeepsNativeOwnershipChecks(t *testing.T) {
	for _, row := range []string{
		`{"id":"a","modelProvider":"aihub","createdAt":1}`,
		`{"id":"a","modelProvider":null,"createdAt":1}`,
		`{"id":"a","modelProvider":"aihub","modelProvider":"openai","createdAt":1}`,
		`{"id":"a","modelProvider":"openai","createdAt":1,"parentThreadId":"foreign"}`,
	} {
		r := controlPlaneRouter(t)
		r.threads["foreign"] = "aihub"
		out := lineageList167(t, r, `{"parentThreadId":null}`)
		reply(t, r, out[1], `{"data":[],"nextCursor":null}`)
		m, _ := parseObject(out[0].Message)
		done, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "result": json.RawMessage(`{"data":[` + row + `],"nextCursor":null}`)}))
		if err == nil || len(done) != 0 {
			t.Fatal("null lineage bypassed native provider, duplicate or parent ownership checks")
		}
	}
	r := controlPlaneRouter(t)
	out := lineageList167(t, r, `{"parentThreadId":null}`)
	reply(t, r, out[0], `{"data":[{"id":"same","modelProvider":"openai","createdAt":1}],"nextCursor":null}`)
	m, _ := parseObject(out[1].Message)
	if done, err := r.Server(out[1].Backend, raw(object{"id": m["id"], "result": json.RawMessage(`{"data":[{"id":"same","modelProvider":"aihub","createdAt":1}],"nextCursor":null}`)})); err == nil || len(done) != 0 {
		t.Fatal("null lineage bypassed cross-backend thread ID collision checks")
	}
}

func TestControlPlane167LineageKeepsPaginationCaps(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		r := controlPlaneRouter(t)
		out := lineageList167(t, r, `{"parentThreadId":null}`)
		d := out[0]
		for i := 0; i <= 100; i++ {
			cursor := fmt.Sprint(i)
			if repeated {
				cursor = "repeat"
			}
			m, _ := parseObject(d.Message)
			next, err := r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw([]any{}), "nextCursor": raw(cursor)})}))
			if (repeated && i == 1) || i == 100 {
				if err == nil || len(next) != 0 {
					t.Fatal("null lineage bypassed native cursor repetition or page count cap")
				}
				break
			}
			if err != nil || len(next) != 1 || next[0].Backend != d.Backend {
				t.Fatal("bounded native pagination failed before its cap")
			}
			d = next[0]
		}
	}
}
