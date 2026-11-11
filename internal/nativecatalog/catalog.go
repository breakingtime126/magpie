// Package nativecatalog renders Magpie's own client policy as a small Codex
// catalog. It reads no native records, credentials, files or provider state.
//
// The wire contract is the public OpenAI Codex source at
// c40729ea5cf8e947481b130c59807273eda2f8e9 (0.167.0-alpha.3.2):
// codex-rs/protocol/src/openai_models.rs, ModelInfo and ModelsResponse, and
// codex-rs/core/src/config/mod.rs, load_catalog_json. ModelsResponse requires an
// instruction template, but does not require complete vendor metadata.
package nativecatalog

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxModels       = 1024
	maxCatalogBytes = 1 << 20
	maxIDBytes      = 512
	maxNameBytes    = 256
	maxEfforts      = 64
	// Pinned ModelInfo::auto_compact_token_limit multiplies context by nine
	// before dividing by ten. Reject values that overflow that native i64.
	maxContextBudget = int64(1<<63-1) / 9

	// These are client working budgets, not claims about an unknown supplier's
	// capacity. A configured Context takes precedence. Even this small fallback
	// cannot guarantee that a supplier with an unknown smaller limit accepts it.
	fallbackContext = 8192
	fallbackCompact = 6144
	toolOutputLimit = 2048
)

// ErrInvalidCatalog deliberately includes no supplied model data.
var ErrInvalidCatalog = errors.New("native catalog input is invalid or exceeds its limit")

// Model contains only route identity and explicitly configured model facts.
// ID is already gateway-qualified (provider/model); Render never invents routes.
// Context, when positive, is used as the client's working budget. Zero means
// unknown. Efforts and DefaultEffort describe declared picker choices, not a
// filter of reasoning values explicitly requested elsewhere in native config.
type Model struct {
	ID            string
	Name          string
	Context       int
	Efforts       []string
	DefaultEffort string
}

// These instructions are authored for Magpie, not taken from a native catalog.
const instructions = "You are a coding assistant connected through Magpie. Help the user complete their requested work. Inspect relevant files before changing them, follow repository guidance, and verify changes with appropriate checks. Use the tools actually available to you and respect their approval and sandbox requirements. Report completed work, checks, and remaining limitations accurately."

type reasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type catalogModel struct {
	Slug                           string           `json:"slug"`
	DisplayName                    string           `json:"display_name"`
	DefaultReasoningLevel          string           `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels       []reasoningLevel `json:"supported_reasoning_levels"`
	ShellType                      string           `json:"shell_type"`
	Visibility                     string           `json:"visibility"`
	SupportedInAPI                 bool             `json:"supported_in_api"`
	Priority                       int              `json:"priority"`
	SupportVerbosity               bool             `json:"support_verbosity"`
	SupportsReasoningSummary       bool             `json:"supports_reasoning_summary_parameter"`
	DefaultReasoningSummary        string           `json:"default_reasoning_summary"`
	IncludeAppsUsageInstructions   bool             `json:"include_apps_usage_instructions"`
	IncludeSkillsUsageInstructions bool             `json:"include_skills_usage_instructions"`
	IncludePluginUsageInstructions bool             `json:"include_plugin_usage_instructions"`
	TruncationPolicy               truncationPolicy `json:"truncation_policy"`
	ContextWindow                  int              `json:"context_window"`
	AutoCompactTokenLimit          int              `json:"auto_compact_token_limit"`
	EffectiveContextWindowPercent  int              `json:"effective_context_window_percent"`
	ExperimentalSupportedTools     []string         `json:"experimental_supported_tools"`
	InputModalities                []string         `json:"input_modalities"`
	SupportsSearchTool             bool             `json:"supports_search_tool"`
	SupportsImageDetailOriginal    bool             `json:"supports_image_detail_original"`
	SupportsExperimentalContext    bool             `json:"supports_experimental_context"`
	SupportsReasoningEffortUpdates bool             `json:"supports_reasoning_effort_updates"`
	UseResponsesLite               bool             `json:"use_responses_lite"`
	NodeREPLDisabled               bool             `json:"node_repl_disabled"`
	ToolMode                       string           `json:"tool_mode"`
	ModelMessages                  modelMessages    `json:"model_messages"`
}

type truncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

type modelMessages struct {
	InstructionsTemplate string `json:"instructions_template"`
}

// Render returns a deterministic, bounded catalog for the exact configured
// routes. ShellType selects native's standard JSON function tools, not a
// supplier-owned shell. Omitting apply_patch_tool_type avoids its sole freeform
// variant. No guardian or other approval-policy metadata is supplied.
// Visibility and supported_in_api are picker policy for these configured
// routes, not proof of supplier entitlement or successful API transport.
//
// Catalog choices are not a global tool or request filter: configured MCP and
// other native tools remain separate, native may send explicit reasoning, and
// this pin sends parallel_tool_calls independently of catalog metadata. Actual
// supplier compatibility and tool execution need separate verification.
func Render(models []Model) ([]byte, error) {
	if len(models) == 0 || len(models) > maxModels {
		return nil, ErrInvalidCatalog
	}
	result := struct {
		Models []catalogModel `json:"models"`
	}{Models: make([]catalogModel, 0, len(models))}
	seen := make(map[string]bool, len(models))
	for i, m := range models {
		if !validID(m.ID) || !validName(m.Name) || m.Context < 0 || int64(m.Context) > maxContextBudget || len(m.Efforts) > maxEfforts || seen[m.ID] {
			return nil, ErrInvalidCatalog
		}
		seen[m.ID] = true
		levels, defaultEffort := declaredEfforts(m.Efforts, m.DefaultEffort)
		budget, compact := m.Context, 0
		if budget == 0 {
			budget, compact = fallbackContext, fallbackCompact
		} else {
			// Three quarters without overflowing even at the largest int.
			compact = (budget/4)*3 + (budget%4)*3/4
		}
		result.Models = append(result.Models, catalogModel{
			Slug: m.ID, DisplayName: m.Name, Priority: i,
			DefaultReasoningLevel: defaultEffort, SupportedReasoningLevels: levels,
			ShellType: "unified_exec", ToolMode: "direct", Visibility: "list", SupportedInAPI: true,
			DefaultReasoningSummary: "none", NodeREPLDisabled: true,
			TruncationPolicy: truncationPolicy{Mode: "tokens", Limit: toolOutputLimit},
			ContextWindow:    budget, AutoCompactTokenLimit: compact, EffectiveContextWindowPercent: 95,
			ExperimentalSupportedTools: []string{}, InputModalities: []string{"text"},
			ModelMessages: modelMessages{InstructionsTemplate: instructions},
		})
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > maxCatalogBytes {
		return nil, ErrInvalidCatalog
	}
	return data, nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > maxIDBytes {
		return false
	}
	parts := strings.Split(id, "/")
	if len(parts) < 2 {
		return false
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
				continue
			}
			// Model identifiers may contain tags or vendor namespaces, but
			// the provider segment is never a URL or an authority component.
			if i > 0 && (r == ':' || r == '@') {
				continue
			}
			return false
		}
	}
	return true
}

func validName(name string) bool {
	if name == "" || len(name) > maxNameBytes || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) && r != ' ' {
			return false
		}
	}
	return true
}

func declaredEfforts(efforts []string, defaultEffort string) ([]reasoningLevel, string) {
	levels := make([]reasoningLevel, 0, len(efforts))
	seen := map[string]bool{}
	for _, effort := range efforts {
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent":
		default:
			continue
		}
		if !seen[effort] {
			levels = append(levels, reasoningLevel{Effort: effort, Description: "Declared " + effort + " reasoning effort."})
			seen[effort] = true
		}
	}
	if len(defaultEffort) > 16 || !seen[defaultEffort] {
		defaultEffort = ""
	}
	return levels, defaultEffort
}
