package main

import (
	"bytes"
	"encoding/json"
)

const acceptanceSynthetic4239 = "synthetic-models-4239"
const selfAuthoredScope = "self-authored-models-loopback-only"
const syntheticReasoningDescription = "Synthetic fixed response without reasoning."

// These records are written for this fixture, not derived from any bundled
// catalog. Public schema: openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9,
// protocol/src/openai_models.rs ModelInfo, ModelMessages and ReasoningEffort.
// supported_in_api describes only our loopback endpoint. It asserts no hosted
// model capability, account entitlement or permission-policy override.
func selfAuthoredCatalog(name string) ([]byte, error) {
	priority := 0
	switch name {
	case "alpha":
	case "beta":
		priority = 1
	default:
		return nil, failure("fixture_invalid")
	}
	model := map[string]any{
		"slug": "test-" + name, "display_name": syntheticDisplayName(name),
		"description": "Local fixed-text protocol fixture; not a hosted model.",
		"default_reasoning_level": "none",
		"supported_reasoning_levels": []any{map[string]any{
			"effort": "none", "description": syntheticReasoningDescription,
		}},
		"shell_type": "disabled", "visibility": "list", "supported_in_api": true,
		"priority": priority, "support_verbosity": false, "default_verbosity": nil,
		"truncation_policy": map[string]any{"mode": "tokens", "limit": 1024},
		"experimental_supported_tools": []any{}, "input_modalities": []string{"text"},
		"include_apps_usage_instructions": false, "include_skills_usage_instructions": false,
		"include_plugin_usage_instructions": false, "supports_reasoning_summary_parameter": false,
		"default_reasoning_summary": "none", "node_repl_disabled": true,
		"supports_search_tool": false, "apply_patch_tool_type": nil,
		"upgrade": nil, "availability_nux": nil, "additional_speed_tiers": []any{},
		"service_tiers": []any{},
		"model_messages": map[string]any{"instructions_template": syntheticInstructions(name)},
	}
	// In particular, omit guardian: an empty policy map would change approval
	// behavior. The existing read-only/never fixture configuration is untouched.
	return json.Marshal(map[string]any{"models": []any{model}})
}

func syntheticDisplayName(name string) string {
	if name == "alpha" {
		return "Synthetic Alpha"
	}
	return "Synthetic Beta"
}

func syntheticInstructions(name string) string {
	return "Return the fixed " + name + " synthetic test response."
}

func acceptanceScope(profile string) string {
	if profile == acceptanceSynthetic4239 {
		return selfAuthoredScope
	}
	return "synthetic-loopback-only"
}

func acceptanceModels(profile string) ([2]string, error) {
	switch profile {
	case "", acceptance4239:
		return [2]string{modelName, modelName}, nil
	case acceptanceSynthetic4239:
		return [2]string{"test-alpha", "test-beta"}, nil
	default:
		return [2]string{}, failure("input_invalid")
	}
}

func selfAuthoredReasoning(defaultEffort, efforts json.RawMessage) bool {
	var def string
	var levels []struct {
		Effort      string `json:"reasoningEffort"`
		Description string `json:"description"`
	}
	return json.Unmarshal(defaultEffort, &def) == nil && def == "none" &&
		json.Unmarshal(efforts, &levels) == nil && len(levels) == 1 &&
		levels[0].Effort == "none" && levels[0].Description == syntheticReasoningDescription
}

// This checks the real request without saving or logging its instructions or
// input. Tool declarations are not executions; every input item must be a text
// message, and the fixture only emits the existing assistant-text SSE events.
func selfAuthoredRequest(req map[string]json.RawMessage, name string) bool {
	if stringField(req, "instructions") != syntheticInstructions(name) {
		return false
	}
	if raw := req["reasoning"]; len(raw) != 0 && !bytes.Equal(raw, []byte("null")) {
		obj, err := object(raw)
		if err != nil || stringField(obj, "effort") != "none" {
			return false
		}
		for key, value := range obj {
			if key != "effort" && (key != "summary" || stringField(obj, key) != "none") {
				return false
			}
			if len(value) == 0 {
				return false
			}
		}
	}
	var input []map[string]json.RawMessage
	if json.Unmarshal(req["input"], &input) != nil || len(input) == 0 {
		return false
	}
	for _, item := range input {
		for key := range item {
			switch key {
			case "type", "id", "role", "content", "phase", "status", "end_turn":
			default:
				return false
			}
		}
		if _, present := item["type"]; present && stringField(item, "type") != "message" {
			return false
		}
		switch stringField(item, "role") {
		case "user", "assistant", "developer", "system":
		default:
			return false
		}
		var content []map[string]json.RawMessage
		if json.Unmarshal(item["content"], &content) != nil || len(content) == 0 {
			return false
		}
		for _, part := range content {
			for key := range part {
				if key != "type" && key != "text" {
					return false
				}
			}
			switch stringField(part, "type") {
			case "input_text", "output_text":
			default:
				return false
			}
			var text string
			if bytes.Equal(bytes.TrimSpace(part["text"]), []byte("null")) || json.Unmarshal(part["text"], &text) != nil {
				return false
			}
		}
	}
	return true
}
