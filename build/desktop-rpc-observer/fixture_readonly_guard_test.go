package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func guardFixture(t *testing.T) *fixtureReadOnlyGuard {
	t.Helper()
	g, err := parseFixtureReadOnlyGuard(`{"profile":"unelevated-read-only-4239","workspace":"C:\\fixture\\workspace"}`)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func guardFrame(t *testing.T, g *fixtureReadOnlyGuard, direction string, value any) error {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return g.inspect(direction, b)
}

func guardStartResult() map[string]any {
	return map[string]any{
		"thread":                  map[string]any{"id": "synthetic-thread", "modelProvider": "magpie_bridge"},
		"modelProvider":           "magpie_bridge",
		"model":                   "magpie/alpha/test-alpha",
		"cwd":                     `C:\fixture\workspace`,
		"runtimeWorkspaceRoots":   []any{`C:\fixture\workspace`},
		"approvalPolicy":          "never",
		"sandbox":                 map[string]any{"type": "readOnly", "networkAccess": false},
		"activePermissionProfile": map[string]any{"id": ":read-only", "extends": nil},
	}
}

func guardRegister(t *testing.T, g *fixtureReadOnlyGuard) {
	t.Helper()
	if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 1, "method": "thread/start", "params": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if err := guardFrame(t, g, "from_bridge", map[string]any{"id": 1, "result": guardStartResult()}); err != nil {
		t.Fatal(err)
	}
}

func guardTurnParams() map[string]any {
	return map[string]any{"threadId": "synthetic-thread", "input": []any{map[string]any{
		"type": "text", "text": "synthetic-alpha-turn-1", "text_elements": []any{},
	}}}
}

func TestFixtureReadOnlyGuardRegistrationAndTurn(t *testing.T) {
	g := guardFixture(t)
	guardRegister(t, g)
	p := guardTurnParams()
	p["permissions"] = ":read-only"
	p["runtimeWorkspaceRoots"] = []any{`C:\fixture\workspace`}
	if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 2, "method": "turn/start", "params": p}); err != nil {
		t.Fatal(err)
	}
	if err := guardFrame(t, g, "from_bridge", map[string]any{"id": 2, "result": map[string]any{"turn": map[string]any{"id": "synthetic-turn"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestFixtureReadOnlyGuardRejectsTurnBeforeVerifiedReply(t *testing.T) {
	g := guardFixture(t)
	if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 2, "method": "turn/start", "params": guardTurnParams()}); err == nil {
		t.Fatal("unverified thread admitted")
	}
}

func TestFixtureReadOnlyGuardRejectsUnsafeParameters(t *testing.T) {
	cases := map[string]any{
		"permissions": ":workspace-write", "cwd": `C:\other`,
		"runtimeWorkspaceRoots": []any{`C:\`}, "approvalPolicy": "on-request",
		"sandboxPolicy":     map[string]any{"type": "workspaceWrite"},
		"config":            map[string]any{"sandbox_mode": "danger-full-access"},
		"collaborationMode": map[string]any{}, "environments": []any{map[string]any{"id": "remote"}},
		"selectedCapabilityRoots": []any{"C:\\"}, "toolOutput": map[string]any{},
		"unknownFutureOverride": nil, "model": "magpie/beta/test-beta",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			g := guardFixture(t)
			guardRegister(t, g)
			p := guardTurnParams()
			p[key] = value
			if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 2, "method": "turn/start", "params": p}); err == nil {
				t.Fatal("unsafe field admitted")
			}
			if err := g.inspect("to_bridge", []byte(`{"id":3,"method":"model/list","params":{}}`)); err == nil {
				t.Fatal("fatal guard resumed")
			}
		})
	}
}

func TestFixtureReadOnlyGuardRejectsEffectivePermissions(t *testing.T) {
	cases := map[string]any{
		"sandbox":                 map[string]any{"type": "readOnly", "networkAccess": true},
		"activePermissionProfile": nil, "cwd": `C:\other`,
		"runtimeWorkspaceRoots": []any{`C:\other`}, "approvalPolicy": "on-request",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			g := guardFixture(t)
			if err := g.inspect("to_bridge", []byte(`{"id":1,"method":"thread/resume","params":{"threadId":"synthetic-thread"}}`)); err != nil {
				t.Fatal(err)
			}
			r := guardStartResult()
			r[key] = value
			if err := guardFrame(t, g, "from_bridge", map[string]any{"id": 1, "result": r}); err == nil {
				t.Fatal("unsafe effective response admitted")
			}
			if len(g.threads) != 0 {
				t.Fatal("unsafe thread registered")
			}
		})
	}
}

func TestFixtureReadOnlyGuardBlocksOtherExecutionAndApproval(t *testing.T) {
	for _, method := range []string{"thread/fork", "thread/compact/start", "thread/settings/update", "thread/review/start"} {
		g := guardFixture(t)
		if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 1, "method": method, "params": map[string]any{}}); err == nil {
			t.Fatal("unreviewed execution admitted", method)
		}
	}
	g := guardFixture(t)
	if err := g.inspect("from_bridge", []byte(`{"id":1,"method":"item/commandExecution/requestApproval","params":{}}`)); err == nil {
		t.Fatal("server approval admitted")
	}
	g = guardFixture(t)
	if err := g.inspect("from_bridge", []byte(`{"method":"item/started","params":{"item":{"type":"commandExecution"}}}`)); err == nil {
		t.Fatal("tool event admitted")
	}
}

func TestFixtureReadOnlyGuardWireBoundaries(t *testing.T) {
	g := guardFixture(t)
	var out bytes.Buffer
	good := "{\"id\":1,\"method\":\"model/list\",\"params\":{}}\r\n"
	if err := pumpFixtureGuard(&out, strings.NewReader(good), nil, g, "to_bridge"); err != nil || out.String() != good {
		t.Fatal("accepted bytes changed", err)
	}
	for _, bad := range []string{
		`{"id":1,"method":"thread/start","params":{"permissions":null,"permissions":":workspace-write"}}` + "\n",
		`{"id":1,"method":"model/list","params":{}}`,
		strings.Repeat(" ", maxLine) + "\n",
	} {
		g = guardFixture(t)
		out.Reset()
		if err := pumpFixtureGuard(&out, strings.NewReader(bad), nil, g, "to_bridge"); err == nil || out.Len() != 0 {
			t.Fatal("rejected frame leaked bytes")
		}
	}
}

func TestFixtureReadOnlyGuardEnvironmentAndArgv(t *testing.T) {
	for _, bad := range []string{"", `{}`, `{"profile":"wrong","workspace":"C:\\fixture\\workspace"}`, `{"profile":"unelevated-read-only-4239","workspace":"C:\\fixture\\workspace","extra":null}`} {
		if _, err := parseFixtureReadOnlyGuard(bad); err == nil {
			t.Fatal("invalid opt-in admitted")
		}
	}
	args := []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"}
	g := guardFixture(t)
	if probe, err := g.argv(args, false); err != nil || probe {
		t.Fatal("observed runtime argv rejected", err)
	}
	probeArgs := append(append([]string{}, args...), "--print-startup-config", "--startup-client-info", `{"name":"codex_desktop","title":"Codex Desktop","version":"26.1103.4239.0"}`)
	if probe, err := g.argv(probeArgs, false); err != nil || !probe {
		t.Fatal("observed startup probe rejected", err)
	}
	if _, err := g.argv(args, true); err == nil {
		t.Fatal("registered core presence admitted")
	}
	for _, extra := range [][]string{{"-c", "features.prefer_mxc=true"}, {"-c", `windows.sandbox="elevated"`}, {"--startup-sign-out"}, {"--config", "sandbox_mode=read-only"}} {
		if _, err := g.argv(append(append([]string{}, args...), extra...), false); err == nil {
			t.Fatal("unreviewed startup override admitted")
		}
	}
	t.Setenv(fixtureReadOnlyGuardEnv, "not-a-child-variable")
	for _, value := range childEnvironment() {
		if strings.HasPrefix(value, fixtureReadOnlyGuardEnv+"=") {
			t.Fatal("guard metadata inherited by real bridge")
		}
	}
}

func TestFixtureReadOnlyGuardRequiresRequestedOwner(t *testing.T) {
	g := guardFixture(t)
	if err := g.inspect("to_bridge", []byte(`{"id":1,"method":"thread/start","params":{"model":"magpie/beta/test-beta"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := guardFrame(t, g, "from_bridge", map[string]any{"id": 1, "result": guardStartResult()}); err == nil {
		t.Fatal("response changed requested owner")
	}
}

func TestFixtureReadOnlyGuardCompletionCorrelation(t *testing.T) {
	for _, early := range []bool{false, true} {
		g := guardFixture(t)
		guardRegister(t, g)
		var events []event
		g.emit = func(e event) { events = append(events, e) }
		for round := 1; round <= 2; round++ {
			turnID := []string{"", "turn-one", "turn-two"}[round]
			if err := guardFrame(t, g, "to_bridge", map[string]any{"id": round + 1, "method": "turn/start", "params": guardTurnParams()}); err != nil {
				t.Fatal(err)
			}
			response := map[string]any{"id": round + 1, "result": map[string]any{"turn": map[string]any{"id": turnID}}}
			completed := map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "synthetic-thread", "turn": map[string]any{"id": turnID, "status": "completed", "error": nil}}}
			first, second := response, completed
			if early {
				first, second = completed, response
			}
			if err := guardFrame(t, g, "from_bridge", first); err != nil {
				t.Fatal(err)
			}
			if len(events) != round-1 {
				t.Fatal("completion emitted without both correlated messages")
			}
			if err := guardFrame(t, g, "from_bridge", second); err != nil {
				t.Fatal(err)
			}
			if len(events) != round || events[round-1].FixtureReadOnly.Owner != "alpha" || events[round-1].FixtureReadOnly.Completed != round || events[round-1].FixtureReadOnly.ThreadTurn != round {
				t.Fatal("invalid completion attribution")
			}
		}
		if err := guardFrame(t, g, "from_bridge", map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "synthetic-thread", "turn": map[string]any{"id": "turn-two", "status": "completed", "error": nil}}}); err == nil {
			t.Fatal("duplicate completion admitted")
		}
	}
}

func TestFixtureReadOnlyGuardFailureAndUnboundCompletionNotCounted(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		g := guardFixture(t)
		guardRegister(t, g)
		var events []event
		g.emit = func(e event) { events = append(events, e) }
		if err := guardFrame(t, g, "from_bridge", map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "synthetic-thread", "turn": map[string]any{"id": "unrequested", "status": status, "error": nil}}}); err == nil {
			t.Fatal("completion without a pending or active turn admitted")
		}
		if len(events) != 0 {
			t.Fatal("uncorrelated completion counted")
		}
	}
}

