package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSelfAuthoredRecordsAreIndependentTextOnlyDefinitions(t *testing.T) {
	for i, name := range []string{"alpha", "beta"} {
		data, err := selfAuthoredCatalog(name)
		if err != nil {
			t.Fatal("self-authored record could not be constructed")
		}
		var catalog struct {
			Models []map[string]any `json:"models"`
		}
		if json.Unmarshal(data, &catalog) != nil || len(catalog.Models) != 1 {
			t.Fatal("backend did not receive exactly one self-authored record")
		}
		m := catalog.Models[0]
		if m["slug"] != "test-"+name || m["display_name"] != syntheticDisplayName(name) ||
			m["priority"] != float64(i) || m["shell_type"] != "disabled" ||
			m["visibility"] != "list" || m["supported_in_api"] != true ||
			m["default_reasoning_level"] != "none" || m["default_reasoning_summary"] != "none" ||
			m["node_repl_disabled"] != true {
			t.Fatal("self-authored model identity or text-only contract changed")
		}
		for _, key := range []string{"support_verbosity", "include_apps_usage_instructions",
			"include_skills_usage_instructions", "include_plugin_usage_instructions",
			"supports_reasoning_summary_parameter", "supports_search_tool"} {
			if m[key] != false {
				t.Fatal("synthetic-only metadata was omitted or enabled")
			}
		}
		for _, key := range []string{"experimental_supported_tools", "additional_speed_tiers", "service_tiers"} {
			items, ok := m[key].([]any)
			if !ok || len(items) != 0 {
				t.Fatal("synthetic model advertised unsupported tools or service tiers")
			}
		}
		for _, key := range []string{"guardian", "confirmation_policies", "permissions", "auto_review_model_override",
			"node_repl_auto_review_required", "available_access_programs", "base_instructions"} {
			if _, exists := m[key]; exists {
				t.Fatal("synthetic model introduced policy or non-fixture instructions")
			}
		}
		if !reflect.DeepEqual(m["input_modalities"], []any{"text"}) ||
			!reflect.DeepEqual(m["truncation_policy"], map[string]any{"mode": "tokens", "limit": float64(1024)}) ||
			!reflect.DeepEqual(m["model_messages"], map[string]any{"instructions_template": syntheticInstructions(name)}) ||
			!reflect.DeepEqual(m["supported_reasoning_levels"], []any{map[string]any{
				"effort": "none", "description": syntheticReasoningDescription,
			}}) {
			t.Fatal("self-authored text, instructions or none-effort definition changed")
		}
		again, err := selfAuthoredCatalog(name)
		if err != nil || !bytes.Equal(data, again) || bytes.Contains(data, []byte("test-"+[]string{"beta", "alpha"}[i])) {
			t.Fatal("self-authored catalog was unstable or mixed backend records")
		}
	}
	if _, err := selfAuthoredCatalog("unreviewed"); err != failure("fixture_invalid") {
		t.Fatal("unknown synthetic backend accepted")
	}
}

func TestSelfAuthoredProfileCannotReadAnyCatalogInput(t *testing.T) {
	root := t.TempDir()
	args := []string{"-bridge", filepath.Join(root, "bridge.exe"), "-codex", filepath.Join(root, "codex.exe"),
		"-scratch-parent", root, "-acceptance-profile", acceptanceSynthetic4239}
	if o, err := arguments(args); err != nil || o.catalog != "" {
		t.Fatal("self-authored profile required an external catalog")
	}
	for _, value := range []string{"", " ", filepath.Join(root, "never-open.json")} {
		if _, err := arguments(append(append([]string(nil), args...), "-catalog", value)); err != failure("input_invalid") {
			t.Fatal("self-authored profile accepted a catalog flag")
		}
	}
	if _, err := arguments(append(append([]string(nil), args...), "--catalog=")); err != failure("input_invalid") {
		t.Fatal("self-authored profile accepted an explicitly empty catalog assignment")
	}
	var report summary
	if err := run(context.Background(), options{acceptanceProfile: acceptanceSynthetic4239, catalog: "never-open"}, &report); err != failure("input_invalid") || report.BinaryVerified {
		t.Fatal("direct run did not reject mixed catalog inputs before setup")
	}
	if models, err := acceptanceModels(acceptanceSynthetic4239); err != nil || models != [2]string{"test-alpha", "test-beta"} {
		t.Fatal("synthetic model ownership changed")
	}
	for _, profile := range []string{"synthetic-models", "SYNTHETIC-MODELS-4239", acceptanceSynthetic4239 + " "} {
		if _, err := acceptanceSHA(profile); err != failure("input_invalid") {
			t.Fatal("unknown profile was silently accepted")
		}
		if _, err := acceptanceModels(profile); err != failure("input_invalid") {
			t.Fatal("unknown profile fell back to another model set")
		}
	}
}

