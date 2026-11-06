package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// This oracle is deliberately independent of internal/appserverbridge.
// Pinned native sources at openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9:
// app-server-protocol/src/protocol/v2/config.rs defines the layer envelope;
// config/src/thread_config.rs maps service-owned session values to sessionFlags;
// config/src/fingerprint.rs hashes recursively sorted compact JSON and records
// primitive leaf origins, with decimal array indices joined by dots.
//
// The reviewed fixture has only fixed ASCII strings, integer 1 and booleans.
// Keep its already-sorted JSON literals instead of reusing the bridge's config,
// origin traversal or serializer. No native config is requested or inspected.
const initialConfigJSON = `{"magpie_bridge":{"backends":[{"catalog_source":"native-model-list","id":"alpha","model_prefix":"magpie/alpha/","provider":"alpha_provider"},{"catalog_source":"native-model-list","id":"beta","model_prefix":"magpie/beta/","provider":"beta_provider"}],"execution_policy":"backend-owned-per-thread","model_catalog":"model/list","provenance":{"model_provider":"bridge-routing"},"provider_routing":"fixed-per-thread","supports":{"config_writes":false,"cwd_reads":false,"layer_reads":true},"version":1,"view":"routing-defaults"},"model_provider":"magpie_bridge"}`
const resolvedConfigJSON = `{"magpie_bridge":{"backends":[{"catalog_source":"native-model-list","id":"alpha","model_prefix":"magpie/alpha/","provider":"alpha_provider"},{"catalog_source":"native-model-list","id":"beta","model_prefix":"magpie/beta/","provider":"beta_provider"}],"execution_policy":"backend-owned-per-thread","model_catalog":"model/list","provenance":{"model":"bridge-catalog-default","model_provider":"bridge-routing"},"provider_routing":"fixed-per-thread","supports":{"config_writes":false,"cwd_reads":false,"layer_reads":true},"version":1,"view":"routing-defaults"},"model":"%s","model_provider":"magpie_bridge"}`

var configOriginPaths = []string{
	"model_provider",
	"magpie_bridge.version",
	"magpie_bridge.view",
	"magpie_bridge.provider_routing",
	"magpie_bridge.execution_policy",
	"magpie_bridge.model_catalog",
	"magpie_bridge.supports.config_writes",
	"magpie_bridge.supports.cwd_reads",
	"magpie_bridge.supports.layer_reads",
	"magpie_bridge.provenance.model_provider",
	"magpie_bridge.backends.0.catalog_source",
	"magpie_bridge.backends.0.id",
	"magpie_bridge.backends.0.model_prefix",
	"magpie_bridge.backends.0.provider",
	"magpie_bridge.backends.1.catalog_source",
	"magpie_bridge.backends.1.id",
	"magpie_bridge.backends.1.model_prefix",
	"magpie_bridge.backends.1.provider",
}

func configOracle(model string, includeLayers bool) (map[string]any, string, error) {
	canonical := initialConfigJSON
	if model != "" {
		if model != "magpie/alpha/"+modelName && model != "magpie/beta/"+modelName {
			return nil, "", failure("config_shape_mismatch")
		}
		canonical = fmt.Sprintf(resolvedConfigJSON, model)
	}
	digest := sha256.Sum256([]byte(canonical))
	version := fmt.Sprintf("sha256:%x", digest)
	var config any
	if json.Unmarshal([]byte(canonical), &config) != nil {
		return nil, "", failure("config_shape_mismatch")
	}
	name := map[string]any{"type": "sessionFlags"}
	metadata := map[string]any{"name": name, "version": version}
	origins := map[string]any{}
	for _, path := range configOriginPaths {
		origins[path] = metadata
	}
	if model != "" {
		origins["model"] = metadata
		origins["magpie_bridge.provenance.model"] = metadata
	}
	result := map[string]any{"config": config, "origins": origins}
	if includeLayers {
		result["layers"] = []any{map[string]any{"name": name, "version": version, "config": config}}
	}
	return result, version, nil
}

type configReader interface {
	rpc(string, any) (json.RawMessage, error)
}

// Every phase performs three independent RPCs. The adapter must still obtain
// fresh paired native requirements for each; no preceding success authorizes a
// cached response. The second true read checks stable session provenance.
func readConfig(p configReader, model string, report *summary) (string, error) {
	version := ""
	for _, includeLayers := range []bool{false, true, true} {
		raw, err := p.rpc("config/read", map[string]any{"includeLayers": includeLayers})
		if err != nil {
			return "", err
		}
		got, err := checkConfig(raw, model, includeLayers)
		if err != nil {
			return "", err
		}
		if version != "" && got != version {
			return "", failure("config_shape_mismatch")
		}
		version = got
		report.ConfigReads++
		if includeLayers {
			report.LayerReads++
		}
	}
	report.StableLayerRereads++
	return version, nil
}

func checkConfig(raw json.RawMessage, model string, includeLayers bool) (string, error) {
	want, version, err := configOracle(model, includeLayers)
	if err != nil {
		return "", err
	}
	actual, err := decodeConfigJSON(raw)
	if err != nil || !reflect.DeepEqual(actual, want) {
		return "", failure("config_shape_mismatch")
	}
	return version, nil
}

// Reject duplicate fields rather than allowing a last-key-wins parse to hide
// an extra value in config, its layer, source metadata or origins.
func decodeConfigJSON(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > maxMessage {
		return nil, failure("config_shape_mismatch")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	value, err := configJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, failure("config_shape_mismatch")
	}
	return value, nil
}

func configJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, failure("config_shape_mismatch")
	}
	token, err := d.Token()
	if err != nil {
		return nil, failure("config_shape_mismatch")
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	switch delim {
	case '{':
		result := map[string]any{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok {
				return nil, failure("config_shape_mismatch")
			}
			if _, exists := result[key]; exists {
				return nil, failure("config_shape_mismatch")
			}
			value, err := configJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, failure("config_shape_mismatch")
		}
		return result, nil
	case '[':
		result := []any{}
		for d.More() {
			value, err := configJSONValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, failure("config_shape_mismatch")
		}
		return result, nil
	default:
		return nil, failure("config_shape_mismatch")
	}
}
