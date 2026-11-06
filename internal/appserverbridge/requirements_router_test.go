package appserverbridge

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func requirementsRouter(t *testing.T) *Router {
	t.Helper()
	r := testRouter(t, false)
	r.configRequirementsRead = true
	return r
}

func requirementsRequest(t *testing.T, r *Router, input string) []Delivery {
	t.Helper()
	out := client(t, r, input)
	if len(out) != 2 || out[0].Backend == "" || out[1].Backend == "" || out[0].Backend == out[1].Backend {
		t.Fatalf("requirements read did not reach both authorities: %v", out)
	}
	for _, delivery := range out {
		message, err := parseObject(delivery.Message)
		if err != nil || text(message, "method") != "configRequirements/read" || string(message["params"]) != "null" {
			t.Fatalf("child request was not canonical params:null: %s", delivery.Message)
		}
	}
	return out
}

func expectRequirementsResult(t *testing.T, out []Delivery, id, want json.RawMessage) {
	t.Helper()
	if len(out) != 1 || out[0].Backend != "" {
		t.Fatalf("expected one client result: %v", out)
	}
	message, err := parseObject(out[0].Message)
	if err != nil || string(message["id"]) != string(id) || exists(message, "error") || !sameJSON(message["result"], want) {
		t.Fatalf("requirements result changed or was not preserved: %s", out[0].Message)
	}
}

func expectRequirementsError(t *testing.T, out []Delivery, id json.RawMessage) json.RawMessage {
	t.Helper()
	expectError(t, out)
	message, err := parseObject(out[0].Message)
	if err != nil || string(message["id"]) != string(id) || exists(message, "result") {
		t.Fatalf("requirements rejection lost its client identity: %s", out[0].Message)
	}
	if strings.Contains(string(out[0].Message), "requirements-secret-canary") {
		t.Fatal("requirements rejection disclosed backend data")
	}
	return message["error"]
}

func TestConfigRequirementsReadDisabledAndNotReady(t *testing.T) {
	r := testRouter(t, false)
	expectRequirementsError(t, client(t, r, `{"id":"disabled","method":"configRequirements/read"}`), raw("disabled"))
	if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
		t.Fatal("disabled requirements read retained request state")
	}
	cold, err := NewRouter(r.backends)
	if err != nil {
		t.Fatal(err)
	}
	cold.configRequirementsRead = true
	expectRequirementsError(t, client(t, cold, `{"id":"cold","method":"configRequirements/read"}`), raw("cold"))
	if len(cold.pending) != 0 {
		t.Fatal("requirements read reached children before initialization")
	}
}

func TestConfigRequirementsReadCanonicalParamsAndFullPolicy(t *testing.T) {
	for _, params := range []string{"", `,"params":null`, `,"params":{}`} {
		t.Run(params, func(t *testing.T) {
			r := requirementsRouter(t)
			requests := requirementsRequest(t, r, `{"id":9007199254740993,"method":"configRequirements/read"`+params+"}")
			want := raw(safeRequirementsFixture())
			if out := reply(t, r, requests[1], string(want)); len(out) != 0 {
				t.Fatal("returned requirements before both authorities replied")
			}
			// Whitespace and root key order differ without changing the policy.
			original, _ := parseObject(want)
			reordered := "{\n\"requirements\":" + string(original["requirements"]) + ",\n\"supportsIndependentSpeedModes\":true\n}"
			out := reply(t, r, requests[0], reordered)
			expectRequirementsResult(t, out, json.RawMessage("9007199254740993"), want)
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("completed requirements read retained active state")
			}
		})
	}
}

func TestConfigRequirementsReadRejectsUnknownParamsBeforeFanout(t *testing.T) {
	for _, params := range []string{
		`{"reload":false}`, `{"unknown":null}`, `{"requirements":null}`,
		`{"includeSecrets":false}`, `[]`, `true`, `1`, `"secret"`,
	} {
		t.Run(params, func(t *testing.T) {
			r := requirementsRouter(t)
			out := client(t, r, `{"id":"params","method":"configRequirements/read","params":`+params+"}")
			expectRequirementsError(t, out, raw("params"))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("unsupported parameters reached an authority")
			}
		})
	}
}

