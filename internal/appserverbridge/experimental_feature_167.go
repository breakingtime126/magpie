package appserverbridge

import (
	"encoding/json"
	"strconv"
)

// ExperimentalFeatureListCompare167 selects the reviewed, default-off native
// page comparison. It enables no feature and authorizes no thread-scoped read.
const ExperimentalFeatureListCompare167 = "compare-v0.167.0-alpha.3.2"

const experimentalFeatureUnavailable = "shared experimental features unavailable"
const experimentalFeatureTextLimit = 4096
const experimentalFeatureCursorLimit = 256
const experimentalFeaturePendingLimit = 4

// The pinned registry has 201 entries (39-byte longest key and 90-byte longest
// metadata literal). Its full Windows default page is 30 KiB. Leave bounded
// headroom for encoding and metadata without using policy's smaller byte cap.
const experimentalFeatureJSONLimit = 128 << 10
const experimentalFeatureRowLimit = 256

// The source oracle is openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9,
// protocol/v2/experimental_feature.rs and catalog_processor.rs. Preserve the
// actual Option<u32> limit (including zero) and opaque cursor. The native child,
// not the bridge's merged-list adapter, owns pagination and config reloads.
func validateExperimentalFeatureRequest167(line []byte) bool {
	if validateRequirementsJSON(line) != nil {
		return false
	}
	m, err := startupObject(line)
	if err != nil || len(m) < 3 || len(m) > 4 ||
		!requirementsEnum(m["method"], "experimentalFeature/list") || !NativeRequestID167Valid(m["id"]) {
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
	if err != nil || len(params) > 3 {
		return false
	}
	for key, value := range params {
		switch key {
		case "cursor":
			if !requirementsNull(value) && !experimentalFeatureString(value, experimentalFeatureCursorLimit) {
				return false
			}
		case "limit":
			var limit uint32
			if !requirementsNull(value) && json.Unmarshal(value, &limit) != nil {
				return false
			}
		case "threadId":
			if !requirementsNull(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func experimentalFeatureString(data json.RawMessage, limit int) bool {
	var value string
	return !requirementsNull(data) && json.Unmarshal(data, &value) == nil && len(value) <= limit
}

func (r *Router) experimentalFeatureListRequest167(g *request, line []byte, params json.RawMessage) []Delivery {
	if !r.experimentalFeatureList167 {
		return []Delivery{fail(g.id, -32601, "method is not supported by this experimental bridge")}
	}
	if !validateExperimentalFeatureRequest167(line) {
		return []Delivery{fail(g.id, -32602, "invalid experimental feature list request")}
	}
	// Count logical reads, not their transport requests. A group still owns
	// its slot after one child replies, until the other completes or fails.
	groups := make(map[*request]struct{}, experimentalFeaturePendingLimit)
	for _, pending := range r.pending {
		if pending.group.method == g.method {
			groups[pending.group] = struct{}{}
		}
	}
	if len(groups) >= experimentalFeaturePendingLimit {
		return []Delivery{fail(g.id, -32000, "experimental feature list request limit reached")}
	}
	r.active[g.key] = true
	r.activeBytes += g.size
	g.remaining = len(r.backends)
	out := []Delivery{}
	for _, b := range r.backends {
		out = append(out, r.sendParams(g, b.ID, params))
	}
	return out
}

// Share the bounded native JSON decoder used by discarded auth errors rather
// than weakening policy/trace text checks. Static native announcements may
// contain newlines; malformed UTF-8/surrogates and decoded duplicates still
// fail. Exact fields and types below constrain every returned metadata value.
func validateExperimentalFeaturePage167(data []byte) bool {
	if !nativeReadJSON(data, experimentalFeatureJSONLimit, experimentalFeatureRowLimit) {
		return false
	}
	page, err := parseObject(data)
	if err != nil || len(page) != 2 || len(page["nextCursor"]) == 0 || requirementsNull(page["data"]) {
		return false
	}
	var rows []json.RawMessage
	if json.Unmarshal(page["data"], &rows) != nil {
		return false
	}
	seen := map[string]bool{}
	for _, encoded := range rows {
		row, ok := requirementsObject(encoded, "name", "stage", "displayName", "description", "announcement", "enabled", "defaultEnabled")
		if !ok || !experimentalFeatureString(row["name"], 128) || text(row, "name") == "" ||
			!requirementsEnum(row["stage"], "beta", "underDevelopment", "stable", "deprecated", "removed") ||
			!requirementsBool(row["enabled"]) || !requirementsBool(row["defaultEnabled"]) {
			return false
		}
		name := text(row, "name")
		if !startupText(name) || seen[name] {
			return false
		}
		seen[name] = true
		for _, key := range []string{"displayName", "description", "announcement"} {
			if requirementsEnum(row["stage"], "beta") {
				if !experimentalFeatureString(row[key], experimentalFeatureTextLimit) {
					return false
				}
			} else if !requirementsNull(row[key]) {
				return false
			}
		}
	}
	if requirementsNull(page["nextCursor"]) {
		return true
	}
	// The native producer uses usize.to_string(), never an arbitrary cursor
	// payload. Do not rewrite the cursor or create a bridge snapshot/token.
	var cursor string
	if json.Unmarshal(page["nextCursor"], &cursor) != nil || len(cursor) > 20 || len(rows) == 0 {
		return false
	}
	offset, err := strconv.ParseUint(cursor, 10, 64)
	return err == nil && offset > 0 && strconv.FormatUint(offset, 10) == cursor
}

func (r *Router) experimentalFeatureListResponse167(g *request, backend string, line []byte) []Delivery {
	if !nativeReadJSON(line, experimentalFeatureJSONLimit, experimentalFeatureRowLimit) {
		return r.reject(g, -32000, experimentalFeatureUnavailable)
	}
	m, err := parseObject(line)
	if err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) ||
		!validateExperimentalFeaturePage167(m["result"]) {
		// This runs before generic backend error forwarding: config reload
		// failures and all error message/data remain private.
		return r.reject(g, -32000, experimentalFeatureUnavailable)
	}
	g.bytes += len(m["result"])
	if g.bytes > 2*experimentalFeatureJSONLimit {
		return r.reject(g, -32000, experimentalFeatureUnavailable)
	}
	g.results[backend] = []json.RawMessage{m["result"]}
	g.remaining--
	if g.remaining != 0 {
		return nil
	}
	first := g.results[r.backends[0].ID][0]
	second := g.results[r.backends[1].ID][0]
	if !sameJSON(first, second) {
		return r.reject(g, -32000, experimentalFeatureUnavailable)
	}
	// Compare every row field, order and nextCursor. Never merge, intersect,
	// replace flags, cache an earlier success or synthesize an empty catalog.
	r.end(g)
	return []Delivery{deliver("", object{"id": g.id, "result": first})}
}
