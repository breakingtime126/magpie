package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

const desktopOwnedCatalogProfile = "bridge-owned-catalog-4239"
const desktopOwnedCatalogScope = "desktop-bridge-owned-catalog-fixtures-only"

func desktopUsesSelfAuthoredCatalog(profile string) bool {
	return profile == acceptanceSynthetic4239 || profile == desktopOwnedCatalogProfile
}

// The only input is the existing original fixture builder. The public file
// maps slug and display_name; every other authored field remains unchanged.
// It is separate from both native catalogs and is not a native configuration.
func bridgeOwnedSelfAuthoredCatalog() ([]byte, error) {
	models := make([]map[string]json.RawMessage, 0, 2)
	for _, name := range []string{"alpha", "beta"} {
		data, err := selfAuthoredCatalog(name)
		if err != nil {
			return nil, err
		}
		var document struct {
			Models []map[string]json.RawMessage `json:"models"`
		}
		if json.Unmarshal(data, &document) != nil || len(document.Models) != 1 {
			return nil, failure("fixture_invalid")
		}
		model := document.Models[0]
		model["slug"] = jsonBytes("magpie/" + name + "/test-" + name)
		model["display_name"] = jsonBytes(syntheticDisplayName(name) + " [" + name + "]")
		models = append(models, model)
	}
	return json.Marshal(map[string]any{"models": models})
}

func writeDesktopFixtureReadyFromRoot(out io.Writer, pinnedSHA, profile, root string) error {
	if profile != desktopOwnedCatalogProfile {
		return writeDesktopFixtureReadyForProfile(out, pinnedSHA, profile)
	}
	if pinnedSHA != native4239SHA {
		return failure("input_invalid")
	}
	data, err := readBounded(filepath.Join(root, "control-home", "bridge-model-catalog.json"), 64<<10)
	if err != nil {
		return err
	}
	want, err := bridgeOwnedSelfAuthoredCatalog()
	if err != nil || !bytes.Equal(data, want) {
		return failure("fixture_invalid")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	// Readiness proves fixture creation only. The separate correlated config
	// observation proves bridge loading and complete native catalog validation.
	if _, err := fmt.Fprintf(out, "{\"schema\":1,\"scope\":%q,\"status\":\"ready\",\"lifetime_seconds\":180,\"native_sha256\":%q,\"catalog_source\":\"bridge-owned-self-authored-test-models-v1\",\"owned_catalog_sha256\":%q}\n", desktopOwnedCatalogScope, pinnedSHA, digest); err != nil {
		return failure("output_failed")
	}
	return nil
}
