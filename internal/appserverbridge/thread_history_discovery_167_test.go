package appserverbridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// These are synthetic router peers, not an execution of the official runtime,
// a native history store, or Desktop. The default/null/empty distinction is
// sourced separately from the pinned official thread-list implementation.
func historyDiscoveryRouter167(t *testing.T) *Router {
	t.Helper()
	r := controlPlaneRouter(t)
	requests := client(t, r, `{"id":"synthetic-catalog","method":"model/list","params":{}}`)
	for _, request := range requests {
		model := "test-alpha"
		if request.Backend == "aihub" {
			model = "test-beta"
		}
		reply(t, r, request, string(raw(object{"data": raw([]json.RawMessage{fixtureModel(model, true)}), "nextCursor": raw(nil)})))
	}
	return r
}

func historyDiscoveryRow167(id, backend string, created int) object {
	model := "test-alpha"
	if backend == "aihub" {
		model = "test-beta"
	}
	return object{"id": raw(id), "modelProvider": raw(backend), "model": raw(model), "createdAt": raw(created), "turns": raw([]object{})}
}

func historyDiscoveryReply167(t *testing.T, r *Router, request Delivery, rows []object, cursor any) []Delivery {
	t.Helper()
	return reply(t, r, request, string(raw(object{"data": raw(rows), "nextCursor": raw(cursor)})))
}

