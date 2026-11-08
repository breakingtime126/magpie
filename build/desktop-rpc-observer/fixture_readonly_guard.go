package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

const fixtureReadOnlyGuardEnv = "MAGPIE_CI_READONLY_GUARD"
const fixtureReadOnlyGuardProfile = "unelevated-read-only-4239"

// This is a test admission gate, not an OS sandbox or a transparent observer.
// It knows only the disposable self-authored two-model fixture. No rejected
// frame is rewritten or forwarded, and no synthetic success is generated.
type fixtureReadOnlyGuard struct {
	mu             sync.Mutex
	workspace      string
	pending        map[string]fixtureGuardPending
	threads        map[string]string
	turns          map[string]string
	completedEarly map[string]string
	completedTurns map[string]bool
	threadCounts   map[string]int
	ownerCounts    map[string]int
	emit           func(event)
	failed         bool
}

type fixtureGuardPending struct {
	method string
	thread string
	owner  string
}

type fixtureReadOnlyTurn struct {
	Schema     int    `json:"schema"`
	Owner      string `json:"owner"`
	Completed  int    `json:"completed"`
	ThreadTurn int    `json:"threadTurn"`
}

func guardFailure() error { return errors.New("fixture_readonly_guard_rejected") }

func parseFixtureReadOnlyGuard(value string) (*fixtureReadOnlyGuard, error) {
	p, ok := authMetadataObject([]byte(value), 2)
	if !ok || len(p) != 2 || guardString(p["profile"]) != fixtureReadOnlyGuardProfile {
		return nil, guardFailure()
	}
	workspace, valid := lexicalWindowsAbsolute(guardString(p["workspace"]), true)
	if !valid {
		return nil, guardFailure()
	}
	return &fixtureReadOnlyGuard{workspace: workspace, pending: map[string]fixtureGuardPending{}, threads: map[string]string{}, turns: map[string]string{}, completedEarly: map[string]string{}, completedTurns: map[string]bool{}, threadCounts: map[string]int{}, ownerCounts: map[string]int{}}, nil
}

func guardString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
func guardAbsent(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
func guardEmpty(raw json.RawMessage) bool {
	return guardAbsent(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("[]"))
}
func guardID(raw json.RawMessage) (string, bool) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || len(b) > 512 {
		return "", false
	}
	if b[0] == '"' {
		s := guardString(b)
		if s == "" {
			return "", false
		}
		return "s:" + s, true
	}
	for i, c := range b {
		if (c < '0' || c > '9') && !(i == 0 && c == '-') {
			return "", false
		}
	}
	if !json.Valid(b) {
		return "", false
	}
	return "n:" + string(b), true
}
func (g *fixtureReadOnlyGuard) samePath(raw json.RawMessage) bool {
	p, ok := lexicalWindowsAbsolute(guardString(raw), true)
	return ok && strings.EqualFold(p, g.workspace)
}
func (g *fixtureReadOnlyGuard) roots(raw json.RawMessage) bool {
	if guardAbsent(raw) {
		return true
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) > 1 {
		return false
	}
	return len(values) == 0 || g.samePath(values[0])
}
func guardReadOnlyPolicy(raw json.RawMessage) bool {
	p, ok := authMetadataObject(raw, 2)
	return ok && len(p) == 2 && guardString(p["type"]) == "readOnly" && string(bytes.TrimSpace(p["networkAccess"])) == "false"
}
func guardBuiltinProfile(raw json.RawMessage) bool {
	p, ok := authMetadataObject(raw, 2)
	return ok && len(p) == 2 && guardString(p["id"]) == ":read-only" && bytes.Equal(bytes.TrimSpace(p["extends"]), []byte("null"))
}
func guardModel(raw json.RawMessage) string {
	s := guardString(raw)
	if s == "magpie/alpha/test-alpha" {
		return "alpha"
	}
	if s == "magpie/beta/test-beta" {
		return "beta"
	}
	return ""
}

