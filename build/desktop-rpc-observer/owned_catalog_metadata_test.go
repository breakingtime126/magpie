package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func ownedCatalogMetadataFixture(t *testing.T) *ownedModelCatalog {
	t.Helper()
	value, _ := json.Marshal(map[string]string{"path": `C:\fixture\control\bridge-model-catalog.json`, "sha256": strings.Repeat("a", 64)})
	catalog, err := parseOwnedModelCatalog(string(value))
	if err != nil {
		t.Fatal("test expectation setup rejected")
	}
	return catalog
}

func ownedCatalogConfigFixture() map[string]any {
	return map[string]any{"id": 1, "result": map[string]any{"config": map[string]any{
		"model_catalog_json": `C:\fixture\control\bridge-model-catalog.json`,
		"magpie_bridge": map[string]any{
			"catalog":    map[string]any{"version": 1, "source": "bridge-owned-alias-catalog", "sha256": strings.Repeat("a", 64), "validation": "complete-native-model-list"},
			"provenance": map[string]any{"model_catalog_json": "bridge-owned-alias-catalog"},
		},
	}, "origins": map[string]any{}}}
}

func TestOwnedCatalogMetadataIsStrictAndNeverReadsFiles(t *testing.T) {
	catalog := ownedCatalogMetadataFixture(t)
	if !catalog.matchesPath(`c:/FIXTURE/control/bridge-model-catalog.json`) || catalog.matchesPath(`C:\fixture\control\other.json`) || catalog.matchesPath(`C:\fixture\control\..\control\bridge-model-catalog.json`) {
		t.Fatal("known path comparison was widened")
	}
	for _, raw := range []string{"", "{}", `{"path":null,"sha256":"` + strings.Repeat("a", 64) + `"}`,
		`{"path":"relative","sha256":"` + strings.Repeat("a", 64) + `"}`,
		`{"path":"C:/x","sha256":"` + strings.Repeat("A", 64) + `"}`,
		`{"path":"C:/x","sha256":"` + strings.Repeat("a", 63) + `"}`,
		`{"path":"C:/x","path":"C:/y","sha256":"` + strings.Repeat("a", 64) + `"}`,
		`{"path":"C:/x","sha256":"` + strings.Repeat("a", 64) + `","extra":true}`,
	} {
		if _, err := parseOwnedModelCatalog(raw); err == nil {
			t.Fatal("invalid observer expectation accepted")
		}
	}
}

func TestOwnedCatalogConfigObservationRequiresExactProvenance(t *testing.T) {
	catalog := ownedCatalogMetadataFixture(t)
	for _, mutation := range []string{"none", "path", "digest", "source", "validation", "version", "origin", "missing", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			document := ownedCatalogConfigFixture()
			config := document["result"].(map[string]any)["config"].(map[string]any)
			extension := config["magpie_bridge"].(map[string]any)
			declaration := extension["catalog"].(map[string]any)
			switch mutation {
			case "path":
				config["model_catalog_json"] = `C:\other\private.json`
			case "digest":
				declaration["sha256"] = strings.Repeat("b", 64)
			case "source":
				declaration["source"] = "native"
			case "validation":
				declaration["validation"] = "configured-only"
			case "version":
				declaration["version"] = 2
			case "origin":
				extension["provenance"] = map[string]any{"model_catalog_json": "native"}
			case "missing":
				delete(config, "model_catalog_json")
			}
			raw, _ := json.Marshal(document)
			if mutation == "duplicate" {
				raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":2,"version":1`), 1)
			}
			got := catalog.observeConfig(raw)
			passed := got.PathMatches && got.DigestMatches && got.Validated
			if got.Schema != 1 || passed != (mutation == "none") {
				t.Fatal("config observation accepted a mismatched or unverified declaration")
			}
			safe, _ := json.Marshal(got)
			if bytes.Contains(safe, []byte("fixture")) || bytes.Contains(safe, []byte(strings.Repeat("a", 64))) || bytes.Contains(safe, []byte("private")) {
				t.Fatal("observer leaked expected or observed data")
			}
		})
	}
}

func TestOwnedCatalogObservationIsCorrelatedOptInAndFSIsOnlyBoolean(t *testing.T) {
	request := []byte(`{"id":1,"method":"config/read","params":{"includeLayers":false}}`)
	response, _ := json.Marshal(ownedCatalogConfigFixture())
	for _, enabled := range []bool{false, true} {
		var trace bytes.Buffer
		o := newObserver(&trace)
		if enabled {
			o.ownedModelCatalog = ownedCatalogMetadataFixture(t)
		}
		o.line("from_bridge", response) // No request: never attribute this response.
		o.line("to_bridge", request)
		o.line("from_bridge", response)
		if bytes.Count(trace.Bytes(), []byte(`"ownedModelCatalog"`)) != ownedMetadataBoolCount(enabled) {
			t.Fatal("config observation lacks opt-in or request correlation")
		}
		trace.Reset()
		o.line("to_bridge", []byte(`{"id":2,"method":"fs/readFile","params":{"path":"C:/fixture/control/bridge-model-catalog.json"}}`))
		if enabled {
			if !bytes.Contains(trace.Bytes(), []byte(`"ownedCatalogPathMatches":true`)) || bytes.Contains(trace.Bytes(), []byte("fsReadPathForm")) || bytes.Contains(trace.Bytes(), []byte("fsReadBasename")) || bytes.Contains(trace.Bytes(), []byte("fsReadLexicalRoot")) {
				t.Fatal("owned mode recorded more than known-path match")
			}
		} else if bytes.Contains(trace.Bytes(), []byte("ownedCatalogPathMatches")) || !bytes.Contains(trace.Bytes(), []byte("fsReadPathForm")) {
			t.Fatal("legacy observer behavior changed")
		}
		if bytes.Contains(trace.Bytes(), []byte("fixture/control")) {
			t.Fatal("fs observation leaked a raw path")
		}
	}
	t.Setenv(ownedModelCatalogEnv, "private-expectation")
	for _, item := range childEnvironment() {
		if strings.HasPrefix(item, ownedModelCatalogEnv+"=") {
			t.Fatal("observer expectation reached child environment")
		}
	}
}

func ownedMetadataBoolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
