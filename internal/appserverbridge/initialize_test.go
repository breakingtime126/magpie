package appserverbridge

import (
	"encoding/json"
	"testing"
)

// The pinned native servers commit session readiness before their initialize
// reply; initialized is only a logged notification. These synthetic fixtures
// exercise that ordering without claiming native runtime or auth acceptance.
func initializationRouter(t *testing.T, mode string) *Router {
	t.Helper()
	r, err := NewRouter([]Backend{{ID: "openai", Provider: "openai"}, {ID: "aihub", Provider: "aihub"}})
	if err != nil {
		t.Fatal(err)
	}
	r.configRequirementsRead = true
	if mode != "default" {
		r.auth = authCompatibility{attestationUnavailable: true, gatewayRead: true, gatewayProviderName: "OpenAI"}
	}
	if mode == "0.167" {
		r.configRequirementsNull167 = true
		r.auth.rejectHumanApproval = true
	}
	return r
}

func initializationDenied(t *testing.T, r *Router) {
	t.Helper()
	for _, method := range []string{"account/read", "model/list", "configRequirements/read"} {
		out := client(t, r, string(raw(object{"id": raw("before-ready"), "method": raw(method)})))
		expectError(t, out)
		message, _ := parseObject(out[0].Message)
		failure, _ := child(message, "error")
		if string(failure["code"]) != "-32600" {
			t.Fatalf("%s did not stay behind initialization: %s", method, out[0].Message)
		}
	}
}

func initializedPassthrough(t *testing.T, r *Router) {
	t.Helper()
	message := `{"method":"initialized","params":{}}`
	out := client(t, r, message)
	if len(out) != 2 {
		t.Fatalf("real initialized notification did not reach both children: %v", out)
	}
	for i, delivery := range out {
		if delivery.Backend != r.backends[i].ID || !sameJSON(delivery.Message, []byte(message)) {
			t.Fatalf("initialized notification changed: %s", delivery.Message)
		}
	}
}

func TestInitializeReadyWithoutNotification(t *testing.T) {
	for _, mode := range []string{"default", "0.166", "0.167"} {
		for _, id := range []string{`"initialize"`, `""`, `"escaped\u0020id"`, "0", "-1", "9007199254740993", "9223372036854775807", "-9223372036854775808"} {
			for _, first := range []int{0, 1} {
				t.Run(mode+"/"+id+"/"+string(rune('0'+first)), func(t *testing.T) {
					r := initializationRouter(t, mode)
					initializationDenied(t, r)
					params := json.RawMessage(`{"clientInfo":{"name":"initialize-fixture","version":"1"},"capabilities":{"experimentalApi":true,"requestAttestation":false,"explicitGatewayOauth":false,"extensions":{"fixture":{"preserved":true}},"optOutNotificationMethods":["fixture/notice"]}}`)
					requests := client(t, r, string(raw(object{"id": json.RawMessage(id), "method": raw("initialize"), "params": params})))
					if len(requests) != 2 {
						t.Fatal("initialize did not fan out to both children")
					}
					for _, delivery := range requests {
						message, _ := parseObject(delivery.Message)
						if !sameJSON(message["params"], params) {
							t.Fatal("initialize changed capabilities or client identity")
						}
					}
					initializationDenied(t, r)
					results := []string{`{"userAgent":"primary-fixture","platformFamily":"unix","platformOs":"linux","fixtureMetadata":{"preserved":true}}`, `{"userAgent":"secondary-fixture"}`}
					if out := reply(t, r, requests[first], results[first]); len(out) != 0 {
						t.Fatal("initialize returned before both native replies")
					}
					initializationDenied(t, r)
					out := reply(t, r, requests[1-first], results[1-first])
					if len(out) != 1 || out[0].Backend != "" {
						t.Fatalf("completion emitted extra or synthetic child traffic: %v", out)
					}
					message, _ := parseObject(out[0].Message)
					if string(message["id"]) != id || !sameJSON(message["result"], []byte(results[0])) || exists(message, "error") {
						t.Fatalf("native initialize result or ID changed: %s", out[0].Message)
					}
					// This is the behavioral regression: old code returns -32600
					// here, despite both real initialize responses succeeding.
					read := client(t, r, `{"id":"read","method":"account/read","params":{"refreshToken":false}}`)
					if len(read) != 1 || read[0].Backend != "openai" {
						t.Fatalf("account/read blocked after both initialize responses without initialized: %v", read)
					}
					account := `{"account":null,"requiresOpenaiAuth":true}`
					answered := reply(t, r, read[0], account)
					front, _ := parseObject(answered[0].Message)
					if !sameJSON(front["result"], []byte(account)) {
						t.Fatal("account result changed")
					}
					requirements := client(t, r, `{"id":"policy","method":"configRequirements/read"}`)
					if len(requirements) != 2 {
						t.Fatal("requirements did not reach both ready authorities")
					}
					if len(reply(t, r, requirements[0], requirements167Fixture)) != 0 {
						t.Fatal("requirements returned before both replies")
					}
					expectRequirementsResult(t, reply(t, r, requirements[1], requirements167Fixture), raw("policy"), []byte(requirements167Fixture))
					for _, method := range []string{"getAuthStatus", "account/login/start", "fixture/unknown"} {
						rejected := client(t, r, string(raw(object{"id": raw("unsupported"), "method": raw(method)})))
						expectError(t, rejected)
						envelope, _ := parseObject(rejected[0].Message)
						failure, _ := child(envelope, "error")
						if string(failure["code"]) != "-32601" {
							t.Fatalf("unsupported RPC changed: %s", rejected[0].Message)
						}
					}
					expectError(t, client(t, r, `{"id":"again","method":"initialize"}`))
					if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
						t.Fatal("completed initialization or reads retained active state")
					}
				})
			}
		}
	}
}

