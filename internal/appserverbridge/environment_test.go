package appserverbridge

import (
	"bytes"
	"testing"
)

func TestLocalEnvironmentQueriesRequireMatchingBackendResults(t *testing.T) {
	for _, tc := range []struct{ method, first, second string }{
		{"environment/status", `{"status":"ready"}`, `{"status":"ready"}`},
		{"environment/status", `{"status":"pending"}`, `{"status":"pending"}`},
		{"environment/status", `{"status":"disconnected","error":"fixture"}`, `{"error":"fixture","status":"disconnected"}`},
		{"environment/info", `{"cwd":"file:///C:/work","shell":{"path":"powershell.exe","name":"powershell"}}`, `{"shell":{"name":"powershell","path":"powershell.exe"},"cwd":"file:///C:/work"}`},
		{"environment/info", `{"cwd":null,"shell":{"name":"bash","path":"/bin/bash"}}`, `{"shell":{"name":"bash","path":"/bin/bash"},"cwd":null}`},
	} {
		t.Run(tc.method+tc.first, func(t *testing.T) {
			r := testRouter(t, true)
			out := client(t, r, `{"id":9007199254740993,"method":"`+tc.method+`","params":{"environmentId":"local"}}`)
			if len(out) != 2 || out[0].Backend != "openai" || out[1].Backend != "aihub" {
				t.Fatalf("query must inspect both children: %v", out)
			}
			for _, d := range out {
				m, _ := parseObject(d.Message)
				if string(m["params"]) != `{"environmentId":"local"}` {
					t.Fatal("environment selection changed", string(d.Message))
				}
			}
			if response := reply(t, r, out[1], tc.second); len(response) != 0 {
				t.Fatal("returned before both backends answered")
			}
			response := reply(t, r, out[0], tc.first)
			m, _ := parseObject(response[0].Message)
			if response[0].Backend != "" || string(m["id"]) != "9007199254740993" || !sameJSON(m["result"], []byte(tc.first)) {
				t.Fatal("shared result or request id changed", string(response[0].Message))
			}
		})
	}
}

func TestEnvironmentAmbiguityDoesNotChoosePrimaryOrLastModel(t *testing.T) {
	for _, tc := range []struct{ method, first, second string }{
		{"environment/status", `{"status":"ready"}`, `{"status":"disconnected","error":"no access"}`},
		{"environment/status", `{"status":"ready","extra":9007199254740992}`, `{"status":"ready","extra":9007199254740993}`},
		{"environment/info", `{"cwd":"file:///private/first","shell":{"name":"bash","path":"/bin/bash"}}`, `{"cwd":"file:///private/second","shell":{"name":"bash","path":"/bin/bash"}}`},
		{"environment/info", `{"cwd":null,"shell":{"name":"bash","path":"/bin/bash"}}`, `{"cwd":"file:///work","shell":{"name":"bash","path":"/bin/bash"}}`},
	} {
		r := testRouter(t, true)
		start(t, r, "aihub", "latest", "deepseek-flash-ft")
		out := client(t, r, `{"id":2,"method":"`+tc.method+`","params":{"environmentId":"local"}}`)
		reply(t, r, out[0], tc.first)
		response := reply(t, r, out[1], tc.second)
		expectError(t, response)
		if !bytes.Contains(response[0].Message, []byte("differs between backends")) || len(r.pending) != 0 || r.activeBytes != 0 {
			t.Fatal("ambiguous environment synthesized or retained", response)
		}
	}
}

func TestEnvironmentQueriesRejectUnscopedRemoteAndUnknownFields(t *testing.T) {
	for _, message := range []string{
		`{"id":2,"method":"environment/info","params":{"environmentId":"remote"}}`,
		`{"id":2,"method":"environment/status","params":{"environmentId":"magpie/aihub/local"}}`,
		`{"id":2,"method":"environment/info","params":{"environmentId":"local","threadId":"known"}}`,
		`{"id":2,"method":"environment/info","params":{"environmentId":null}}`,
		`{"id":2,"method":"environment/status/info","params":{"environmentId":"local"}}`,
	} {
		r := testRouter(t, true)
		expectError(t, client(t, r, message))
		if len(r.pending) != 0 {
			t.Fatal("unsupported environment query reached a child")
		}
	}
}

