package appserverbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// BridgeModelSettingsNative167 persists picker defaults against complete native
// lists. It does not replace official metadata with an authored alias catalog.
const BridgeModelSettingsNative167 = "owned-file-native-lists-v0.167.0-alpha.3.2"

func (cfg Config) validateNativeSettings167() error {
	s := cfg.SharedNativeRoute
	if cfg.ControlPlane != ControlPlaneRouting167 || cfg.BridgeModelCatalogJSON != "" ||
		s == nil || s.SecondaryCatalogSHA256 == "" ||
		cfg.StartupProbe != StartupProbeCompareUnrestricted167 || cfg.RequirementsRead != ConfigRequirementsCompareNull167 ||
		cfg.ExperimentalFeatureList != ExperimentalFeatureListCompare167 || cfg.AuthStatus != AuthStatusPrimaryTrustedStdio167 ||
		cfg.DesktopPluginOverride != DesktopPluginCodexAppTools167 {
		return errors.New("native model settings require pinned shared-native Desktop contracts")
	}
	for _, b := range cfg.Backends {
		if b.Models != nil || b.StartupProbeSHA256 == "" {
			return errors.New("native model settings require pinned native lists")
		}
	}
	return nil
}

// Reuse the catalog's publication barrier, not its file projection. The supplier
// file stays separately pinned and only genuine complete replies define choices.
func newNativeSettingsCatalog167(cfg Config) (*ownedAliasCatalog, error) {
	s := cfg.SharedNativeRoute
	info, err := os.Lstat(s.SecondaryCatalog)
	if err != nil {
		return nil, errors.New(ownedCatalogUnavailable)
	}
	c := &ownedAliasCatalog{nativeLists: true, path: s.SecondaryCatalog, digest: s.SecondaryCatalogSHA256, identity: info}
	if err := c.verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *ownedAliasCatalog) verifyNativeSettingsCatalog167() error {
	info, err := os.Lstat(c.path)
	if c.failed || err != nil || !info.Mode().IsRegular() || !os.SameFile(info, c.identity) ||
		info.Size() != c.identity.Size() || !info.ModTime().Equal(c.identity.ModTime()) ||
		ownedSettingsCheckHome(filepath.Dir(c.path)) != nil || ownedSettingsCheckReparse(c.path) != nil ||
		!pinnedCatalogMatches(c.path, c.digest) {
		return c.failure(nativehandshake.CatalogReasonPin)
	}
	return nil
}

// Pinned c40729ea protocol/openai_models.rs ReasoningEffort::as_str.
// Accept the nine named variants, not its open-ended Custom(String) branch.
func nativeSettingsEffort167(value json.RawMessage) bool {
	return requirementsEnum(value, "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent")
}

func nativeSettingsValues167(projection map[string]object, value object) bool {
	if len(value) != 2 || !experimentalFeatureString(value["model"], 1024) || !nativeSettingsEffort167(value["model_reasoning_effort"]) {
		return false
	}
	m, ok := projection[text(value, "model")]
	if !ok || !sameJSON(m["hidden"], raw(false)) {
		return false
	}
	var efforts []object
	if json.Unmarshal(m["supportedReasoningEfforts"], &efforts) != nil {
		return false
	}
	// A native model without selectable reasoning still has an effective default.
	if len(efforts) == 0 {
		return sameJSON(value["model_reasoning_effort"], m["defaultReasoningEffort"])
	}
	for _, effort := range efforts {
		if sameJSON(value["model_reasoning_effort"], effort["reasoningEffort"]) {
			return true
		}
	}
	return false
}

func (c *ownedAliasCatalog) validateNativeSettingsLists167(results map[string][]json.RawMessage, backends []Backend) error {
	if c.verify() != nil { return c.failure(nativehandshake.CatalogReasonPin) }
	if len(results) != len(backends) { return c.failure(nativehandshake.CatalogReasonBackendCount) }
	projection := map[string]object{}
	snapshot := object{}
	defaultModel := ""
	for _, b := range backends {
		rows := results[b.ID]
		if len(rows) == 0 {
			return c.failure(nativehandshake.CatalogReasonEmptyList)
		}
		backendRows := object{}
		for _, row := range rows {
			// Duplicate keys are ambiguous even when the rest of the native descriptor
			// contains optional fields unknown to this bridge. Preserve all those fields.
			if validateRequirementsJSON(row) != nil {
				return c.failure(nativehandshake.CatalogReasonJSON)
			}
			m, err := parseObject(row)
			if err != nil || validateModel(m) != nil || !requirementsBool(m["hidden"]) || !requirementsBool(m["isDefault"]) {
				return c.failure(nativehandshake.CatalogReasonModelSchema)
			}
			if requirementsNull(m["supportedReasoningEfforts"]) { return c.failure(nativehandshake.CatalogReasonEffortNull) }
			if !nativeSettingsEffort167(m["defaultReasoningEffort"]) { return c.failure(nativehandshake.CatalogReasonEffortUnknown) }
			model := text(m, "model")
			if exists(backendRows, model) {
				return c.failure(nativehandshake.CatalogReasonDuplicateModel)
			}
			backendRows[model] = row
			a := alias(b.ID, model)
			projection[a] = m
			var efforts []object
			if json.Unmarshal(m["supportedReasoningEfforts"], &efforts) != nil {
				return c.failure(nativehandshake.CatalogReasonModelSchema)
			}
			seen := map[string]bool{}
			for _, effort := range efforts {
				e := text(effort, "reasoningEffort")
				if !nativeSettingsEffort167(effort["reasoningEffort"]) { return c.failure(nativehandshake.CatalogReasonEffortUnknown) }
				if seen[e] { return c.failure(nativehandshake.CatalogReasonDuplicateEffort) }
				seen[e] = true
			}
			if len(efforts) != 0 && !seen[text(m, "defaultReasoningEffort")] {
				return c.failure(nativehandshake.CatalogReasonDefaultUnsupported)
			}
			if defaultModel == "" && sameJSON(m["isDefault"], raw(true)) && sameJSON(m["hidden"], raw(false)) {
				defaultModel = a
			}
		}
		snapshot[b.ID] = raw(backendRows)
	}
	encoded := raw(snapshot)
	if defaultModel == "" { return c.failure(nativehandshake.CatalogReasonNoVisibleDefault) }
	if c.nativeSnapshot != nil && (!sameJSON(c.nativeSnapshot, encoded) || c.defaultModel != defaultModel) {
		return c.failure(nativehandshake.CatalogReasonSnapshot)
	}
	c.nativeSnapshot, c.projection, c.defaultModel = encoded, projection, defaultModel
	return nil
}
