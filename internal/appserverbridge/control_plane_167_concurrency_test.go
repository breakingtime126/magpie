package appserverbridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// This file uses only APIs present at 09b526eb76a1f6e033c51db57eba58ed80026e86.
// Copy it alone there and run TestControlPlane167ConcurrentReads for an
// assertion-level behavioral red against the former singleton read guard.
func controlPlaneConcurrentAsk167(t *testing.T, r *Router, id, params string) []Delivery {
	t.Helper()
	before := make(map[string]bool, len(r.pending))
	for key := range r.pending {
		before[key] = true
	}
	out := client(t, r, `{"id":`+id+`,"method":"config/read","params":`+params+`,"trace":{"traceparent":"PRIVATE_TRACE","tracestate":null}}`)
	if len(out) != 2 || out[0].Backend != "openai" || out[1].Backend != "aihub" {
		t.Fatal("second overlapping native-equivalent config read must reach both backends")
	}
	for _, delivery := range out {
		message, _ := parseObject(delivery.Message)
		key, err := idKey(message["id"])
		if err != nil || before[key] || len(message) != 3 ||
			text(message, "method") != "configRequirements/read" || string(message["params"]) != "null" ||
			strings.Contains(string(delivery.Message), "PRIVATE_") {
			t.Fatal("concurrent config read reused native requests or forwarded config, params or trace")
		}
		before[key] = true
	}
	if len(r.pending) != len(before) {
		t.Fatal("concurrent config read coalesced independent native policy requests")
	}
	return out
}

func controlPlaneConcurrentResult167(t *testing.T, out []Delivery, id, modelJSON string, included bool) string {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatal("concurrent config read did not return one frontend result")
	}
	message, _ := parseObject(out[0].Message)
	if len(message) != 2 || string(message["id"]) != id || exists(message, "error") {
		t.Fatal("concurrent config result mixed request IDs or failed")
	}
	result, _ := child(message, "result")
	return controlPlaneCheckLayer167(t, result, modelJSON, included)
}

func controlPlaneConcurrentEmpty167(t *testing.T, r *Router) {
	t.Helper()
	if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.pages) != 0 {
		t.Fatal("concurrent config reads retained active payloads or created pagination state")
	}
}

func TestControlPlane167ConcurrentReads(t *testing.T) {
	r := controlPlaneRouter(t)
	// Desktop issued true request 88 followed by false request 89 before 88
	// completed. The second request must get its own fresh native policy pair.
	first := controlPlaneConcurrentAsk167(t, r, "88", `{"includeLayers":true,"cwd":null}`)
	second := controlPlaneConcurrentAsk167(t, r, "89", `{"includeLayers":false,"cwd":null}`)
	again := controlPlaneConcurrentAsk167(t, r, "9007199254740993", `{"includeLayers":true,"cwd":null}`)
	defaulted := controlPlaneConcurrentAsk167(t, r, `"defaulted"`, `{}`)
	if len(r.pending) != 8 || len(r.active) != 4 {
		t.Fatal("four independent config reads must issue eight native policy calls")
	}
	for _, delivery := range []Delivery{first[0], second[1], again[1], defaulted[0]} {
		if len(reply(t, r, delivery, controlPlaneNullRequirements)) != 0 {
			t.Fatal("config read completed using another group's policy response")
		}
	}
	// Complete in a different order from both request and first-reply order.
	version := controlPlaneConcurrentResult167(t, reply(t, r, second[0], controlPlaneNullRequirements), "89", "", false)
	for _, item := range []struct {
		delivery Delivery
		id       string
		included bool
	}{
		{defaulted[1], `"defaulted"`, false},
		{again[0], "9007199254740993", true},
		{first[1], "88", true},
	} {
		if controlPlaneConcurrentResult167(t, reply(t, r, item.delivery, controlPlaneNullRequirements), item.id, "", item.included) != version {
			t.Fatal("equivalent overlapping reads changed the published content version")
		}
	}
	controlPlaneConcurrentEmpty167(t, r)
}

