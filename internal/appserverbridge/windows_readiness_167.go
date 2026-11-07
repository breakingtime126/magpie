package appserverbridge

import (
	"encoding/json"
	"os"
	"strings"
)

// This is the reviewed native Windows x64 binary, not a caller-declared trust
// anchor. Source: openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9.
const windowsReadinessSHA167 = "4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382"
const windowsReadinessUnavailable167 = "shared Windows readiness unavailable"

// RunWithOptions has already validated the executable paths and verified their
// actual digests. Never enable this for wrappers, arbitrary pins or other OSes.
// The reviewed native process cannot set this CODEX_* variable via dotenv.
// Its absence from startup makes registered_core_requested() false, excluding
// the readiness handler's otherwise mutating Registered Core refresh branch.
func windowsReadinessLaunch167(goos string, cfg Config) bool {
	if _, present := os.LookupEnv("CODEX_WINDOWS_REGISTERED_CORE"); present {
		return false
	}
	if goos != "windows" || cfg.ControlPlane != ControlPlaneRouting167 || len(cfg.Backends) != 2 {
		return false
	}
	for _, b := range cfg.Backends {
		if b.StartupProbeSHA256 != windowsReadinessSHA167 || !windowsReadinessEnv167(backendEnvironment(b)) {
			return false
		}
	}
	return true
}

func windowsReadinessEnv167(env []string) bool {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "CODEX_WINDOWS_REGISTERED_CORE") {
			return false
		}
	}
	return true
}

func validateWindowsReadinessRequest167(line []byte) bool {
	if validateRequirementsJSON(line) != nil {
		return false
	}
	m, err := startupObject(line)
	if err != nil || !requirementsEnum(m["method"], "windowsSandbox/readiness") || !NativeRequestID167Valid(m["id"]) {
		return false
	}
	for key, value := range m {
		switch key {
		case "id", "method":
		case "params":
			if !requirementsNull(value) {
				return false
			}
		case "trace":
			if !NativeTrace167Valid(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (r *Router) windowsReadinessRequest167(g *request, line []byte) []Delivery {
	if !r.controlPlaneRouting167 || !r.windowsReadiness167 {
		return []Delivery{fail(g.id, -32601, "Windows readiness requires the reviewed isolated Windows backend")}
	}
	if !validateWindowsReadinessRequest167(line) {
		return []Delivery{fail(g.id, -32602, "invalid Windows readiness request")}
	}
	groups := map[*request]bool{}
	for _, p := range r.pending {
		if p.group.method == g.method {
			groups[p.group] = true
		}
	}
	if len(groups) >= 4 {
		return []Delivery{fail(g.id, -32000, "Windows readiness request limit reached")}
	}
	r.active[g.key] = true
	r.activeBytes += g.size
	g.remaining = len(r.backends)
	out := []Delivery{}
	for _, b := range r.backends {
		// Option<()> is null, not an empty parameter object. Trace stays local.
		out = append(out, r.sendParams(g, b.ID, json.RawMessage("null")))
	}
	return out
}

func (r *Router) windowsReadinessResponse167(g *request, backend string, line []byte) []Delivery {
	if !nativeReadJSON(line, 4096, 16) {
		return r.reject(g, -32000, windowsReadinessUnavailable167)
	}
	m, err := parseObject(line)
	status, ok := requirementsObject(m["result"], "status")
	if err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) || !ok ||
		!requirementsEnum(status["status"], "ready", "notConfigured", "updateRequired") {
		// Do not forward errors containing paths, config or other private data.
		return r.reject(g, -32000, windowsReadinessUnavailable167)
	}
	g.results[backend] = []json.RawMessage{m["result"]}
	g.remaining--
	if g.remaining != 0 {
		return nil
	}
	first := g.results[r.backends[0].ID][0]
	if !sameJSON(first, g.results[r.backends[1].ID][0]) {
		return r.reject(g, -32000, windowsReadinessUnavailable167)
	}
	r.end(g)
	return []Delivery{deliver("", object{"id": g.id, "result": first})}
}
