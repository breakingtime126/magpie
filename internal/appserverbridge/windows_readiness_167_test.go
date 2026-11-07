package appserverbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func readinessRouter167(t *testing.T) *Router {
	r := testRouter(t, false)
	r.controlPlaneRouting167 = true
	r.windowsReadiness167 = true
	return r
}

func readinessAsk167(t *testing.T, r *Router, id int) []Delivery {
	t.Helper()
	out := client(t, r, fmt.Sprintf(`{"id":%d,"method":"windowsSandbox/readiness","trace":{"traceparent":"PRIVATE_TRACE"}}`, id))
	if len(out) != 2 {
		t.Fatalf("expected two native reads: %v", out)
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if d.Backend == "" || string(m["params"]) != "null" || len(m) != 3 || strings.Contains(string(d.Message), "PRIVATE_") {
			t.Fatal("incorrect native envelope")
		}
	}
	return out
}

func TestWindowsReadiness167NativeStates(t *testing.T) {
	for _, a := range []string{"ready", "notConfigured", "updateRequired"} {
		for _, b := range []string{"ready", "notConfigured", "updateRequired"} {
			r := readinessRouter167(t)
			out := readinessAsk167(t, r, 71)
			first := `{"status":"` + a + `"}`
			if got := reply(t, r, out[0], first); len(got) != 0 {
				t.Fatal("replied before both native states")
			}
			got := reply(t, r, out[1], `{"status":"`+b+`"}`)
			if a != b {
				expectError(t, got)
			} else {
				m, _ := parseObject(got[0].Message)
				if string(m["result"]) != first {
					t.Fatal("native status replaced")
				}
			}
			if len(r.active) != 0 || r.activeBytes != 0 || len(r.pending) != 0 {
				t.Fatal("read budget retained")
			}
		}
	}
}

func TestWindowsReadiness167ParamsAndAdmission(t *testing.T) {
	for _, suffix := range []string{"", `,"params":null`, `,"trace":null`} {
		if !validateWindowsReadinessRequest167([]byte(`{"id":1,"method":"windowsSandbox/readiness"` + suffix + `}`)) {
			t.Fatal("native unit params rejected")
		}
	}
	for _, suffix := range []string{`,"params":{}`, `,"params":[]`, `,"params":1`, `,"other":null`, `,"params":null,"params":null`, `,"trace":{"bad":1}`} {
		expectError(t, client(t, readinessRouter167(t), `{"id":1,"method":"windowsSandbox/readiness"`+suffix+`}`))
	}
	r := testRouter(t, false)
	expectError(t, client(t, r, `{"id":1,"method":"windowsSandbox/readiness"}`))
	r = readinessRouter167(t)
	expectError(t, client(t, r, `{"id":2,"method":"windowsSandbox/setupStart","params":{}}`))
	cfg := Config{ControlPlane: ControlPlaneRouting167, Backends: []Backend{{StartupProbeSHA256: windowsReadinessSHA167}, {StartupProbeSHA256: windowsReadinessSHA167}}}
	// Synthetic parent values only; no native child runs. Even empty and zero
	// are explicit caller selections, not permission to erase the flag.
	for _, value := range []string{"", "0", "1"} {
		t.Setenv("CODEX_WINDOWS_REGISTERED_CORE", value)
		if windowsReadinessLaunch167("windows", cfg) {
			t.Fatal("parent Registered Core selection silently discarded")
		}
	}
	if err := os.Unsetenv("CODEX_WINDOWS_REGISTERED_CORE"); err != nil {
		t.Fatal(err)
	}
	if !windowsReadinessLaunch167("windows", cfg) || windowsReadinessLaunch167("linux", cfg) {
		t.Fatal("isolated launch admission incorrect")
	}
	for _, key := range []string{"CODEX_WINDOWS_REGISTERED_CORE", "codex_windows_registered_core"} {
		if windowsReadinessEnv167([]string{key + "=0"}) || !reservedEnv(strings.ToUpper(key)) {
			t.Fatal("reserved startup variable admitted")
		}
	}
	cfg.Backends[1].StartupProbeSHA256 = strings.Repeat("a", 64)
	if windowsReadinessLaunch167("windows", cfg) {
		t.Fatal("caller-declared arbitrary pin admitted")
	}
}

func TestWindowsReadiness167MalformedReleaseAndLateReply(t *testing.T) {
	for _, result := range []string{`null`, `{}`, `{"status":"PRIVATE_CANARY"}`, `{"status":false}`, `{"status":"ready","extra":null}`, `{"status":"ready","status":"ready"}`} {
		r := readinessRouter167(t)
		out := readinessAsk167(t, r, 1)
		got := reply(t, r, out[0], result)
		expectError(t, got)
		if strings.Contains(string(got[0].Message), "PRIVATE_") || len(r.pending) != 0 || r.activeBytes != 0 {
			t.Fatal("failed read leaked data or retained budget")
		}
		if late := reply(t, r, out[1], `{"status":"ready"}`); len(late) != 0 {
			t.Fatal("late sibling was forwarded")
		}
	}
	r := readinessRouter167(t)
	out := readinessAsk167(t, r, 1)
	m, _ := parseObject(out[0].Message)
	payload, _ := json.Marshal(object{"id": m["id"], "error": json.RawMessage(`{"code":-1,"message":"PRIVATE_CANARY","data":{"path":"PRIVATE_PATH"}}`)})
	got := server(t, r, out[0].Backend, string(payload))
	expectError(t, got)
	if strings.Contains(string(got[0].Message), "PRIVATE_") || len(r.pending) != 0 || r.activeBytes != 0 {
		t.Fatal("native error escaped or retained failed group")
	}
}

func TestWindowsReadiness167BoundedGroups(t *testing.T) {
	r := readinessRouter167(t)
	var first []Delivery
	for i := 1; i <= 4; i++ {
		out := readinessAsk167(t, r, i)
		if i == 1 {
			first = out
		}
		reply(t, r, out[0], `{"status":"notConfigured"}`)
	}
	expectError(t, client(t, r, `{"id":5,"method":"windowsSandbox/readiness"}`))
	reply(t, r, first[1], `{"status":"ready"}`) // Disagreement releases the logical slot.
	readinessAsk167(t, r, 6)
}

func TestWindowsReadiness167NativeIDsAndForeignReply(t *testing.T) {
	r := readinessRouter167(t)
	out := client(t, r, `{"id":-0,"method":"windowsSandbox/readiness","params":null}`)
	if len(out) != 2 {
		t.Fatal("native negative zero rejected")
	}
	if _, err := r.Client([]byte(`{"id":0,"method":"windowsSandbox/readiness"}`)); err == nil {
		t.Fatal("equivalent active native ID admitted")
	}
	m, _ := parseObject(out[0].Message)
	payload, _ := json.Marshal(object{"id": m["id"], "result": json.RawMessage(`{"status":"ready"}`)})
	if _, err := r.Server(out[1].Backend, payload); err == nil {
		t.Fatal("foreign backend response accepted")
	}
}
