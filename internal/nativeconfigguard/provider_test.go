package nativeconfigguard

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func secondaryFixture() (map[string]any, SecondarySpec) {
	spec := SecondarySpec{Provider: "magpie_preview_aihub", Name: "AIHub preview", Endpoint: "https://supplier.invalid/v1", EnvKey: "MAGPIE_NATIVE_PREVIEW_KEY"}
	selected := map[string]any{
		"name": spec.Name, "base_url": spec.Endpoint, "env_key": spec.EnvKey,
		"wire_api": "responses", "requires_openai_auth": false,
	}
	return map[string]any{"config": map[string]any{
		"model_provider": spec.Provider,
		"model_providers": map[string]any{spec.Provider: selected},
	}}, spec
}

func fixtureJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func secondarySelected(fixture map[string]any, spec SecondarySpec) map[string]any {
	return fixture["config"].(map[string]any)["model_providers"].(map[string]any)[spec.Provider].(map[string]any)
}

func TestSecondaryAcceptsOnlyReviewedProviderShape(t *testing.T) {
	fixture, spec := secondaryFixture()
	if !MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("minimal reviewed secondary rejected")
	}
	selected := secondarySelected(fixture, spec)
	for _, key := range []string{"model_catalog_url", "env_key_instructions", "experimental_bearer_token", "auth", "gateway_oauth", "aws", "project_id", "location", "credential_file", "capabilities", "query_params", "http_headers", "env_http_headers", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms", "websocket_connect_timeout_ms"} {
		selected[key] = nil
	}
	selected["supports_websockets"] = false
	selected["supports_standalone_web_search"] = false
	data := fixtureJSON(t, fixture)
	before := append([]byte(nil), data...)
	if !MatchSecondary(data, spec) || !bytes.Equal(data, before) {
		t.Fatal("serialized inert defaults rejected or input mutated")
	}
	fixture["config"].(map[string]any)["model_catalog_json"] = "/synthetic/catalog.json"
	spec.CatalogPath = "/synthetic/catalog.json"
	if !MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("exact catalog path rejected")
	}
	spec.CatalogPath = "/synthetic/other.json"
	if MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("catalog mismatch accepted")
	}
	delete(fixture["config"].(map[string]any), "model_catalog_json")
	if MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("missing required catalog path accepted")
	}
}

func TestSecondaryRejectsAuthRoutingAndUnknownFields(t *testing.T) {
	for _, key := range []string{"model_catalog_url", "env_key_instructions", "experimental_bearer_token", "auth", "gateway_oauth", "aws", "project_id", "location", "credential_file", "capabilities", "query_params", "http_headers", "env_http_headers", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms", "websocket_connect_timeout_ms", "unknown"} {
		t.Run(key, func(t *testing.T) {
			for _, value := range []any{"PRIVATE_CANARY", map[string]any{"header": "PRIVATE_CANARY"}, false, 0, []any{}} {
				fixture, spec := secondaryFixture()
				secondarySelected(fixture, spec)[key] = value
				if MatchSecondary(fixtureJSON(t, fixture), spec) {
					t.Fatal("non-inert provider field accepted")
				}
			}
		})
	}
	for _, key := range []string{"supports_websockets", "supports_standalone_web_search", "requires_openai_auth"} {
		for _, value := range []any{true, nil, "false", 0} {
			fixture, spec := secondaryFixture()
			secondarySelected(fixture, spec)[key] = value
			if MatchSecondary(fixtureJSON(t, fixture), spec) {
				t.Fatal("non-false capability/auth field accepted")
			}
		}
	}
	fixture, spec := secondaryFixture()
	secondarySelected(fixture, spec)["unknown"] = nil
	if MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("unknown null field accepted")
	}
}

func TestSecondaryRequiresEveryIdentityField(t *testing.T) {
	for _, key := range []string{"name", "base_url", "env_key", "wire_api", "requires_openai_auth"} {
		fixture, spec := secondaryFixture()
		delete(secondarySelected(fixture, spec), key)
		if MatchSecondary(fixtureJSON(t, fixture), spec) {
			t.Fatal("missing identity field accepted")
		}
		for _, value := range []any{"", "different", nil, 1, false} {
			fixture, spec = secondaryFixture()
			secondarySelected(fixture, spec)[key] = value
			if key == "requires_openai_auth" && value == false {
				continue
			}
			if MatchSecondary(fixtureJSON(t, fixture), spec) {
				t.Fatal("incorrect identity field accepted")
			}
		}
	}
	fixture, spec := secondaryFixture()
	fixture["config"].(map[string]any)["model_provider"] = "openai"
	if MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("wrong selected provider accepted")
	}
}

func primaryFixture() map[string]any {
	return map[string]any{"config": map[string]any{
		"model_provider": "openai", "openai_base_url": PrimaryInferenceURL,
		"chatgpt_base_url": PrimaryChatGPTURL, "model_providers": map[string]any{},
	}}
}