// Validate only the exact already observed startup arguments. The production
// adapter still performs its own independent argument validation. A one-shot
// startup probe is identified explicitly, not inferred from non-RPC output.
func (g *fixtureReadOnlyGuard) argv(args []string, registeredCorePresent bool) (bool, error) {
	if registeredCorePresent {
		return false, guardFailure()
	}
	app, analytics, probe, client := 0, 0, 0, 0
	configs := map[string]bool{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "app-server":
			app++
		case "--analytics-default-enabled":
			analytics++
		case "--print-startup-config":
			probe++
		case "--startup-client-info":
			i++
			if i >= len(args) {
				return false, guardFailure()
			}
			p, ok := authMetadataObject([]byte(args[i]), 3)
			if !ok || len(p) != 3 || guardString(p["name"]) != "codex_desktop" || guardString(p["title"]) != "Codex Desktop" || guardString(p["version"]) == "" {
				return false, guardFailure()
			}
			client++
		case "-c":
			i++
			if i >= len(args) || configs[args[i]] {
				return false, guardFailure()
			}
			if args[i] != "features.code_mode_host=true" && args[i] != "plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true" && args[i] != `plugins."codex-app-tools@openai-bundled".mcp_servers.codex_app.enabled=true` {
				return false, guardFailure()
			}
			configs[args[i]] = true
		default:
			return false, guardFailure()
		}
	}
	if app != 1 || analytics != 1 || probe > 1 || client != probe || !configs["features.code_mode_host=true"] {
		return false, guardFailure()
	}
	return probe == 1, nil
}

func (g *fixtureReadOnlyGuard) requestParams(method string, raw json.RawMessage) bool {
	p, ok := authMetadataObject(raw, 48)
	if !ok {
		return false
	}
	start := method == "thread/start" || method == "thread/resume"
	if !guardAbsent(p["permissions"]) && (!guardAbsent(p["sandbox"]) || !guardAbsent(p["sandboxPolicy"])) {
		return false
	}
	owner := ""
	if !start {
		owner = g.threads[guardString(p["threadId"])]
		if owner == "" {
			return false
		}
	}
	if method == "thread/resume" && guardString(p["threadId"]) == "" {
		return false
	}
	for k, v := range p {
		switch k {
		case "threadId", "cwd", "runtimeWorkspaceRoots", "permissions", "sandbox", "sandboxPolicy", "approvalPolicy", "approvalsReviewer", "environments", "selectedCapabilityRoots", "dynamicTools", "disabledPluginIds", "model", "modelProvider", "effort", "ephemeral", "allowProviderModelFallback", "experimentalRawEvents", "historyMode", "input", "expectedTurnId", "clientUserMessageId", "config", "baseInstructions", "developerInstructions", "collaborationMode", "toolOutput", "path", "serviceTier", "serviceTierForTurn", "serviceName", "personality", "multiAgentMode", "sessionStartSource", "threadSource", "projectId", "daybreakEnabled", "mockExperimentalField", "summary", "outputSchema", "turnTrigger", "parentTurnId", "rootTurnId", "responsesapiClientMetadata", "responsesapiClientMetadataPatch", "additionalContext", "initialTurnsPage":
		default:
			return false
		}
		if guardAbsent(v) {
			continue
		}
		switch k {
		case "threadId":
			if guardString(v) == "" || len(guardString(v)) > 512 {
				return false
			}
		case "cwd":
			if !g.samePath(v) {
				return false
			}
		case "runtimeWorkspaceRoots":
			if !g.roots(v) {
				return false
			}
		case "permissions":
			if guardString(v) != ":read-only" {
				return false
			}
		case "sandbox":
			if !start || guardString(v) != "read-only" {
				return false
			}
		case "sandboxPolicy":
			if start || !guardReadOnlyPolicy(v) {
				return false
			}
		case "approvalPolicy":
			if guardString(v) != "never" {
				return false
			}
		case "approvalsReviewer":
			if guardString(v) != "user" {
				return false
			}
		case "environments", "selectedCapabilityRoots", "dynamicTools", "disabledPluginIds":
			if !guardEmpty(v) {
				return false
			}
		case "model":
			if guardModel(v) == "" || (!start && guardModel(v) != owner) {
				return false
			}
		case "modelProvider":
			if !start || guardString(v) != "magpie_bridge" {
				return false
			}
		case "effort":
			if guardString(v) != "none" {
				return false
			}
		case "ephemeral":
			if !start || (string(v) != "true" && string(v) != "false") {
				return false
			}
		case "allowProviderModelFallback", "experimentalRawEvents":
			if !start || string(v) != "false" {
				return false
			}
		case "historyMode":
			if !start || guardString(v) != "full" {
				return false
			}
		case "input":
			if start || !guardSyntheticInput(v, owner) {
				return false
			}
		case "expectedTurnId":
			if method != "turn/steer" || guardString(v) == "" {
				return false
			}
		case "clientUserMessageId":
			if start || guardString(v) == "" {
				return false
			}
		// A non-null value for any other key requires a separately reviewed
		// fixture extension. In particular no config, collaboration, tool
		// output, instruction, environment or named-profile substitution.
		default:
			return false
		}
	}
	return start || !guardAbsent(p["input"])
}

