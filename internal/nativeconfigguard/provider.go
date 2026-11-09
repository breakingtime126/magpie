// Package nativeconfigguard checks narrow native config/read identities without
// launching a process, reading credentials, or exposing native configuration.
package nativeconfigguard

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

const maxConfigBytes = 4 << 20

// These pins describe configured destinations, not authentication or transport
// success. They are independent inference and ChatGPT backend service URLs.
const (
	PrimaryInferenceURL = "https://chatgpt.com/backend-api/codex"
	PrimaryChatGPTURL   = "https://chatgpt.com/backend-api/"
)

// SecondarySpec contains reviewed expected metadata, never a credential value.
// CatalogPath, when nonempty, requires the exact native catalog configuration.
type SecondarySpec struct {
	Provider    string
	Name        string
	Endpoint    string
	EnvKey      string
	CatalogPath string
}

// MatchSecondary checks only the selected provider and optional catalog path.
// The caller owns endpoint authorization and environment/credential isolation.
func MatchSecondary(data json.RawMessage, spec SecondarySpec) bool {
	config, ok := readConfig(data)
	if !ok || spec.Provider == "" || spec.Name == "" || spec.Endpoint == "" || spec.EnvKey == "" || spec.Provider == "openai" || !equalString(config["model_provider"], spec.Provider) {
		return false
	}
	if spec.CatalogPath != "" && !equalString(config["model_catalog_json"], spec.CatalogPath) {
		return false
	}
	var providers map[string]map[string]json.RawMessage
	if json.Unmarshal(config["model_providers"], &providers) != nil || providers == nil {
		return false
	}
	selected := providers[spec.Provider]
	if selected == nil {
		return false
	}
	// ConfigToml serializes configured ModelProviderInfo defaults. Admit only
	// these fixed fields and reviewed inert defaults; never ignore new fields.
	for key, value := range selected {
		switch key {
		case "name", "base_url", "env_key", "wire_api", "requires_openai_auth":
		case "supports_websockets", "supports_standalone_web_search":
			if !equalLiteral(value, "false") {
				return false
			}
		case "model_catalog_url", "env_key_instructions", "experimental_bearer_token", "auth", "gateway_oauth", "aws", "project_id", "location", "credential_file", "capabilities", "query_params", "http_headers", "env_http_headers", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms", "websocket_connect_timeout_ms":
			if !equalLiteral(value, "null") {
				return false
			}
		default:
			return false
		}
	}
	return equalString(selected["name"], spec.Name) &&
		equalString(selected["base_url"], spec.Endpoint) &&
		equalString(selected["env_key"], spec.EnvKey) &&
		equalString(selected["wire_api"], "responses") &&
		equalLiteral(selected["requires_openai_auth"], "false")
}

// MatchPrimary requires explicit reviewed service pins. Config/read exposes
// ConfigToml, not the merged built-in provider: any openai override entry is
// rejected rather than treating it as a runtime built-in definition. This is
// not proof of ChatGPT login, an effective proxy route, or inference success.
func MatchPrimary(data json.RawMessage, inferenceURL, chatgptURL string) bool {
	if inferenceURL != PrimaryInferenceURL || chatgptURL != PrimaryChatGPTURL {
		return false
	}
	config, ok := readConfig(data)
	if !ok || !equalString(config["model_provider"], "openai") ||
		!equalString(config["openai_base_url"], inferenceURL) ||
		!equalString(config["chatgpt_base_url"], chatgptURL) {
		return false
	}
	if encoded, present := config["model_providers"]; present {
		var providers map[string]json.RawMessage
		if json.Unmarshal(encoded, &providers) != nil || providers == nil {
			return false
		}
		if _, present := providers["openai"]; present {
			return false
		}
	}
	return true
}

func equalString(encoded json.RawMessage, expected string) bool {
	var value string
	return json.Unmarshal(encoded, &value) == nil && value == expected
}

func equalLiteral(encoded json.RawMessage, expected string) bool {
	return bytes.Equal(bytes.TrimSpace(encoded), []byte(expected))
}

func readConfig(data json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(data) == 0 || len(data) > maxConfigBytes || !utf8.Valid(data) || !uniqueJSON(data) {
		return nil, false
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(data, &response) != nil || response == nil {
		return nil, false
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(response["config"], &config) != nil || config == nil {
		return nil, false
	}
	return config, true
}

// Validate duplicate keys throughout the result, including unused subtrees,
// before interpreting it. Depth and input bounds also limit parser work.
func uniqueJSON(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
