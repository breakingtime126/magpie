package appserverbridge

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPermissionEnvironmentFieldsStayOnThreadOwner(t *testing.T) {
	for _, method := range []string{"thread/start", "thread/resume", "thread/fork", "turn/start"} {
		for _, backend := range []string{"openai", "aihub"} {
			t.Run(method+"/"+backend, func(t *testing.T) {
				r := testRouter(t, true)
				model := "gpt"
				if backend == "aihub" {
					model = "deepseek-flash-ft"
				}
				start(t, r, backend, "owned", model)
				p := object{"permissions": raw("company-restricted"), "runtimeWorkspaceRoots": raw([]string{`C:\work\selected`, "/workspace/selected"}), "approvalPolicy": raw(map[string]any{"granular": map[string]bool{"sandbox_approval": true, "rules": false, "skill_approval": true, "request_permissions": true, "mcp_elicitations": false}})}
				if method == "thread/start" {
					p["model"] = raw(alias(backend, model))
				} else {
					p["threadId"] = raw("owned")
				}
				if method == "thread/start" || method == "turn/start" {
					p["environments"] = raw([]object{{"environmentId": raw("local"), "cwd": raw(`C:\work\selected`), "runtimeWorkspaceRoots": raw([]string{`C:\work\selected`, `C:\work\other`})}})
				}
				if method == "turn/start" {
					p["input"] = raw([]any{})
				}
				out := client(t, r, string(raw(object{"id": raw("permission-request"), "method": raw(method), "params": raw(p)})))
				if len(out) != 1 || out[0].Backend != backend {
					t.Fatalf("not routed only to owner: %v", out)
				}
				m, _ := parseObject(out[0].Message)
				got, _ := child(m, "params")
				for _, field := range []string{"permissions", "runtimeWorkspaceRoots", "environments", "approvalPolicy"} {
					if !bytes.Equal(got[field], p[field]) {
						t.Fatalf("%s changed: %s -> %s", field, p[field], got[field])
					}
				}
				if exists(got, "sandbox") || exists(got, "sandboxPolicy") {
					t.Fatal("bridge invented sandbox policy for named profile")
				}
				failure := server(t, r, backend, string(raw(object{"id": m["id"], "error": raw(map[string]any{"code": -32602, "message": "permissions profile is unavailable", "data": map[string]any{"profile": "company-restricted"}})})))
				if len(failure) != 1 || failure[0].Backend != "" || !bytes.Contains(failure[0].Message, []byte("permissions profile is unavailable")) {
					t.Fatal("backend denial was changed or rerouted", failure)
				}
			})
		}
	}
}

func TestPermissionEnvironmentNullEmptyAndAbsent(t *testing.T) {
	for _, fields := range []string{`{}`, `{"permissions":null,"runtimeWorkspaceRoots":null,"environments":null}`, `{"runtimeWorkspaceRoots":[],"environments":[]}`} {
		r := testRouter(t, true)
		p, _ := parseObject([]byte(fields))
		p["model"] = raw("magpie/openai/gpt")
		out := client(t, r, string(raw(object{"id": raw(2), "method": raw("thread/start"), "params": raw(p)})))
		if len(out) != 1 || out[0].Backend != "openai" {
			t.Fatal(out)
		}
		m, _ := parseObject(out[0].Message)
		got, _ := child(m, "params")
		for _, field := range []string{"permissions", "runtimeWorkspaceRoots", "environments"} {
			if !bytes.Equal(got[field], p[field]) {
				t.Fatalf("%s presence or value changed: %s -> %s", field, p[field], got[field])
			}
		}
	}
}

func TestPermissionEnvironmentInvalidFieldsFailLocally(t *testing.T) {
	for _, fields := range []string{
		`{"permissions":{}}`, `{"permissions":""}`, `{"permissions":"restricted","sandbox":"read-only"}`,
		`{"runtimeWorkspaceRoots":"/workspace"}`, `{"runtimeWorkspaceRoots":[7]}`,
		`{"environments":{}}`, `{"environments":[null]}`, `{"environments":[{"environmentId":"local"}]}`,
		`{"environments":[{"environmentId":"local","cwd":"/work","runtimeWorkspaceRoots":false}]}`,
		`{"environments":[{"environmentId":"local","cwd":"/work","config":{"model_provider":"other"}}]}`,
	} {
		r := testRouter(t, true)
		p, _ := parseObject([]byte(fields))
		p["model"] = raw("magpie/openai/gpt")
		expectError(t, client(t, r, string(raw(object{"id": raw(2), "method": raw("thread/start"), "params": raw(p)}))))
	}
	r := testRouter(t, true)
	start(t, r, "aihub", "owned", "deepseek-flash-ft")
	expectError(t, client(t, r, `{"id":2,"method":"turn/start","params":{"threadId":"owned","input":[],"permissions":"restricted","sandboxPolicy":{"type":"readOnly"}}}`))
	expectError(t, client(t, r, `{"id":3,"method":"turn/start","params":{"threadId":"owned","input":[],"permissions":"restricted","model":"magpie/openai/gpt","environments":[]}}`))
	expectError(t, client(t, r, `{"id":4,"method":"thread/resume","params":{"threadId":"owned","environments":[]}}`))
}

