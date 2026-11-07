package appserverbridge

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Synthetic messages only: these tests never launch an app-server or mutate
// persisted history. beforeTurnId comes from native ThreadRevertParams.
func revertRouter(t *testing.T, controlPlane bool) *Router {
	t.Helper()
	r := testRouter(t, true)
	if controlPlane {
		if err := r.enableControlPlane167(); err != nil {
			t.Fatal(err)
		}
	}
	start(t, r, "openai", "thread-a", "gpt")
	start(t, r, "openai", "thread-a2", "gpt")
	start(t, r, "aihub", "thread-b", "deepseek-flash-ft")
	for _, thread := range []string{"thread-a", "thread-a2", "thread-b"} {
		out := client(t, r, string(raw(map[string]any{
			"id":     "discover-" + thread,
			"method": "thread/turns/list",
			"params": map[string]any{"threadId": thread},
		})))
		if len(out) != 1 || out[0].Backend == "" {
			t.Fatalf("turn discovery did not reach an owner: %v", out)
		}
		out = reply(t, r, out[0], string(raw(map[string]any{
			"data":       []any{map[string]any{"id": "turn-" + thread, "items": []any{}}},
			"nextCursor": nil,
		})))
		if len(out) != 1 || out[0].Backend != "" {
			t.Fatalf("turn discovery response: %v", out)
		}
	}
	return r
}

func TestThreadRevertBeforeTurnIDRoutesToOwner(t *testing.T) {
	for _, controlPlane := range []bool{false, true} {
		for _, backend := range []string{"openai", "aihub"} {
			name := backend
			if controlPlane {
				name += "/control-plane"
			}
			t.Run(name, func(t *testing.T) {
				r := revertRouter(t, controlPlane)
				thread := "thread-a"
				if backend == "aihub" {
					thread = "thread-b"
				}
				params := raw(map[string]any{"threadId": thread, "beforeTurnId": "turn-" + thread})
				id := json.RawMessage(`9007199254740993`)
				out := client(t, r, string(raw(object{"id": id, "method": raw("thread/revert"), "params": params})))
				if len(out) != 1 || out[0].Backend != backend {
					t.Fatalf("native beforeTurnId did not reach its owner: %v", out)
				}
				request, err := parseObject(out[0].Message)
				if err != nil || text(request, "method") != "thread/revert" || !sameJSON(request["params"], params) {
					t.Fatalf("revert params changed: %s, %v", out[0].Message, err)
				}
				// Native ThreadRevertResponse returns empty turns and opaque cursors.
				out = reply(t, r, out[0], string(raw(map[string]any{
					"thread":               map[string]any{"id": thread, "modelProvider": backend, "turns": []any{}},
					"turnsBackwardsCursor": "opaque-turn-cursor",
					"itemsBackwardsCursor": "opaque-item-cursor",
				})))
				if len(out) != 1 || out[0].Backend != "" {
					t.Fatalf("revert response delivery: %v", out)
				}
				response, _ := parseObject(out[0].Message)
				result, _ := child(response, "result")
				returnedThread, _ := child(result, "thread")
				provider := backend
				if controlPlane {
					provider = controlPlaneProvider167
				}
				if !bytes.Equal(response["id"], id) || text(returnedThread, "id") != thread ||
					text(returnedThread, "modelProvider") != provider || !sameJSON(returnedThread["turns"], []byte(`[]`)) ||
					text(result, "turnsBackwardsCursor") != "opaque-turn-cursor" || text(result, "itemsBackwardsCursor") != "opaque-item-cursor" {
					t.Fatalf("revert response changed: %s", out[0].Message)
				}
				notification := raw(map[string]any{"method": "thread/reverted", "params": map[string]any{"threadId": thread}})
				out = server(t, r, backend, string(notification))
				if len(out) != 1 || out[0].Backend != "" || !sameJSON(out[0].Message, notification) {
					t.Fatalf("reverted notification changed: %v", out)
				}
				foreign := "aihub"
				if backend == foreign {
					foreign = "openai"
				}
				if out, err := r.Server(foreign, notification); err == nil || len(out) != 0 {
					t.Fatalf("foreign reverted notification accepted: %v, %v", out, err)
				}
			})
		}
	}
}

