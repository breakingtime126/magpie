package nativesupplier

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativeprofile"
)

const syntheticDesktopToken = "SYNTHETIC_PRIMARY_AUTH_TOKEN_NOT_A_CREDENTIAL"
const syntheticRequirements = `{"requirements":null,"supportsIndependentSpeedModes":true}`
const syntheticFeaturePage = `{"data":[{"name":"shell_tool","stage":"stable","displayName":null,"description":null,"announcement":null,"enabled":true,"defaultEnabled":true}],"nextCursor":null}`

// This copied Go test binary speaks authored protocol fixtures only. It never
// discovers native state, invokes an installed native executable or dials URLs.
func init() {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "factory-native-fixture") {
		os.Exit(factoryDesktopChild())
	}
}
func factoryDesktopChild() int {
	overrides := map[string]string{}
	probe := false
	for i, arg := range os.Args[1:] {
		if arg == "--print-startup-config" {
			probe = true
		}
		if (arg == "-c" || arg == "--config") && i+2 < len(os.Args) {
			pair := strings.SplitN(os.Args[i+2], "=", 2)
			if len(pair) == 2 {
				overrides[pair[0]] = pair[1]
			}
		}
	}
	unquote := func(s string) string { v, _ := strconv.Unquote(s); return v }
	provider := unquote(overrides["model_provider"])
	home := os.Getenv("HOME")
	mode := func() string { data, _ := os.ReadFile(filepath.Join(home, "fixture-case")); return string(data) }
	log, err := os.OpenFile(filepath.Join(home, "requests-"+provider+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 81
	}
	defer log.Close()
	args, _ := json.Marshal(os.Args[1:])
	if os.WriteFile(filepath.Join(home, "argv-"+provider+".json"), args, 0600) != nil {
		return 82
	}
	if probe {
		requirements := syntheticRequirements
		if mode() == "policy" {
			requirements = `{"requirements":{},"supportsIndependentSpeedModes":true}`
		}
		fmt.Printf(`{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":%s}`, requirements)
		return 0
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	var pendingAccount json.RawMessage
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		fmt.Fprintln(log, string(line))
		var m map[string]json.RawMessage
		if json.Unmarshal(line, &m) != nil {
			return 83
		}
		var method string
		json.Unmarshal(m["method"], &method)
		var result any
		switch method {
		case "":
			if provider != "openai" || len(pendingAccount) == 0 || string(m["id"]) != `"synthetic-primary-proof"` {
				return 88
			}
			if encoder.Encode(map[string]any{"id": pendingAccount, "result": map[string]any{"account": nil, "requiresOpenaiAuth": true}}) != nil {
				return 89
			}
			pendingAccount = nil
			continue
		case "initialized":
			continue
		case "initialize":
			result = map[string]any{"userAgent": "authored factory fixture", "codexHome": os.Getenv("CODEX_HOME"), "platformFamily": "fixture", "platformOs": "fixture"}
		case "configRequirements/read":
			result = json.RawMessage(syntheticRequirements)
			if mode() == "policy" {
				result = json.RawMessage(`{"requirements":{"PRIVATE_POLICY":true},"supportsIndependentSpeedModes":true}`)
			}
		case "config/read":
			config := map[string]any{"model_provider": provider, "chatgpt_base_url": unquote(overrides["chatgpt_base_url"])}
			if provider == "openai" {
				config["openai_base_url"] = unquote(overrides["openai_base_url"])
				config["model_providers"] = map[string]any{}
			} else {
				selected := map[string]any{"requires_openai_auth": false}
				for _, p := range regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(overrides["model_providers."+provider], -1) {
					selected[p[1]] = unquote(p[2])
				}
				config["model_providers"] = map[string]any{provider: selected}
				config["model_catalog_json"] = unquote(overrides["model_catalog_json"])
			}
			var params map[string]any
			json.Unmarshal(m["params"], &params)
			if cwd, ok := params["cwd"].(string); ok {
				config["projects"] = map[string]any{cwd: map[string]any{"trust_level": "trusted"}}
			}
			result = map[string]any{"config": config, "origins": map[string]any{}}
		case "model/list":
			models := []string{"synthetic-official"}
			if provider != "openai" {
				// Read only the authored catalog passed by this factory test.
				// No native model cache, installed bundle or inferred record.
				path := unquote(overrides["model_catalog_json"])
				if !filepath.IsAbs(path) || filepath.Base(path) != "supplier-catalog.json" {
					return 91
				}
				data, err := os.ReadFile(path)
				if err != nil || len(data) > catalogLimit {
					return 92
				}
				var catalog struct {
					Models []struct {
						Slug string `json:"slug"`
					} `json:"models"`
				}
				if json.Unmarshal(data, &catalog) != nil || len(catalog.Models) == 0 {
					return 93
				}
				models = nil
				for _, m := range catalog.Models {
					if m.Slug == "" {
						return 94
					}
					models = append(models, m.Slug)
				}
			}
			if provider == "openai" && strings.HasPrefix(mode(), "picker") {
				models = append(models, "synthetic-official-extra", "synthetic-hidden")
			}
			rows := []any{}
			for i, model := range models {
				rows = append(rows, map[string]any{"id": model, "model": model, "displayName": model, "description": "authored synthetic model/list; no vendor capability claim", "hidden": model == "synthetic-hidden", "isDefault": i == 0, "supportsPersonality": false, "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium", "description": "authored medium"}, map[string]any{"reasoningEffort": "high", "description": "authored high"}}, "defaultReasoningEffort": "medium", "inputModalities": []string{"text"}})
			}
			if mode() == "picker-no-high" {
				for _, row := range rows {
					row.(map[string]any)["supportedReasoningEfforts"] = []any{map[string]any{"reasoningEffort": "medium", "description": "authored medium"}}
				}
			}
			if mode() == "picker-invalid" && provider == "openai" {
				rows[0].(map[string]any)["defaultReasoningEffort"] = "none"
			}
			if mode() == "picker-drift" && provider == "openai" {
				rows[len(rows)-1].(map[string]any)["description"] = "changed hidden metadata"
			}
			result = map[string]any{"data": rows, "nextCursor": nil}
		case "thread/start":
			result = map[string]any{"modelProvider": provider, "thread": map[string]any{"id": "synthetic-thread-" + provider, "modelProvider": provider}}
		case "getAuthStatus":
			if provider != "openai" {
				return 84
			}
			var params map[string]any
			json.Unmarshal(m["params"], &params)
			var token any
			if params["includeToken"] == true {
				token = syntheticDesktopToken
			}
			authMethod := "chatgpt"
			if mode() == "unsupported-token" {
				authMethod = "headers"
			}
			result = map[string]any{"authMethod": authMethod, "authToken": token, "requiresOpenaiAuth": true}
		case "account/read":
			if provider != "openai" {
				return 85
			}
			if mode() == "proof" {
				pendingAccount = m["id"]
				if encoder.Encode(map[string]any{"id": "synthetic-primary-proof", "method": "attestation/generate", "params": map[string]any{}}) != nil {
					return 90
				}
				continue
			}
			result = map[string]any{"account": nil, "requiresOpenaiAuth": true}
		case "experimentalFeature/list":
			page := syntheticFeaturePage
			if mode() == "features" && provider != "openai" {
				page = strings.Replace(page, `"enabled":true`, `"enabled":false`, 1)
			}
			result = json.RawMessage(page)
		default:
			return 86
		}
		if encoder.Encode(map[string]any{"id": m["id"], "result": result}) != nil {
			return 87
		}
	}
	return 0
}

