package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func ownedConfigFixture(t *testing.T, model string, layers bool) map[string]any {
	t.Helper()
	value, _, err := configOracle(model, layers)
	if err != nil {
		t.Fatal("cannot construct reviewed fixture")
	}
	// Separate shared maps so each mutation changes exactly its named location.
	var result map[string]any
	if json.Unmarshal(jsonBytes(value), &result) != nil {
		t.Fatal("cannot copy reviewed fixture")
	}
	return result
}

func fixtureLayer(value map[string]any) map[string]any {
	return value["layers"].([]any)[0].(map[string]any)
}

func replaceFixtureVersion(value map[string]any, version string) {
	for _, origin := range value["origins"].(map[string]any) {
		origin.(map[string]any)["version"] = version
	}
	if _, ok := value["layers"]; ok {
		fixtureLayer(value)["version"] = version
	}
}

func TestConfigOracleMatchesPinnedNativeFingerprint(t *testing.T) {
	// Exact canonical bytes and digest from config/src/fingerprint_tests.rs at
	// c40729ea5cf8e947481b130c59807273eda2f8e9, not a bridge helper or output.
	canonical := `{"nested":{"a":true,"z":"text"},"rows":[{"a":1,"z":2},{"a":3,"z":4}]}`
	if got := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(canonical))); got != "sha256:a5e4c8fbdbc75d185b0f2b5291bfb2de10e8c523d6c89248876fd78a97c83176" {
		t.Fatal("native fingerprint oracle changed")
	}
	for _, test := range []struct{ model, version string }{
		{"", "sha256:2321f7ff8a7d52027c364fa6da2dce92d864e8e2b4038bbf6ae5e5a739c6e55a"},
		{"magpie/alpha/gpt-5.5", "sha256:7198cb2e8c6ae27053963a18131d3b565bd11f63cea1cbcb74085eabb2314775"},
		{"magpie/beta/gpt-5.5", "sha256:95db4ecd36075a143ec16856f2af98eef9a58ecfa4ee7c605855755f858e3881"},
	} {
		value, version, err := configOracle(test.model, true)
		if err != nil || version != test.version {
			t.Fatal("reviewed routing fingerprint changed")
		}
		want := initialConfigJSON
		if test.model != "" {
			want = fmt.Sprintf(resolvedConfigJSON, test.model)
		}
		if string(jsonBytes(value["config"])) != want {
			t.Fatal("oracle bytes are not recursively sorted compact JSON")
		}
	}
}

func TestConfigReadOwnsOnlyReviewedRoutingFields(t *testing.T) {
	for _, model := range []string{"", "magpie/alpha/gpt-5.5", "magpie/beta/gpt-5.5"} {
		for _, layers := range []bool{false, true} {
			value := ownedConfigFixture(t, model, layers)
			if _, err := checkConfig(jsonBytes(value), model, layers); err != nil {
				t.Fatal("reviewed owned routing view rejected")
			}
			origins := value["origins"].(map[string]any)
			want := 18
			if model != "" {
				want = 20
			}
			if len(origins) != want {
				t.Fatal("primitive leaf provenance count changed")
			}
		}
	}
	initial := ownedConfigFixture(t, "", true)
	if _, err := checkConfig(jsonBytes(initial), "magpie/alpha/gpt-5.5", true); err == nil {
		t.Fatal("unresolved default accepted after catalog")
	}
	if _, _, err := configOracle("magpie/unknown/gpt-5.5", true); err == nil {
		t.Fatal("unreviewed model added to acceptance oracle")
	}
}

func TestConfigCheckerRejectsLayerOriginAndVersionMutations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing-layer", func(v map[string]any) { delete(v, "layers") }},
		{"null-layers", func(v map[string]any) { v["layers"] = nil }},
		{"empty-layers", func(v map[string]any) { v["layers"] = []any{} }},
		{"extra-layer", func(v map[string]any) { v["layers"] = append(v["layers"].([]any), fixtureLayer(v)) }},
		{"native-source", func(v map[string]any) {
			fixtureLayer(v)["name"] = map[string]any{"type": "user", "file": "/synthetic/private/config.toml", "profile": nil}
		}},
		{"source-extra-field", func(v map[string]any) {
			fixtureLayer(v)["name"].(map[string]any)["file"] = "/synthetic/fictional/config.toml"
		}},
		{"disabled-layer", func(v map[string]any) { fixtureLayer(v)["disabledReason"] = "synthetic" }},
		{"null-disabled-layer", func(v map[string]any) { fixtureLayer(v)["disabledReason"] = nil }},
		{"layer-config-differs", func(v map[string]any) {
			fixtureLayer(v)["config"].(map[string]any)["model_provider"] = "alpha_provider"
		}},
		{"native-policy", func(v map[string]any) { v["config"].(map[string]any)["approval_policy"] = "never" }},
		{"secret-field", func(v map[string]any) { fixtureLayer(v)["config"].(map[string]any)["token"] = "SYNTHETIC-SECRET" }},
		{"missing-origins", func(v map[string]any) { delete(v, "origins") }},
		{"empty-origins", func(v map[string]any) { v["origins"] = map[string]any{} }},
		{"missing-leaf", func(v map[string]any) { delete(v["origins"].(map[string]any), "magpie_bridge.backends.1.provider") }},
		{"container-origin", func(v map[string]any) {
			v["origins"].(map[string]any)["magpie_bridge"] = v["origins"].(map[string]any)["model_provider"]
		}},
		{"wrong-array-origin", func(v map[string]any) {
			o := v["origins"].(map[string]any)
			o["magpie_bridge.backends[0].id"] = o["magpie_bridge.backends.0.id"]
			delete(o, "magpie_bridge.backends.0.id")
		}},
		{"origin-source-differs", func(v map[string]any) {
			v["origins"].(map[string]any)["model"].(map[string]any)["name"] = map[string]any{"type": "system", "file": "/synthetic/private/config.toml"}
		}},
		{"origin-version-differs", func(v map[string]any) {
			v["origins"].(map[string]any)["model"].(map[string]any)["version"] = "sha256:" + strings.Repeat("0", 64)
		}},
		{"layer-version-differs", func(v map[string]any) { fixtureLayer(v)["version"] = "sha256:" + strings.Repeat("0", 64) }},
		{"consistent-wrong-digest", func(v map[string]any) { replaceFixtureVersion(v, "sha256:"+strings.Repeat("0", 64)) }},
		{"missing-hash-prefix", func(v map[string]any) {
			replaceFixtureVersion(v, strings.TrimPrefix(fixtureLayer(v)["version"].(string), "sha256:"))
		}},
		{"reordered-backends-with-new-digest", func(v map[string]any) {
			c := v["config"].(map[string]any)
			b := c["magpie_bridge"].(map[string]any)["backends"].([]any)
			b[0], b[1] = b[1], b[0]
			fixtureLayer(v)["config"] = c
			replaceFixtureVersion(v, fmt.Sprintf("sha256:%x", sha256.Sum256(jsonBytes(c))))
		}},
		{"unreviewed-result-field", func(v map[string]any) { v["private_path"] = "/synthetic/private" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := ownedConfigFixture(t, "magpie/alpha/gpt-5.5", true)
			test.mutate(value)
			if _, err := checkConfig(jsonBytes(value), "magpie/alpha/gpt-5.5", true); err != failure("config_shape_mismatch") {
				t.Fatal("untruthful layer, origin or fingerprint accepted")
			}
		})
	}
}