func TestThreadRevertInvalidBoundaryFailsBeforeSend(t *testing.T) {
	cases := []struct {
		name, params, message string
	}{
		{"missing-thread", `{"beforeTurnId":"turn-thread-a"}`, "threadId is required"},
		{"null-thread", `{"threadId":null,"beforeTurnId":"turn-thread-a"}`, "threadId is required"},
		{"malformed-thread", `{"threadId":7,"beforeTurnId":"turn-thread-a"}`, "threadId is required"},
		{"missing", `{"threadId":"thread-a"}`, "beforeTurnId is required"},
		{"null", `{"threadId":"thread-a","beforeTurnId":null}`, "beforeTurnId is required"},
		{"empty", `{"threadId":"thread-a","beforeTurnId":""}`, "invalid beforeTurnId"},
		{"number", `{"threadId":"thread-a","beforeTurnId":7}`, "invalid beforeTurnId"},
		{"boolean", `{"threadId":"thread-a","beforeTurnId":false}`, "invalid beforeTurnId"},
		{"array", `{"threadId":"thread-a","beforeTurnId":[]}`, "invalid beforeTurnId"},
		{"object", `{"threadId":"thread-a","beforeTurnId":{}}`, "invalid beforeTurnId"},
		{"unknown-turn", `{"threadId":"thread-a","beforeTurnId":"unknown"}`, "unknown turn or turn belongs to another thread"},
		{"other-thread", `{"threadId":"thread-a","beforeTurnId":"turn-thread-a2"}`, "unknown turn or turn belongs to another thread"},
		{"other-provider", `{"threadId":"thread-a","beforeTurnId":"turn-thread-b"}`, "unknown turn or turn belongs to another thread"},
		{"unknown-thread", `{"threadId":"unknown","beforeTurnId":"turn-thread-a"}`, "unknown or ambiguous thread; list its owning backend's threads first"},
		{"old-spelling", `{"threadId":"thread-a","turnId":"turn-thread-a"}`, "unsupported thread/revert field: turnId"},
		{"both-spellings", `{"threadId":"thread-a","beforeTurnId":"turn-thread-a","turnId":"turn-thread-a"}`, "unsupported thread/revert field: turnId"},
		{"provider-override", `{"threadId":"thread-a","beforeTurnId":"turn-thread-a","modelProvider":"aihub"}`, "unsupported thread/revert field: modelProvider"},
		{"model-override", `{"threadId":"thread-a","beforeTurnId":"turn-thread-a","model":"magpie/aihub/deepseek-flash-ft"}`, "unsupported thread/revert field: model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, controlPlane := range []bool{false, true} {
				r := revertRouter(t, controlPlane)
				out := client(t, r, `{"id":"reject","method":"thread/revert","params":`+tc.params+`}`)
				expectError(t, out)
				response, _ := parseObject(out[0].Message)
				failure, _ := child(response, "error")
				if string(failure["code"]) != "-32602" || text(failure, "message") != tc.message || text(response, "id") != "reject" {
					t.Fatalf("wrong local error: %s", out[0].Message)
				}
				if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
					t.Fatal("rejected revert acquired backend request state")
				}
			}
		})
	}
}

func TestThreadRevertNativeErrorNeverRetries(t *testing.T) {
	r := revertRouter(t, false)
	out := client(t, r, `{"id":"revert-denied","method":"thread/revert","params":{"threadId":"thread-b","beforeTurnId":"turn-thread-b"}}`)
	if len(out) != 1 || out[0].Backend != "aihub" {
		t.Fatalf("revert request routing: %v", out)
	}
	request, _ := parseObject(out[0].Message)
	// Representative native refusal, not a claim of a captured live response.
	failure := raw(map[string]any{"code": -32600, "message": "thread/revert only supports paginated threads", "data": map[string]any{"fixture": true}})
	out = server(t, r, "aihub", string(raw(object{"id": request["id"], "error": failure})))
	expectError(t, out)
	response, _ := parseObject(out[0].Message)
	if text(response, "id") != "revert-denied" || !sameJSON(response["error"], failure) || exists(response, "result") {
		t.Fatalf("native error was changed: %s", out[0].Message)
	}
	if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
		t.Fatal("native refusal retained or retried the request")
	}
}

