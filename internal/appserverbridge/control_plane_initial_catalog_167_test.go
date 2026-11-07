package appserverbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func initialCatalogRouter167(t *testing.T) *Router {
	t.Helper()
	r, err := NewRouter([]Backend{
		{ID: "openai", Provider: "openai", CodexHome: t.TempDir()},
		{ID: "aihub", Provider: "aihub", CodexHome: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.enableControlPlane167(); err != nil {
		t.Fatal(err)
	}
	r.controlHome167 = t.TempDir()
	return r
}

func initialCatalogBegin167(t *testing.T, r *Router) []Delivery {
	t.Helper()
	out := client(t, r, `{"id":"init","method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`)
	for i, d := range out {
		result := object{"userAgent": raw("native-fixture"), "codexHome": raw(r.byID[d.Backend].CodexHome),
			"platformFamily": raw("fixture"), "platformOs": raw("fixture")}
		next := reply(t, r, d, string(raw(result)))
		if i == len(out)-1 {
			return next
		}
		if len(next) != 0 {
			t.Fatal("initialize returned before both peers")
		}
	}
	t.Fatal("initialize did not request native peers")
	return nil
}

func catalogPage167(t *testing.T, r *Router, d Delivery, rows []json.RawMessage, cursor any) []Delivery {
	t.Helper()
	return reply(t, r, d, string(raw(object{"data": raw(rows), "nextCursor": raw(cursor)})))
}

func assertInitialCatalogResult167(t *testing.T, r *Router, out []Delivery) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "" || r.phase != "ready" || len(r.active) != 0 || r.activeBytes != 0 || len(r.pending) != 0 {
		t.Fatal("initial catalog did not release exactly one initialized result")
	}
	m, _ := parseObject(out[0].Message)
	result, _ := child(m, "result")
	if text(m, "id") != "init" || text(result, "codexHome") != r.controlHome167 || text(result, "userAgent") != "native-fixture" || len(result) != 4 {
		t.Fatal("catalog data replaced original initialize result")
	}
	if r.defaultModel != "magpie/openai/gpt" || text(controlPlaneRead(t, r), "model") != r.defaultModel {
		t.Fatal("first config read lost the committed native default")
	}
}

func TestControlPlane167InitialCatalogBarrierAndRefresh(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		r := initialCatalogRouter167(t)
		out := initialCatalogBegin167(t, r)
		if len(out) != 2 || r.phase != "initializing" || len(r.active) != 1 || len(r.pending) != 2 {
			t.Fatal("catalog was not an owned initialization stage")
		}
		for _, d := range out {
			m, _ := parseObject(d.Message)
			p, _ := child(m, "params")
			if text(m, "method") != "model/list" || len(p) != 1 || string(p["limit"]) != "100" {
				t.Fatal("initialization parameters leaked into catalog read")
			}
		}
		for i := 0; i < 200; i++ {
			expectError(t, client(t, r, `{"id":"early","method":"config/read","params":{}}`))
		}
		expectError(t, client(t, r, `{"id":"early-list","method":"model/list","params":{}}`))
		expectError(t, client(t, r, `{"id":"early-thread","method":"thread/start","params":{"model":"magpie/openai/gpt"}}`))
		if len(r.active) != 1 || len(r.pending) != 2 || len(r.models) != 0 {
			t.Fatal("early requests grew state or published partial catalog")
		}
		// Optional initialized remains a capability-preserving notification,
		// not a prerequisite or a command that completes our catalog stage.
		if len(client(t, r, `{"method":"initialized"}`)) != 2 || r.phase != "initializing" {
			t.Fatal("initialized notification changed catalog barrier")
		}
		primary, secondary := out[0], out[1]
		if reverse {
			if len(catalogPage167(t, r, secondary, []json.RawMessage{fixtureModel("other", true)}, nil)) != 0 {
				t.Fatal("secondary published a partial default")
			}
		}
		more := catalogPage167(t, r, primary, []json.RawMessage{fixtureModel("not-default", false)}, "next")
		if len(more) != 1 || more[0].Backend != primary.Backend || r.phase != "initializing" {
			t.Fatal("initial catalog pagination changed owner or readiness")
		}
		if !reverse {
			catalogPage167(t, r, secondary, []json.RawMessage{fixtureModel("other", true)}, nil)
		}
		done := catalogPage167(t, r, more[0], []json.RawMessage{fixtureModel("gpt", true)}, nil)
		assertInitialCatalogResult167(t, r, done)
		old := string(r.controlSession167.layer)
		controlPlaneCatalog(t, r, true)
		if string(r.controlSession167.layer) != old {
			t.Fatal("same routing default changed the owned layer version")
		}
		refresh := client(t, r, `{"id":"changed","method":"model/list","params":{}}`)
		catalogPage167(t, r, refresh[0], []json.RawMessage{fixtureModel("new-default", true)}, nil)
		catalogPage167(t, r, refresh[1], []json.RawMessage{fixtureModel("other", true)}, nil)
		if string(r.controlSession167.layer) == old || text(controlPlaneRead(t, r), "model") != "magpie/openai/new-default" {
			t.Fatal("changed true default failed to update routing and layer")
		}
	}
}

func TestControlPlane167InitialCatalogFailsClosed(t *testing.T) {
	for _, shape := range []string{"error", "missing-default", "hidden-default", "malformed", "repeated-cursor"} {
		r := initialCatalogRouter167(t)
		out := initialCatalogBegin167(t, r)
		var done []Delivery
		var err error
		switch shape {
		case "error":
			m, _ := parseObject(out[0].Message)
			done, err = r.Server(out[0].Backend, raw(object{"id": m["id"], "error": raw(object{"code": raw(-1), "message": raw("PRIVATE_SECRET")})}))
		case "malformed":
			m, _ := parseObject(out[0].Message)
			done, err = r.Server(out[0].Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw("PRIVATE_SECRET")})}))
		case "repeated-cursor":
			more := catalogPage167(t, r, out[0], []json.RawMessage{}, "repeat")
			m, _ := parseObject(more[0].Message)
			done, err = r.Server(more[0].Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw([]any{}), "nextCursor": raw("repeat")})}))
		default:
			m, _ := parseObject(fixtureModel("gpt", shape == "hidden-default"))
			m["hidden"] = raw(shape == "hidden-default")
			catalogPage167(t, r, out[0], []json.RawMessage{raw(m)}, nil)
			done = catalogPage167(t, r, out[1], []json.RawMessage{}, nil)
		}
		if err == nil {
			expectError(t, done)
			m, _ := parseObject(done[0].Message)
			e, _ := child(m, "error")
			if text(m, "id") != "init" || text(e, "message") != controlPlaneInitUnavailable167 {
				t.Fatal("bootstrap error used model-list shape or native data")
			}
		} else if err.Error() != controlPlaneInitUnavailable167 {
			t.Fatal("bootstrap fatal error disclosed native diagnostics")
		}
		if strings.Contains(string(raw(done)), "PRIVATE_") || r.phase != "failed" || len(r.active) != 0 || r.activeBytes != 0 || len(r.pending) != 0 || len(r.models) != 0 || r.defaultModel != "" {
			t.Fatal("failed initial catalog retained state or published partial routes")
		}
		if shape == "error" || shape == "malformed" || shape == "repeated-cursor" {
			if late := catalogPage167(t, r, out[1], []json.RawMessage{fixtureModel("late", true)}, nil); len(late) != 0 {
				t.Fatal("late catalog completed a failed initialization")
			}
		}
	}
}

