package appserverbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func nativeSettingsRouter167(t *testing.T) *Router {
	t.Helper()
	r, path, _ := ownedTestSetup(t)
	r.ownedCatalog.nativeLists = true
	r.ownedCatalog.nativeSnapshot = nil
	r.ownedCatalog.projection = nil
	store, err := openOwnedSettingsStoreMode(filepath.Dir(path), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	r.ownedSettings = store
	return r
}
func nativeSettingsRows167(t *testing.T) map[string][]json.RawMessage {
	t.Helper()
	rows := ownedTestNative(t)
	for backend, list := range rows {
		m, _ := parseObject(list[0])
		m["defaultReasoningEffort"] = raw("medium")
		m["supportedReasoningEfforts"] = raw([]object{{"reasoningEffort": raw("medium"), "description": raw("authored medium")}, {"reasoningEffort": raw("high"), "description": raw("authored high")}})
		rows[backend] = []json.RawMessage{raw(m)}
	}
	return rows
}
func nativeSettingsFinishList167(t *testing.T, r *Router, list []Delivery) []Delivery {
	t.Helper()
	var out []Delivery
	rows := nativeSettingsRows167(t)
	for _, d := range list {
		next, err := ownedTestReply(t, r, d, rows[d.Backend], nil)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, next...)
	}
	return out
}
func nativeSettingsWrite167(id int, model, effort string) []byte {
	m, _ := parseObject(ownedSettingsTestRequest(id, model, ""))
	p, _ := child(m, "params")
	var edits []object
	json.Unmarshal(p["edits"], &edits)
	edits[1]["value"] = raw(effort)
	p["edits"] = raw(edits)
	m["params"] = raw(p)
	return raw(m)
}

// Router calls are serialized by Run. Interleave outstanding native requests
// deterministically rather than race Router methods, which are not thread safe.
func TestNativeSettingsReadWriteRefreshPublication(t *testing.T) {
	for _, writeFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "refresh-before-write", true: "write-before-refresh"}[writeFirst], func(t *testing.T) {
			r := nativeSettingsRouter167(t)
			nativeSettingsFinishList167(t, r, ownedReadWaitRefresh(t, r, 1))
			write, err := r.Client(nativeSettingsWrite167(2, "magpie/two/second", "high"))
			if err != nil {
				t.Fatal(err)
			}
			read := ownedReadWaitStart(t, r, 3)
			var list []Delivery
			if writeFirst {
				ownedSettingsTestPolicy(t, r, write)
			}
			list = ownedReadWaitRefresh(t, r, 4)
			for _, d := range read {
				out, err := ownedReadWaitPolicy(t, r, d)
				if err != nil || len(out) != 0 {
					t.Fatal("read escaped refresh barrier", err)
				}
			}
			if !writeFirst {
				out := ownedSettingsTestPolicy(t, r, write)
				if len(out) != 1 {
					t.Fatal("pending write did not reject")
				}
				m, _ := parseObject(out[0].Message)
				if !exists(m, "error") {
					t.Fatal("write committed during incomplete list")
				}
			}
			blocked, err := r.Client(nativeSettingsWrite167(5, "magpie/two/second", "high"))
			if err != nil || len(blocked) != 1 || blocked[0].Backend != "" {
				t.Fatal("concurrent write reached native", err)
			}
			out := nativeSettingsFinishList167(t, r, list)
			found := false
			for _, d := range out {
				m, _ := parseObject(d.Message)
				if !sameJSON(m["id"], raw(3)) {
					continue
				}
				found = true
				result, _ := child(m, "result")
				cfg, _ := child(result, "config")
				model, effort := "magpie/one/first", "medium"
				if writeFirst {
					model, effort = "magpie/two/second", "high"
				}
				if text(cfg, "model") != model || text(cfg, "model_reasoning_effort") != effort {
					t.Fatal("read observed stale or reset default")
				}
			}
			if !found || r.ownedCatalog.refreshing || len(r.controlReadGroups167) != 0 {
				t.Fatal("read did not drain after complete publication")
			}
		})
	}
}

func TestNativeSettingsSnapshotRejectsCapabilityDrift(t *testing.T) {
	r := nativeSettingsRouter167(t)
	nativeSettingsFinishList167(t, r, ownedReadWaitRefresh(t, r, 1))
	list := ownedReadWaitRefresh(t, r, 2)
	rows := nativeSettingsRows167(t)
	m, _ := parseObject(rows["two"][0])
	m["inputModalities"] = raw([]string{"text", "image"})
	rows["two"][0] = raw(m)
	var failure error
	for _, d := range list {
		_, err := ownedTestReply(t, r, d, rows[d.Backend], nil)
		if err != nil {
			failure = err
		}
	}
	if failure == nil || !r.ownedCatalog.failed || len(r.models) != 0 || r.controlSession167 != nil {
		t.Fatal("drifting metadata retained writable settings")
	}
}

