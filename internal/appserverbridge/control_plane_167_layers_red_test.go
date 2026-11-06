package appserverbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// Copy this file alone onto 66e39e44c2c26d23023278d1a0b8799d26f2b8bb.
// It uses only APIs already present there and must fail by assertion, not build.
func TestControlPlane167IncludeLayersBehavioralRed(t *testing.T) {
	for _, params := range []string{
		`{"includeLayers":true}`, `{"includeLayers":true,"cwd":null}`,
	} {
		if !validateControlPlaneRead167([]byte(`{"id":1,"method":"config/read","params":` + params + `}`)) {
			t.Fatal("native includeLayers:true config/read must be accepted")
		}
		r := controlPlaneRouter(t)
		out := client(t, r, `{"id":1,"method":"config/read","params":`+params+`}`)
		if len(out) != 2 {
			t.Fatal("layer read must require two fresh native policy authorities")
		}
		reply(t, r, out[0], controlPlaneNullRequirements)
		done := reply(t, r, out[1], controlPlaneNullRequirements)
		if len(done) != 1 {
			t.Fatal("layer read must return one owned result")
		}
		message, _ := parseObject(done[0].Message)
		result, _ := child(message, "result")
		var layers []object
		if len(result) != 3 || json.Unmarshal(result["layers"], &layers) != nil || len(layers) != 1 {
			t.Fatal("native includeLayers:true must return one genuine owned layer")
		}
		layer := layers[0]
		name := raw(object{"type": raw("sessionFlags")})
		if len(layer) != 3 || !sameJSON(layer["name"], name) || !sameJSON(layer["config"], result["config"]) {
			t.Fatal("layer must identify the service-owned session and equal merged config")
		}
		// This known no-catalog fixture is ASCII-only, so encoding/json matches
		// native sorted compact JSON without HTML or Unicode escape differences.
		var config any
		if json.Unmarshal(result["config"], &config) != nil {
			t.Fatal("layer config is not valid JSON")
		}
		canonical, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(canonical)
		version := "sha256:" + hex.EncodeToString(digest[:])
		if text(layer, "version") != version {
			t.Fatal("layer version must be the native hash of its actual config")
		}
		origins, err := child(result, "origins")
		if err != nil || len(origins) != 18 {
			t.Fatal("every owned primitive leaf must have a session origin")
		}
		metadata := raw(object{"name": name, "version": raw(version)})
		for _, key := range []string{
			"model_provider", "magpie_bridge.version", "magpie_bridge.view",
			"magpie_bridge.provider_routing", "magpie_bridge.execution_policy", "magpie_bridge.model_catalog",
			"magpie_bridge.supports.config_writes", "magpie_bridge.supports.layer_reads", "magpie_bridge.supports.cwd_reads",
			"magpie_bridge.provenance.model_provider",
			"magpie_bridge.backends.0.id", "magpie_bridge.backends.0.provider",
			"magpie_bridge.backends.0.model_prefix", "magpie_bridge.backends.0.catalog_source",
			"magpie_bridge.backends.1.id", "magpie_bridge.backends.1.provider",
			"magpie_bridge.backends.1.model_prefix", "magpie_bridge.backends.1.catalog_source",
		} {
			if !sameJSON(origins[key], metadata) {
				t.Fatal("owned origin must identify the same source and content version")
			}
		}
	}
}