// Removing the cross-backend sameJSON check must make this test fail.
func TestConfigRequirementsRejectsDifferentBackends(t *testing.T) {
	for _, nullFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "different nonempty policies", true: "null versus object"}[nullFirst], func(t *testing.T) {
			r := requirementsRouter(t)
			first := safeRequirementsFixture()
			second := safeRequirementsFixture()
			policy, _ := child(second, "requirements")
			policy["allowedLoginMethods"] = raw([]string{"api"})
			second["requirements"] = raw(policy)
			if nullFirst {
				first["requirements"] = raw(nil)
			}
			requests := requirementsRequest(t, r, `{"id":"different","method":"configRequirements/read"}`)
			if out := reply(t, r, requests[0], string(raw(first))); len(out) != 0 {
				t.Fatal("returned first authority without comparison")
			}
			expectRequirementsError(t, reply(t, r, requests[1], string(raw(second))), raw("different"))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("different requirements retained active state")
			}
		})
	}
}

func TestConfigRequirementsReadRejectsMatchingSecretPolicies(t *testing.T) {
	for _, field := range []string{"modelProviders", "browserUse", "otel"} {
		t.Run(field, func(t *testing.T) {
			r := requirementsRouter(t)
			result := safeRequirementsFixture()
			policy, _ := child(result, "requirements")
			policy[field] = raw(map[string]any{"value": "requirements-secret-canary"})
			result["requirements"] = raw(policy)
			requests := requirementsRequest(t, r, `{"id":"secret","method":"configRequirements/read"}`)
			expectRequirementsError(t, reply(t, r, requests[0], string(raw(result))), raw("secret"))
			if out := reply(t, r, requests[1], string(raw(result))); len(out) != 0 {
				t.Fatal("matching secret-bearing policy was forwarded after rejection")
			}
		})
	}
}

func TestConfigRequirementsReadBackendErrorsAreStatic(t *testing.T) {
	var baseline json.RawMessage
	for _, backendError := range []string{
		`{"code":-32001,"message":"requirements-secret-canary","data":{"token":"requirements-secret-canary"}}`,
		`{"code":403,"message":"different backend failure","unexpected":"requirements-secret-canary"}`,
		`"requirements-secret-canary"`,
	} {
		r := requirementsRouter(t)
		requests := requirementsRequest(t, r, `{"id":"failure","method":"configRequirements/read"}`)
		message, _ := parseObject(requests[0].Message)
		out := server(t, r, requests[0].Backend, string(raw(object{"id": message["id"], "error": json.RawMessage(backendError)})))
		got := expectRequirementsError(t, out, raw("failure"))
		if baseline == nil {
			baseline = got
		} else if !sameJSON(baseline, got) {
			t.Fatal("backend error content changed the public error")
		}
		if out := reply(t, r, requests[1], string(raw(safeRequirementsFixture()))); len(out) != 0 {
			t.Fatal("late success escaped an abandoned error")
		}
	}
}