func TestControlPlane167InitialDeclaredCatalogPreservesProvenance(t *testing.T) {
	r := initialCatalogRouter167(t)
	r.backends[0].Models = []json.RawMessage{fixtureModel("gpt", true)}
	r.backends[1].Models = []json.RawMessage{}
	done := initialCatalogBegin167(t, r)
	assertInitialCatalogResult167(t, r, done)
	config := controlPlaneRead(t, r)
	ext, _ := child(config, "magpie_bridge")
	var backends []object
	if json.Unmarshal(ext["backends"], &backends) != nil || text(backends[0], "catalog_source") != "configured-descriptors" {
		t.Fatal("declared descriptors were represented as native entitlement")
	}
}

func TestControlPlane167InitialMixedCatalogAndSecondPageFailure(t *testing.T) {
	r := initialCatalogRouter167(t)
	r.backends[0].Models = []json.RawMessage{fixtureModel("gpt", true)}
	out := initialCatalogBegin167(t, r)
	if len(out) != 1 || out[0].Backend != "aihub" || len(r.models) != 0 {
		t.Fatal("declared half catalog became ready before native pages")
	}
	more := catalogPage167(t, r, out[0], []json.RawMessage{}, "page-two")
	assertInitialCatalogResult167(t, r, catalogPage167(t, r, more[0], []json.RawMessage{fixtureModel("other", true)}, nil))

	r = initialCatalogRouter167(t)
	out = initialCatalogBegin167(t, r)
	more = catalogPage167(t, r, out[0], []json.RawMessage{fixtureModel("gpt", true)}, "page-two")
	m, _ := parseObject(more[0].Message)
	failed := server(t, r, more[0].Backend, string(raw(object{"id": m["id"], "error": raw(object{"code": raw(-1), "message": raw("PRIVATE_PAGE")})})))
	expectError(t, failed)
	if strings.Contains(string(failed[0].Message), "PRIVATE_") || len(r.models) != 0 || len(r.pending) != 0 || r.activeBytes != 0 {
		t.Fatal("second page failure leaked or retained partial initialization")
	}
	if late := catalogPage167(t, r, out[1], []json.RawMessage{fixtureModel("late", true)}, nil); len(late) != 0 {
		t.Fatal("late sibling returned initialization after page failure")
	}
}
