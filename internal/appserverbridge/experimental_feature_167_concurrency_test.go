package appserverbridge

import (
	"fmt"
	"strings"
	"testing"
)

// This file uses only APIs from ace340842ea5d61818e8e416df1b5017bf7847ae.
// Copy it alone there and run TestExperimentalFeature167ConcurrentPages for
// an assertion-level red against the former single-pending-read guard.
func experimentalFeatureConcurrentAsk(t *testing.T, r *Router, id, params string) []Delivery {
	t.Helper()
	before := make(map[string]bool, len(r.pending))
	for key := range r.pending {
		before[key] = true
	}
	out := client(t, r, `{"id":`+id+`,"method":"experimentalFeature/list","params":`+params+`,"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
	if len(out) != 2 || out[0].Backend != "openai" || out[1].Backend != "aihub" {
		t.Fatal("concurrent native feature pages must each reach both children")
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		key, err := idKey(m["id"])
		if err != nil || before[key] || len(m) != 3 || text(m, "method") != "experimentalFeature/list" ||
			string(m["params"]) != authStatusTestCompact(t, params) || strings.Contains(string(d.Message), "PRIVATE_") {
			t.Fatal("concurrent feature read reused a request or changed native params/trace")
		}
		before[key] = true
	}
	if len(r.pending) != len(before) {
		t.Fatal("concurrent feature read coalesced pending native requests")
	}
	return out
}

func experimentalFeatureConcurrentResult(t *testing.T, out []Delivery, id, page string) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatal("concurrent feature read did not return one client result")
	}
	m, _ := parseObject(out[0].Message)
	if len(m) != 2 || string(m["id"]) != id || string(m["result"]) != authStatusTestCompact(t, page) {
		t.Fatal("concurrent feature result mixed request IDs or native pages")
	}
}

func experimentalFeatureConcurrentEmpty(t *testing.T, r *Router) {
	t.Helper()
	if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.pages) != 0 {
		t.Fatal("concurrent feature reads retained state or created a snapshot")
	}
}

func TestExperimentalFeature167ConcurrentPages(t *testing.T) {
	r := experimentalFeatureRouter(t)
	firstParams := `{"cursor":"60","limit":1,"threadId":null}`
	secondParams := `{ "cursor" : "65", "limit" : 0 }`
	firstPage := strings.Replace(experimentalFeatureTestPage, `"nextCursor":"1"`, `"nextCursor":"61"`, 1)
	secondPage := strings.Replace(experimentalFeatureTestLast, `"nextCursor":null`, `"nextCursor":"66"`, 1)
	first := experimentalFeatureConcurrentAsk(t, r, "9007199254740993", firstParams)
	second := experimentalFeatureConcurrentAsk(t, r, `"65"`, secondParams)
	// Even identical params get fresh native pairs, never a coalesced result.
	again := experimentalFeatureConcurrentAsk(t, r, `"same-page"`, firstParams)
	for _, item := range []struct {
		delivery Delivery
		page     string
	}{
		{first[0], firstPage},
		{second[1], secondPage},
		{again[0], firstPage},
	} {
		if len(reply(t, r, item.delivery, item.page)) != 0 {
			t.Fatal("concurrent page succeeded before its matching sibling")
		}
	}
	experimentalFeatureConcurrentResult(t, reply(t, r, second[0], secondPage), `"65"`, secondPage)
	experimentalFeatureConcurrentResult(t, reply(t, r, again[1], firstPage), `"same-page"`, firstPage)
	experimentalFeatureConcurrentResult(t, reply(t, r, first[1], firstPage), "9007199254740993", firstPage)
	experimentalFeatureConcurrentEmpty(t, r)
}

func TestExperimentalFeature167ConcurrentFailureIsolation(t *testing.T) {
	for _, kind := range []string{"native error", "malformed result", "unequal pages"} {
		t.Run(kind, func(t *testing.T) {
			r := experimentalFeatureRouter(t)
			failed := experimentalFeatureConcurrentAsk(t, r, `"failed"`, `{"cursor":"60","limit":5}`)
			survivor := experimentalFeatureConcurrentAsk(t, r, `"survivor"`, `{"cursor":"65","limit":5}`)
			if len(reply(t, r, survivor[1], experimentalFeatureTestLast)) != 0 {
				t.Fatal("survivor completed before both native replies")
			}
			var failure []Delivery
			switch kind {
			case "native error":
				m, _ := parseObject(failed[0].Message)
				failure = server(t, r, failed[0].Backend, `{"id":`+string(m["id"])+`,"error":{"code":-32000,"message":"PRIVATE_ERROR","data":"PRIVATE_DATA"}}`)
			case "malformed result":
				failure = reply(t, r, failed[0], `{"data":null,"nextCursor":null}`)
			case "unequal pages":
				if len(reply(t, r, failed[0], experimentalFeatureTestPage)) != 0 {
					t.Fatal("failed page completed before comparison")
				}
				failure = reply(t, r, failed[1], experimentalFeatureTestLast)
			}
			experimentalFeatureError(t, failure)
			m, _ := parseObject(failure[0].Message)
			if string(m["id"]) != `"failed"` || len(r.pending) != 1 || len(r.active) != 1 {
				t.Fatal("one failed group cancelled another active page")
			}
			if kind != "unequal pages" && len(reply(t, r, failed[1], experimentalFeatureTestPage)) != 0 {
				t.Fatal("late failed sibling returned a page")
			}
			// Reusing the completed client ID must allocate a new independent pair.
			recovery := experimentalFeatureConcurrentAsk(t, r, `"failed"`, `{"cursor":"60","limit":5}`)
			experimentalFeatureConcurrentResult(t, reply(t, r, survivor[0], experimentalFeatureTestLast), `"survivor"`, experimentalFeatureTestLast)
			if len(reply(t, r, recovery[1], experimentalFeatureTestPage)) != 0 {
				t.Fatal("recovery completed before both native replies")
			}
			experimentalFeatureConcurrentResult(t, reply(t, r, recovery[0], experimentalFeatureTestPage), `"failed"`, experimentalFeatureTestPage)
			experimentalFeatureConcurrentEmpty(t, r)
		})
	}
}

func TestExperimentalFeature167ConcurrentDuplicateIDs(t *testing.T) {
	for _, ids := range [][2]string{
		{`"same"`, `"same"`}, {"9007199254740993", "9007199254740993"}, {"-0", "0"}, {"0", "-0"},
	} {
		r := experimentalFeatureRouter(t)
		first := experimentalFeatureConcurrentAsk(t, r, ids[0], `{"cursor":"60"}`)
		other := experimentalFeatureConcurrentAsk(t, r, `"other"`, `{"cursor":"65"}`)
		activeBytes := r.activeBytes
		out, err := r.Client([]byte(`{"id":` + ids[1] + `,"method":"experimentalFeature/list","params":{"cursor":"70"}}`))
		if err == nil || len(out) != 0 || len(r.pending) != 4 || len(r.active) != 2 || r.activeBytes != activeBytes {
			t.Fatal("duplicate client ID changed concurrent page ownership")
		}
		reply(t, r, first[0], experimentalFeatureTestPage)
		experimentalFeatureConcurrentResult(t, reply(t, r, first[1], experimentalFeatureTestPage), ids[0], experimentalFeatureTestPage)
		reply(t, r, other[1], experimentalFeatureTestLast)
		experimentalFeatureConcurrentResult(t, reply(t, r, other[0], experimentalFeatureTestLast), `"other"`, experimentalFeatureTestLast)
		experimentalFeatureConcurrentEmpty(t, r)
	}
	// String and integer IDs remain separate native request identities.
	r := experimentalFeatureRouter(t)
	numeric := experimentalFeatureConcurrentAsk(t, r, "7", "{}")
	textual := experimentalFeatureConcurrentAsk(t, r, `"7"`, "{}")
	reply(t, r, textual[0], experimentalFeatureTestLast)
	experimentalFeatureConcurrentResult(t, reply(t, r, textual[1], experimentalFeatureTestLast), `"7"`, experimentalFeatureTestLast)
	reply(t, r, numeric[1], experimentalFeatureTestPage)
	experimentalFeatureConcurrentResult(t, reply(t, r, numeric[0], experimentalFeatureTestPage), "7", experimentalFeatureTestPage)
	experimentalFeatureConcurrentEmpty(t, r)
}

func TestExperimentalFeature167ConcurrentCapAndRecovery(t *testing.T) {
	r := experimentalFeatureRouter(t)
	var requests [][]Delivery
	for i := 0; i < 4; i++ {
		requests = append(requests, experimentalFeatureConcurrentAsk(t, r, fmt.Sprint(i), fmt.Sprintf(`{"cursor":"%d","limit":5}`, 60+i*5)))
	}
	rejected := func() {
		t.Helper()
		count, active, activeBytes := len(r.pending), len(r.active), r.activeBytes
		out := client(t, r, `{"id":"overflow","method":"experimentalFeature/list","params":{"cursor":"80","limit":5}}`)
		expectError(t, out)
		m, _ := parseObject(out[0].Message)
		failure, _ := child(m, "error")
		if string(m["id"]) != `"overflow"` || string(failure["code"]) != "-32000" ||
			text(failure, "message") != "experimental feature list request limit reached" ||
			len(r.pending) != count || len(r.active) != active || r.activeBytes != activeBytes {
			t.Fatal("feature concurrency cap changed active reads or sent native requests")
		}
	}
	rejected()
	for _, pair := range requests {
		if len(reply(t, r, pair[0], experimentalFeatureTestLast)) != 0 {
			t.Fatal("half-completed pair returned a result")
		}
	}
	// Four remaining transport requests still represent four logical reads.
	rejected()
	experimentalFeatureConcurrentResult(t, reply(t, r, requests[0][1], experimentalFeatureTestLast), "0", experimentalFeatureTestLast)
	replacement := experimentalFeatureConcurrentAsk(t, r, `"replacement"`, `{"cursor":"80","limit":5}`)
	rejected()
	// A failed half-completed pair also frees exactly one slot.
	experimentalFeatureError(t, reply(t, r, requests[1][1], "{}"))
	recovery := experimentalFeatureConcurrentAsk(t, r, `"recovery"`, `{"cursor":"85","limit":5}`)
	rejected()
	for i := 2; i < 4; i++ {
		experimentalFeatureConcurrentResult(t, reply(t, r, requests[i][1], experimentalFeatureTestLast), fmt.Sprint(i), experimentalFeatureTestLast)
	}
	for _, item := range []struct {
		pair []Delivery
		id   string
	}{
		{replacement, `"replacement"`},
		{recovery, `"recovery"`},
	} {
		reply(t, r, item.pair[1], experimentalFeatureTestLast)
		experimentalFeatureConcurrentResult(t, reply(t, r, item.pair[0], experimentalFeatureTestLast), item.id, experimentalFeatureTestLast)
	}
	experimentalFeatureConcurrentEmpty(t, r)
}