func guardSyntheticInput(raw json.RawMessage, owner string) bool {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) != 1 {
		return false
	}
	p, ok := authMetadataObject(items[0], 3)
	if !ok || len(p) < 2 || guardString(p["type"]) != "text" || !guardEmpty(p["text_elements"]) {
		return false
	}
	for key := range p {
		if key != "type" && key != "text" && key != "text_elements" {
			return false
		}
	}
	s := guardString(p["text"])
	prefix := "synthetic-" + owner + "-"
	if !strings.HasPrefix(s, prefix) || len(s) <= len(prefix) || len(s) > len(prefix)+80 {
		return false
	}
	for _, c := range s[len(prefix):] {
		if !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != ' ' && c != '-' {
			return false
		}
	}
	return true
}

func (g *fixtureReadOnlyGuard) inspect(direction string, line []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failed {
		return guardFailure()
	}
	if !g.inspectLocked(direction, line) {
		g.failed = true
		return guardFailure()
	}
	return nil
}
func (g *fixtureReadOnlyGuard) inspectLocked(direction string, line []byte) bool {
	m, ok := authMetadataObject(line, 8)
	if !ok {
		return false
	}
	method := guardString(m["method"])
	id, hasID := guardID(m["id"])
	if direction == "to_bridge" {
		// Replies to native approval/tool requests are never admitted.
		if method == "" {
			return false
		}
		if method == "initialized" {
			return len(m["id"]) == 0
		}
		if !hasID || len(m["result"]) != 0 || len(m["error"]) != 0 {
			return false
		}
		switch method {
		case "thread/start", "thread/resume", "turn/start", "turn/steer":
			if !g.requestParams(method, m["params"]) || len(g.pending) >= 128 || g.pending[id].method != "" {
				return false
			}
			params := mustGuardObject(m["params"])
			thread, owner := guardString(params["threadId"]), guardModel(params["model"])
			if method == "thread/resume" {
				if previous := g.threads[thread]; previous != "" {
					if owner != "" && owner != previous {
						return false
					}
					owner = previous
				}
				delete(g.threads, thread)
			}
			if method == "turn/start" {
				if g.pendingTurn(thread) {
					return false
				}
				for _, active := range g.turns {
					if active == thread {
						return false
					}
				}
			}
			g.pending[id] = fixtureGuardPending{method: method, thread: thread, owner: owner}
		case "thread/read", "thread/list", "thread/loaded/list", "thread/turns/list", "thread/items/list", "thread/unsubscribe", "turn/interrupt":
			// Production routing validates ownership and parameters. These
			// cannot start or change execution permissions in the pinned code.
		default:
			if strings.HasPrefix(method, "thread/") || strings.HasPrefix(method, "turn/") {
				return false
			}
			// All other native mutable methods remain unsupported by this
			// exact production bridge. Its only enabled write is the separately
			// validated bridge-owned model/effort file, never native config.
		}
		return true
	}
	if direction != "from_bridge" {
		return false
	}
	if method != "" {
		if len(m["id"]) != 0 {
			return false
		}
		if method == "turn/completed" {
			p := mustGuardObject(m["params"])
			turn := mustGuardObject(p["turn"])
			thread, turnID := guardString(p["threadId"]), guardString(turn["id"])
			if g.threads[thread] == "" || turnID == "" || len(turnID) > 512 || guardString(turn["status"]) != "completed" || !guardAbsent(turn["error"]) || g.completedTurns[turnID] {
				return false
			}
			if bound := g.turns[turnID]; bound != "" {
				if bound != thread {
					return false
				}
				return g.recordCompletion(thread, turnID)
			}
			if !g.pendingTurn(thread) || len(g.completedEarly) >= 128 || g.completedEarly[turnID] != "" {
				return false
			}
			for _, earlyThread := range g.completedEarly {
				if earlyThread == thread {
					return false
				}
			}
			g.completedEarly[turnID] = thread
		}
		if method == "thread/settings/updated" {
			p, ok := authMetadataObject(m["params"], 2)
			if !ok {
				return false
			}
			s, ok := authMetadataObject(p["threadSettings"], 32)
			if !ok || !g.validEffective(s, "sandboxPolicy") {
				return false
			}
			if owner := g.threads[guardString(p["threadId"])]; owner != "" && guardModel(s["model"]) != owner {
				return false
			}
		}
		if method == "item/started" || method == "item/completed" {
			p := mustGuardObject(m["params"])
			item := mustGuardObject(p["item"])
			switch guardString(item["type"]) {
			case "userMessage", "agentMessage", "reasoning":
			default:
				return false
			}
		}
		return true
	}
	if !hasID {
		return false
	}
	pending := g.pending[id]
	if pending.method == "" {
		return true
	}
	delete(g.pending, id)
	if len(m["error"]) != 0 {
		if pending.method == "turn/start" {
			for _, thread := range g.completedEarly {
				if thread == pending.thread {
					return false
				}
			}
		}
		return len(m["result"]) == 0
	}
	if pending.method == "turn/steer" {
		return len(m["result"]) != 0
	}
	if pending.method == "turn/start" {
		r := mustGuardObject(m["result"])
		turn := mustGuardObject(r["turn"])
		turnID := guardString(turn["id"])
		if turnID == "" || len(turnID) > 512 || len(g.turns) >= 128 || g.turns[turnID] != "" || g.completedTurns[turnID] {
			return false
		}
		g.turns[turnID] = pending.thread
		if thread := g.completedEarly[turnID]; thread != "" {
			if thread != pending.thread {
				return false
			}
			delete(g.completedEarly, turnID)
			return g.recordCompletion(thread, turnID)
		}
		return true
	}
	r, ok := authMetadataObject(m["result"], 32)
	if !ok || !g.validEffective(r, "sandbox") || !g.roots(r["runtimeWorkspaceRoots"]) {
		return false
	}
	t := mustGuardObject(r["thread"])
	thread := guardString(t["id"])
	owner := guardModel(r["model"])
	if thread == "" || len(thread) > 512 || owner == "" || len(g.threads) >= 128 || guardString(r["modelProvider"]) != "magpie_bridge" || guardString(t["modelProvider"]) != "magpie_bridge" {
		return false
	}
	if (pending.owner != "" && pending.owner != owner) || (pending.method == "thread/resume" && pending.thread != thread) {
		return false
	}
	if old := g.threads[thread]; old != "" && old != owner {
		return false
	}
	g.threads[thread] = owner
	return true
}
func (g *fixtureReadOnlyGuard) pendingTurn(thread string) bool {
	for _, p := range g.pending {
		if p.method == "turn/start" && p.thread == thread {
			return true
		}
	}
	return false
}