func TestPrimaryRequiresExplicitExactServicePins(t *testing.T) {
	fixture := primaryFixture()
	if !MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
		t.Fatal("explicit reviewed primary rejected")
	}
	delete(fixture["config"].(map[string]any), "model_providers")
	if !MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
		t.Fatal("absent configured provider map rejected")
	}
	for _, key := range []string{"model_provider", "openai_base_url", "chatgpt_base_url"} {
		fixture = primaryFixture()
		delete(fixture["config"].(map[string]any), key)
		if MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
			t.Fatal("implicit default substituted for required explicit field")
		}
		for _, value := range []any{nil, "", false, "https://supplier.invalid/v1", "https://chatgpt.com/backend-api", PrimaryChatGPTURL + "?redirect=PRIVATE_CANARY"} {
			fixture = primaryFixture()
			fixture["config"].(map[string]any)[key] = value
			if MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
				t.Fatal("non-exact primary identity or endpoint accepted")
			}
		}
	}
	fixture = primaryFixture()
	fixture["config"].(map[string]any)["openai_base_url"] = "https://supplier.invalid/v1"
	if MatchPrimary(fixtureJSON(t, fixture), "https://supplier.invalid/v1", PrimaryChatGPTURL) || MatchPrimary(fixtureJSON(t, primaryFixture()), PrimaryInferenceURL, "") {
		t.Fatal("caller expectation widened reviewed primary pins")
	}
}

func TestPrimaryRejectsEveryOpenAIProviderOverride(t *testing.T) {
	for _, override := range []any{nil, map[string]any{}, map[string]any{"model_catalog_url": nil}, map[string]any{"model_catalog_url": "https://chatgpt.com/backend-api/codex/models"}, map[string]any{"base_url": "https://supplier.invalid/v1"}, map[string]any{"requires_openai_auth": true}, "PRIVATE_CANARY"} {
		fixture := primaryFixture()
		fixture["config"].(map[string]any)["model_providers"] = map[string]any{"openai": override}
		if MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
			t.Fatal("configured OpenAI override accepted")
		}
	}
	for _, malformed := range []any{nil, "", []any{}, false, 1} {
		fixture := primaryFixture()
		fixture["config"].(map[string]any)["model_providers"] = malformed
		if MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
			t.Fatal("malformed provider map treated as absent")
		}
	}
	fixture := primaryFixture()
	fixture["config"].(map[string]any)["model_providers"] = map[string]any{"unselected": map[string]any{"name": "synthetic"}}
	if !MatchPrimary(fixtureJSON(t, fixture), PrimaryInferenceURL, PrimaryChatGPTURL) {
		t.Fatal("unselected provider mistaken for OpenAI override")
	}
}

func TestGuardsRejectAmbiguousOrUnboundedResults(t *testing.T) {
	fixture, spec := secondaryFixture()
	secondary := string(fixtureJSON(t, fixture))
	primary := string(fixtureJSON(t, primaryFixture()))
	for _, base := range []string{secondary, primary} {
		cases := []string{
			base + `{}`, base[:len(base)-1], `null`, `[]`, `{"config":null}`,
			strings.Replace(base, `"config":`, `"config":{},"config":`, 1),
			strings.Replace(base, `"model_provider":`, `"model_provider":"wrong","model_provider":`, 1),
			base[:len(base)-1] + `,"unused":{"key":1,"\u006bey":2}}`,
			base[:len(base)-1] + `,"unused":` + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34) + "}",
			base[:len(base)-1] + ",\"unused\":\"" + string([]byte{0xff}) + "\"}",
		}
		if base == secondary {
			cases = append(cases, strings.Replace(base, `"wire_api":`, `"wire_api":"wrong","wire_api":`, 1))
		}
		for _, invalid := range cases {
			if MatchSecondary(json.RawMessage(invalid), spec) || MatchPrimary(json.RawMessage(invalid), PrimaryInferenceURL, PrimaryChatGPTURL) {
				t.Fatal("ambiguous or invalid input accepted")
			}
		}
		atLimit := []byte(base + strings.Repeat(" ", maxConfigBytes-len(base)))
		if base == secondary && !MatchSecondary(atLimit, spec) {
			t.Fatal("valid result at byte limit rejected")
		}
		if base == primary && !MatchPrimary(atLimit, PrimaryInferenceURL, PrimaryChatGPTURL) {
			t.Fatal("valid primary at byte limit rejected")
		}
		tooLarge := append(atLimit, ' ')
		if MatchSecondary(tooLarge, spec) || MatchPrimary(tooLarge, PrimaryInferenceURL, PrimaryChatGPTURL) {
			t.Fatal("byte limit not enforced")
		}
	}
}

func TestSecondaryRejectsMalformedProviderMapsAndEmptyExpectations(t *testing.T) {
	for _, value := range []any{nil, "", []any{}, false, map[string]any{"magpie_preview_aihub": nil}} {
		fixture, spec := secondaryFixture()
		fixture["config"].(map[string]any)["model_providers"] = value
		if MatchSecondary(fixtureJSON(t, fixture), spec) {
			t.Fatal("malformed provider map accepted")
		}
	}
	fixture, spec := secondaryFixture()
	providers := fixture["config"].(map[string]any)["model_providers"].(map[string]any)
	providers["unselected"] = "malformed"
	if MatchSecondary(fixtureJSON(t, fixture), spec) {
		t.Fatal("refactor weakened existing provider-map shape check")
	}
	fixture, spec = secondaryFixture()
	data := fixtureJSON(t, fixture)
	for _, invalid := range []SecondarySpec{
		{},
		{Provider: spec.Provider, Name: spec.Name, EnvKey: spec.EnvKey},
		{Provider: spec.Provider, Name: spec.Name, Endpoint: spec.Endpoint},
		{Provider: spec.Provider, Endpoint: spec.Endpoint, EnvKey: spec.EnvKey},
		{Provider: "openai", Name: spec.Name, Endpoint: spec.Endpoint, EnvKey: spec.EnvKey},
	} {
		if MatchSecondary(data, invalid) {
			t.Fatal("incomplete or primary expectation accepted as secondary")
		}
	}
}
