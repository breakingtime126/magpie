package appserverbridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func ownedReadWaitReady(t *testing.T) *Router {
	t.Helper()
	r := ownedSettingsTestRouter(t)
	ownedSettingsTestCatalog(t, r, 1)
	out, err := r.Client(ownedSettingsTestRequest(2, "magpie/two/second", ""))
	if err != nil {
		t.Fatal(err)
	}
	out = ownedSettingsTestPolicy(t, r, out)
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatal("saved preference was not acknowledged")
	}
	return r
}

func ownedReadWaitStart(t *testing.T, r *Router, id int) []Delivery {
	t.Helper()
	out, err := r.Client(raw(object{"id": raw(id), "method": raw("config/read"), "params": raw(object{"includeLayers": raw(true)})}))
	if err != nil || len(out) != 2 {
		t.Fatalf("config/read must issue its own policy pair: %v", err)
	}
	for _, d := range out {
		m, err := parseObject(d.Message)
		if err != nil || d.Backend == "" || text(m, "method") != "configRequirements/read" || !requirementsNull(m["params"]) {
			t.Fatal("read forwarded something other than null policy check")
		}
	}
	return out
}

func ownedReadWaitPolicy(t *testing.T, r *Router, d Delivery) ([]Delivery, error) {
	t.Helper()
	m, err := parseObject(d.Message)
	if err != nil {
		t.Fatal(err)
	}
	return r.Server(d.Backend, raw(object{"id": m["id"], "result": raw(object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)})}))
}

func ownedReadWaitRefresh(t *testing.T, r *Router, id int) []Delivery {
	t.Helper()
	out, err := r.Client(raw(object{"id": raw(id), "method": raw("model/list"), "params": raw(object{})}))
	if err != nil {
		t.Fatal(err)
	}
	out = ownedSettingsTestPolicy(t, r, out)
	if len(out) != 2 || !r.ownedCatalog.refreshing {
		t.Fatal("refresh did not enter real native list stage")
	}
	return out
}

func ownedReadWaitFinishCatalog(t *testing.T, r *Router, list []Delivery) []Delivery {
	t.Helper()
	native := ownedTestNative(t)
	var out []Delivery
	for i, d := range list {
		next, err := ownedTestReply(t, r, d, native[d.Backend], nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && len(next) != 0 {
			t.Fatal("published before complete catalog")
		}
		out = append(out, next...)
	}
	return out
}

func ownedReadWaitAssertView(t *testing.T, out []Delivery, ids ...int) {
	t.Helper()
	found := map[int]bool{}
	for _, d := range out {
		if d.Backend != "" {
			t.Fatal("drain forwarded a new native request")
		}
		m, err := parseObject(d.Message)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if !sameJSON(m["id"], raw(id)) {
				continue
			}
			if found[id] {
				t.Fatal("duplicate config response")
			}
			found[id] = true
			result, _ := child(m, "result")
			config, _ := child(result, "config")
			var layers []object
			if text(config, "model") != "magpie/two/second" || text(config, "model_reasoning_effort") != "none" || json.Unmarshal(result["layers"], &layers) != nil || len(layers) != 2 {
				t.Fatal("drained read lost saved preference or actual layers")
			}
		}
	}
	if len(found) != len(ids) {
		t.Fatal("missing deferred config response")
	}
}

func TestOwnedConfigReadWaitBeforeAndDuringRefresh(t *testing.T) {
	r := ownedReadWaitReady(t)
	before := ownedReadWaitStart(t, r, 10)
	list := ownedReadWaitRefresh(t, r, 20)
	during := ownedReadWaitStart(t, r, 11)
	for _, pair := range [][]Delivery{before, during} {
		for _, d := range pair {
			out, err := ownedReadWaitPolicy(t, r, d)
			if err != nil || len(out) != 0 {
				t.Fatal("valid policy pair must wait without responding during refresh")
			}
		}
	}
	if len(r.controlReadGroups167) != 2 || len(r.active) != 3 || r.activeBytes == 0 {
		t.Fatal("held reads lost logical/active accounting")
	}
	for g := range r.controlReadGroups167 {
		if g.remaining != 0 {
			t.Fatal("held read still has pending policy")
		}
	}
	out := ownedReadWaitFinishCatalog(t, r, list)
	if len(out) != 3 {
		t.Fatal("expected list response plus both held reads")
	}
	ownedReadWaitAssertView(t, out, 10, 11)
	if len(r.controlReadGroups167) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.pending) != 0 {
		t.Fatal("successful drain leaked request accounting")
	}
}