func (g *fixtureReadOnlyGuard) recordCompletion(thread, turn string) bool {
	owner := g.threads[thread]
	if owner == "" || len(g.completedTurns) >= 128 {
		return false
	}
	delete(g.turns, turn)
	g.completedTurns[turn] = true
	g.threadCounts[thread]++
	g.ownerCounts[owner]++
	if g.emit != nil {
		g.emit(event{Kind: "fixture_readonly_turn_completed", FixtureReadOnly: &fixtureReadOnlyTurn{Schema: 1, Owner: owner, Completed: g.ownerCounts[owner], ThreadTurn: g.threadCounts[thread]}})
	}
	return true
}

func mustGuardObject(raw json.RawMessage) map[string]json.RawMessage {
	p, _ := authMetadataObject(raw, 64)
	return p
}
func (g *fixtureReadOnlyGuard) validEffective(p map[string]json.RawMessage, policy string) bool {
	return g.samePath(p["cwd"]) && guardString(p["approvalPolicy"]) == "never" && guardReadOnlyPolicy(p[policy]) && guardBuiltinProfile(p["activePermissionProfile"])
}

// Unlike the normal pump, this opt-in buffers a complete bounded JSON line
// before forwarding it. Allowed bytes, including line endings, remain exact.
func pumpFixtureGuard(dst io.Writer, src io.Reader, observe *lineObserver, guard *fixtureReadOnlyGuard, direction string) error {
	buf := make([]byte, 32<<10)
	line := make([]byte, 0, maxLine)
	for {
		n, readErr := src.Read(buf)
		for _, b := range buf[:n] {
			if len(line) >= maxLine {
				return guardFailure()
			}
			line = append(line, b)
			if b != '\n' {
				continue
			}
			if err := guard.inspect(direction, line); err != nil {
				return err
			}
			if observe != nil {
				observe.feed(line)
			}
			written, err := dst.Write(line)
			if err != nil {
				return err
			}
			if written != len(line) {
				return io.ErrShortWrite
			}
			line = line[:0]
		}
		if readErr != nil {
			if readErr == io.EOF && len(line) == 0 {
				if observe != nil {
					observe.end(true)
				}
				return nil
			}
			return guardFailure()
		}
	}
}

// Guard rejection is independent of child exit status and trace availability.
func fixtureGuardExitCode(childCode int, rejected bool) int {
	if rejected {
		return 125
	}
	return childCode
}
