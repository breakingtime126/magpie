package appserverbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const ownedCatalogUnavailable = "bridge-owned model catalog unavailable; restart required"
const ownedCatalogLimit = 64 << 10

// The immutable file is an explicit bridge-owned declaration, not a native
// configuration. Only its public Model projection is verified against native
// model/list. Instructions and non-public capabilities remain authored data.
type ownedAliasCatalog struct {
	path, home, digest, defaultModel string
	identity                         os.FileInfo
	projection                       map[string]object
	refreshing, failed               bool
}

func ownedCatalogPath(home, path string) (string, error) {
	bad := errors.New(ownedCatalogUnavailable)
	if !filepath.IsAbs(home) || !filepath.IsAbs(path) {
		return "", bad
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", bad
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", bad
	}
	rel, err := filepath.Rel(controlPlanePath167(home), controlPlanePath167(parent))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", bad
	}
	path = filepath.Join(parent, filepath.Base(path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > ownedCatalogLimit {
		return "", bad
	}
	return path, nil
}

func readOwnedCatalog(home, path string) ([]byte, os.FileInfo, error) {
	bad := errors.New(ownedCatalogUnavailable)
	resolved, err := ownedCatalogPath(home, path)
	if err != nil {
		return nil, nil, bad
	}
	before, err := os.Lstat(resolved)
	if err != nil {
		return nil, nil, bad
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, nil, bad
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, bad
	}
	data, err := io.ReadAll(io.LimitReader(file, ownedCatalogLimit+1))
	after, statErr := file.Stat()
	current, pathErr := os.Lstat(resolved)
	if err != nil || statErr != nil || pathErr != nil || len(data) == 0 || len(data) > ownedCatalogLimit ||
		!os.SameFile(opened, current) || !current.Mode().IsRegular() ||
		int64(len(data)) != opened.Size() || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, nil, bad
	}
	return data, opened, nil
}

func loadOwnedCatalog(home, path string, backends []Backend) (*ownedAliasCatalog, error) {
	bad := errors.New(ownedCatalogUnavailable)
	resolved, err := ownedCatalogPath(home, path)
	if err != nil || len(backends) != 2 || backends[0].ID == backends[1].ID || backends[0].Models != nil || backends[1].Models != nil {
		return nil, bad
	}
	data, info, err := readOwnedCatalog(home, resolved)
	if err != nil || !ownedCatalogJSON(data) {
		return nil, bad
	}
	top, err := parseObject(data)
	if err != nil || len(top) != 1 {
		return nil, bad
	}
	var rows []json.RawMessage
	if json.Unmarshal(top["models"], &rows) != nil || len(rows) != 2 {
		return nil, bad
	}
	c := &ownedAliasCatalog{path: resolved, home: home, identity: info, projection: map[string]object{}}
	priorities := map[string]int32{}
	for _, row := range rows {
		model, err := parseObject(row)
		if err != nil {
			return nil, bad
		}
		projected, priority, err := ownedModelProjection(model)
		if err != nil {
			return nil, bad
		}
		slug := text(projected, "model")
		parts := strings.SplitN(slug, "/", 3)
		if len(parts) != 3 || parts[0] != "magpie" || parts[2] == "" || strings.ContainsAny(parts[2], "\r\n\x00") ||
			(parts[1] != backends[0].ID && parts[1] != backends[1].ID) || prioritiesContains(priorities, parts[1]) {
			return nil, bad
		}
		priorities[parts[1]] = priority
		projected["isDefault"] = raw(parts[1] == backends[0].ID)
		c.projection[slug] = projected
		if parts[1] == backends[0].ID {
			c.defaultModel = slug
		}
	}
	if len(priorities) != 2 || priorities[backends[0].ID] >= priorities[backends[1].ID] {
		return nil, bad
	}
	digest := sha256.Sum256(data)
	c.digest = hex.EncodeToString(digest[:])
	return c, nil
}

func prioritiesContains(values map[string]int32, key string) bool {
	_, found := values[key]
	return found
}

