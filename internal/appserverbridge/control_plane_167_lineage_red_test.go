package appserverbridge

import (
	"encoding/json"
	"testing"
)

// Copy this file alone onto 02600624b069bd5a00a339953eea5b8348e5a467.
// It uses only baseline APIs and fails by assertion, not by compilation.
func TestControlPlane167NullLineageRedNativeList(t *testing.T) {
	r := controlPlaneRouter(t)
	params := `{"parentThreadId":null,"ancestorThreadId":null,"attachedToThreadId":null,"modelProviders":["magpie_bridge"],"limit":1}`
	out := client(t, r, `{"id":"lineage-red","method":"thread/list","params":`+params+`}`)
	if len(out) != 2 || out[0].Backend == "" || out[1].Backend == "" {
		t.Fatal("native null lineage thread/list filters must be accepted")
	}
	for _, first := range out {
		check := func(d Delivery) {
			t.Helper()
			m, _ := parseObject(d.Message)
			p, _ := child(m, "params")
			for _, key := range []string{"parentThreadId", "ancestorThreadId", "attachedToThreadId"} {
				if string(p[key]) != "null" {
					t.Fatal("native null lineage must survive every backend page")
				}
			}
			if string(p["modelProviders"]) != string(raw([]string{r.byID[d.Backend].Provider})) {
				t.Fatal("native lineage listing lost owner-specific provider filtering")
			}
		}
		check(first)
		more := reply(t, r, first, `{"data":[],"nextCursor":"native-next"}`)
		if len(more) != 1 || more[0].Backend != first.Backend {
			t.Fatal("native lineage pagination lost its backend owner")
		}
		check(more[0])
		row := object{"id": raw(first.Backend + "-thread"), "modelProvider": raw(r.byID[first.Backend].Provider), "createdAt": raw(1)}
		done := reply(t, r, more[0], string(raw(object{"data": raw([]json.RawMessage{raw(row)}), "nextCursor": raw(nil)})))
		if first.Backend == out[0].Backend {
			if len(done) != 0 {
				t.Fatal("native lineage listing returned before both backends finished")
			}
			continue
		}
		if len(done) != 1 || done[0].Backend != "" {
			t.Fatal("native lineage listing did not return its merged page")
		}
		m, _ := parseObject(done[0].Message)
		result, _ := child(m, "result")
		cursor := text(result, "nextCursor")
		if cursor == "" {
			t.Fatal("native lineage listing lost frontend pagination")
		}
		next := client(t, r, string(raw(object{"id": raw("continue"), "method": raw("thread/list"), "params": raw(object{"cursor": raw(cursor)})})))
		if len(next) != 1 || next[0].Backend != "" {
			t.Fatal("cursor-only lineage continuation must use the existing snapshot")
		}
		m, _ = parseObject(next[0].Message)
		result, _ = child(m, "result")
		var rows []json.RawMessage
		if json.Unmarshal(result["data"], &rows) != nil || len(rows) != 1 || string(result["nextCursor"]) != "null" {
			t.Fatal("cursor-only lineage continuation changed the snapshot")
		}
	}
}
