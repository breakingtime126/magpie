package main

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/yetone/magpie/internal/appserverbridge"
)

const ownedModelCatalogEnv = "MAGPIE_CI_OWNED_MODEL_CATALOG"

// Harness expectations only. This tap never opens the file or changes RPCs.
// Neither the configured path nor digest is emitted in observer events.
type ownedModelCatalog struct {
	path   string
	digest string
}

type ownedModelCatalogObservation struct {
	Schema        int  `json:"schema"`
	PathMatches   bool `json:"pathMatches"`
	DigestMatches bool `json:"digestMatches"`
	Validated     bool `json:"validated"`
}

func parseOwnedModelCatalog(value string) (*ownedModelCatalog, error) {
	bad := errors.New("invalid test-only owned catalog")
	if len(value) == 0 || len(value) > maxReadTargetConfig {
		return nil, bad
	}
	fields, ok := authMetadataObject([]byte(value), 2)
	if !ok || len(fields) != 2 {
		return nil, bad
	}
	path, valid := ownedMetadataString(fields["path"])
	canonical, pathOK := lexicalWindowsAbsolute(path, false)
	digest, digestOK := ownedMetadataString(fields["sha256"])
	if !valid || !pathOK || !digestOK || len(digest) != 64 {
		return nil, bad
	}
	for _, c := range digest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, bad
		}
	}
	return &ownedModelCatalog{path: canonical, digest: digest}, nil
}

func ownedMetadataString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if !nativeJSONString(raw) {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func (catalog *ownedModelCatalog) matchesPath(path string) bool {
	canonical, ok := lexicalWindowsAbsolute(path, false)
	return ok && canonical == catalog.path
}

func ownedCatalogRequestEligible(raw []byte) bool {
	envelope, ok := observedRequestEnvelope(raw, "config/read")
	if !ok {
		return false
	}
	if trace, present := envelope["trace"]; present && !appserverbridge.NativeTrace167Valid(trace) {
		return false
	}
	valid, _, _ := observedConfigRead(raw)
	return valid != nil && *valid
}

// Called only after direction/ID correlation with an eligible config/read.
// A malformed/missing declaration yields fixed false flags, never raw content.
func (catalog *ownedModelCatalog) observeConfig(raw []byte) *ownedModelCatalogObservation {
	out := &ownedModelCatalogObservation{Schema: 1}
	envelope, ok := authMetadataObject(raw, 3)
	if !ok || !appserverbridge.NativeRequestID167Valid(envelope["id"]) {
		return out
	}
	for key := range envelope {
		if key != "id" && key != "result" && key != "trace" {
			return out
		}
	}
	if trace, present := envelope["trace"]; present && !appserverbridge.NativeTrace167Valid(trace) {
		return out
	}
	result, ok := authMetadataObject(envelope["result"], 3)
	if !ok {
		return out
	}
	config, ok := authMetadataObject(result["config"], 128)
	if !ok {
		return out
	}
	path, valid := ownedMetadataString(config["model_catalog_json"])
	out.PathMatches = valid && catalog.matchesPath(path)
	extension, ok := authMetadataObject(config["magpie_bridge"], 16)
	if !ok {
		return out
	}
	declaration, ok := authMetadataObject(extension["catalog"], 4)
	if !ok || len(declaration) != 4 {
		return out
	}
	digest, valid := ownedMetadataString(declaration["sha256"])
	out.DigestMatches = valid && digest == catalog.digest
	provenance, ok := authMetadataObject(extension["provenance"], 8)
	if !ok {
		return out
	}
	source, sourceOK := ownedMetadataString(declaration["source"])
	validation, validationOK := ownedMetadataString(declaration["validation"])
	origin, originOK := ownedMetadataString(provenance["model_catalog_json"])
	out.Validated = bytes.Equal(bytes.TrimSpace(declaration["version"]), []byte("1")) &&
		sourceOK && source == "bridge-owned-alias-catalog" &&
		validationOK && validation == "complete-native-model-list" &&
		originOK && origin == "bridge-owned-alias-catalog"
	return out
}
