package appserverbridge

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// BridgeModelSettingsOwned167 enables only the two persisted frontend defaults.
// It does not enable native config writes, policies, credentials or profiles.
const BridgeModelSettingsOwned167 = "owned-file-v0.167.0-alpha.3.2"
const ownedSettingsUnavailable167 = "bridge-owned model settings unavailable; restart required"

func ownedSettingsWriteError167(id json.RawMessage, code string) Delivery {
	return deliver("", object{"id": id, "error": raw(object{
		"code": raw(-32600), "message": raw("bridge-owned model settings write rejected"),
		"data": raw(object{"config_write_error_code": raw(code)}),
	})})
}

// A complete, deliberately narrow subset of public c407 ConfigBatchWriteParams.
// No omission, profile reset or unknown edit is silently translated.
func (r *Router) parseOwnedSettingsWrite167(line []byte) (object, *string, bool) {
	if validateRequirementsJSON(line) != nil {
		return nil, nil, false
	}
	m, err := startupObject(line)
	if err != nil || len(m) < 3 || len(m) > 4 || !NativeRequestID167Valid(m["id"]) || !requirementsEnum(m["method"], "config/batchWrite") {
		return nil, nil, false
	}
	for k := range m {
		if k != "id" && k != "method" && k != "params" && k != "trace" {
			return nil, nil, false
		}
	}
	if trace, ok := m["trace"]; ok && !NativeTrace167Valid(trace) {
		return nil, nil, false
	}
	p, err := startupObject(m["params"])
	if err != nil || len(p) < 1 || len(p) > 4 {
		return nil, nil, false
	}
	var expected *string
	for k, v := range p {
		switch k {
		case "edits":
		case "filePath":
			if !requirementsNull(v) {
				var path string
				if json.Unmarshal(v, &path) != nil || path != r.ownedSettings.path || !filepath.IsAbs(path) {
					return nil, nil, false
				}
			}
		case "expectedVersion":
			if !requirementsNull(v) {
				var version string
				if !experimentalFeatureString(v, 128) || json.Unmarshal(v, &version) != nil {
					return nil, nil, false
				}
				expected = &version
			}
		case "reloadUserConfig":
			if !requirementsBool(v) {
				return nil, nil, false
			}
		default:
			return nil, nil, false
		}
	}
	var edits []object
	if json.Unmarshal(p["edits"], &edits) != nil || len(edits) != 2 {
		return nil, nil, false
	}
	next := object{}
	for _, edit := range edits {
		if len(edit) != 3 || (!requirementsEnum(edit["mergeStrategy"], "replace") && !requirementsEnum(edit["mergeStrategy"], "upsert")) {
			return nil, nil, false
		}
		key := text(edit, "keyPath")
		if (key != "model" && key != "model_reasoning_effort") || exists(next, key) {
			return nil, nil, false
		}
		next[key] = edit["value"]
	}
	return next, expected, r.ownedSettingsValues167(next)
}

func (r *Router) ownedSettingsValues167(value object) bool {
	if r.ownedCatalog.nativeLists {
		return nativeSettingsValues167(r.ownedCatalog.projection, value)
	}
	if len(value) != 2 || !requirementsEnum(value["model_reasoning_effort"], "none") || !experimentalFeatureString(value["model"], 1024) {
		return false
	}
	m, ok := r.ownedCatalog.projection[text(value, "model")]
	return ok && requirementsEnum(m["defaultReasoningEffort"], "none")
}

func (r *Router) ownedSettingsWriteRequest167(g *request, line []byte) []Delivery {
	if r.ownedSettings == nil {
		return []Delivery{fail(g.id, -32601, "method is not supported by this experimental bridge")}
	}
	if r.controlSession167 == nil || r.ownedCatalog.refreshing {
		return []Delivery{fail(g.id, -32000, "bridge-owned model settings are not ready")}
	}
	if _, _, ok := r.parseOwnedSettingsWrite167(line); !ok {
		return []Delivery{ownedSettingsWriteError167(g.id, "configValidationError")}
	}
	for _, p := range r.pending {
		if p.group.method == "config/batchWrite" {
			return []Delivery{fail(g.id, -32000, "bridge-owned model settings write already in progress")}
		}
	}
	r.active[g.key] = true
	r.activeBytes += g.size
	return r.ownedSettingsPolicyStart167(g)
}

func (r *Router) ownedSettingsPolicyStart167(g *request) []Delivery {
	g.settingsPolicyStage = true
	g.remaining = len(r.backends)
	g.results = map[string][]json.RawMessage{}
	g.bytes = 0
	out := []Delivery{}
	for _, b := range r.backends {
		id := r.next("request")
		r.pending["s:"+id] = pending{g, b.ID}
		out = append(out, deliver(b.ID, object{"id": raw(id), "method": raw("configRequirements/read"), "params": raw(nil)}))
	}
	return out
}