func TestConfigRequirementsReadRawDuplicatesNeverBecomeSuccess(t *testing.T) {
	for _, kind := range []string{"envelope id", "envelope result", "result key", "nested policy key"} {
		t.Run(kind, func(t *testing.T) {
			r := requirementsRouter(t)
			requests := requirementsRequest(t, r, `{"id":"duplicates","method":"configRequirements/read"}`)
			request, _ := parseObject(requests[0].Message)
			id := string(request["id"])
			valid := string(raw(safeRequirementsFixture()))
			wire := `{"id":` + id + `,"result":` + valid + "}"
			switch kind {
			case "envelope id":
				wire = `{"id":` + id + `,"id":` + id + `,"result":` + valid + "}"
			case "envelope result":
				wire = `{"id":` + id + `,"result":` + valid + `,"result":` + valid + "}"
			case "result key":
				wire = `{"id":` + id + `,"result":{"supportsIndependentSpeedModes":true,"supportsIndependentSpeedModes":true,"requirements":null}}`
			case "nested policy key":
				wire = strings.Replace(wire, `"allowRemoteControl":false`, `"allowRemoteControl":false,"allowRemoteControl":false`, 1)
			}
			expectRequirementsError(t, server(t, r, requests[0].Backend, wire), raw("duplicates"))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("malformed correlated response retained pending state")
			}
			if out := reply(t, r, requests[1], valid); len(out) != 0 {
				t.Fatal("late success followed a malformed correlated response")
			}
		})
	}
}

func TestConfigRequirementsReadWrongBackendIDIsFatal(t *testing.T) {
	r := requirementsRouter(t)
	requests := requirementsRequest(t, r, `{"id":"owner","method":"configRequirements/read"}`)
	request, _ := parseObject(requests[0].Message)
	wire := raw(object{"id": request["id"], "result": raw(safeRequirementsFixture())})
	if out, err := r.Server(requests[1].Backend, wire); err == nil || len(out) != 0 {
		t.Fatal("wrong authority answered another authority's requirements request")
	}
}

func TestConfigRequirementsReadFailedFanoutClearsStateAndBoundsTombstones(t *testing.T) {
	r := requirementsRouter(t)
	var late Delivery
	for i := 0; i < 300; i++ {
		requests := requirementsRequest(t, r, `{"id":"again","method":"configRequirements/read"}`)
		request, _ := parseObject(requests[0].Message)
		key, _ := idKey(request["id"])
		group := r.pending[key].group
		late = requests[1]
		out := server(t, r, requests[0].Backend, string(raw(object{"id": request["id"], "error": raw(map[string]any{"code": -32000, "message": "requirements-secret-canary"})})))
		expectRequirementsError(t, out, raw("again"))
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 || len(r.staleResponses) > 256 || len(r.staleResponseOrder) > 256 {
			t.Fatal("failed requirements fanout retained unbounded state")
		}
		if group.params != nil || group.results != nil || group.seen != nil {
			t.Fatal("abandoned requirements request retained response payload")
		}
	}
	if out := reply(t, r, late, string(raw(safeRequirementsFixture()))); len(out) != 0 {
		t.Fatal("late abandoned requirements response reached the client")
	}
}

func TestConfigRequirementsReadAlwaysRefreshesAfterPolicyOrAccountChange(t *testing.T) {
	r := requirementsRouter(t)
	initial := safeRequirementsFixture()
	updated := safeRequirementsFixture()
	policy, _ := child(updated, "requirements")
	policy["allowedLoginMethods"] = raw([]string{"api"})
	updated["requirements"] = raw(policy)
	for i, result := range []object{initial, updated, initial} {
		if i == 2 {
			server(t, r, "openai", `{"method":"account/updated","params":{"authMode":"api"}}`)
		}
		requests := requirementsRequest(t, r, `{"id":"fresh","method":"configRequirements/read"}`)
		if out := reply(t, r, requests[0], string(raw(result))); len(out) != 0 {
			t.Fatal("requirements cache answered before the second fresh response")
		}
		expectRequirementsResult(t, reply(t, r, requests[1], string(raw(result))), raw("fresh"), raw(result))
	}
}

func TestConfigRequirementsReadDoesNotRelaxInitializeGuards(t *testing.T) {
	for _, capability := range []string{"requestAttestation", "explicitGatewayOauth"} {
		r, err := NewRouter([]Backend{{ID: "first", Provider: "openai"}, {ID: "second", Provider: "aihub"}})
		if err != nil {
			t.Fatal(err)
		}
		r.configRequirementsRead = true
		out := client(t, r, `{"id":"capability","method":"initialize","params":{"capabilities":{"`+capability+`":true}}}`)
		expectRequirementsError(t, out, raw("capability"))
		if len(r.pending) != 0 {
			t.Fatal("requirements mode relaxed the unsupported initialize capability guard")
		}
	}
}

