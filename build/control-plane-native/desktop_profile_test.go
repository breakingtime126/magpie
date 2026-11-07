package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDesktopFixtureReviewedBuildPins(t *testing.T) {
	for _, tc := range []struct{ build, want string }{
		{"", "40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39"},
		{"desktop-4239", "4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382"},
	} {
		got, err := desktopFixtureSHA(tc.build)
		if err != nil || got != tc.want {
			t.Fatalf("reviewed build %q selected %q, %v", tc.build, got, err)
		}
	}
	if nativeSHA != "40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39" || nativeVersion != "codex-cli 0.167.0-alpha.3.2" {
		t.Fatal("independent native acceptance pin or version changed")
	}
	for _, build := range []string{"4239", "desktop-4240", "Desktop-4239", "desktop-4239 ", nativeSHA, strings.Repeat("0", 64)} {
		if got, err := desktopFixtureSHA(build); got != "" || err != failure("input_invalid") {
			t.Fatalf("unreviewed build %q selected %q, %v", build, got, err)
		}
		var out bytes.Buffer
		err := runDesktopFixtures([]string{"-reviewed-build", build}, io.NopCloser(strings.NewReader("")), &out, &desktopAudit{})
		if err != failure("input_invalid") || out.Len() != 0 {
			t.Fatalf("unreviewed build reached fixture setup: %v, %q", err, out.String())
		}
	}
	root := t.TempDir()
	args := []string{"-bridge", filepath.Join(root, "bridge.exe"), "-codex", filepath.Join(root, "codex.exe"), "-catalog", filepath.Join(root, "models.json"), "-scratch-parent", root}
	if _, err := arguments(args); err != nil {
		t.Fatal("valid independent acceptance arguments rejected", err)
	}
	for _, value := range []string{"", "desktop-4239"} {
		if _, err := arguments(append(args, "-reviewed-build", value)); err != failure("input_invalid") {
			t.Fatal("independent acceptance accepted a Desktop-only build flag")
		}
	}
}

func TestDesktopFixtureSelectedPinsReachConfigAndReadiness(t *testing.T) {
	for _, build := range []string{"", "desktop-4239"} {
		t.Run("build="+build, func(t *testing.T) {
			pin, err := desktopFixtureSHA(build)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			codex := filepath.Join(root, "codex.exe")
			selected := filepath.Join(root, "models.json")
			alpha, beta := newDesktopFixture("alpha"), newDesktopFixture("beta")
			defer alpha.close()
			defer beta.close()
			if err := writeDesktopFixtureConfig(root, codex, selected, pin, alpha, beta); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(root, "bridge.json"))
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			backends := []any{}
			for _, name := range []string{"alpha", "beta"} {
				backends = append(backends, map[string]any{
					"id": name, "provider": name + "_provider", "command": codex,
					"home": filepath.Join(root, name+"-home"), "codex_home": filepath.Join(root, name+"-codex"),
					"startup_probe_sha256": pin,
				})
			}
			wantConfig := map[string]any{"backends": backends, "working_directory": filepath.Join(root, "workspace"), "control_plane": controlMode, "control_home": filepath.Join(root, "control-home")}
			if !reflect.DeepEqual(config, wantConfig) {
				t.Fatal("fixture config must preserve both selected pins and its existing fields")
			}
			var out bytes.Buffer
			if err := writeDesktopFixtureReady(&out, pin); err != nil {
				t.Fatal(err)
			}
			var ready map[string]any
			decoder := json.NewDecoder(&out)
			if err := decoder.Decode(&ready); err != nil {
				t.Fatal(err)
			}
			wantReady := map[string]any{"schema": float64(1), "scope": "desktop-loopback-fixtures-only", "status": "ready", "lifetime_seconds": float64(180), "native_sha256": pin, "catalog_blob": catalogBlob}
			if !reflect.DeepEqual(ready, wantReady) {
				t.Fatal("readiness must identify the selected pin without adding fields")
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatal("readiness emitted an extra record")
			}
		})
	}
}

func TestNativeBinaryHashCheckedFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.exe")
	data := []byte("synthetic non-executable hash fixture")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	expected := hex.EncodeToString(digest[:])
	if err := verifyBinaryHash(path, expected); err != nil {
		t.Fatal("matching file digest was rejected", err)
	}
	for _, build := range []string{"", "desktop-4239"} {
		pin, err := desktopFixtureSHA(build)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyBinaryHash(path, pin); err != failure("native_hash_mismatch") {
			t.Fatal("unmatched reviewed runtime bytes were accepted", err)
		}
	}
	if err := verifyBinary(path); err != failure("native_hash_mismatch") {
		t.Fatal("default verifier accepted unpinned bytes", err)
	}
	if err := os.WriteFile(path, append(data, '!'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryHash(path, expected); err != failure("native_hash_mismatch") {
		t.Fatal("modified file bytes were accepted", err)
	}
	if err := verifyBinaryHash(path+"-missing", expected); err != failure("input_invalid") {
		t.Fatal("missing runtime was accepted", err)
	}
	if err := verifyBinaryHash(filepath.Dir(path), expected); err != failure("input_invalid") {
		t.Fatal("directory was accepted as a runtime", err)
	}
}