func (r *Router) ownedSettingsPolicyResponse167(g *request, backend string, line []byte) ([]Delivery, error) {
	m, err := parseObject(line)
	if validateRequirementsJSON(line) != nil || err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) || validateConfigRequirementsNull167(m["result"]) != nil {
		if r.ownedCatalog != nil {
			reason := nativehandshake.CatalogReasonPolicySchema
			// Classify only the rejected shape in memory; never expose its value.
			if result, parseErr := parseObject(m["result"]); parseErr == nil {
				if value, present := result["requirements"]; present && !requirementsNull(value) {
					reason = nativehandshake.CatalogReasonPolicyNonNull
				}
			}
			r.ownedCatalog.failure(reason)
		}
		return r.reject(g, -32000, "bridge-owned settings policy precondition failed"), nil
	}
	g.bytes += len(m["result"])
	if g.bytes > requirementsJSONLimit {
		if r.ownedCatalog != nil {
			r.ownedCatalog.failure(nativehandshake.CatalogReasonPolicySize)
		}
		return r.reject(g, -32000, "bridge-owned settings policy precondition failed"), nil
	}
	g.results[backend] = []json.RawMessage{m["result"]}
	g.remaining--
	if g.remaining != 0 {
		return nil, nil
	}
	if !sameJSON(g.results[r.backends[0].ID][0], g.results[r.backends[1].ID][0]) {
		if r.ownedCatalog != nil {
			r.ownedCatalog.failure(nativehandshake.CatalogReasonPolicyMismatch)
		}
		return r.reject(g, -32000, "bridge-owned settings policy precondition failed"), nil
	}
	g.settingsPolicyStage = false
	g.settingsPolicyReady = true
	g.results = map[string][]json.RawMessage{}
	g.bytes = 0
	if g.method == "model/list" {
		return r.startList(g)
	}
	return r.finishOwnedSettingsWrite167(g)
}

// Prepare the complete projection before committing to disk. Persistence or
// post-commit verification uncertainty closes the connection, never claims rollback.
func (r *Router) finishOwnedSettingsWrite167(g *request) ([]Delivery, error) {
	if r.ownedCatalog.refreshing || r.controlSession167 == nil {
		return r.reject(g, -32000, "bridge-owned model settings are not ready"), nil
	}
	line := raw(object{"id": g.id, "method": raw(g.method), "params": raw(g.params)})
	next, expected, ok := r.parseOwnedSettingsWrite167(line)
	if !ok {
		r.abandon(g)
		return []Delivery{ownedSettingsWriteError167(g.id, "configValidationError")}, nil
	}
	version, err := ownedSettingsVersion(next)
	if err != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	session, err := r.ownedSettingsSession167(next, version, r.defaultModel)
	if err != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	actual, err := r.ownedSettings.commit(expected, next)
	if errors.Is(err, errOwnedSettingsConflict) {
		r.abandon(g)
		return []Delivery{ownedSettingsWriteError167(g.id, "configVersionConflict")}, nil
	}
	if err != nil || actual != version {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	if r.ownedCatalog.verify() != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	r.controlSession167 = session
	return r.finish(g, object{"status": raw("ok"), "version": raw(version), "filePath": raw(r.ownedSettings.path), "overriddenMetadata": raw(nil)}), nil
}

func (r *Router) ownedSettingsCatalogSession167(catalogDefault string) (*controlPlaneSession167, error) {
	if r.ownedSettings == nil {
		return newControlPlaneSession167(r.controlPlaneSessionConfig167(catalogDefault))
	}
	value, version, err := r.ownedSettings.snapshot()
	if err != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	seed := len(value) == 0
	if seed {
		effort := raw("none")
		if r.ownedCatalog.nativeLists {
			effort = r.ownedCatalog.projection[catalogDefault]["defaultReasoningEffort"]
		}
		value = object{"model": raw(catalogDefault), "model_reasoning_effort": effort}
	}
	if !r.ownedSettingsValues167(value) {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	nextVersion, err := ownedSettingsVersion(value)
	if err != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	session, err := r.ownedSettingsSession167(value, nextVersion, catalogDefault)
	if err != nil {
		return nil, err
	}
	if seed {
		actual, err := r.ownedSettings.commit(&version, value)
		if err != nil || actual != nextVersion {
			return nil, errors.New(ownedSettingsUnavailable167)
		}
	}
	if r.ownedCatalog.verify() != nil {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	return session, nil
}

func (r *Router) ownedSettingsSession167(user object, version, catalogDefault string) (*controlPlaneSession167, error) {
	base, err := newControlPlaneSession167(r.controlPlaneSessionConfig167(catalogDefault))
	if err != nil {
		return nil, err
	}
	config, err := parseObject(base.config)
	if err != nil {
		return nil, err
	}
	origins, err := parseObject(base.origins)
	if err != nil {
		return nil, err
	}
	metadata := object{"name": raw(object{"type": raw("user"), "file": raw(r.ownedSettings.path), "profile": raw(nil)}), "version": raw(version)}
	for _, key := range []string{"model", "model_reasoning_effort"} {
		if exists(config, key) || exists(origins, key) {
			return nil, errors.New("session overrides persisted model settings")
		}
		config[key] = user[key]
		origins[key] = raw(metadata)
	}
	layer := clone(metadata)
	layer["config"] = raw(user)
	base.config = raw(config)
	base.origins = raw(origins)
	base.layers = raw([]json.RawMessage{raw(layer), base.layer})
	base.model = text(user, "model")
	if len(raw(base.read(true))) > controlPlaneResultLimit167 {
		return nil, errors.New(ownedSettingsUnavailable167)
	}
	return base, nil
}