func TestConfigRequirementsReadMalformedResponsesUseGenericError(t *testing.T) {
	var baseline json.RawMessage
	for _, kind := range []string{"null result", "array result", "missing capability", "unknown field", "empty requirements", "depth", "size", "error and result"} {
		t.Run(kind, func(t *testing.T) {
			r := requirementsRouter(t)
			requests := requirementsRequest(t, r, `{"id":"malformed","method":"configRequirements/read"}`)
			request, _ := parseObject(requests[0].Message)
			result := "null"
			switch kind {
			case "array result":
				result = "[]"
			case "missing capability":
				result = `{"requirements":null}`
			case "unknown field":
				result = `{"supportsIndependentSpeedModes":true,"requirements":null,"unknown":"requirements-secret-canary"}`
			case "empty requirements":
				result = `{"supportsIndependentSpeedModes":true,"requirements":{}}`
			case "depth":
				result = `{"supportsIndependentSpeedModes":true,"requirements":` + strings.Repeat("[", 20) + `"requirements-secret-canary"` + strings.Repeat("]", 20) + "}"
			case "size", "error and result":
				result = string(raw(safeRequirementsFixture()))
			}
			wire := `{"id":` + string(request["id"]) + `,"result":` + result + "}"
			if kind == "size" {
				wire += strings.Repeat(" ", 64<<10)
			}
			if kind == "error and result" {
				wire = strings.TrimSuffix(wire, "}") + `,"error":{"message":"requirements-secret-canary"}}`
			}
			out := server(t, r, requests[0].Backend, wire)
			got := expectRequirementsError(t, out, raw("malformed"))
			if baseline == nil {
				baseline = got
			} else if !sameJSON(baseline, got) {
				t.Fatal("malformed backend content changed the public error")
			}
			if out := reply(t, r, requests[1], string(raw(safeRequirementsFixture()))); len(out) != 0 {
				t.Fatal("malformed response allowed a later success")
			}
		})
	}
}

func TestConfigRequirementsRejectsMixedBackendMessages(t *testing.T) {
	for _, kind := range []string{"thread result", "thread null result", "account result", "account error", "account null error"} {
		t.Run(kind, func(t *testing.T) {
			r := requirementsRouter(t)
			r.threads["known"] = "openai"
			requests := requirementsRequest(t, r, `{"id":"mixed","method":"configRequirements/read"}`)
			request, _ := parseObject(requests[0].Message)
			message := object{
				"method": raw("item/agentMessage/delta"),
				"params": raw(map[string]string{"threadId": "known", "delta": "fixture"}),
				"id":     request["id"],
				"result": raw(map[string]string{"token": "requirements-secret-canary"}),
			}
			switch kind {
			case "thread null result":
				message["result"] = raw(nil)
			case "account result", "account error", "account null error":
				delete(message, "id")
				message["method"] = raw("account/updated")
				message["params"] = raw(map[string]string{"authMode": "api"})
				if kind != "account result" {
					delete(message, "result")
					message["error"] = raw(map[string]string{"message": "requirements-secret-canary"})
				}
				if kind == "account null error" {
					message["error"] = raw(nil)
				}
			}
			out, err := r.Server("openai", raw(message))
			if err == nil || len(out) != 0 || strings.Contains(err.Error(), "requirements-secret-canary") {
				t.Fatal("method-bearing backend envelope escaped response validation")
			}
		})
	}
}

