package appserverbridge

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

// ControlPlaneRouting167 selects a bridge-owned routing view, not a native
// effective configuration or a shared execution-policy authority.
const ControlPlaneRouting167 = "routing-v0.167.0-alpha.3.2"

const controlPlaneProvider167 = "magpie_bridge"
const controlPlaneUnavailable167 = "bridge routing configuration unavailable"
const controlPlaneReadPendingLimit167 = 4

// The source oracle is openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9,
// protocol/v2/config.rs and its JSON schema. Config is extensible and has no
// required properties. ConfigReadResponse requires config and origins; native
// serialization omits layers when includeLayers is false. This independent
// view never fetches, filters or impersonates either child's native config.
func validateControlPlaneRead167(line []byte) bool {
	if validateRequirementsJSON(line) != nil {
		return false
	}
	m, err := startupObject(line)
	if err != nil || len(m) < 3 || len(m) > 4 ||
		!requirementsEnum(m["method"], "config/read") || !NativeRequestID167Valid(m["id"]) {
		return false
	}
	for key := range m {
		if key != "id" && key != "method" && key != "params" && key != "trace" {
			return false
		}
	}
	if trace, present := m["trace"]; present && !NativeTrace167Valid(trace) {
		return false
	}
	params, err := startupObject(m["params"])
	if err != nil || len(params) > 2 {
		return false
	}
	// Native omission defaults includeLayers to false and cwd to None.
	// Both includeLayers values select the same unscoped session authority.
	for key, value := range params {
		switch key {
		case "includeLayers":
			if !requirementsBool(value) {
				return false
			}
		case "cwd":
			if !requirementsNull(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (r *Router) controlPlaneRead167(g *request, line []byte) []Delivery {
	if !r.controlPlaneRouting167 {
		return []Delivery{fail(g.id, -32601, "method is not supported by this experimental bridge")}
	}
	if !validateControlPlaneRead167(line) {
		return []Delivery{fail(g.id, -32602, "invalid bridge routing config request")}
	}
	// Count logical reads, not their transport requests. A half-completed
	// policy pair keeps its slot until that same group completes or fails.
	groups := make(map[*request]struct{}, controlPlaneReadPendingLimit167)
	for _, pending := range r.pending {
		if pending.group.method == g.method {
			groups[pending.group] = struct{}{}
		}
	}
	if len(groups) >= controlPlaneReadPendingLimit167 {
		return []Delivery{fail(g.id, -32000, "routing config read request limit reached")}
	}
	r.active[g.key] = true
	r.activeBytes += g.size
	g.remaining = len(r.backends)
	out := []Delivery{}
	for _, b := range r.backends {
		id := r.next("request")
		r.pending["s:"+id] = pending{g, b.ID}
		// This is an explicit internal policy precondition, not a forwarding
		// of config/read. It does not enable the public requirements route.
		out = append(out, deliver(b.ID, object{
			"id": raw(id), "method": raw("configRequirements/read"), "params": raw(nil),
		}))
	}
	return out
}

func (r *Router) controlPlaneReadResponse167(g *request, backend string, line []byte) []Delivery {
	if validateRequirementsJSON(line) != nil {
		return r.reject(g, -32000, controlPlaneUnavailable167)
	}
	m, err := parseObject(line)
	if err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) ||
		validateConfigRequirementsNull167(m["result"]) != nil {
		// Run before generic error forwarding. No backend error, policy,
		// credential or unreviewed branch can enter this owned response.
		return r.reject(g, -32000, controlPlaneUnavailable167)
	}
	g.bytes += len(m["result"])
	if g.bytes > requirementsJSONLimit {
		return r.reject(g, -32000, controlPlaneUnavailable167)
	}
	g.results[backend] = []json.RawMessage{m["result"]}
	g.remaining--
	if g.remaining != 0 {
		return nil
	}
	if !sameJSON(g.results[r.backends[0].ID][0], g.results[r.backends[1].ID][0]) {
		return r.reject(g, -32000, controlPlaneUnavailable167)
	}
	if r.controlSession167 == nil {
		return r.reject(g, -32000, controlPlaneUnavailable167)
	}
	var includeLayers bool
	_ = json.Unmarshal(g.params["includeLayers"], &includeLayers)
	return r.finish(g, r.controlSession167.read(includeLayers))
}

// These are the session-owned settings applied by the frontend router. The
// session snapshot is published at activation and complete catalog commits,
// not synthesized by config/read or copied from a native configuration.
func (r *Router) controlPlaneSessionConfig167(defaultModel string) object {
	backends := []object{}
	for _, b := range r.backends {
		source := "native-model-list"
		if b.Models != nil {
			source = "configured-descriptors"
		}
		backends = append(backends, object{
			"id": raw(b.ID), "provider": raw(b.Provider),
			"model_prefix": raw(alias(b.ID, "")), "catalog_source": raw(source),
		})
	}
	provenance := object{"model_provider": raw("bridge-routing")}
	config := object{"model_provider": raw(controlPlaneProvider167)}
	// Never manufacture a default before a complete model/list succeeds.
	// The same committed catalog and default already govern thread/start.
	if defaultModel != "" {
		config["model"] = raw(defaultModel)
		provenance["model"] = raw("bridge-catalog-default")
	}
	config["magpie_bridge"] = raw(object{
		"version": raw(1), "view": raw("routing-defaults"),
		"provider_routing": raw("fixed-per-thread"),
		"execution_policy": raw("backend-owned-per-thread"),
		"model_catalog":    raw("model/list"),
		"supports": raw(object{
			"config_writes": raw(false), "layer_reads": raw(true), "cwd_reads": raw(false),
		}),
		"provenance": raw(provenance), "backends": raw(backends),
	})
	return config
}

func controlPlaneThreadCreate167(method string) bool {
	return method == "thread/start" || method == "thread/resume" || method == "thread/fork"
}

func controlPlaneThreadResult167(method string) bool {
	return controlPlaneThreadCreate167(method) || method == "thread/read" ||
		method == "thread/unarchive" || method == "thread/revert" || method == "thread/rollback"
}

// Inspect only the objects whose routing keys we consume, never arbitrary
// nested user/tool payloads. Existing message byte limits remain in force.
func validateControlPlaneParams167(method string, encoded json.RawMessage) bool {
	switch method {
	case "thread/start", "thread/resume", "thread/fork", "thread/list":
		if len(encoded) != 0 && !requirementsNull(encoded) {
			return validateBackendEnvelopeKeys(encoded) == nil
		}
	}
	return true
}

func validateControlPlaneResult167(method string, encoded json.RawMessage) bool {
	if !controlPlaneThreadResult167(method) && method != "thread/list" {
		return true
	}
	if validateBackendEnvelopeKeys(encoded) != nil {
		return false
	}
	result, err := parseObject(encoded)
	if err != nil {
		return false
	}
	if controlPlaneThreadResult167(method) {
		return validateBackendEnvelopeKeys(result["thread"]) == nil
	}
	return true
}

func validateControlPlaneThreadFilter167(p object) bool {
	encoded, present := p["modelProviders"]
	if !present || requirementsNull(encoded) {
		return true
	}
	var values []json.RawMessage
	if json.Unmarshal(encoded, &values) != nil || len(values) > 1 {
		return false
	}
	return len(values) == 0 || requirementsEnum(values[0], controlPlaneProvider167)
}

func (r *Router) controlPlaneThreadFilter167(backend string, p object) object {
	encoded, present := p["modelProviders"]
	if !present || requirementsNull(encoded) {
		return p
	}
	var values []json.RawMessage
	_ = json.Unmarshal(encoded, &values) // Validated before pagination or send.
	if len(values) == 0 {
		return p
	}
	p = clone(p)
	p["modelProviders"] = raw([]string{r.byID[backend].Provider})
	return p
}

// Native ownership is checked before projection. Call only at schema-defined
// provider fields; a provider-shaped string in a tool result is not routing.
func (r *Router) projectControlPlaneProvider167(backend string, value object) error {
	if !requirementsEnum(value["modelProvider"], r.byID[backend].Provider) {
		return errors.New("backend returned an unexpected native provider")
	}
	value["modelProvider"] = raw(controlPlaneProvider167)
	return nil
}

// Match only the configured native home or its resolved spelling captured
// before child launch. Never resolve/stat an arbitrary path from the child.
// Native canonical spelling and Windows case differences are supported.
func (r *Router) controlPlaneNativeHome167(backend string, encoded json.RawMessage) bool {
	var home string
	if json.Unmarshal(encoded, &home) != nil || !filepath.IsAbs(home) {
		return false
	}
	home = controlPlanePath167(home)
	for _, expected := range []string{r.byID[backend].CodexHome, r.controlNativeHomes167[backend]} {
		if expected != "" && home == controlPlanePath167(expected) {
			return true
		}
	}
	return false
}

func (r *Router) controlPlaneInitialize167(g *request, backend string, line []byte) []Delivery {
	failInit := func() []Delivery {
		r.phase = "failed"
		return r.reject(g, -32000, "bridge control initialization unavailable")
	}
	if validateRequirementsJSON(line) != nil || !filepath.IsAbs(r.controlHome167) {
		return failInit()
	}
	m, ok := requirementsObject(line, "id", "result")
	if !ok {
		return failInit()
	}
	result, ok := requirementsObject(m["result"], "userAgent", "codexHome", "platformFamily", "platformOs")
	if !ok || !r.controlPlaneNativeHome167(backend, result["codexHome"]) {
		return failInit()
	}
	for _, key := range []string{"userAgent", "platformFamily", "platformOs"} {
		if !experimentalFeatureString(result[key], 4096) || text(result, key) == "" {
			return failInit()
		}
	}
	g.results[backend] = []json.RawMessage{m["result"]}
	g.remaining--
	if g.remaining != 0 {
		return nil
	}
	first, _ := parseObject(g.results[r.backends[0].ID][0])
	second, _ := parseObject(g.results[r.backends[1].ID][0])
	for _, key := range []string{"userAgent", "platformFamily", "platformOs"} {
		if !sameJSON(first[key], second[key]) {
			return failInit()
		}
	}
	// This caller-created directory is the frontend server's actual state
	// namespace, not a native home alias, generated config or filesystem grant.
	first["codexHome"] = raw(r.controlHome167)
	r.phase = "ready"
	return r.finish(g, first)
}

// Rust canonicalization may spell Windows paths with the extended-length
// drive or UNC prefix. Normalize only those ordinary path spellings, not
// arbitrary device namespaces, and never access the reported path.
func controlPlanePath167(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(path)
		if strings.HasPrefix(upper, `\\?\UNC\`) {
			path = `\\` + path[len(`\\?\UNC\`):]
		} else if len(path) >= 7 && strings.HasPrefix(path, `\\?\`) &&
			path[5] == ':' && path[6] == '\\' &&
			((path[4] >= 'a' && path[4] <= 'z') || (path[4] >= 'A' && path[4] <= 'Z')) {
			path = path[4:]
		}
		path = strings.ToLower(filepath.Clean(path))
	}
	return path
}
