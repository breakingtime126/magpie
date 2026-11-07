package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDesktopSelfAuthoredProfileRejectsCatalogPresenceAndUnknownModes(t *testing.T) {
	valid := []string{"-fixture-profile", acceptanceSynthetic4239, "-reviewed-build", "desktop-4239"}
	for _, suffix := range [][]string{{"-catalog", ""}, {"--catalog="}, {"-catalog", filepath.Join(t.TempDir(), "never-read.json")}} {
		var out bytes.Buffer
		if err := runDesktopFixtures(append(append([]string{}, valid...), suffix...), io.NopCloser(strings.NewReader("")), &out, &desktopAudit{}); err != failure("input_invalid") || out.Len() != 0 {
			t.Fatal("self-authored Desktop accepted catalog input or emitted readiness")
		}
	}
	for _, tc := range []struct{ profile, build string }{
		{"synthetic-models-4239 ", "desktop-4239"}, {"unknown", "desktop-4239"},
		{acceptanceSynthetic4239, ""}, {acceptanceSynthetic4239, "desktop-4240"},
	} {
		if validateDesktopFixtureProfile(tc.profile, tc.build, false) != failure("input_invalid") {
			t.Fatal("unknown or mismatched Desktop profile accepted")
		}
	}
	if validateDesktopFixtureProfile(acceptanceSynthetic4239, "desktop-4239", false) != nil ||
		validateDesktopFixtureProfile("", "", true) != nil {
		t.Fatal("valid explicit or unchanged legacy profile rejected")
	}
}

func TestDesktopSelfAuthoredConfigurationUsesIndependentOwnedRecords(t *testing.T) {
	root := t.TempDir()
	alpha := newDesktopFixtureForModel("alpha", "test-alpha", syntheticInstructions("alpha"))
	beta := newDesktopFixtureForModel("beta", "test-beta", syntheticInstructions("beta"))
	t.Cleanup(alpha.close)
	t.Cleanup(beta.close)
	if err := writeDesktopFixtureConfigForProfile(root, filepath.Join(root, "codex.exe"), "", native4239SHA, acceptanceSynthetic4239, alpha, beta); err != nil {
		t.Fatal("self-authored fixture configuration failed")
	}
	for _, f := range []*fixture{alpha, beta} {
		path := filepath.Join(root, f.name+"-self-authored-catalog.json")
		want, err := selfAuthoredCatalog(f.name)
		if err != nil {
			t.Fatal("self-authored builder failed")
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatal("Desktop catalog differs from the independent native record")
		}
		config, err := os.ReadFile(filepath.Join(root, f.name+"-codex", "config.toml"))
		wantConfig, wantErr := nativeConfigForModel(f.name+"_provider", f.server.URL, path, "test-"+f.name)
		if err != nil || wantErr != nil || !bytes.Equal(config, wantConfig) {
			t.Fatal("Desktop fixture changed native isolation or crossed model ownership")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "models.json")); !os.IsNotExist(err) {
		t.Fatal("self-authored Desktop created the legacy shared catalog")
	}
	if _, err := os.Stat(filepath.Join(root, "control-home", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("self-authored Desktop invented frontend config")
	}
	for _, tc := range []struct{ selected, pin, profile string }{
		{"unread-input", native4239SHA, acceptanceSynthetic4239},
		{"", nativeSHA, acceptanceSynthetic4239},
		{"", native4239SHA, "unknown"},
	} {
		if writeDesktopFixtureConfigForProfile(t.TempDir(), "unused", tc.selected, tc.pin, tc.profile) != failure("input_invalid") {
			t.Fatal("unreviewed configuration branch accepted")
		}
	}
}

func TestDesktopSelfAuthoredReadinessCannotClaimLegacyCatalog(t *testing.T) {
	var out bytes.Buffer
	if writeDesktopFixtureReadyForProfile(&out, native4239SHA, acceptanceSynthetic4239) != nil {
		t.Fatal("self-authored readiness failed")
	}
	var got map[string]any
	decoder := json.NewDecoder(&out)
	if decoder.Decode(&got) != nil {
		t.Fatal("self-authored readiness is not JSON")
	}
	want := map[string]any{"schema": float64(1), "scope": desktopSelfAuthoredScope, "status": "ready",
		"lifetime_seconds": float64(180), "native_sha256": native4239SHA, "catalog_source": "self-authored-test-models-v1"}
	if !reflect.DeepEqual(got, want) || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("self-authored readiness scope, source or fixed bounds changed")
	}
	for _, tc := range []struct{ pin, profile string }{{nativeSHA, acceptanceSynthetic4239}, {native4239SHA, "unknown"}} {
		var rejected bytes.Buffer
		if writeDesktopFixtureReadyForProfile(&rejected, tc.pin, tc.profile) != failure("input_invalid") || rejected.Len() != 0 {
			t.Fatal("unreviewed readiness emitted")
		}
	}
}

func desktopSelfAuthoredRequestBody(name string) map[string]any {
	return map[string]any{"model": "test-" + name, "stream": true,
		"reasoning": map[string]any{"effort": "none"}, "input": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{
				map[string]any{"type": "input_text", "text": syntheticInstructions(name)},
			}},
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "synthetic-" + name + "-UI test"},
			}},
		}}
}

func TestDesktopSelfAuthoredEndpointsKeepModelsInstructionsAndTextOwned(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	for _, name := range []string{"alpha", "beta"} {
		f := newDesktopFixtureForModel(name, "test-"+name, syntheticInstructions(name))
		t.Cleanup(f.close)
		for _, mutate := range []func(map[string]any){
			nil,
			func(req map[string]any) { req["model"] = "test-other" },
			func(req map[string]any) {
				req["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] = "Different test instructions."
			},
			func(req map[string]any) { req["reasoning"] = map[string]any{"effort": "low"} },
			func(req map[string]any) { req["reasoning"] = map[string]any{"effort": "none", "summary": "auto"} },
			func(req map[string]any) {
				req["input"] = []any{map[string]any{"type": "function_call", "name": "shell"}}
			},
			func(req map[string]any) { req["input"] = desktopSelfAuthoredRequestBody("other")["input"] },
		} {
			req := desktopSelfAuthoredRequestBody(name)
			wantStatus := http.StatusOK
			if mutate != nil {
				mutate(req)
				wantStatus = http.StatusBadRequest
			}
			resp, err := client.Post(f.server.URL+"/v1/responses", "application/json", bytes.NewReader(jsonBytes(req)))
			if err != nil {
				t.Fatal("local Desktop fixture request failed")
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != wantStatus || (wantStatus == http.StatusOK && !bytes.Contains(data, []byte("fixture-answer-"+name))) {
				t.Fatal("self-authored Desktop request oracle failed")
			}
		}
		if accepted, rejected := f.counts(); accepted != 1 || rejected != 6 {
			t.Fatal("self-authored Desktop fixture counts changed")
		}
	}
}