// Use actual factory config generation and immutable envelope construction.
// Only runtime discovery and host transport ownership are synthetic boundaries.
func factoryDesktopProfile(t *testing.T, models ...string) (appserverbridge.Config, []byte) {
	t.Helper()
	if len(models) == 0 {
		models = []string{"deepseek-flash-ft"}
	}
	configuredFixture(t, models...)
	root, d, _, host := fixture(t)
	d.catalog, d.now = configuredCatalog, time.Now
	runtime := d.runtime
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(t.TempDir(), "factory-native-fixture.exe")
	if err = os.WriteFile(command, data, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	d.runtime = func(ctx context.Context, env []string, scratch string) ([]appserverbridge.Backend, error) {
		backends, err := runtime(ctx, env, scratch)
		for i := range backends {
			backends[i].Command = command
			backends[i].StartupProbeSHA256 = hex.EncodeToString(sum[:])
		}
		return backends, err
	}
	owner, err := newFactory(root, nil, d)(context.Background(), context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	plan, err := owner.Plan()
	if err != nil {
		t.Fatal(err)
	}
	profile := plan.PreparedProfile.Bytes()
	cfg, err := nativeprofile.LoadConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PreparedProfile.ValidateFor(cfg) != nil || (*host).plan.PreparedProfile.SHA256() != plan.PreparedProfile.SHA256() {
		t.Fatal("factory envelope diverged")
	}
	t.Setenv("HOME", cfg.Backends[0].Home)
	t.Setenv("USERPROFILE", cfg.Backends[0].Home)
	if _, err := owner.Plan(); err != nil {
		t.Fatal("synthetic native environment revoked configured supplier source", err)
	}
	return cfg, profile
}

func TestFactoryPreparedProfileSelectsReviewedDesktopContracts(t *testing.T) {
	cfg, profile := factoryDesktopProfile(t)
	if cfg.StartupProbe != appserverbridge.StartupProbeCompareUnrestricted167 || cfg.RequirementsRead != appserverbridge.ConfigRequirementsCompareNull167 || cfg.ExperimentalFeatureList != appserverbridge.ExperimentalFeatureListCompare167 || cfg.AuthStatus != appserverbridge.AuthStatusPrimaryTrustedStdio167 || cfg.DesktopPluginOverride != appserverbridge.DesktopPluginCodexAppTools167 || cfg.BridgeModelSettings != appserverbridge.BridgeModelSettingsNative167 {
		t.Fatal("normal Desktop contract absent from actual factory profile")
	}
	if cfg.Attestation != "" || cfg.GatewayOAuth != "" || cfg.SharedNativeRoute == nil || cfg.ControlPlane != appserverbridge.ControlPlaneRouting167 {
		t.Fatal("primary route replaced by diagnostic authentication")
	}
	if len(cfg.Backends) != 2 || cfg.Backends[0].StartupProbeSHA256 == "" || cfg.Backends[0].StartupProbeSHA256 != cfg.Backends[1].StartupProbeSHA256 {
		t.Fatal("factory lost pinned shared runtime")
	}
	if bytes.Contains(profile, []byte("SYNTHETIC_CONFIG_KEY_CANARY")) || bytes.Contains(profile, []byte(syntheticDesktopToken)) {
		t.Fatal("credentials reached factory profile")
	}
}

func TestFactoryPreparedProfileProbeHandlerPreservesPolicy(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	// These are direct process-handler checks from the exact factory envelope,
	// NOT a claim that Linux executed the Windows authenticated bootstrap path.
	out, err := appserverbridge.ProbeStartup(context.Background(), cfg, appserverbridge.LaunchOptions{}, appserverbridge.StartupClientInfo{Name: "codex_desktop", Version: "synthetic"})
	if err != nil || !bytes.Contains(out, []byte(`"appServerVersion":"0.167.0-alpha.3.2"`)) {
		t.Fatal("factory probe mode unavailable", err)
	}
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "argv-"+b.Provider+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("plugins.codex-app-tools")) {
			t.Fatal("factory injected unsolicited plugin override")
		}
	}
	if err = os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-case"), []byte("policy"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err = appserverbridge.ProbeStartup(context.Background(), cfg, appserverbridge.LaunchOptions{}, appserverbridge.StartupClientInfo{Name: "codex_desktop", Version: "synthetic"}); err == nil || len(out) != 0 {
		t.Fatal("factory manufactured unrestricted startup policy")
	}
}