func TestFixtureReadOnlyGuardResumePreservesPreviouslyVerifiedOwner(t *testing.T) {
	g := guardFixture(t)
	guardRegister(t, g)
	if err := g.inspect("to_bridge", []byte(`{"id":2,"method":"thread/resume","params":{"threadId":"synthetic-thread"}}`)); err != nil {
		t.Fatal(err)
	}
	r := guardStartResult()
	r["model"] = "magpie/beta/test-beta"
	if err := guardFrame(t, g, "from_bridge", map[string]any{"id": 2, "result": r}); err == nil {
		t.Fatal("resume changed previously verified owner")
	}
}

func TestFixtureReadOnlyGuardRejectionOverridesCleanChildExit(t *testing.T) {
	for _, code := range []int{0, 1, 42, 125} {
		if got := fixtureGuardExitCode(code, true); got == 0 {
			t.Fatal("guard rejection reported success", code)
		}
		if got := fixtureGuardExitCode(code, false); got != code {
			t.Fatal("ordinary child status changed", code, got)
		}
	}
}

func TestFixtureReadOnlyGuardRejectsConflictingPermissionSelectors(t *testing.T) {
	g := guardFixture(t)
	guardRegister(t, g)
	p := guardTurnParams()
	p["permissions"] = ":read-only"
	p["sandboxPolicy"] = map[string]any{"type": "readOnly", "networkAccess": false}
	if err := guardFrame(t, g, "to_bridge", map[string]any{"id": 2, "method": "turn/start", "params": p}); err == nil {
		t.Fatal("mutually exclusive profile/policy selectors admitted")
	}
}