func TestEnvironmentFailureAndLateSiblingAreNotMasked(t *testing.T) {
	r := testRouter(t, true)
	out := client(t, r, `{"id":2,"method":"environment/info","params":{"environmentId":"local"}}`)
	m, _ := parseObject(out[0].Message)
	response := server(t, r, out[0].Backend, string(raw(object{"id": m["id"], "error": raw(map[string]any{"code": -32600, "message": "experimental API not enabled"})})))
	expectError(t, response)
	if !bytes.Contains(response[0].Message, []byte("experimental API not enabled")) {
		t.Fatal("native experimental gating failure changed")
	}
	if late := reply(t, r, out[1], `{"shell":{"name":"bash","path":"/bin/bash"},"cwd":null}`); len(late) != 0 {
		t.Fatal("late success overrode error")
	}
	if len(r.pending) != 0 || r.activeBytes != 0 {
		t.Fatal("failed environment query retained request")
	}
}

func TestMalformedEnvironmentResultsFailClosed(t *testing.T) {
	for _, tc := range []struct{ method, result string }{
		{"environment/info", `{"shell":{"name":"bash","path":null},"cwd":null}`},
		{"environment/info", `{"shell":{"name":"bash","path":"/bin/bash"}}`},
		{"environment/status", `{"status":"ready","error":null}`},
		{"environment/status", `{"status":"invented"}`},
	} {
		r := testRouter(t, true)
		out := client(t, r, `{"id":2,"method":"`+tc.method+`","params":{"environmentId":"local"}}`)
		m, _ := parseObject(out[0].Message)
		result, _ := parseObject([]byte(tc.result))
		if _, err := r.Server(out[0].Backend, raw(object{"id": m["id"], "result": raw(result)})); err == nil {
			t.Fatal("malformed environment result accepted", tc)
		}
	}
}

func TestEnvironmentConnectionNotificationsFollowThreadOwnership(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "openai", "one", "gpt")
	start(t, r, "aihub", "two", "deepseek-flash-ft")
	for _, tc := range []struct{ backend, thread, method string }{
		{"openai", "one", "thread/environment/connected"},
		{"aihub", "two", "thread/environment/disconnected"},
	} {
		message := `{"method":"` + tc.method + `","params":{"threadId":"` + tc.thread + `","environmentId":"local"}}`
		out := server(t, r, tc.backend, message)
		if len(out) != 1 || out[0].Backend != "" || !sameJSON(out[0].Message, []byte(message)) {
			t.Fatal("thread environment notification changed", out)
		}
	}
	if _, err := r.Server("aihub", []byte(`{"method":"thread/environment/connected","params":{"threadId":"one","environmentId":"local"}}`)); err == nil {
		t.Fatal("same native environment id bypassed thread ownership")
	}
}

func TestEnvironmentResultLimitReleasesAndTombstonesSibling(t *testing.T) {
	r := testRouter(t, true)
	out := client(t, r, `{"id":2,"method":"environment/status","params":{"environmentId":"local"}}`)
	response := reply(t, r, out[0], string(raw(map[string]any{"status": "unknown", "error": string(bytes.Repeat([]byte{'x'}, 64<<10))})))
	expectError(t, response)
	if len(r.pending) != 0 || r.activeBytes != 0 {
		t.Fatal("over-limit response retained request")
	}
	// Reusing the finished client ID creates fresh transport IDs. The old
	// sibling's success must not answer the new request or override the error.
	next := client(t, r, `{"id":2,"method":"environment/status","params":{"environmentId":"local"}}`)
	if late := reply(t, r, out[1], `{"status":"ready"}`); len(late) != 0 {
		t.Fatal("late sibling answered a reused client request id")
	}
	if len(r.pending) != 2 {
		t.Fatal("late sibling changed current request")
	}
	reply(t, r, next[1], `{"status":"pending"}`)
	final := reply(t, r, next[0], `{"status":"pending"}`)
	m, _ := parseObject(final[0].Message)
	if string(m["result"]) != `{"status":"pending"}` || string(m["id"]) != "2" {
		t.Fatal("current request result changed")
	}
}