func TestNativeSettingsReasoningUsesActualNativeChoices(t *testing.T) {
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent"} {
		t.Run(effort, func(t *testing.T) {
			r := nativeSettingsRouter167(t)
			rows := nativeSettingsRows167(t)
			m, _ := parseObject(rows["one"][0])
			m["defaultReasoningEffort"] = raw(effort)
			m["supportedReasoningEfforts"] = raw([]object{})
			rows["one"][0] = raw(m)
			if err := r.ownedCatalog.validateNative(rows, r.backends); err != nil {
				t.Fatal(err)
			}
			value := object{"model": raw("magpie/one/first"), "model_reasoning_effort": raw(effort)}
			if !r.ownedSettingsValues167(value) {
				t.Fatal("native effective default rejected without selectable choices")
			}
			value["model_reasoning_effort"] = raw("unadvertised")
			if r.ownedSettingsValues167(value) {
				t.Fatal("invented effort accepted")
			}
			m["supportedReasoningEfforts"] = raw([]object{{"reasoningEffort": raw(effort), "description": raw("authored")}})
			r.ownedCatalog.projection["magpie/one/first"] = m
			value["model_reasoning_effort"] = raw(effort)
			if !r.ownedSettingsValues167(value) {
				t.Fatal("advertised native effort rejected")
			}
		})
	}
	for _, kind := range []string{"unknown", "duplicate", "default-absent", "null", "null-choices"} {
		t.Run(kind, func(t *testing.T) {
			r := nativeSettingsRouter167(t)
			rows := nativeSettingsRows167(t)
			m, _ := parseObject(rows["one"][0])
			switch kind {
			case "unknown":
				m["defaultReasoningEffort"] = raw("unreviewed-custom")
			case "duplicate":
				m["supportedReasoningEfforts"] = raw([]object{{"reasoningEffort": raw("medium"), "description": raw("a")}, {"reasoningEffort": raw("medium"), "description": raw("b")}})
			case "default-absent":
				m["defaultReasoningEffort"] = raw("none")
			case "null":
				m["defaultReasoningEffort"] = raw(nil)
			case "null-choices":
				m["supportedReasoningEfforts"] = raw(nil)
			}
			rows["one"][0] = raw(m)
			if r.ownedCatalog.validateNative(rows, r.backends) == nil {
				t.Fatal("invalid initial native effort metadata accepted")
			}
		})
	}
}

func TestNativeSettingsWaitsForCompleteHiddenPagination(t *testing.T) {
	r := nativeSettingsRouter167(t)
	list := ownedReadWaitRefresh(t, r, 1)
	rows := nativeSettingsRows167(t)
	next, err := ownedTestReply(t, r, list[0], rows[list[0].Backend], "next-authored-page")
	if err != nil || len(next) != 1 || next[0].Backend != list[0].Backend {
		t.Fatal("native pagination lost owner", err)
	}
	request, _ := parseObject(next[0].Message)
	params, _ := child(request, "params")
	if !sameJSON(params["includeHidden"], raw(true)) {
		t.Fatal("pagination stopped requesting hidden rows")
	}
	if out, err := ownedTestReply(t, r, list[1], rows[list[1].Backend], nil); err != nil || len(out) != 0 {
		t.Fatal("partial list published", err)
	}
	if _, err := os.Stat(r.ownedSettings.path); !os.IsNotExist(err) {
		t.Fatal("partial native list seeded settings", err)
	}
	hidden, _ := parseObject(fixtureModel("authored-hidden", false))
	hidden["hidden"] = raw(true)
	out, err := ownedTestReply(t, r, next[0], []json.RawMessage{raw(hidden)}, nil)
	if err != nil || len(out) != 1 || len(r.ownedCatalog.projection) != 3 || r.ownedCatalog.refreshing {
		t.Fatal("complete native pagination was not published", err)
	}
	if _, err := os.Stat(r.ownedSettings.path); err != nil {
		t.Fatal("complete validated list did not seed", err)
	}
}

func TestNativeSettingsNamedNewEffortsRetainMetadataGuards(t *testing.T) {
	for _, effort := range []string{"max", "ultra", "persistent"} {
		for _, kind := range []string{"valid", "duplicate", "missing-default", "custom-choice", "custom-default"} {
			t.Run(effort+"/"+kind, func(t *testing.T) {
				r := nativeSettingsRouter167(t)
				rows := nativeSettingsRows167(t)
				m, _ := parseObject(rows["one"][0])
				m["defaultReasoningEffort"] = raw(effort)
				choices := []object{{"reasoningEffort": raw(effort), "description": raw("authored named effort")}}
				switch kind {
				case "duplicate":
					choices = append(choices, choices[0])
				case "missing-default":
					choices[0]["reasoningEffort"] = raw("medium")
				case "custom-choice":
					choices = append(choices, object{"reasoningEffort": raw("unreviewed-custom"), "description": raw("authored custom")})
				case "custom-default":
					m["defaultReasoningEffort"] = raw("unreviewed-custom")
				}
				m["supportedReasoningEfforts"] = raw(choices)
				rows["one"][0] = raw(m)
				err := r.ownedCatalog.validateNative(rows, r.backends)
				if (err == nil) != (kind == "valid") {
					t.Fatal("named effort weakened metadata guards")
				}
			})
		}
	}
}