func TestInitializedNotificationsNeverGrantReadiness(t *testing.T) {
	r := initializationRouter(t, "0.167")
	for i := 0; i < 2; i++ {
		initializedPassthrough(t, r)
		initializationDenied(t, r)
	}
	requests := client(t, r, `{"id":1,"method":"initialize"}`)
	for responses := 0; responses < 2; responses++ {
		for i := 0; i < 2; i++ {
			initializedPassthrough(t, r)
			initializationDenied(t, r)
		}
		out := reply(t, r, requests[responses], `{"userAgent":"fixture"}`)
		if len(out) != responses {
			t.Fatal("notification changed the two-response initialization gate")
		}
	}
	for i := 0; i < 2; i++ {
		initializedPassthrough(t, r)
		read := client(t, r, `{"id":"late-read","method":"account/read","params":{"refreshToken":false}}`)
		if len(read) != 1 || read[0].Backend != "openai" {
			t.Fatal("delayed or duplicate initialized broke ready connection")
		}
		reply(t, r, read[0], `{"account":null,"requiresOpenaiAuth":true}`)
	}
	if _, err := r.Client([]byte(`{"method":"fixture/unknown"}`)); err == nil {
		t.Fatal("unreviewed client notification accepted")
	}
}

func TestInitializeFailureNeverBecomesReady(t *testing.T) {
	for _, failed := range []int{0, 1} {
		for _, siblingFirst := range []bool{false, true} {
			r := initializationRouter(t, "0.167")
			requests := client(t, r, `{"id":9007199254740993,"method":"initialize"}`)
			if siblingFirst {
				reply(t, r, requests[1-failed], `{"userAgent":"fixture"}`)
			}
			request, _ := parseObject(requests[failed].Message)
			nativeError := json.RawMessage(`{"code":-32017,"message":"native initialization denied","data":{"fixture":"preserved"}}`)
			out := server(t, r, requests[failed].Backend, string(raw(object{"id": request["id"], "error": nativeError})))
			expectError(t, out)
			response, _ := parseObject(out[0].Message)
			if string(response["id"]) != "9007199254740993" || !sameJSON(response["error"], nativeError) {
				t.Fatal("genuine initialize failure was rewritten")
			}
			if !siblingFirst && len(reply(t, r, requests[1-failed], `{"userAgent":"late fixture"}`)) != 0 {
				t.Fatal("late successful sibling revived failed initialization")
			}
			initializedPassthrough(t, r)
			initializationDenied(t, r)
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("failed initialize retained active fanout state")
			}
		}
	}
}

func TestInitializeMalformedResponseNeverBecomesReady(t *testing.T) {
	for _, tail := range []string{`"result":null`, `"result":[]`, `"other":{}`, `"result":{},"error":{"code":-32000,"message":"fixture"}`} {
		r := initializationRouter(t, "0.167")
		requests := client(t, r, `{"id":1,"method":"initialize"}`)
		reply(t, r, requests[0], `{"userAgent":"fixture"}`)
		request, _ := parseObject(requests[1].Message)
		if out, err := r.Server(requests[1].Backend, []byte(`{"id":`+string(request["id"])+`,`+tail+`}`)); err == nil || len(out) != 0 {
			t.Fatal("malformed initialize response became success")
		}
		initializationDenied(t, r)
	}
}

func TestInitializePreResponseAuthNotificationsRemainGated(t *testing.T) {
	for _, successes := range []int{0, 1} {
		r := initializationRouter(t, "0.167")
		requests := client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{"requestAttestation":true,"explicitGatewayOauth":true}}}`)
		if successes == 1 {
			reply(t, r, requests[0], `{"userAgent":"fixture"}`)
		}
		initializedPassthrough(t, r)
		for _, wire := range []string{
			`{"id":7,"method":"attestation/generate","params":{}}`,
			`{"method":"account/gatewayOAuth/changed","params":{"providerId":"openai","status":"notReady","authUrl":null,"error":null}}`,
		} {
			if out, err := r.Server("openai", []byte(wire)); err == nil || len(out) != 0 {
				t.Fatal("pre-response auth traffic bypassed readiness gate")
			}
		}
		initializationDenied(t, r)
	}
}