func TestControlPlane167ConcurrentFailureIsolation(t *testing.T) {
	for _, kind := range []string{"native error", "non-null policy", "malformed result", "oversized response", "policy mismatch"} {
		for _, first := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", kind, first), func(t *testing.T) {
				r := controlPlaneRouter(t)
				failed := controlPlaneConcurrentAsk167(t, r, `"failed"`, `{"includeLayers":true}`)
				survivor := controlPlaneConcurrentAsk167(t, r, `"survivor"`, `{"includeLayers":false}`)
				failedMessage, _ := parseObject(failed[first].Message)
				failedKey, _ := idKey(failedMessage["id"])
				failedGroup := r.pending[failedKey].group
				survivorMessage, _ := parseObject(survivor[first].Message)
				survivorKey, _ := idKey(survivorMessage["id"])
				survivorSize := r.pending[survivorKey].group.size
				if len(reply(t, r, survivor[first], controlPlaneNullRequirements)) != 0 {
					t.Fatal("surviving config read completed without its own second reply")
				}
				var failure []Delivery
				switch kind {
				case "native error":
					failure = server(t, r, failed[first].Backend, `{"id":`+string(failedMessage["id"])+`,"error":{"code":-32000,"message":"PRIVATE_ERROR","data":"PRIVATE_DATA"}}`)
				case "non-null policy":
					failure = reply(t, r, failed[first], `{"requirements":{},"supportsIndependentSpeedModes":true}`)
				case "malformed result":
					failure = reply(t, r, failed[first], `{}`)
				case "oversized response":
					failure = server(t, r, failed[first].Backend, `{"id":`+string(failedMessage["id"])+`,"result":`+controlPlaneNullRequirements+`}`+strings.Repeat(" ", requirementsJSONLimit))
				case "policy mismatch":
					if len(reply(t, r, failed[first], controlPlaneNullRequirements)) != 0 {
						t.Fatal("config read completed before policy comparison")
					}
					failure = reply(t, r, failed[1-first], `{"requirements":null,"supportsIndependentSpeedModes":false}`)
				}
				controlPlaneError(t, failure)
				message, _ := parseObject(failure[0].Message)
				if string(message["id"]) != `"failed"` || len(r.pending) != 1 || len(r.active) != 1 || r.activeBytes != survivorSize ||
					failedGroup.params != nil || failedGroup.results != nil || failedGroup.seen != nil || !failedGroup.failed {
					t.Fatal("one failed config group changed another read or retained failed payloads")
				}
				// Reuse the failed client ID before its old native sibling arrives.
				recovery := controlPlaneConcurrentAsk167(t, r, `"failed"`, `{"includeLayers":true}`)
				count, activeBytes := len(r.pending), r.activeBytes
				if kind != "policy mismatch" {
					if len(reply(t, r, failed[1-first], controlPlaneNullRequirements)) != 0 ||
						len(r.pending) != count || r.activeBytes != activeBytes {
						t.Fatal("late failed sibling affected the survivor or reused client ID")
					}
				}
				controlPlaneConcurrentResult167(t, reply(t, r, survivor[1-first], controlPlaneNullRequirements), `"survivor"`, "", false)
				if len(reply(t, r, recovery[1-first], controlPlaneNullRequirements)) != 0 {
					t.Fatal("recovery reused a previous policy result")
				}
				controlPlaneConcurrentResult167(t, reply(t, r, recovery[first], controlPlaneNullRequirements), `"failed"`, "", true)
				controlPlaneConcurrentEmpty167(t, r)
			})
		}
	}
}