func (c *ownedAliasCatalog) verify() error {
	data, info, err := readOwnedCatalog(c.home, c.path)
	if err != nil || c.failed || !os.SameFile(c.identity, info) || info.Size() != c.identity.Size() || !info.ModTime().Equal(c.identity.ModTime()) {
		return errors.New(ownedCatalogUnavailable)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != c.digest {
		return errors.New(ownedCatalogUnavailable)
	}
	return nil
}

// Fixed public source c40729ea: openai_models.rs:1066-1099 and
// app-server/src/models.rs:8-69. This deliberately supports only the explicit
// text/no-tool subset below. Unknown fields, guardian and capability expansion
// need review, not permissive deserialization or inferred native acceptance.
func ownedModelProjection(m object) (object, int32, error) {
	bad := errors.New(ownedCatalogUnavailable)
	fixed := object{
		"shell_type": raw("disabled"), "visibility": raw("list"), "supported_in_api": raw(true),
		"support_verbosity": raw(false), "default_verbosity": raw(nil),
		"experimental_supported_tools": raw([]any{}), "input_modalities": raw([]string{"text"}),
		"include_apps_usage_instructions": raw(false), "include_skills_usage_instructions": raw(false),
		"include_plugin_usage_instructions": raw(false), "supports_reasoning_summary_parameter": raw(false),
		"default_reasoning_summary": raw("none"), "node_repl_disabled": raw(true),
		"supports_search_tool": raw(false), "apply_patch_tool_type": raw(nil),
		"upgrade": raw(nil), "availability_nux": raw(nil), "additional_speed_tiers": raw([]any{}), "service_tiers": raw([]any{}),
	}
	variable := []string{"slug", "display_name", "description", "default_reasoning_level", "supported_reasoning_levels", "priority", "truncation_policy", "model_messages"}
	if len(m) != len(fixed)+len(variable) {
		return nil, 0, bad
	}
	for key, expected := range fixed {
		if _, present := m[key]; !present || !sameJSON(m[key], expected) {
			return nil, 0, bad
		}
	}
	for _, key := range variable {
		if _, ok := m[key]; !ok {
			return nil, 0, bad
		}
	}
	for _, key := range []string{"slug", "display_name"} {
		if !experimentalFeatureString(m[key], 1024) || text(m, key) == "" {
			return nil, 0, bad
		}
	}
	description := ""
	if !requirementsNull(m["description"]) && (!experimentalFeatureString(m["description"], 4096) || json.Unmarshal(m["description"], &description) != nil) {
		return nil, 0, bad
	}
	if !requirementsNull(m["default_reasoning_level"]) && !requirementsEnum(m["default_reasoning_level"], "none") {
		return nil, 0, bad
	}
	var efforts []object
	if json.Unmarshal(m["supported_reasoning_levels"], &efforts) != nil || len(efforts) != 1 || len(efforts[0]) != 2 ||
		!requirementsEnum(efforts[0]["effort"], "none") || !experimentalFeatureString(efforts[0]["description"], 4096) {
		return nil, 0, bad
	}
	var priority int32
	if requirementsNull(m["priority"]) || json.Unmarshal(m["priority"], &priority) != nil {
		return nil, 0, bad
	}
	truncation, err := parseObject(m["truncation_policy"])
	var limit int64
	if err != nil || len(truncation) != 2 || !requirementsEnum(truncation["mode"], "tokens") || json.Unmarshal(truncation["limit"], &limit) != nil || limit <= 0 {
		return nil, 0, bad
	}
	messages, err := parseObject(m["model_messages"])
	if err != nil || len(messages) != 1 || !experimentalFeatureString(messages["instructions_template"], 4096) || text(messages, "instructions_template") == "" {
		return nil, 0, bad
	}
	return object{
		"id": m["slug"], "model": m["slug"], "displayName": m["display_name"], "description": raw(description),
		"upgrade": raw(nil), "upgradeInfo": raw(nil), "availabilityNux": raw(nil), "modelSpecialty": raw(nil),
		"hidden": raw(false), "supportedReasoningEfforts": raw([]object{{"reasoningEffort": raw("none"), "description": efforts[0]["description"]}}),
		"defaultReasoningEffort": raw("none"), "inputModalities": m["input_modalities"], "supportsPersonality": raw(false),
		"multiAgentVersion": raw(nil), "additionalSpeedTiers": m["additional_speed_tiers"], "serviceTiers": m["service_tiers"],
		"defaultServiceTier": raw(nil), "availableAccessPrograms": raw(nil), "isDefault": raw(false),
	}, priority, nil
}

func (c *ownedAliasCatalog) validateNative(results map[string][]json.RawMessage, backends []Backend) error {
	bad := errors.New(ownedCatalogUnavailable)
	if c.verify() != nil || len(results) != 2 {
		return bad
	}
	for i, b := range backends {
		rows := results[b.ID]
		if len(rows) != 1 || !ownedCatalogJSON(rows[0]) {
			return bad
		}
		m, err := parseObject(rows[0])
		if err != nil || len(m) != 19 || text(m, "id") != text(m, "model") || !sameJSON(m["isDefault"], raw(true)) {
			return bad
		}
		a := alias(b.ID, text(m, "model"))
		expected, ok := c.projection[a]
		if !ok {
			return bad
		}
		m["id"], m["model"] = raw(a), raw(a)
		m["displayName"] = raw(text(m, "displayName") + " [" + b.ID + "]")
		m["isDefault"] = raw(i == 0)
		if !sameJSON(raw(m), raw(expected)) {
			return bad
		}
	}
	return nil
}

// Reject duplicate keys at every level before decoding maps; limits apply to
// tokens and nesting as well as bytes. This parser never reads model records
// from any source except the caller's explicit owned startup file.
func ownedCatalogJSON(data []byte) bool {
	if len(data) == 0 || len(data) > ownedCatalogLimit || !utf8.Valid(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	tokens := 0
	var value func(int) bool
	value = func(depth int) bool {
		tokens++
		if depth > 16 || tokens > 16384 {
			return false
		}
		t, err := d.Token()
		if err != nil {
			return false
		}
		delim, container := t.(json.Delim)
		if !container {
			return true
		}
		if delim != '{' && delim != '[' {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				s, ok := key.(string)
				if err != nil || !ok || seen[s] {
					return false
				}
				seen[s] = true
			}
			if !value(depth + 1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && ((delim == '{' && end == json.Delim('}')) || (delim == '[' && end == json.Delim(']')))
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func (r *Router) invalidateOwnedCatalog() {
	r.models = map[string]modelRoute{}
	r.defaultModel = ""
	r.controlSession167 = nil
	for key, p := range r.pages {
		if p.method == "model/list" {
			delete(r.pages, key)
		}
	}
}

func (r *Router) failOwnedCatalog() {
	if r.ownedCatalog != nil {
		r.ownedCatalog.failed = true
		r.invalidateOwnedCatalog()
	}
}

func (r *Router) Client(line []byte) ([]Delivery, error) {
	if r.ownedCatalog != nil {
		if r.ownedCatalog.verify() != nil {
			r.failOwnedCatalog()
			return nil, errors.New(ownedCatalogUnavailable)
		}
		if r.ownedCatalog.refreshing {
			m, _ := parseObject(line)
			// This existing primary-only read does not use model routes or the
			// session config. Keep its normal mode, params, limits and result
			// validation below; all catalog-dependent requests remain blocked.
			if _, request := m["id"]; request && text(m, "method") != "" && !(r.auth.authStatusNoAuth && text(m, "method") == "getAuthStatus") {
				return []Delivery{fail(m["id"], -32000, "bridge model catalog refresh is in progress")}, nil
			}
		}
	}
	out, err := r.client(line)
	if r.ownedCatalog != nil && (err != nil || r.ownedCatalog.failed) {
		r.failOwnedCatalog()
		return nil, errors.New(ownedCatalogUnavailable)
	}
	return out, err
}

func (r *Router) Server(backend string, line []byte) ([]Delivery, error) {
	if r.ownedCatalog != nil && r.ownedCatalog.verify() != nil {
		r.failOwnedCatalog()
		return nil, errors.New(ownedCatalogUnavailable)
	}
	out, err := r.server(backend, line)
	if r.ownedCatalog != nil && (err != nil || r.ownedCatalog.failed) {
		r.failOwnedCatalog()
		return nil, errors.New(ownedCatalogUnavailable)
	}
	return out, err
}