func TestSelfAuthoredSummaryCannotMasqueradeAsLegacy(t *testing.T) {
	s := passedSummary()
	s.NativeSHA256 = native4239SHA
	legacy := s
	s.Scope = selfAuthoredScope
	if !validSummaryForProfile(s, acceptanceSynthetic4239) || validSummary(s) ||
		validSummaryForProfile(s, acceptance4239) || validSummaryForProfile(legacy, acceptanceSynthetic4239) {
		t.Fatal("summary provenance was not bound to its explicit model profile")
	}
	for _, change := range []func(*summary){
		func(s *summary) { s.NativeSHA256 = nativeSHA },
		func(s *summary) { s.Turns = 3 },
		func(s *summary) { s.CatalogModels = 1 },
		func(s *summary) { s.AssistantMessages = 5 },
		func(s *summary) { s.AlphaRequests = 1 },
		func(s *summary) { s.FixtureErrors = 1 },
		func(s *summary) { s.CleanupComplete = false },
	} {
		bad := s
		change(&bad)
		if validSummaryForProfile(bad, acceptanceSynthetic4239) {
			t.Fatal("self-authored profile weakened the existing acceptance counts")
		}
	}
}

func selfAuthoredModelList() map[string]any {
	rows := []any{}
	for i, name := range []string{"alpha", "beta"} {
		alias := "magpie/" + name + "/test-" + name
		rows = append(rows, map[string]any{"id": alias, "model": alias,
			"displayName": syntheticDisplayName(name) + " [" + name + "]", "isDefault": i == 0, "hidden": false,
			"defaultReasoningEffort": "none", "supportedReasoningEfforts": []any{map[string]any{
				"reasoningEffort": "none", "description": syntheticReasoningDescription,
			}}})
	}
	return map[string]any{"data": rows, "nextCursor": nil}
}

func TestSelfAuthoredCatalogOracleRequiresActualOwnedModelsAndNone(t *testing.T) {
	list := selfAuthoredModelList()
	if model, n, err := checkCatalogForProfile(jsonBytes(list), acceptanceSynthetic4239); err != nil || model != "magpie/alpha/test-alpha" || n != 2 {
		t.Fatal("complete self-authored native model response rejected")
	}
	if _, _, err := checkCatalog(jsonBytes(list)); err == nil {
		t.Fatal("legacy oracle accepted the independent model set")
	}
	for _, change := range []func(map[string]any){
		func(row map[string]any) { row["model"] = "magpie/alpha/test-beta" },
		func(row map[string]any) { row["id"] = "collapsed" },
		func(row map[string]any) { row["displayName"] = "Synthetic Beta [alpha]" },
		func(row map[string]any) { row["hidden"] = true },
		func(row map[string]any) { row["defaultReasoningEffort"] = "low" },
		func(row map[string]any) { row["supportedReasoningEfforts"] = []any{} },
		func(row map[string]any) { delete(row, "isDefault") },
	} {
		bad := selfAuthoredModelList()
		change(bad["data"].([]any)[0].(map[string]any))
		if _, _, err := checkCatalogForProfile(jsonBytes(bad), acceptanceSynthetic4239); err == nil {
			t.Fatal("incomplete, crossed or hidden native catalog accepted")
		}
	}
}

func TestSelfAuthoredConfigAndThreadOraclesUseMatchingAliases(t *testing.T) {
	for _, name := range []string{"alpha", "beta"} {
		alias := "magpie/" + name + "/test-" + name
		got, version, err := configOracleForProfile(alias, true, acceptanceSynthetic4239)
		if err != nil || version == "" {
			t.Fatal("self-authored config alias rejected")
		}
		if _, err := checkConfigForProfile(jsonBytes(got), alias, true, acceptanceSynthetic4239); err != nil {
			t.Fatal("self-authored config layer did not round trip")
		}
		if _, _, err := configOracle(alias, true); err == nil {
			t.Fatal("legacy config oracle accepted the independent model set")
		}
	}
	threads := [2]string{"first", "second"}
	row := map[string]any{"id": threads[0], "model": "magpie/alpha/test-alpha", "preview": "synthetic",
		"source": "vscode", "modelProvider": "magpie_bridge", "ephemeral": false}
	page := map[string]any{"data": []any{row}, "nextCursor": nil}
	if _, _, err := checkThreadListPageForProfile(jsonBytes(page), threads, map[string]bool{}, true, acceptanceSynthetic4239); err != nil {
		t.Fatal("owned synthetic thread model rejected")
	}
	for _, model := range []any{nil, "magpie/alpha/test-beta", "magpie/beta/test-beta"} {
		row["model"] = model
		if _, _, err := checkThreadListPageForProfile(jsonBytes(page), threads, map[string]bool{}, true, acceptanceSynthetic4239); err == nil {
			t.Fatal("unproven or wrong-owner thread model accepted")
		}
	}
}