func TestThreadRevertDoesNotRenameSiblingBoundaries(t *testing.T) {
	for _, method := range []string{"thread/items/list", "turn/interrupt"} {
		t.Run(method, func(t *testing.T) {
			r := revertRouter(t, false)
			params := raw(map[string]any{"threadId": "thread-a", "turnId": "turn-thread-a"})
			out := client(t, r, string(raw(object{"id": raw("sibling"), "method": raw(method), "params": params})))
			if len(out) != 1 || out[0].Backend != "openai" {
				t.Fatalf("sibling turnId stopped routing: %v", out)
			}
			request, _ := parseObject(out[0].Message)
			if !sameJSON(request["params"], params) {
				t.Fatalf("sibling turnId changed: %s", out[0].Message)
			}
			expectError(t, client(t, r, string(raw(map[string]any{
				"id":     "wrong-sibling",
				"method": method,
				"params": map[string]any{"threadId": "thread-a", "beforeTurnId": "turn-thread-a"},
			}))))
		})
	}
}

func TestThreadRevertSharedHistoricalTurnKeepsThreadScope(t *testing.T) {
	r := revertRouter(t, false)
	for _, thread := range []string{"thread-a", "thread-a2"} {
		// Fork histories can contain the same native turn ID in both threads.
		out := client(t, r, string(raw(map[string]any{
			"id":     "shared-" + thread,
			"method": "thread/turns/list",
			"params": map[string]any{"threadId": thread},
		})))
		reply(t, r, out[0], `{"data":[{"id":"shared-turn","items":[]}],"nextCursor":null}`)
		out = client(t, r, string(raw(map[string]any{
			"id":     "revert-" + thread,
			"method": "thread/revert",
			"params": map[string]any{"threadId": thread, "beforeTurnId": "shared-turn"},
		})))
		if len(out) != 1 || out[0].Backend != "openai" {
			t.Fatalf("shared historical turn lost its thread membership: %v", out)
		}
		reply(t, r, out[0], string(raw(map[string]any{
			"thread": map[string]any{"id": thread, "modelProvider": "openai", "turns": []any{}},
		})))
	}
	expectError(t, client(t, r, `{"id":"foreign-shared","method":"thread/revert","params":{"threadId":"thread-b","beforeTurnId":"shared-turn"}}`))
}

func TestThreadRevertResponseKeepsOwnershipChecks(t *testing.T) {
	for _, controlPlane := range []bool{false, true} {
		for _, tc := range []struct {
			name, backend, thread, provider string
		}{
			{"wrong-backend", "openai", "thread-b", "aihub"},
			{"wrong-thread", "aihub", "thread-a", "aihub"},
			{"wrong-provider", "aihub", "thread-b", "openai"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := revertRouter(t, controlPlane)
				out := client(t, r, `{"id":"revert","method":"thread/revert","params":{"threadId":"thread-b","beforeTurnId":"turn-thread-b"}}`)
				if len(out) != 1 || out[0].Backend != "aihub" {
					t.Fatalf("revert request routing: %v", out)
				}
				request, _ := parseObject(out[0].Message)
				result := raw(map[string]any{"thread": map[string]any{"id": tc.thread, "modelProvider": tc.provider, "turns": []any{}}})
				out, err := r.Server(tc.backend, raw(object{"id": request["id"], "result": result}))
				if err == nil || len(out) != 0 {
					t.Fatalf("foreign revert result accepted: %v, %v", out, err)
				}
			})
		}
	}
}