func requirementsResultAtSize(t *testing.T, size int) json.RawMessage {
	t.Helper()
	result := safeRequirementsFixture()
	policy, _ := child(result, "requirements")
	domains := map[string]string{}
	suffix := "." + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".invalid"
	for i := 0; i < 156; i++ {
		domains[strconv.Itoa(i)+suffix] = "deny"
	}
	encode := func() json.RawMessage {
		policy["application"] = raw(map[string]any{
			"network": map[string]any{"enabled": true, "domains": domains},
			"storage": nil,
		})
		result["requirements"] = raw(policy)
		return raw(result)
	}
	padding := size - len(encode())
	if padding < 0 || padding > 120 {
		t.Fatal("requirements size fixture cannot reach the requested boundary")
	}
	// Lengthen two host labels without exceeding the per-label or total
	// hostname limits. The complete policy remains independently valid.
	for i, label := range []string{"d", "e"} {
		added := min(padding, 60)
		delete(domains, strconv.Itoa(i)+suffix)
		domains[strings.Repeat(label, added+1)+suffix] = "deny"
		padding -= added
	}
	encoded := encode()
	if len(encoded) != size {
		t.Fatalf("requirements fixture is %d bytes, want %d", len(encoded), size)
	}
	if _, err := validateConfigRequirementsResponse(encoded); err != nil {
		t.Fatal("requirements budget fixture is not an independently valid policy")
	}
	return encoded
}

func TestConfigRequirementsReadBoundsCombinedAcceptedResults(t *testing.T) {
	for _, size := range []int{32 << 10, (32 << 10) + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			r := requirementsRouter(t)
			result := requirementsResultAtSize(t, size)
			requests := requirementsRequest(t, r, `{"id":"budget","method":"configRequirements/read"}`)
			if out := reply(t, r, requests[0], string(result)); len(out) != 0 {
				t.Fatal("individually valid response exhausted the combined budget")
			}
			out := reply(t, r, requests[1], string(result))
			if size == 32<<10 {
				expectRequirementsResult(t, out, raw("budget"), result)
			} else {
				// Equal valid views only two bytes above the shared cap must
				// fail because of the budget, not policy inequality or shape.
				expectRequirementsError(t, out, raw("budget"))
			}
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("requirements budget completion retained active state")
			}
		})
	}
}

func TestConfigRequirementsReadLateSiblingCannotAnswerReusedClientID(t *testing.T) {
	for _, failed := range []int{0, 1} {
		t.Run(strconv.Itoa(failed), func(t *testing.T) {
			r := requirementsRouter(t)
			old := requirementsRequest(t, r, `{"id":"reused","method":"configRequirements/read"}`)
			first, _ := parseObject(old[failed].Message)
			errorResponse := raw(object{
				"id":    first["id"],
				"error": raw(map[string]any{
					"code": -32000, "message": "requirements-secret-canary",
				}),
			})
			expectRequirementsError(t, server(t, r, old[failed].Backend, string(errorResponse)), raw("reused"))

			fresh := requirementsRequest(t, r, `{"id":"reused","method":"configRequirements/read"}`)
			updated := safeRequirementsFixture()
			policy, _ := child(updated, "requirements")
			policy["allowedLoginMethods"] = raw([]string{"api"})
			updated["requirements"] = raw(policy)
			if out := reply(t, r, fresh[failed], string(raw(updated))); len(out) != 0 {
				t.Fatal("reused client ID completed before both fresh replies")
			}
			activeBytes := r.activeBytes
			if out := reply(t, r, old[1-failed], string(raw(safeRequirementsFixture()))); len(out) != 0 {
				t.Fatal("old sibling answered a new request reusing the client ID")
			}
			if len(r.pending) != 1 || len(r.active) != 1 || !r.active["s:reused"] || r.activeBytes != activeBytes {
				t.Fatal("late old sibling changed the new request's active state")
			}
			expectRequirementsResult(t, reply(t, r, fresh[1-failed], string(raw(updated))), raw("reused"), raw(updated))
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("reused client ID retained state after fresh completion")
			}
		})
	}
}
