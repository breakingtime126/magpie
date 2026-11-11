package nativecatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func renderedModels(t *testing.T, models []Model) ([]byte, []map[string]json.RawMessage) {
	t.Helper()
	data, err := Render(models)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Models) != len(models) {
		t.Fatal("catalog wrapper or model count changed")
	}
	return data, result.Models
}

func field[T any](t *testing.T, model map[string]json.RawMessage, key string) T {
	t.Helper()
	var value T
	if raw, ok := model[key]; !ok || json.Unmarshal(raw, &value) != nil {
		t.Fatalf("missing or invalid %s", key)
	}
	return value
}

func TestRenderMinimalConfiguredModel(t *testing.T) {
	_, models := renderedModels(t, []Model{{ID: "supplier/my-model", Name: "My Model"}})
	m := models[0]
	for key, want := range map[string]string{
		"slug": "supplier/my-model", "display_name": "My Model", "shell_type": "unified_exec",
		"tool_mode": "direct", "visibility": "list", "default_reasoning_summary": "none",
	} {
		if got := field[string](t, m, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if !field[bool](t, m, "supported_in_api") || !field[bool](t, m, "node_repl_disabled") || field[int](t, m, "priority") != 0 {
		t.Fatal("client selection policy changed")
	}
	for _, key := range []string{
		"support_verbosity", "supports_reasoning_summary_parameter", "supports_search_tool",
		"use_responses_lite", "supports_image_detail_original", "supports_experimental_context",
		"supports_reasoning_effort_updates", "include_apps_usage_instructions",
		"include_skills_usage_instructions", "include_plugin_usage_instructions",
	} {
		if field[bool](t, m, key) {
			t.Errorf("unsupported optional behavior enabled: %s", key)
		}
	}
	for _, key := range []string{"supported_reasoning_levels", "experimental_supported_tools"} {
		if !bytes.Equal(m[key], []byte("[]")) {
			t.Errorf("%s must be an empty array, not missing or null", key)
		}
	}
	if got := field[[]string](t, m, "input_modalities"); !reflect.DeepEqual(got, []string{"text"}) {
		t.Fatal("unknown model inherited image or audio capability")
	}
	for _, key := range []string{
		"guardian", "apply_patch_tool_type", "default_reasoning_level", "max_context_window",
		"max_output_tokens", "output_modalities", "supports_parallel_tool_calls", "service_tiers",
		"auto_review_model_override", "multi_agent_version", "base_instructions",
	} {
		if _, ok := m[key]; ok {
			t.Errorf("invented vendor fact or unrelated policy present: %s", key)
		}
	}
	if got := field[map[string]string](t, m, "model_messages"); len(got) != 1 || got["instructions_template"] != instructions || instructions == "" {
		t.Fatal("required independent instruction template missing or changed")
	}
	policy := field[truncationPolicy](t, m, "truncation_policy")
	if policy.Mode != "tokens" || policy.Limit != 2048 {
		t.Fatal("bounded client tool-output policy changed")
	}
	if field[int](t, m, "context_window") != 8192 || field[int](t, m, "auto_compact_token_limit") != 6144 || field[int](t, m, "effective_context_window_percent") != 95 {
		t.Fatal("unknown supplier did not receive the documented client working budget")
	}
}

func TestRenderConfiguredContextAndReasoning(t *testing.T) {
	input := []Model{{
		ID: "vendor/model:tag", Name: "Configured Model", Context: 32000,
		Efforts: []string{"low", "future-effort", "high", "low"}, DefaultEffort: "high",
	}}
	before, _ := json.Marshal(input)
	first, models := renderedModels(t, input)
	second, _ := renderedModels(t, input)
	after, _ := json.Marshal(input)
	if !bytes.Equal(first, second) || !bytes.Equal(before, after) {
		t.Fatal("rendering is not deterministic or mutated its inputs")
	}
	m := models[0]
	if field[int](t, m, "context_window") != 32000 || field[int](t, m, "auto_compact_token_limit") != 24000 {
		t.Fatal("configured context was replaced by a guessed window")
	}
	if field[string](t, m, "default_reasoning_level") != "high" {
		t.Fatal("declared supported default lost")
	}
	levels := field[[]reasoningLevel](t, m, "supported_reasoning_levels")
	if len(levels) != 2 || levels[0].Effort != "low" || levels[1].Effort != "high" || levels[0].Description == "" {
		t.Fatal("declared reasoning choices were invented, duplicated or reordered")
	}
}

func TestRenderUnknownReasoningDoesNotBlockCatalog(t *testing.T) {
	for _, input := range []Model{
		{ID: "vendor/model", Name: "Unknown", Efforts: []string{"future-effort"}, DefaultEffort: "future-effort"},
		{ID: "vendor/model", Name: "Unknown", Efforts: []string{"HIGH", " high", "high\n", ""}, DefaultEffort: "high"},
		{ID: "vendor/model", Name: "Unknown", DefaultEffort: "high"},
		{ID: "vendor/model", Name: "Unknown", Efforts: []string{strings.Repeat("x", 4096)}, DefaultEffort: strings.Repeat("x", 4096)},
	} {
		_, models := renderedModels(t, []Model{input})
		if !bytes.Equal(models[0]["supported_reasoning_levels"], []byte("[]")) {
			t.Fatal("unknown levels did not remain unknown")
		}
		if _, ok := models[0]["default_reasoning_level"]; ok {
			t.Fatal("undeclared reasoning default was invented")
		}
	}
	_, models := renderedModels(t, []Model{{ID: "vendor/model", Name: "Known choices", Efforts: []string{"low"}, DefaultEffort: "high"}})
	if _, ok := models[0]["default_reasoning_level"]; ok || len(field[[]reasoningLevel](t, models[0], "supported_reasoning_levels")) != 1 {
		t.Fatal("invalid default changed declared choices")
	}
}

func TestRenderKnownEffortsAndContextBoundaries(t *testing.T) {
	efforts := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent"}
	for _, effort := range efforts {
		_, models := renderedModels(t, []Model{{ID: "v/m", Name: "Model", Efforts: []string{effort}, DefaultEffort: effort}})
		if field[string](t, models[0], "default_reasoning_level") != effort {
			t.Fatalf("recognized declared effort %s was lost", effort)
		}
	}
	largest := int(^uint(0) >> 1)
	if limit := maxContextBudget; int64(largest) > limit {
		largest = int(limit)
	}
	for _, budget := range []int{1, 2, 3, 4, 8191, largest} {
		_, models := renderedModels(t, []Model{{ID: "v/m", Name: "Model", Context: budget}})
		m := models[0]
		compact := field[int](t, m, "auto_compact_token_limit")
		if field[int](t, m, "context_window") != budget || compact < 0 || compact > budget || compact != (budget/4)*3+(budget%4)*3/4 {
			t.Fatalf("working budget overflow or substitution for %d", budget)
		}
	}
}

func TestRenderValidRouteAndLabelForms(t *testing.T) {
	for _, id := range []string{"provider/model", "provider.id/@vendor/model:latest", "provider_1/model-2.0"} {
		name := `Model "quoted" <label> 中文`
		_, models := renderedModels(t, []Model{{ID: id, Name: name}})
		if field[string](t, models[0], "slug") != id || field[string](t, models[0], "display_name") != name {
			t.Fatal("route or label changed during JSON encoding")
		}
	}
}

func TestRenderRejectsUnsafeAndDuplicateInputs(t *testing.T) {
	valid := Model{ID: "provider/model", Name: "Model"}
	cases := map[string][]Model{"empty": nil, "duplicate": {valid, valid}}
	for _, id := range []string{"", "model", "/model", "p/", "p//m", "p/../m", "p/./m", "https://host/model", "p/m?key=x", "p/m#x", "p/m%2fother", "p/m\\other", "p/m\n", "p/with space", "p/模型", "p/\x00", "p/" + strings.Repeat("x", maxIDBytes)} {
		m := valid
		m.ID = id
		cases["id:"+id] = []Model{m}
	}
	for _, name := range []string{"", " ", " leading", "trailing ", "line\nbreak", "tab\tname", "bad\x00name", "bidi\u202ename", "zero\u200bwidth", "line\u2028break", string([]byte{0xff}), strings.Repeat("x", maxNameBytes+1)} {
		m := valid
		m.Name = name
		cases["name:"+name] = []Model{m}
	}
	negative := valid
	negative.Context = -1
	cases["negative context"] = []Model{negative}
	if largest := int(^uint(0) >> 1); int64(largest) > maxContextBudget {
		overflow := valid
		overflow.Context = largest
		cases["native context arithmetic overflow"] = []Model{overflow}
	}
	manyEfforts := valid
	manyEfforts.Efforts = make([]string, maxEfforts+1)
	cases["too many efforts"] = []Model{manyEfforts}
	cases["too many models"] = make([]Model, maxModels+1)
	for name, models := range cases {
		t.Run(name, func(t *testing.T) {
			if data, err := Render(models); !errors.Is(err, ErrInvalidCatalog) || data != nil {
				t.Fatal("unsafe input produced a catalog or an unexpected error")
			}
		})
	}
}

func TestRenderCatalogSizeAndOrder(t *testing.T) {
	_, models := renderedModels(t, []Model{{ID: "z/model", Name: "Z"}, {ID: "a/model", Name: "A"}})
	if field[string](t, models[0], "slug") != "z/model" || field[int](t, models[1], "priority") != 1 {
		t.Fatal("configured model order or stable priority changed")
	}
	large := make([]Model, maxModels)
	for i := range large {
		large[i] = Model{ID: fmt.Sprintf("p/%04d%s", i, strings.Repeat("x", maxIDBytes-6)), Name: strings.Repeat("x", maxNameBytes)}
	}
	if data, err := Render(large); !errors.Is(err, ErrInvalidCatalog) || data != nil {
		t.Fatal("oversized serialized catalog accepted")
	}
}