func TestControlPlane167ConcurrentDuplicateIDs(t *testing.T) {
	for _, ids := range [][2]string{
		{`"same"`, `"same"`}, {`"same"`, `"\u0073ame"`},
		{"9007199254740993", "9007199254740993"}, {"-0", "0"}, {"0", "-0"},
	} {
		r := controlPlaneRouter(t)
		first := controlPlaneConcurrentAsk167(t, r, ids[0], `{"includeLayers":true}`)
		other := controlPlaneConcurrentAsk167(t, r, `"other"`, `{}`)
		if len(reply(t, r, first[0], controlPlaneNullRequirements)) != 0 {
			t.Fatal("half-completed config read returned a result")
		}
		activeBytes := r.activeBytes
		out, err := r.Client([]byte(`{"id":` + ids[1] + `,"method":"config/read","params":{"includeLayers":false}}`))
		if err == nil || len(out) != 0 || len(r.pending) != 3 || len(r.active) != 2 || r.activeBytes != activeBytes {
			t.Fatal("duplicate client ID changed overlapping config ownership")
		}
		controlPlaneConcurrentResult167(t, reply(t, r, first[1], controlPlaneNullRequirements), ids[0], "", true)
		reply(t, r, other[1], controlPlaneNullRequirements)
		controlPlaneConcurrentResult167(t, reply(t, r, other[0], controlPlaneNullRequirements), `"other"`, "", false)
		controlPlaneConcurrentEmpty167(t, r)
	}
	// Native integer identity does not collapse an equal-looking string ID.
	r := controlPlaneRouter(t)
	numeric := controlPlaneConcurrentAsk167(t, r, "7", `{"includeLayers":true}`)
	textual := controlPlaneConcurrentAsk167(t, r, `"7"`, `{}`)
	reply(t, r, textual[0], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, textual[1], controlPlaneNullRequirements), `"7"`, "", false)
	reply(t, r, numeric[1], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, numeric[0], controlPlaneNullRequirements), "7", "", true)
	controlPlaneConcurrentEmpty167(t, r)
}

func TestControlPlane167ConcurrentCapAndRecovery(t *testing.T) {
	r := controlPlaneRouter(t)
	var requests [][]Delivery
	for i := 0; i < 4; i++ {
		requests = append(requests, controlPlaneConcurrentAsk167(t, r, fmt.Sprint(i), fmt.Sprintf(`{"includeLayers":%t}`, i%2 == 0)))
	}
	if len(r.pending) != 8 || len(r.active) != 4 {
		t.Fatal("four config read slots did not create eight native policy requests")
	}
	rejected := func() {
		t.Helper()
		count, active, activeBytes := len(r.pending), len(r.active), r.activeBytes
		out := client(t, r, `{"id":"overflow","method":"config/read","params":{"includeLayers":true}}`)
		expectError(t, out)
		message, _ := parseObject(out[0].Message)
		failure, _ := child(message, "error")
		if string(message["id"]) != `"overflow"` || string(failure["code"]) != "-32000" ||
			text(failure, "message") != "routing config read request limit reached" ||
			len(r.pending) != count || len(r.active) != active || r.activeBytes != activeBytes {
			t.Fatal("config read cap changed active groups or sent a native request")
		}
	}
	rejected()
	for _, pair := range requests {
		if len(reply(t, r, pair[0], controlPlaneNullRequirements)) != 0 {
			t.Fatal("half-completed config group returned a result")
		}
	}
	// Four transport requests remain, but all four logical slots stay occupied.
	rejected()
	controlPlaneConcurrentResult167(t, reply(t, r, requests[0][1], controlPlaneNullRequirements), "0", "", true)
	replacement := controlPlaneConcurrentAsk167(t, r, `"replacement"`, `{"includeLayers":true}`)
	rejected()
	controlPlaneError(t, reply(t, r, requests[1][1], `{"requirements":{},"supportsIndependentSpeedModes":true}`))
	recovery := controlPlaneConcurrentAsk167(t, r, `"recovery"`, `{}`)
	rejected()
	for i := 2; i < 4; i++ {
		controlPlaneConcurrentResult167(t, reply(t, r, requests[i][1], controlPlaneNullRequirements), fmt.Sprint(i), "", i%2 == 0)
	}
	reply(t, r, replacement[1], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, replacement[0], controlPlaneNullRequirements), `"replacement"`, "", true)
	reply(t, r, recovery[0], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, recovery[1], controlPlaneNullRequirements), `"recovery"`, "", false)
	controlPlaneConcurrentEmpty167(t, r)
}

func TestControlPlane167ConcurrentStrictRequests(t *testing.T) {
	r := controlPlaneRouter(t)
	first := controlPlaneConcurrentAsk167(t, r, `"first"`, `{"includeLayers":true}`)
	second := controlPlaneConcurrentAsk167(t, r, `"second"`, `{}`)
	activeBytes := r.activeBytes
	for _, line := range []string{
		`{"id":"invalid","method":"config/read","params":{"includeLayers":null}}`,
		`{"id":"invalid","method":"config/read","params":{"includeLayers":true,"cwd":"PRIVATE_PATH"}}`,
		`{"id":"invalid","method":"config/read","params":{"includeLayers":false,"other":true}}`,
		`{"id":"invalid","method":"config/read","params":{},"trace":{"unknown":"PRIVATE_TRACE"}}`,
		`{"id":1.5,"method":"config/read","params":{}}`,
	} {
		expectError(t, client(t, r, line))
		if len(r.pending) != 4 || len(r.active) != 2 || r.activeBytes != activeBytes {
			t.Fatal("invalid overlapping config read changed active groups")
		}
	}
	reply(t, r, second[1], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, second[0], controlPlaneNullRequirements), `"second"`, "", false)
	reply(t, r, first[0], controlPlaneNullRequirements)
	controlPlaneConcurrentResult167(t, reply(t, r, first[1], controlPlaneNullRequirements), `"first"`, "", true)
	controlPlaneConcurrentEmpty167(t, r)
}