func TestOwnedConfigReadWaitPartialPolicySurvivesCatalogCommit(t *testing.T) {
	r := ownedReadWaitReady(t)
	list := ownedReadWaitRefresh(t, r, 20)
	pair := ownedReadWaitStart(t, r, 10)
	out, err := ownedReadWaitPolicy(t, r, pair[0])
	if err != nil || len(out) != 0 {
		t.Fatal("partial policy replied early")
	}
	out = ownedReadWaitFinishCatalog(t, r, list)
	if len(out) != 1 || len(r.controlReadGroups167) != 1 {
		t.Fatal("catalog completion bypassed missing policy sibling")
	}
	out, err = ownedReadWaitPolicy(t, r, pair[1])
	if err != nil || len(out) != 1 {
		t.Fatal("late valid policy did not release read")
	}
	ownedReadWaitAssertView(t, out, 10)
	if len(r.controlReadGroups167) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
		t.Fatal("late completion leaked accounting")
	}
}

func ownedReadWaitAssertFailed(t *testing.T, r *Router) {
	t.Helper()
	if !r.ownedCatalog.failed || r.controlSession167 != nil || len(r.models) != 0 || len(r.controlReadGroups167) != 0 {
		t.Fatal("failure retained routing authority or held read payload")
	}
	for _, p := range r.pending {
		if p.group.method == "config/read" {
			t.Fatal("failure retained native config-read payload")
		}
	}
	for _, id := range []int{10, 11} {
		key, _ := idKey(raw(id))
		if r.active[key] {
			t.Fatal("failure retained config-read client ID")
		}
	}
}

func TestOwnedConfigReadWaitInvalidPolicyRejectsOnlyOriginatingRead(t *testing.T) {
	for _, kind := range []string{"missing", "non-null", "rpc-error"} {
		t.Run(kind, func(t *testing.T) {
			r := ownedReadWaitReady(t)
			list := ownedReadWaitRefresh(t, r, 20)
			held := ownedReadWaitStart(t, r, 10)
			for _, d := range held {
				out, err := ownedReadWaitPolicy(t, r, d)
				if err != nil || len(out) != 0 {
					t.Fatal("held policy failed")
				}
			}
			pair := ownedReadWaitStart(t, r, 11)
			m, _ := parseObject(pair[0].Message)
			response := object{"id": m["id"], "result": raw(object{"requirements": raw(nil)})}
			if kind == "non-null" {
				response["result"] = raw(object{"requirements": raw(object{}), "supportsIndependentSpeedModes": raw(true)})
			}
			if kind == "rpc-error" {
				delete(response, "result")
				response["error"] = raw(object{"code": raw(-32000), "message": raw("PRIVATE_CANARY")})
			}
			out, err := r.Server(pair[0].Backend, raw(response))
			if err != nil || len(out) != 1 || out[0].Backend != "" || strings.Contains(string(out[0].Message), "PRIVATE_CANARY") {
				t.Fatal("invalid policy did not return a sanitized local error")
			}
			reply, _ := parseObject(out[0].Message)
			if !sameJSON(reply["id"], raw(11)) || !exists(reply, "error") || r.ownedCatalog.failed || len(r.controlReadGroups167) != 1 {
				t.Fatal("policy failure affected unrelated held read")
			}
			out, err = ownedReadWaitPolicy(t, r, pair[1])
			if err != nil || len(out) != 0 {
				t.Fatal("late failed-policy sibling was not discarded")
			}
			out = ownedReadWaitFinishCatalog(t, r, list)
			if len(out) != 2 {
				t.Fatal("failed read resumed or unrelated read was lost")
			}
			ownedReadWaitAssertView(t, out, 10)
		})
	}
}

func TestOwnedConfigReadWaitTamperOrCatalogFailureAbandons(t *testing.T) {
	for _, kind := range []string{"settings", "catalog", "native-list"} {
		t.Run(kind, func(t *testing.T) {
			r := ownedReadWaitReady(t)
			list := ownedReadWaitRefresh(t, r, 20)
			pair := ownedReadWaitStart(t, r, 10)
			for _, d := range pair {
				out, err := ownedReadWaitPolicy(t, r, d)
				if err != nil || len(out) != 0 {
					t.Fatal("held policy failed")
				}
			}
			if kind == "settings" {
				if err := os.WriteFile(r.ownedSettings.path, []byte("model = \"foreign\"\nmodel_reasoning_effort = \"none\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "catalog" {
				if err := os.WriteFile(r.ownedCatalog.path, []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m, _ := parseObject(list[0].Message)
			var out []Delivery
			var err error
			if kind == "native-list" {
				out, err = r.Server(list[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-32000), "message": raw("PRIVATE_CANARY")})}))
			} else {
				native := ownedTestNative(t)
				out, err = ownedTestReply(t, r, list[0], native[list[0].Backend], nil)
			}
			if err == nil || len(out) != 0 {
				t.Fatal("unavailable authority released held read")
			}
			ownedReadWaitAssertFailed(t, r)
		})
	}
}

