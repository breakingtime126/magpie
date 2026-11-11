package nativesupplier

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/testenv"
)

func configuredFixture(t *testing.T, models ...string) {
	t.Helper()
	home := t.TempDir()
	testenv.SetHome(t, home)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA", "CODEX_HOME"} {
		t.Setenv(name, filepath.Join(home, name))
	}
	writeConfiguredFixture(t, models...)
}

func writeConfiguredFixture(t *testing.T, models ...string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"providers": []map[string]any{{
		"id": "company-aihub", "name": "AIHub", "chat": configuredAIHubEndpoint,
		"key": "SYNTHETIC_CONFIG_KEY_CANARY", "models": models,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(provider.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.Path(), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredCatalogUsesMinimalCurrentModelsWithoutNativeMetadata(t *testing.T) {
	configuredFixture(t, "deepseek-flash-ft", "gpt-5.5")
	s, err := configuredCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.expires.IsZero() || !s.monitor || strings.Contains(string(s.catalog), "CANARY") || strings.Contains(string(s.catalog), configuredAIHubEndpoint) {
		t.Fatal("configured catalog leaked a credential/address or imposed stale-evidence expiry")
	}
	var c struct {
		Models []struct {
			Slug    string   `json:"slug"`
			Context int      `json:"context_window"`
			Inputs  []string `json:"input_modalities"`
		} `json:"models"`
	}
	if json.Unmarshal(s.catalog, &c) != nil || len(c.Models) != 1 || c.Models[0].Slug != "company-aihub/deepseek-flash-ft" || c.Models[0].Context != 8192 || len(c.Models[0].Inputs) != 1 || c.Models[0].Inputs[0] != "text" {
		t.Fatal("minimal configured model was not mapped with documented client defaults")
	}
	if _, err := s.freeze(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredSourceReachesConnectAndRevokesChangedRoute(t *testing.T) {
	configuredFixture(t, "deepseek-flash-ft")
	root, d, _, host := fixture(t)
	d.catalog, d.now = configuredCatalog, time.Now
	store := &fixtureStore{}
	m := nativeconnect.NewManager(context.Background(), root, store, newFactory(root, nil, d))
	defer m.Close(context.Background())
	status, err := m.Connect(context.Background())
	if err != nil || !status.SelectionConfigured || status.ChatVerified || store.writes != 1 {
		t.Fatal("configured source did not reach original selection transaction", status, err)
	}
	writeConfiguredFixture(t, "different-model")
	select {
	case <-(*host).lifetime.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("changed configured model did not revoke owned generation")
	}
	if _, err := m.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil || (*host).closed != 1 {
		t.Fatal("owned source watcher/host did not drain", err)
	}
}

func TestNativePrimaryNamesAreNotOfferedThroughCompanySupplier(t *testing.T) {
	for _, id := range []string{"gpt-5.5", "OPENAI/GPT-6", "codex", "vendor/o3-mini", "chatgpt-4o-latest"} {
		if !nativePrimaryModel(id) {
			t.Fatal("known primary model was not excluded", id)
		}
	}
	for _, id := range []string{"deepseek-flash-ft", "vendor/qwen3-coder", "o100-company"} {
		if nativePrimaryModel(id) {
			t.Fatal("ordinary supplier model was excluded", id)
		}
	}
}
