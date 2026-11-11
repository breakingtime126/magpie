package nativesupplier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
)

func pickerEdits(model, effort string) any {
	return map[string]any{"edits": []any{
		map[string]any{"keyPath": "model", "value": model, "mergeStrategy": "upsert"},
		map[string]any{"keyPath": "model_reasoning_effort", "value": effort, "mergeStrategy": "upsert"},
	}}
}
func pickerRead(t *testing.T, s *factorySession, id int, model, effort string) {
	t.Helper()
	reply := s.ask(t, id, "config/read", map[string]any{"includeLayers": true})
	var value struct {
		Config  map[string]any `json:"config"`
		Origins map[string]struct {
			Name map[string]any `json:"name"`
		} `json:"origins"`
		Layers []struct {
			Config map[string]any `json:"config"`
		} `json:"layers"`
	}
	if reply["error"] != nil || json.Unmarshal(reply["result"], &value) != nil || value.Config["model"] != model || value.Config["model_reasoning_effort"] != effort || len(value.Layers) != 2 || value.Layers[0].Config["model"] != model || value.Layers[0].Config["model_reasoning_effort"] != effort || value.Origins["model"].Name["type"] != "user" {
		t.Fatalf("picker defaults did not round trip: %s", reply["result"])
	}
	if _, found := value.Config["model_catalog_json"]; found {
		t.Fatal("native list replaced with a fabricated alias file")
	}
}
func pickerMode(t *testing.T, cfg appserverbridge.Config, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-case"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryPickerPersistsRestartsAndKeepsThreadOwnership(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	pickerMode(t, cfg, "picker")
	sentinel := []byte("# synthetic native config, must remain untouched\nmodel = 'native-sentinel'\n")
	nativePath := filepath.Join(cfg.Backends[0].CodexHome, "config.toml")
	if err := os.WriteFile(nativePath, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	official := "magpie/openai/synthetic-official"
	supplier := "magpie/aihub/company-aihub/deepseek-flash-ft"
	t.Run("save", func(t *testing.T) {
		s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
		if reply := factoryInitialize(t, s, false); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		pickerRead(t, s, 2, official, "medium")
		list := s.ask(t, 3, "model/list", map[string]any{"includeHidden": true})
		var page struct {
			Data []json.RawMessage `json:"data"`
		}
		if json.Unmarshal(list["result"], &page) != nil || len(page.Data) != 4 {
			t.Fatal("complete genuine list was reduced", string(list["result"]))
		}
		if reply := s.ask(t, 4, "config/batchWrite", pickerEdits(supplier, "high")); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		pickerRead(t, s, 5, supplier, "high")
		if reply := s.ask(t, 6, "thread/start", map[string]any{"cwd": cfg.WorkingDirectory, "sandbox": "workspace-write"}); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		if reply := s.ask(t, 7, "config/batchWrite", pickerEdits(official, "medium")); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		reply := s.ask(t, 8, "thread/resume", map[string]any{"threadId": "synthetic-thread-magpie_shared_aihub", "model": official, "cwd": cfg.WorkingDirectory})
		if !bytes.Contains(reply["error"], []byte("cannot change provider")) {
			t.Fatal("existing owner switched with picker", string(reply["error"]))
		}
		if reply := s.ask(t, 9, "config/batchWrite", pickerEdits(supplier, "high")); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		if reply := s.ask(t, 10, "model/list", map[string]any{}); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		pickerRead(t, s, 11, supplier, "high")
	})
	t.Run("restart-same-generation", func(t *testing.T) {
		s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
		if reply := factoryInitialize(t, s, false); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		pickerRead(t, s, 2, supplier, "high")
	})
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "requests-"+b.Provider+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("config/batchWrite")) {
			t.Fatal("picker write forwarded to native")
		}
		if b.Provider != "openai" && !bytes.Contains(data, []byte(`"model":"company-aihub/deepseek-flash-ft"`)) {
			t.Fatal("implicit new thread ignored persisted supplier")
		}
		if native, err := os.ReadFile(filepath.Join(b.CodexHome, "config.toml")); err != nil || !bytes.Equal(native, sentinel) {
			t.Fatal("native sentinel configuration changed", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(cfg.ControlHome, "config.toml"))
	if err != nil || !bytes.Contains(data, []byte(supplier)) || !bytes.Contains(data, []byte("high")) {
		t.Fatal("isolated defaults not persisted", err)
	}
}

func TestFactoryPickerRejectsInvalidChoicesAndPolicy(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	pickerMode(t, cfg, "picker")
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, false); reply["error"] != nil {
		t.Fatal(string(reply["error"]))
	}
	path := filepath.Join(cfg.ControlHome, "config.toml")
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range [][2]string{{"unknown", "medium"}, {"magpie/openai/synthetic-official", "none"}, {"magpie/openai/synthetic-official", "xhigh"}, {"magpie/openai/synthetic-hidden", "medium"}} {
		reply := s.ask(t, 2+i, "config/batchWrite", pickerEdits(tc[0], tc[1]))
		if !bytes.Contains(reply["error"], []byte("configValidationError")) {
			t.Fatal("invalid native choice saved", tc, string(reply["error"]))
		}
	}
	for i, edits := range []any{
		[]any{map[string]any{"keyPath": "model", "value": "magpie/openai/synthetic-official", "mergeStrategy": "upsert"}, map[string]any{"keyPath": "model", "value": "magpie/openai/synthetic-official", "mergeStrategy": "upsert"}},
		[]any{map[string]any{"keyPath": "unknown", "value": "x", "mergeStrategy": "replace"}, map[string]any{"keyPath": "model_reasoning_effort", "value": "medium", "mergeStrategy": "upsert"}},
		[]any{map[string]any{"keyPath": "model", "value": nil, "mergeStrategy": "upsert"}, map[string]any{"keyPath": "model_reasoning_effort", "value": "medium", "mergeStrategy": "upsert"}},
		[]any{map[string]any{"keyPath": "model", "value": "magpie/openai/synthetic-official", "mergeStrategy": "upsert"}, map[string]any{"keyPath": "model_reasoning_effort", "value": nil, "mergeStrategy": "upsert"}},
	} {
		if reply := s.ask(t, 20+i, "config/batchWrite", map[string]any{"edits": edits}); !bytes.Contains(reply["error"], []byte("configValidationError")) {
			t.Fatal("unsupported edit accepted", string(reply["error"]))
		}
	}
	conflict := pickerEdits("magpie/openai/synthetic-official", "high").(map[string]any)
	conflict["expectedVersion"] = "sha256:stale"
	if reply := s.ask(t, 30, "config/batchWrite", conflict); !bytes.Contains(reply["error"], []byte("configVersionConflict")) {
		t.Fatal("stale version accepted", string(reply["error"]))
	}
	pickerMode(t, cfg, "policy")
	if reply := s.ask(t, 8, "config/batchWrite", pickerEdits("magpie/openai/synthetic-official", "high")); reply["error"] == nil {
		t.Fatal("non-null native policy allowed a write")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(initial, after) {
		t.Fatal("refused write changed defaults", err)
	}
}

func TestFactoryPickerFailsClosedOnHiddenListAndFileDrift(t *testing.T) {
	for _, mode := range []string{"picker-drift", "owned-file"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _ := factoryDesktopProfile(t)
			pickerMode(t, cfg, "picker")
			s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
			if reply := factoryInitialize(t, s, false); reply["error"] != nil {
				t.Fatal(string(reply["error"]))
			}
			method := "model/list"
			if mode == "owned-file" {
				method = "config/read"
				if err := os.WriteFile(filepath.Join(cfg.ControlHome, "config.toml"), []byte("model = 'foreign'\nmodel_reasoning_effort = 'medium'\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				pickerMode(t, cfg, mode)
			}
			fmt.Fprintf(s.input, "{\"id\":2,\"method\":%q,\"params\":{}}\n", method)
			select {
			case err := <-s.done:
				s.done <- nil // Cleanup owns pipes and scanner after the asserted failure.
				if err == nil || !strings.Contains(err.Error(), "protocol failure") {
					t.Fatal("drift did not close bridge", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("drift did not close connection")
			}
			select {
			case response := <-s.replies:
				if bytes.Contains(response, []byte(`"result"`)) {
					t.Fatal("stale success released", string(response))
				}
			default:
			}
		})
	}
}

func TestFactoryPickerRequiresExactContractsAndDefaultOff(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	for name, change := range map[string]func(*appserverbridge.Config){
		"probe":        func(c *appserverbridge.Config) { c.StartupProbe = "" },
		"requirements": func(c *appserverbridge.Config) { c.RequirementsRead = "" },
		"features":     func(c *appserverbridge.Config) { c.ExperimentalFeatureList = "" },
		"auth":         func(c *appserverbridge.Config) { c.AuthStatus = "" },
		"plugins":      func(c *appserverbridge.Config) { c.DesktopPluginOverride = "" },
		"old-mode":     func(c *appserverbridge.Config) { c.BridgeModelSettings = appserverbridge.BridgeModelSettingsOwned167 },
		"native-pin": func(c *appserverbridge.Config) {
			c.Backends = append([]appserverbridge.Backend(nil), c.Backends...)
			c.Backends[0].StartupProbeSHA256 = ""
		},
		"catalog-pin": func(c *appserverbridge.Config) {
			s := *c.SharedNativeRoute
			s.SecondaryCatalogSHA256 = ""
			c.SharedNativeRoute = &s
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cfg
			change(&candidate)
			if candidate.Validate() == nil {
				t.Fatal("incomplete picker contract admitted")
			}
		})
	}
	cfg.BridgeModelSettings = ""
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, false); reply["error"] != nil {
		t.Fatal(string(reply["error"]))
	}
	reply := s.ask(t, 2, "config/batchWrite", pickerEdits("magpie/openai/synthetic-official", "medium"))
	if !bytes.Contains(reply["error"], []byte("-32601")) {
		t.Fatal("default-off mode accepted picker write", string(reply["error"]))
	}
	if _, err := os.Stat(filepath.Join(cfg.ControlHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("default-off mode created settings", err)
	}
}

func TestFactoryPickerFailureBeforeSeedingWritesNothing(t *testing.T) {
	for _, mode := range []string{"policy", "picker-invalid"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _ := factoryDesktopProfile(t)
			pickerMode(t, cfg, mode)
			s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
			fmt.Fprintln(s.input, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"codex_desktop","version":"synthetic"},"capabilities":{"experimentalApi":true}}}`)
			select {
			case err := <-s.done:
				s.done <- nil
				if err == nil {
					t.Fatal("failed precondition initialized")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("failed initialization did not close")
			}
			if _, err := os.Stat(filepath.Join(cfg.ControlHome, "config.toml")); !os.IsNotExist(err) {
				t.Fatal("failed policy/list seeded settings", err)
			}
		})
	}
}

func TestFactoryPickerRestartRejectsUnsupportedSavedEffort(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	pickerMode(t, cfg, "picker")
	t.Run("save", func(t *testing.T) {
		s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
		if reply := factoryInitialize(t, s, false); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
		if reply := s.ask(t, 2, "config/batchWrite", pickerEdits("magpie/openai/synthetic-official", "high")); reply["error"] != nil {
			t.Fatal(string(reply["error"]))
		}
	})
	path := filepath.Join(cfg.ControlHome, "config.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pickerMode(t, cfg, "picker-no-high")
	t.Run("reject-restart", func(t *testing.T) {
		s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
		fmt.Fprintln(s.input, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"codex_desktop","version":"synthetic"},"capabilities":{"experimentalApi":true}}}`)
		select {
		case err := <-s.done:
			s.done <- nil
			if err == nil {
				t.Fatal("unsupported saved effort initialized")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("invalid saved selection did not close")
		}
	})
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid restart reseeded preference", err)
	}
}