func TestOwnedConfigReadWaitLogicalLimitAndExistingGuards(t *testing.T) {
	r := ownedReadWaitReady(t)
	_ = ownedReadWaitRefresh(t, r, 20)
	for id := 10; id < 14; id++ {
		pair := ownedReadWaitStart(t, r, id)
		// Mix held and half-completed groups in the same four-slot budget.
		for i, d := range pair {
			if id == 13 && i == 1 {
				break
			}
			out, err := ownedReadWaitPolicy(t, r, d)
			if err != nil || len(out) != 0 {
				t.Fatal("policy did not wait")
			}
		}
	}
	count, size := len(r.pending), r.activeBytes
	out, err := r.Client([]byte(`{"id":14,"method":"config/read","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.controlReadGroups167) != 4 || len(r.pending) != count || r.activeBytes != size {
		t.Fatal("fifth logical read escaped bound")
	}
	out, err = r.Client([]byte(`{"id":10,"method":"config/read","params":{}}`))
	if err == nil || len(out) != 0 {
		t.Fatal("held request ID was reusable")
	}
	// Independent aggregate-byte guard must still precede native fanout.
	r2 := ownedReadWaitReady(t)
	_ = ownedReadWaitRefresh(t, r2, 20)
	old := r2.activeBytes
	r2.activeBytes = 32 << 20
	out, err = r2.Client([]byte(`{"id":10,"method":"config/read","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "" || len(r2.controlReadGroups167) != 0 {
		t.Fatal("read bypassed active byte limit")
	}
	r2.activeBytes = old
}

func TestOwnedConfigReadWaitPreservesAdmissionChecks(t *testing.T) {
	for _, line := range []string{
		`{"id":10,"method":"config/read","params":null}`,
		`{"id":10,"method":"config/read","params":{"unknown":true}}`,
		`{"id":10,"method":"config/read","params":{"includeLayers":"true"}}`,
	} {
		r := ownedReadWaitReady(t)
		_ = ownedReadWaitRefresh(t, r, 20)
		pending, size := len(r.pending), r.activeBytes
		out, err := r.Client([]byte(line))
		if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.controlReadGroups167) != 0 || len(r.pending) != pending || r.activeBytes != size {
			t.Fatal("refresh exception bypassed request validation")
		}
		m, _ := parseObject(out[0].Message)
		if !exists(m, "error") {
			t.Fatal("malformed config read succeeded")
		}
	}
	r := ownedReadWaitReady(t)
	_ = ownedReadWaitRefresh(t, r, 20)
	r.phase = "initializing"
	out, err := r.Client([]byte(`{"id":10,"method":"config/read","params":{}}`))
	if err != nil || len(out) != 1 || out[0].Backend != "" || len(r.controlReadGroups167) != 0 {
		t.Fatal("refresh exception bypassed ready-phase check")
	}
}

func TestOwnedConfigReadWaitPreservesStringID(t *testing.T) {
	r := ownedReadWaitReady(t)
	list := ownedReadWaitRefresh(t, r, 20)
	pair, err := r.Client([]byte(`{"id":"10","method":"config/read","params":{"includeLayers":false}}`))
	if err != nil || len(pair) != 2 {
		t.Fatal("string-ID read did not issue policy pair")
	}
	for _, d := range pair {
		out, err := ownedReadWaitPolicy(t, r, d)
		if err != nil || len(out) != 0 {
			t.Fatal("string-ID read did not wait")
		}
	}
	out := ownedReadWaitFinishCatalog(t, r, list)
	found := false
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if !sameJSON(m["id"], raw("10")) {
			continue
		}
		found = true
		result, _ := child(m, "result")
		config, _ := child(result, "config")
		if text(config, "model") != "magpie/two/second" || exists(result, "layers") {
			t.Fatal("deferred read lost original includeLayers parameter")
		}
	}
	if !found || len(out) != 2 {
		t.Fatal("deferred response lost typed string ID")
	}
}