type factorySession struct {
	input   *io.PipeWriter
	replies chan []byte
	done    chan error
}

func startFactorySession(t *testing.T, cfg appserverbridge.Config, options appserverbridge.LaunchOptions) *factorySession {
	t.Helper()
	in, input := io.Pipe()
	output, out := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	s := &factorySession{input: input, replies: make(chan []byte, 16), done: make(chan error, 1)}
	go func() {
		err := appserverbridge.RunWithOptions(ctx, cfg, options, in, out)
		in.Close()
		out.Close()
		s.done <- err
	}()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(s.replies)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			s.replies <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	t.Cleanup(func() {
		input.Close()
		cancel()
		output.Close()
		select {
		case err := <-s.done:
			if err != nil && err != context.Canceled {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("factory handler did not drain")
		}
		<-readDone
	})
	return s
}
func (s *factorySession) ask(t *testing.T, id int, method string, params any) map[string]json.RawMessage {
	t.Helper()
	line, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if _, err := fmt.Fprintln(s.input, string(line)); err != nil {
		t.Fatal(err)
	}
	select {
	case data, ok := <-s.replies:
		if !ok {
			t.Fatal("bridge ended without reply")
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(data, &m) != nil || string(m["id"]) != strconv.Itoa(id) {
			t.Fatalf("unexpected fixture response: %s", data)
		}
		return m
	case <-time.After(10 * time.Second):
		t.Fatal("factory handler reply timed out")
		return nil
	}
}
func factoryInitialize(t *testing.T, s *factorySession, approval bool) map[string]json.RawMessage {
	return s.ask(t, 1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "codex_desktop", "version": "synthetic"}, "capabilities": map[string]any{"experimentalApi": true, "requestAttestation": true, "explicitGatewayOauth": true, "requiredHumanApproval": approval}})
}
func TestFactoryPreparedProfileNormalDesktopRequestsAndRefusals(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, false); reply["error"] != nil {
		t.Fatal("factory initialization rejected", string(reply["error"]))
	}
	fmt.Fprintln(s.input, `{"method":"initialized"}`)
	for i, method := range []string{"getAuthStatus", "account/read", "configRequirements/read", "experimentalFeature/list", "config/read", "model/list"} {
		params := map[string]any{}
		if method == "getAuthStatus" {
			params["includeToken"] = true
			params["refreshToken"] = false
		}
		reply := s.ask(t, i+2, method, params)
		if reply["error"] != nil {
			t.Fatal("normal factory request rejected", method, string(reply["error"]))
		}
		if method == "getAuthStatus" && !bytes.Contains(reply["result"], []byte(syntheticDesktopToken)) {
			t.Fatal("explicit synthetic token request was silently changed")
		}
		if method == "experimentalFeature/list" && !bytes.Equal(reply["result"], []byte(syntheticFeaturePage)) {
			t.Fatal("genuine matching feature page changed")
		}
		if method == "model/list" {
			var page struct {
				Data []struct {
					ID    string `json:"id"`
					Model string `json:"model"`
				} `json:"data"`
			}
			if json.Unmarshal(reply["result"], &page) != nil || len(page.Data) != 2 {
				t.Fatal("combined synthetic model list lost a backend")
			}
			want := map[string]bool{"magpie/openai/synthetic-official": false, "magpie/aihub/company-aihub/deepseek-flash-ft": false}
			for _, row := range page.Data {
				if _, ok := want[row.ID]; !ok || row.Model != row.ID {
					t.Fatal("model route alias changed", row)
				}
				want[row.ID] = true
			}
			for _, found := range want {
				if !found {
					t.Fatal("model route missing")
				}
			}
			// The supplier identifier is from the actual configured catalog,
			// while the backend response is still explicitly authored test data.
			catalog, err := os.ReadFile(cfg.SharedNativeRoute.SecondaryCatalog)
			var configured struct {
				Models []struct {
					Slug string `json:"slug"`
				} `json:"models"`
			}
			if err != nil || json.Unmarshal(catalog, &configured) != nil || len(configured.Models) != 1 || configured.Models[0].Slug != "company-aihub/deepseek-flash-ft" {
				t.Fatal("factory catalog identifier diverged", err)
			}
		}
	}
	for i, tc := range []struct {
		mode, method string
		params       any
	}{{"policy", "configRequirements/read", map[string]any{}}, {"features", "experimentalFeature/list", map[string]any{}}, {"unsupported-token", "getAuthStatus", map[string]any{"includeToken": true}}} {
		if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-case"), []byte(tc.mode), 0600); err != nil {
			t.Fatal(err)
		}
		reply := s.ask(t, 20+i, tc.method, tc.params)
		if reply["error"] == nil || bytes.Contains(reply["error"], []byte("PRIVATE_POLICY")) || bytes.Contains(reply["error"], []byte(syntheticDesktopToken)) {
			t.Fatal("factory manufactured or leaked refused native result", tc.mode)
		}
	}
	// A barrier request ensures the separate child log contains its initialized
	// capabilities before checking primary-only auth isolation.
	secondary, err := os.ReadFile(filepath.Join(cfg.Backends[1].Home, "requests-"+cfg.Backends[1].Provider+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(secondary, []byte("getAuthStatus")) || bytes.Contains(secondary, []byte("account/read")) || bytes.Contains(secondary, []byte(syntheticDesktopToken)) || bytes.Contains(secondary, []byte(`"requestAttestation":true`)) || bytes.Contains(secondary, []byte(`"explicitGatewayOauth":true`)) {
		t.Fatal("primary authentication reached secondary")
	}
}
func TestFactoryPreparedProfilePreservesHumanApprovalRefusal(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, true); reply["error"] == nil {
		t.Fatal("factory silently disabled required human approval")
	}
}