func selfAuthoredRequestBody(name string, n int) map[string]any {
	return map[string]any{"model": "test-" + name, "stream": true,
		"instructions": syntheticInstructions(name), "reasoning": map[string]any{"effort": "none"},
		"input": []any{map[string]any{"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": fmt.Sprintf("synthetic-%s-turn-%d", name, n)}}}}}
}

func TestSelfAuthoredFixturesAcceptOnlyOwnedTextAndFixedResponses(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	for _, name := range []string{"alpha", "beta"} {
		f := newFixtureWithModel(name, "test-"+name, syntheticInstructions(name))
		t.Cleanup(f.close)
		for n := 1; n <= 2; n++ {
			req := selfAuthoredRequestBody(name, n)
			if n == 2 {
				delete(req, "reasoning")
			}
			response, err := client.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(jsonBytes(req)))
			if err != nil {
				t.Fatal("loopback request failed")
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || !bytes.Contains(data, []byte("fixture-answer-"+name)) {
				t.Fatal("owned synthetic text response failed")
			}
		}
		if requests, failures := f.counts(); requests != 2 || failures != 0 {
			t.Fatal("synthetic fixture counts changed")
		}
	}
}

func TestSelfAuthoredFixtureRejectsWrongModelsInstructionsReasoningAndNonText(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(req map[string]any) { req["model"] = "test-beta" },
		func(req map[string]any) { req["instructions"] = "Unreviewed instructions." },
		func(req map[string]any) { req["reasoning"] = map[string]any{"effort": "low"} },
		func(req map[string]any) { req["reasoning"] = map[string]any{"effort": "none", "summary": "auto"} },
		func(req map[string]any) {
			req["input"] = []any{map[string]any{"type": "function_call", "name": "shell"}}
		},
		func(req map[string]any) {
			req["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image"}}}}
		},
		func(req map[string]any) {
			req["input"] = []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "synthetic-alpha-turn-1", "image_url": "never-fetch"},
			}}}
		},
		func(req map[string]any) { req["input"] = selfAuthoredRequestBody("beta", 1)["input"] },
	} {
		f := newFixtureWithModel("alpha", "test-alpha", syntheticInstructions("alpha"))
		t.Cleanup(f.close)
		body := selfAuthoredRequestBody("alpha", 1)
		change(body)
		client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
		response, err := client.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(jsonBytes(body)))
		if err != nil {
			t.Fatal("loopback rejection request failed")
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		client.CloseIdleConnections()
		if response.StatusCode != 400 {
			t.Fatal("unsupported synthetic request accepted")
		}
		if requests, failures := f.counts(); requests != 0 || failures != 1 {
			t.Fatal("rejected synthetic request was counted as accepted")
		}
	}
}

func TestSelfAuthoredRequestRejectsInvalidMessageTypesAndNullText(t *testing.T) {
	for _, kind := range []any{nil, "", false, 1, map[string]any{}} {
		req := selfAuthoredRequestBody("alpha", 1)
		item := req["input"].([]any)[0].(map[string]any)
		item["type"] = kind
		raw, err := object(jsonBytes(req))
		if err != nil || selfAuthoredRequest(raw, "alpha") {
			t.Fatal("explicit invalid message type accepted")
		}
	}
	req := selfAuthoredRequestBody("alpha", 1)
	item := req["input"].([]any)[0].(map[string]any)
	delete(item, "type")
	raw, err := object(jsonBytes(req))
	if err != nil || !selfAuthoredRequest(raw, "alpha") {
		t.Fatal("omitted message type rejected")
	}
	item["content"].([]any)[0].(map[string]any)["text"] = nil
	raw, err = object(jsonBytes(req))
	if err != nil || selfAuthoredRequest(raw, "alpha") {
		t.Fatal("null text accepted")
	}
}

func TestSelfAuthoredNativeConfigChangesOnlyTheModelSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-authored.json")
	for _, name := range []string{"alpha", "beta"} {
		provider := name + "_provider"
		legacy, err := nativeConfig(provider, "http://127.0.0.1:1234", path)
		if err != nil {
			t.Fatal("existing fixture config failed")
		}
		got, err := nativeConfigForModel(provider, "http://127.0.0.1:1234", path, "test-"+name)
		want := strings.Replace(string(legacy), "model = "+string(jsonBytes(modelName)), "model = "+string(jsonBytes("test-"+name)), 1)
		if err != nil || string(got) != want {
			t.Fatal("self-authored model changed the existing native isolation or permission config")
		}
		if _, err := nativeConfigForModel(provider, "http://127.0.0.1:1234", path, "test-unknown"); err == nil {
			t.Fatal("unknown original model accepted")
		}
	}
}