func TestPermissionApprovalAndProfileReplyPreserved(t *testing.T) {
	r := testRouter(t, true)
	start(t, r, "aihub", "owned", "deepseek-flash-ft")
	params := json.RawMessage(`{"threadId":"owned","turnId":"turn","itemId":"item","startedAtMs":1,"cwd":"C:\\work","reason":"fixture","permissions":{"network":{"enabled":false},"fileSystem":{"write":["C:\\work"]}}}`)
	out := server(t, r, "aihub", string(raw(object{"id": raw(8), "method": raw("item/permissions/requestApproval"), "params": params})))
	m, _ := parseObject(out[0].Message)
	if !bytes.Equal(m["params"], params) {
		var want, got any
		json.Unmarshal(params, &want)
		json.Unmarshal(m["params"], &got)
		if !bytes.Equal(raw(want), raw(got)) {
			t.Fatal("approval permissions changed")
		}
	}
	answer := object{"permissions": raw(map[string]any{}), "scope": raw("turn")}
	deliveries := client(t, r, string(raw(object{"id": m["id"], "result": raw(answer)})))
	if len(deliveries) != 1 || deliveries[0].Backend != "aihub" {
		t.Fatal("permission response routed incorrectly", deliveries)
	}
	replyMsg, _ := parseObject(deliveries[0].Message)
	if !bytes.Equal(replyMsg["result"], raw(answer)) || string(replyMsg["id"]) != "8" {
		t.Fatal("empty permission grant changed")
	}
}

func TestNestedEnvironmentRootsAndActiveProfilePreserved(t *testing.T) {
	for _, roots := range []string{"", `,"runtimeWorkspaceRoots":null`, `,"runtimeWorkspaceRoots":[]`} {
		r := testRouter(t, true)
		params := `{"model":"magpie/aihub/deepseek-flash-ft","permissions":":workspace","environments":[{"environmentId":"local","cwd":"C:\\work"` + roots + `}]}`
		out := client(t, r, `{"id":2,"method":"thread/start","params":`+params+`}`)
		if len(out) != 1 || out[0].Backend != "aihub" {
			t.Fatal(out)
		}
		m, _ := parseObject(out[0].Message)
		got, _ := child(m, "params")
		want, _ := parseObject([]byte(params))
		if !sameJSON(got["environments"], want["environments"]) || string(got["permissions"]) != `":workspace"` {
			t.Fatal("nested root semantics or profile changed")
		}
		response := reply(t, r, out[0], `{"thread":{"id":"new","modelProvider":"aihub","turns":[]},"activePermissionProfile":{"id":":workspace","extends":"restricted"},"runtimeWorkspaceRoots":[],"sandbox":{"type":"readOnly"}}`)
		m, _ = parseObject(response[0].Message)
		result, _ := child(m, "result")
		if !sameJSON(result["activePermissionProfile"], []byte(`{"id":":workspace","extends":"restricted"}`)) || string(result["runtimeWorkspaceRoots"]) != "[]" || !sameJSON(result["sandbox"], []byte(`{"type":"readOnly"}`)) {
			t.Fatal("backend effective permission information changed")
		}
	}
	for _, roots := range []string{`[null]`, `["C:\\work",null]`} {
		r := testRouter(t, true)
		expectError(t, client(t, r, `{"id":2,"method":"thread/start","params":{"model":"magpie/openai/gpt","runtimeWorkspaceRoots":`+roots+`}}`))
	}
}

func TestPermissionNullSandboxIsNotAConflict(t *testing.T) {
	for _, tc := range []struct{ method, fields string }{
		{"thread/start", `"model":"magpie/openai/gpt","permissions":":workspace","sandbox":null`},
		{"turn/start", `"threadId":"owned","input":[],"permissions":":workspace","sandboxPolicy":null`},
	} {
		r := testRouter(t, true)
		start(t, r, "openai", "owned", "gpt")
		out := client(t, r, `{"id":2,"method":"`+tc.method+`","params":{`+tc.fields+`}}`)
		if len(out) != 1 || out[0].Backend != "openai" {
			t.Fatal("null sandbox falsely conflicts", out)
		}
		m, _ := parseObject(out[0].Message)
		p, _ := child(m, "params")
		field := "sandbox"
		if tc.method == "turn/start" {
			field = "sandboxPolicy"
		}
		if string(p[field]) != "null" || string(p["permissions"]) != `":workspace"` {
			t.Fatal("null sandbox or profile discarded")
		}
	}
}