func TestFactoryPreparedProfilePreservesPrimaryProofRoute(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t)
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, false); reply["error"] != nil {
		t.Fatal(string(reply["error"]))
	}
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-case"), []byte("proof"), 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(s.input, `{"id":30,"method":"account/read","params":{}}`)
	var request map[string]json.RawMessage
	select {
	case data := <-s.replies:
		if json.Unmarshal(data, &request) != nil || string(request["method"]) != `"attestation/generate"` {
			t.Fatal("primary proof was replaced or lost", string(data))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("proof request timed out")
	}
	const token = "SYNTHETIC_PRIMARY_PROOF_NOT_VALID"
	line, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"token": token}})
	fmt.Fprintln(s.input, string(line))
	select {
	case data := <-s.replies:
		var reply map[string]json.RawMessage
		if json.Unmarshal(data, &reply) != nil || string(reply["id"]) != "30" || reply["error"] != nil {
			t.Fatal("primary proof did not finish", string(data))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("proof response timed out")
	}
	primary, err := os.ReadFile(filepath.Join(cfg.Backends[0].Home, "requests-openai.jsonl"))
	if err != nil || !bytes.Contains(primary, []byte(token)) {
		t.Fatal("proof did not return to primary", err)
	}
	secondary, err := os.ReadFile(filepath.Join(cfg.Backends[1].Home, "requests-magpie_shared_aihub.jsonl"))
	if err != nil || bytes.Contains(secondary, []byte(token)) {
		t.Fatal("proof leaked to secondary", err)
	}
}