func TestControlPlane167ConcurrentCatalogSnapshots(t *testing.T) {
	r := controlPlaneRouter(t)
	controlPlaneCatalog(t, r, true)
	before := controlPlaneConcurrentAsk167(t, r, `"before"`, `{"includeLayers":true}`)
	after := controlPlaneConcurrentAsk167(t, r, `"after"`, `{"includeLayers":false}`)
	retained := controlPlaneConcurrentAsk167(t, r, `"retained"`, `{"includeLayers":true}`)
	removed := controlPlaneConcurrentAsk167(t, r, `"removed"`, `{}`)
	for _, pair := range [][]Delivery{before, after, retained, removed} {
		if len(reply(t, r, pair[0], controlPlaneNullRequirements)) != 0 {
			t.Fatal("overlapping config snapshot returned before both policy replies")
		}
	}
	oldSession := r.controlSession167
	refresh := client(t, r, `{"id":"refresh","method":"model/list","params":{}}`)
	next := raw(object{"data": raw([]json.RawMessage{fixtureModel("next", true)}), "nextCursor": raw(nil)})
	if len(reply(t, r, refresh[0], string(next))) != 0 || r.controlSession167 != oldSession {
		t.Fatal("partial catalog refresh changed the published session")
	}
	oldVersion := controlPlaneConcurrentResult167(t, reply(t, r, before[1], controlPlaneNullRequirements), `"before"`, `"magpie/openai/gpt"`, true)
	if len(reply(t, r, refresh[1], `{"data":[],"nextCursor":null}`)) != 1 ||
		r.controlSession167 == oldSession || r.defaultModel != "magpie/openai/next" || len(r.models) != 1 {
		t.Fatal("complete catalog did not publish routes, default and session together")
	}
	newVersion := controlPlaneConcurrentResult167(t, reply(t, r, after[1], controlPlaneNullRequirements), `"after"`, `"magpie/openai/next"`, false)
	if newVersion == oldVersion {
		t.Fatal("config read captured an old snapshot before its own second policy reply")
	}
	// An oversized candidate must not replace any of the published authority
	// while other read groups still await their second genuine policy result.
	published := r.controlSession167
	oversized := client(t, r, `{"id":"oversized-refresh","method":"model/list","params":{}}`)
	large := fixtureModel(strings.Repeat("<", controlPlaneResultLimit167/4), true)
	reply(t, r, oversized[0], string(raw(object{"data": raw([]json.RawMessage{large}), "nextCursor": raw(nil)})))
	controlPlaneError(t, reply(t, r, oversized[1], `{"data":[],"nextCursor":null}`))
	if r.controlSession167 != published || r.defaultModel != "magpie/openai/next" || len(r.models) != 1 ||
		len(r.pending) != 2 || len(r.active) != 2 {
		t.Fatal("failed catalog refresh changed overlapping reads or published partial routing")
	}
	if controlPlaneConcurrentResult167(t, reply(t, r, retained[1], controlPlaneNullRequirements), `"retained"`, `"magpie/openai/next"`, true) != newVersion {
		t.Fatal("failed refresh changed the successful overlapping read's snapshot")
	}
	controlPlaneCatalog(t, r, false)
	controlPlaneConcurrentResult167(t, reply(t, r, removed[1], controlPlaneNullRequirements), `"removed"`, "", false)
	if r.defaultModel != "" || r.controlSession167.model != "" || len(r.models) != 2 {
		t.Fatal("default removal was not atomic with the current catalog and session")
	}
	controlPlaneConcurrentEmpty167(t, r)
}