func TestThreadHistoryDiscovery167FiltersPagesAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, filter string
	}{
		{"omitted", ""},
		{"null", "null"},
		{"all-native-providers", "[]"},
		{"explicit-logical-provider", `["magpie_bridge"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The second fresh router has no ownership knowledge from the first.
			// Rediscovering the same synthetic rows is not a persistence test.
			for connection := 0; connection < 2; connection++ {
				r := historyDiscoveryRouter167(t)
				expectError(t, client(t, r, `{"id":"before-discovery","method":"thread/read","params":{"threadId":"synthetic-alpha-1"}}`))
				params := object{"limit": raw(1), "sortKey": raw("created_at")}
				if tc.filter != "" {
					params["modelProviders"] = json.RawMessage(tc.filter)
				}
				request := func(id string, p object) []Delivery {
					return client(t, r, string(raw(object{"id": raw(id), "method": raw("thread/list"), "params": raw(p)})))
				}
				checkNative := func(d Delivery, wantCursor string) {
					t.Helper()
					m, err := parseObject(d.Message)
					if err != nil || text(m, "method") != "thread/list" || r.byID[d.Backend].Provider == "" {
						t.Fatal("history request did not target a known native peer")
					}
					p, err := child(m, "params")
					if err != nil || string(p["limit"]) != "100" {
						t.Fatal("native collection page lost its bounded size")
					}
					if text(p, "cursor") != wantCursor {
						t.Fatal("native history continuation cursor was lost or crossed its owner")
					}
					filter, present := p["modelProviders"]
					want := tc.filter
					if tc.name == "explicit-logical-provider" {
						want = string(raw([]string{r.byID[d.Backend].Provider}))
					}
					if present != (want != "") || string(filter) != want {
						t.Fatal("provider filter presence or semantics changed on a native page")
					}
				}
				out := request("discover", params)
				if len(out) != 2 {
					t.Fatal("history did not query both native peers")
				}
				first := map[string]Delivery{}
				for _, d := range out {
					checkNative(d, "")
					first[d.Backend] = d
				}
				if len(first) != 2 {
					t.Fatal("history queried the same native peer twice")
				}
				betaNext := historyDiscoveryReply167(t, r, first["aihub"], []object{historyDiscoveryRow167("synthetic-beta-1", "aihub", 2)}, "native-beta-page")
				alphaNext := historyDiscoveryReply167(t, r, first["openai"], []object{historyDiscoveryRow167("synthetic-alpha-1", "openai", 3)}, "native-alpha-page")
				for _, next := range [][]Delivery{betaNext, alphaNext} {
					if len(next) != 1 || next[0].Backend == "" {
						t.Fatal("history returned a partial frontend list before collecting native pages")
					}
					wantCursor := "native-alpha-page"
					if next[0].Backend == "aihub" {
						wantCursor = "native-beta-page"
					}
					checkNative(next[0], wantCursor)
				}
				if betaNext[0].Backend != "aihub" || alphaNext[0].Backend != "openai" {
					t.Fatal("native history continuation crossed its owner")
				}
				if got := historyDiscoveryReply167(t, r, betaNext[0], []object{historyDiscoveryRow167("synthetic-beta-2", "aihub", 0)}, nil); len(got) != 0 {
					t.Fatal("history returned before the other provider completed")
				}
				page := historyDiscoveryReply167(t, r, alphaNext[0], []object{historyDiscoveryRow167("synthetic-alpha-2", "openai", 1)}, nil)
				wantIDs := []string{"synthetic-alpha-1", "synthetic-beta-1", "synthetic-alpha-2", "synthetic-beta-2"}
				for index, wantID := range wantIDs {
					if len(page) != 1 || page[0].Backend != "" {
						t.Fatal("merged history page was not a single frontend response")
					}
					m, _ := parseObject(page[0].Message)
					result, _ := child(m, "result")
					var rows []object
					if json.Unmarshal(result["data"], &rows) != nil || len(rows) != 1 || text(rows[0], "id") != wantID {
						t.Fatal("merged history omitted, duplicated, or reordered a synthetic thread")
					}
					owner, model := "openai", "test-alpha"
					if strings.Contains(wantID, "beta") {
						owner, model = "aihub", "test-beta"
					}
					if text(rows[0], "modelProvider") != controlPlaneProvider167 || text(rows[0], "model") != alias(owner, model) || r.threads[wantID] != owner {
						t.Fatal("history projection lost native ownership")
					}
					cursor := text(result, "nextCursor")
					if index == len(wantIDs)-1 {
						if cursor != "" {
							t.Fatal("merged history has a spurious trailing page")
						}
						break
					}
					if cursor == "" || strings.Contains(cursor, "native-") {
						t.Fatal("native history cursor escaped to the frontend")
					}
					nextParams := clone(params)
					nextParams["cursor"] = raw(cursor)
					page = request(fmt.Sprintf("client-page-%d", index), nextParams)
				}
				for _, owner := range []string{"openai", "aihub"} {
					id, model, other := "synthetic-alpha-1", "test-alpha", alias("aihub", "test-beta")
					if owner == "aihub" {
						id, model, other = "synthetic-beta-1", "test-beta", alias("openai", "test-alpha")
					}
					for _, method := range []string{"thread/read", "thread/resume", "turn/start"} {
						p := object{"threadId": raw(id)}
						if method == "turn/start" {
							p["input"] = raw([]object{})
							p["model"] = raw(alias(owner, model))
						}
						out := client(t, r, string(raw(object{"id": raw(method + id), "method": raw(method), "params": raw(p)})))
						if len(out) != 1 || out[0].Backend != owner {
							t.Fatal("discovered history routed to another provider")
						}
					}
					expectError(t, client(t, r, string(raw(object{"id": raw("cross-" + id), "method": raw("turn/start"), "params": raw(object{"threadId": raw(id), "input": raw([]object{}), "model": raw(other)})}))))
				}
			}
		})
	}
}

func TestThreadHistoryDiscovery167EmptyFilterDoesNotAdoptForeignProvider(t *testing.T) {
	r := historyDiscoveryRouter167(t)
	out := client(t, r, `{"id":"all","method":"thread/list","params":{"modelProviders":[]}}`)
	if len(out) != 2 {
		t.Fatal("expected two native list requests")
	}
	historyDiscoveryReply167(t, r, out[1], []object{}, nil)
	m, _ := parseObject(out[0].Message)
	foreign := historyDiscoveryRow167("synthetic-foreign-history", "other-provider", 1)
	got, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw([]object{foreign}), "nextCursor": raw(nil)})}))
	if err == nil || len(got) != 0 || r.threads["synthetic-foreign-history"] != "" {
		t.Fatal("all-provider history was silently adopted or published under a different configured owner")
	}
	// Run treats this protocol/ownership error as fatal; do not continue this
	// router or claim that an empty filter makes foreign-provider history usable.
}

func TestThreadHistoryDiscovery167CollidingIDsDoNotMergeProviders(t *testing.T) {
	r := historyDiscoveryRouter167(t)
	out := client(t, r, `{"id":"collision","method":"thread/list","params":{}}`)
	if len(out) != 2 {
		t.Fatal("expected two native list requests")
	}
	historyDiscoveryReply167(t, r, out[0], []object{historyDiscoveryRow167("synthetic-collision", out[0].Backend, 1)}, nil)
	m, _ := parseObject(out[1].Message)
	row := historyDiscoveryRow167("synthetic-collision", out[1].Backend, 2)
	got, err := r.Server(out[1].Backend, raw(object{"id": m["id"], "result": raw(object{"data": raw([]object{row}), "nextCursor": raw(nil)})}))
	if err == nil || len(got) != 0 || r.threads["synthetic-collision"] != "!" {
		t.Fatal("colliding history IDs were silently merged across native owners")
	}
}