func TestFactoryPreparedProfilePreservesExplicitPluginBoolean(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		options    appserverbridge.LaunchOptions
	}{
		{name: "absent"},
		{name: "true", want: appserverbridge.CodexAppToolsMCPOverride, options: appserverbridge.LaunchOptions{CodexAppToolsMCPEnabled: true}},
		{name: "false", want: appserverbridge.CodexAppToolsMCPDisabledOverride, options: appserverbridge.LaunchOptions{CodexAppToolsMCPDisabled: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := factoryDesktopProfile(t)
			s := startFactorySession(t, cfg, tc.options)
			if reply := factoryInitialize(t, s, false); reply["error"] != nil {
				t.Fatal("factory plugin mode unavailable", string(reply["error"]))
			}
			for _, b := range cfg.Backends {
				data, err := os.ReadFile(filepath.Join(b.Home, "argv-"+b.Provider+".json"))
				if err != nil {
					t.Fatal(err)
				}
				var args []string
				if json.Unmarshal(data, &args) != nil {
					t.Fatal("invalid argv fixture")
				}
				count := 0
				for _, arg := range args {
					if strings.HasPrefix(arg, "plugins.") {
						count++
						if arg != tc.want {
							t.Fatal("plugin leaf changed", arg)
						}
					}
				}
				if (tc.want == "" && count != 0) || (tc.want != "" && count != 1) {
					t.Fatal("plugin presence changed", args)
				}
			}
		})
	}
}

func TestFactoryPreparedProfileRejectsChangedPinsAndCatalogBeforeChildren(t *testing.T) {
	for _, mode := range []string{"native-pin", "catalog"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _ := factoryDesktopProfile(t)
			path := cfg.Backends[0].Command
			if mode == "catalog" {
				path = cfg.SharedNativeRoute.SecondaryCatalog
			}
			if err := os.WriteFile(path, []byte("changed after factory preparation"), 0700); err != nil {
				t.Fatal(err)
			}
			var output factoryDiscard
			err := appserverbridge.RunWithOptions(context.Background(), cfg, appserverbridge.LaunchOptions{}, io.NopCloser(strings.NewReader("")), &output)
			if err == nil {
				t.Fatal("changed factory evidence launched")
			}
			for _, b := range cfg.Backends {
				if _, err := os.Stat(filepath.Join(b.Home, "requests-"+b.Provider+".jsonl")); !os.IsNotExist(err) {
					t.Fatal("changed evidence reached a child", err)
				}
			}
		})
	}
}

type factoryDiscard struct{ bytes.Buffer }

func (*factoryDiscard) Close() error { return nil }

func TestFactoryCatalogActuallyReachesSecondaryModelList(t *testing.T) {
	cfg, _ := factoryDesktopProfile(t, "independent-supplier-fixture")
	s := startFactorySession(t, cfg, appserverbridge.LaunchOptions{})
	if reply := factoryInitialize(t, s, false); reply["error"] != nil {
		t.Fatal("authored factory catalog was not loaded", string(reply["error"]))
	}
	reply := s.ask(t, 2, "model/list", map[string]any{})
	var page struct {
		Data []struct{ ID, Model string } `json:"data"`
	}
	if reply["error"] != nil || json.Unmarshal(reply["result"], &page) != nil || len(page.Data) != 2 {
		t.Fatal("factory model/list failed")
	}
	seen := map[string]bool{}
	for _, row := range page.Data {
		if row.Model != row.ID {
			t.Fatal("model/backend alias diverged")
		}
		seen[row.ID] = true
	}
	if !seen["magpie/openai/synthetic-official"] || !seen["magpie/aihub/company-aihub/independent-supplier-fixture"] || bytes.Contains(reply["result"], []byte("deepseek-flash-ft")) {
		t.Fatal("secondary used a canned model instead of supplied factory catalog")
	}
}