func TestConfigCheckerDistinguishesLayerOmissionAndNull(t *testing.T) {
	for _, layers := range []any{nil, []any{}} {
		value := ownedConfigFixture(t, "", false)
		value["layers"] = layers
		if _, err := checkConfig(jsonBytes(value), "", false); err == nil {
			t.Fatal("false read advertised layers")
		}
	}
	value := ownedConfigFixture(t, "", true)
	if _, err := checkConfig(jsonBytes(value), "", false); err == nil {
		t.Fatal("false read exposed its layer")
	}
}

func TestConfigCheckerRejectsDuplicateFieldsAndTrailingData(t *testing.T) {
	raw := string(jsonBytes(ownedConfigFixture(t, "", true)))
	for _, invalid := range []string{
		strings.Replace(raw, `"config":{`, `"config":{},"config":{`, 1),
		strings.Replace(raw, `"type":"sessionFlags"`, `"type":"user","type":"sessionFlags"`, 1),
		strings.Replace(raw, `"model_provider":"magpie_bridge"`, `"model_provider":"alpha_provider","model_provider":"magpie_bridge"`, 1),
		raw + `{}`,
		raw[:len(raw)-1],
		"null",
		"[]",
	} {
		if _, err := checkConfig([]byte(invalid), "", true); err != failure("config_shape_mismatch") {
			t.Fatal("ambiguous or incomplete config accepted")
		}
	}
}

type scriptedConfigReader struct {
	responses []json.RawMessage
	flags     []bool
	failAt    int
}

func (s *scriptedConfigReader) rpc(method string, params any) (json.RawMessage, error) {
	p, ok := params.(map[string]any)
	include, valid := p["includeLayers"].(bool)
	if method != "config/read" || !ok || len(p) != 1 || !valid {
		return nil, failure("protocol_invalid")
	}
	s.flags = append(s.flags, include)
	if s.failAt == len(s.flags) {
		return nil, failure("native_error")
	}
	if len(s.flags) > len(s.responses) {
		return nil, failure("protocol_invalid")
	}
	return s.responses[len(s.flags)-1], nil
}

func configReadScript(t *testing.T, model string) *scriptedConfigReader {
	t.Helper()
	s := &scriptedConfigReader{}
	for _, layers := range []bool{false, true, true} {
		s.responses = append(s.responses, jsonBytes(ownedConfigFixture(t, model, layers)))
	}
	return s
}

func TestConfigReadRequiresFreshFalseTrueTrueExchange(t *testing.T) {
	s := configReadScript(t, "magpie/alpha/gpt-5.5")
	var report summary
	if _, err := readConfig(s, "magpie/alpha/gpt-5.5", &report); err != nil {
		t.Fatal("reviewed config sequence failed")
	}
	if !reflect.DeepEqual(s.flags, []bool{false, true, true}) ||
		report.ConfigReads != 3 || report.LayerReads != 2 || report.StableLayerRereads != 1 {
		t.Fatal("stable reread was not independently observed")
	}
}

func TestConfigReadDoesNotCachePastLaterNativeErrorOrChangedVersion(t *testing.T) {
	for _, mode := range []string{"native-error", "version-change"} {
		t.Run(mode, func(t *testing.T) {
			s := configReadScript(t, "")
			if mode == "native-error" {
				s.failAt = 3
			} else {
				value := ownedConfigFixture(t, "", true)
				replaceFixtureVersion(value, "sha256:"+strings.Repeat("0", 64))
				s.responses[2] = jsonBytes(value)
			}
			var report summary
			if _, err := readConfig(s, "", &report); err == nil {
				t.Fatal("later failed or changed read used cached success")
			}
			if len(s.flags) != 3 || report.ConfigReads != 2 || report.LayerReads != 1 || report.StableLayerRereads != 0 {
				t.Fatal("failed reread counted as verified")
			}
		})
	}
}
