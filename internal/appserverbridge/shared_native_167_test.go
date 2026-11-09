package appserverbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func sharedNativeConfigFixture167(t *testing.T) Config {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	codex, control, work := filepath.Join(home, "codex"), filepath.Join(home, "control"), filepath.Join(home, "work")
	for _, dir := range []string{codex, control, work} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	command, catalog := filepath.Join(home, "inert.exe"), filepath.Join(home, "synthetic-catalog.json")
	for _, path := range []string{command, catalog} {
		if err := os.WriteFile(path, []byte("MZ synthetic nonexecuted fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		ControlPlane: ControlPlaneRouting167, ControlHome: control, WorkingDirectory: work,
		SharedNativeRoute: &SharedNativeRoute167{Mode: SharedNativeHome167, GatewayURL: "http://127.0.0.1:19431/v1", GatewayEnvKey: "MAGPIE_SHARED_TEST_KEY", SecondaryCatalog: catalog},
		Backends: []Backend{
			{ID: "openai", Provider: "openai", Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("a", 64)},
			{ID: "aihub", Provider: sharedSecondaryProvider167, Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: strings.Repeat("b", 64), InheritEnv: []string{"MAGPIE_SHARED_TEST_KEY"}},
		},
	}
}

func TestSharedNative167OptInAndDefaultIsolation(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.SharedNativeRoute = nil
	if cfg.Validate() == nil {
		t.Fatal("default configuration silently allowed shared native state")
	}
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	if !reflect.DeepEqual(cfg.nativeArgs167(options, cfg.Backends[0]), options.backendArgs("openai")) {
		t.Fatal("default-off mode changed native arguments")
	}
}

func TestSharedNative167RejectsMixedModesIdentitiesAndPins(t *testing.T) {
	cases := map[string]func(*Config){
		"unknown-mode":           func(c *Config) { c.SharedNativeRoute.Mode = "shared-any-version" },
		"no-control-plane":       func(c *Config) { c.ControlPlane = "" },
		"legacy-startup-probe":   func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted },
		"attestation":            func(c *Config) { c.Attestation = "unavailable-v0.167.0-alpha.3.2" },
		"gateway-oauth":          func(c *Config) { c.GatewayOAuth = "primary-read-only-v0.167.0-alpha.3.2" },
		"primary-provider":       func(c *Config) { c.Backends[0].Provider = "custom" },
		"secondary-provider":     func(c *Config) { c.Backends[1].Provider = "openai" },
		"primary-static":         func(c *Config) { c.Backends[0].Models = []json.RawMessage{} },
		"secondary-static":       func(c *Config) { c.Backends[1].Models = []json.RawMessage{} },
		"primary-env":            func(c *Config) { c.Backends[0].InheritEnv = []string{"MAGPIE_SHARED_TEST_KEY"} },
		"secondary-env-missing":  func(c *Config) { c.Backends[1].InheritEnv = nil },
		"secondary-env-extra":    func(c *Config) { c.Backends[1].InheritEnv = append(c.Backends[1].InheritEnv, "OTHER") },
		"primary-pin":            func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		"secondary-pin":          func(c *Config) { c.Backends[1].StartupProbeSHA256 = strings.Repeat("z", 64) },
		"different-home":         func(c *Config) { c.Backends[1].Home = t.TempDir() },
		"different-codex-home":   func(c *Config) { c.Backends[1].CodexHome = t.TempDir() },
		"control-native-overlap": func(c *Config) { c.ControlHome = c.Backends[0].CodexHome },
		"work-native-overlap":    func(c *Config) { c.WorkingDirectory = c.Backends[0].CodexHome },
		"work-control-overlap":   func(c *Config) { c.WorkingDirectory = c.ControlHome },
		"relative-catalog":       func(c *Config) { c.SharedNativeRoute.SecondaryCatalog = "relative.json" },
		"directory-catalog":      func(c *Config) { c.SharedNativeRoute.SecondaryCatalog = c.ControlHome },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := sharedNativeConfigFixture167(t)
			mutate(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("invalid shared route configuration admitted")
			}
		})
	}
	for _, name := range []string{"OPENAI_API_KEY", "openai_organization", "OPENAI_PROJECT", "CODEX_HOME", "BAD=KEY"} {
		cfg := sharedNativeConfigFixture167(t)
		cfg.SharedNativeRoute.GatewayEnvKey = name
		cfg.Backends[1].InheritEnv = []string{name}
		if cfg.Validate() == nil {
			t.Fatal("non-dedicated gateway environment admitted")
		}
	}
	for _, url := range []string{"https://127.0.0.1:19431/v1", "http://localhost:19431/v1", "http://127.0.0.1/v1", "http://127.0.0.1:0/v1", "http://127.0.0.1:65536/v1", "http://user@127.0.0.1:19431/v1", "http://127.0.0.1:19431/v1?x=1", "http://127.0.0.1:19431/v1#x", "http://127.0.0.1:19431/v1/"} {
		cfg := sharedNativeConfigFixture167(t)
		cfg.SharedNativeRoute.GatewayURL = url
		if cfg.Validate() == nil {
			t.Fatal("unreviewed gateway URL admitted")
		}
	}
}

func TestSharedNative167ArgumentsAreExplicitAndCredentialFree(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true, CodexAppToolsMCPEnabled: true}
	for i, b := range cfg.Backends {
		base := options.backendArgs(b.Provider)
		want := append(base, "--listen", "stdio://", "-c", "chatgpt_base_url="+strconv.Quote(sharedChatGPTURL167))
		if i == 0 {
			want = append(want, "-c", "openai_base_url="+strconv.Quote(sharedInferenceURL167))
		} else {
			s := cfg.SharedNativeRoute
			definition := "model_providers." + sharedSecondaryProvider167 + "={name=" + strconv.Quote(sharedSecondaryName167) + ",base_url=" + strconv.Quote(s.GatewayURL) + ",env_key=" + strconv.Quote(s.GatewayEnvKey) + ",wire_api=\"responses\",requires_openai_auth=false}"
			want = append(want, "-c", definition, "-c", "model_catalog_json="+strconv.Quote(s.SecondaryCatalog))
		}
		if !reflect.DeepEqual(cfg.nativeArgs167(options, b), want) {
			t.Fatal("native arguments changed provider, safety flags, or explicit route pins")
		}
	}
}

func sharedNativeRouterFixture167(t *testing.T) *Router {
	t.Helper()
	cfg := sharedNativeConfigFixture167(t)
	r, err := NewRouter(cfg.Backends)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.enableControlPlane167(); err != nil {
		t.Fatal(err)
	}
	r.sharedNativeRoute167 = cfg.SharedNativeRoute
	r.auth = cfg.authCompatibility()
	r.controlHome167 = cfg.ControlHome
	r.controlNativeHomes167 = map[string]string{"openai": cfg.Backends[0].CodexHome, "aihub": cfg.Backends[1].CodexHome}
	return r
}

func sharedNativeInitializePolicy167(t *testing.T, r *Router) []Delivery {
	t.Helper()
	initialSession := r.controlSession167
	if initialSession == nil || initialSession.model != "" || len(r.models) != 0 {
		t.Fatal("fixture did not start with the real empty bootstrap session")
	}
	init := client(t, r, `{"id":"shared-init","method":"initialize","params":{"clientInfo":{"name":"synthetic","version":"1"},"capabilities":{"experimentalApi":true}}}`)
	if len(init) != 2 {
		t.Fatal("initialize did not reach both native children")
	}
	var out []Delivery
	for i, d := range init {
		next := reply(t, r, d, string(raw(object{"userAgent": raw("synthetic"), "codexHome": raw(r.controlNativeHomes167[d.Backend]), "platformFamily": raw("test"), "platformOs": raw("test")})))
		if i == 0 && len(next) != 0 {
			t.Fatal("admission started before both initialize responses")
		}
		out = append(out, next...)
	}
	if len(out) != 2 || r.phase != "initializing" || r.controlSession167 != initialSession || initialSession.model != "" || len(r.models) != 0 || r.primaryRouteReady167() {
		t.Fatalf("initialize escaped admission barrier: deliveries=%d phase=%s routes=%d session_changed=%t", len(out), r.phase, len(r.models), r.controlSession167 != initialSession)
	}
	// The real bootstrap session exists but cannot be published before readiness.
	expectError(t, client(t, r, `{"id":"early-config","method":"config/read","params":{}}`))
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if d.Backend == "" || text(m, "method") != "configRequirements/read" || !requirementsNull(m["params"]) {
			t.Fatal("startup did not check both actual policy authorities")
		}
	}
	return out
}

func sharedNativeInitialize167(t *testing.T, r *Router) []Delivery {
	t.Helper()
	policies := sharedNativeInitializePolicy167(t, r)
	var out []Delivery
	for i, d := range policies {
		next := reply(t, r, d, controlPlaneNullRequirements)
		if i == 0 && len(next) != 0 {
			t.Fatal("one policy authority authorized startup config reads")
		}
		out = append(out, next...)
	}
	if len(out) != 2 || r.phase != "initializing" {
		t.Fatal("complete genuine policy pair did not advance to config admission")
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		p, _ := child(m, "params")
		if d.Backend == "" || text(m, "method") != "config/read" || len(p) != 1 || string(p["includeLayers"]) != "false" {
			t.Fatal("admission did not read each child's same-process config")
		}
	}
	return out
}

func sharedNativeConfigResponse167(r *Router, backend string) object {
	config := object{"model_provider": raw("openai"), "openai_base_url": raw(sharedInferenceURL167), "chatgpt_base_url": raw(sharedChatGPTURL167), "model_providers": raw(object{})}
	if backend == r.backends[1].ID {
		s := r.sharedNativeRoute167
		config["model_provider"] = raw(sharedSecondaryProvider167)
		config["model_catalog_json"] = raw(s.SecondaryCatalog)
		config["model_providers"] = raw(object{sharedSecondaryProvider167: raw(object{"name": raw(sharedSecondaryName167), "base_url": raw(s.GatewayURL), "env_key": raw(s.GatewayEnvKey), "wire_api": raw("responses"), "requires_openai_auth": raw(false)})})
	}
	return object{"config": raw(config), "origins": raw(object{})}
}

func TestSharedNative167InitializeWaitsForBothAdmissionAndCatalog(t *testing.T) {
	r := sharedNativeRouterFixture167(t)
	checks := sharedNativeInitialize167(t, r)
	var list []Delivery
	for i, d := range checks {
		out := reply(t, r, d, string(raw(sharedNativeConfigResponse167(r, d.Backend))))
		if i == 0 && len(out) != 0 {
			t.Fatal("one valid provider authorized catalog startup")
		}
		list = append(list, out...)
	}
	if len(list) != 2 || r.phase != "initializing" {
		t.Fatal("admission did not proceed to actual model lists")
	}
	for i, d := range list {
		m, _ := parseObject(d.Message)
		if text(m, "method") != "model/list" {
			t.Fatal("unexpected post-admission RPC")
		}
		out := reply(t, r, d, string(raw(object{"data": raw([]json.RawMessage{fixtureModel("synthetic-model", true)}), "nextCursor": raw(nil)})))
		if i == 0 && len(out) != 0 {
			t.Fatal("partial catalog published initialize")
		}
		if i == 1 {
			if len(out) != 1 || out[0].Backend != "" || r.phase != "ready" {
				t.Fatal("complete valid startup did not acknowledge initialize")
			}
			response, _ := parseObject(out[0].Message)
			if !sameJSON(response["id"], raw("shared-init")) || exists(response, "error") {
				t.Fatal("initialize correlation lost")
			}
		}
	}
}

func TestSharedNative167BadAdmissionNeverLeaksOrStartsList(t *testing.T) {
	for _, side := range []int{0, 1} {
		for _, kind := range []string{"provider", "endpoint", "rpc-error"} {
			r := sharedNativeRouterFixture167(t)
			checks := sharedNativeInitialize167(t, r)
			initialSession := r.controlSession167
			d := checks[side]
			m, _ := parseObject(d.Message)
			result := sharedNativeConfigResponse167(r, d.Backend)
			config, _ := child(result, "config")
			if kind == "provider" {
				config["model_provider"] = raw("PRIVATE_CANARY")
			} else {
				config["openai_base_url"] = raw("PRIVATE_CANARY")
				config["model_providers"] = raw(object{})
			}
			result["config"] = raw(config)
			response := object{"id": m["id"], "result": raw(result)}
			if kind == "rpc-error" {
				delete(response, "result")
				response["error"] = raw(object{"code": raw(-32000), "message": raw("PRIVATE_CANARY")})
			}
			out, err := r.Server(d.Backend, raw(response))
			if err != nil || len(out) != 1 || out[0].Backend != "" || strings.Contains(string(out[0].Message), "PRIVATE_CANARY") || r.phase != "failed" || len(r.pending) != 0 || r.controlSession167 != initialSession || initialSession.model != "" || len(r.models) != 0 || r.primaryRouteReady167() {
				t.Fatal("bad provider admission leaked native data or retained startup authority")
			}
			expectError(t, client(t, r, `{"id":"failed-config","method":"config/read","params":{}}`))
		}
	}
}

func TestSharedNative167CapabilitiesRemainPrimaryOnly(t *testing.T) {
	r := sharedNativeRouterFixture167(t)
	out, err := r.Client([]byte(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"synthetic","version":"1"},"capabilities":{"experimentalApi":true,"requestAttestation":true,"explicitGatewayOauth":true}}}`))
	if err != nil || len(out) != 2 || len(r.pending) != 2 || r.primaryRouteReady167() {
		t.Fatal("capability negotiation bypassed route admission")
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		p, _ := child(m, "params")
		caps, _ := child(p, "capabilities")
		want := raw(d.Backend == "openai")
		if !sameJSON(caps["requestAttestation"], want) || !sameJSON(caps["explicitGatewayOauth"], want) || !sameJSON(caps["experimentalApi"], raw(true)) {
			t.Fatal("primary capabilities changed or secondary negotiated global auth")
		}
	}
}

func TestSharedNative167EnvironmentKeepsGatewayKeySecondaryOnly(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	t.Setenv(cfg.SharedNativeRoute.GatewayEnvKey, "SYNTHETIC_GATEWAY_CANARY")
	t.Setenv("OPENAI_API_KEY", "SYNTHETIC_UNRELATED_CANARY")
	for i, b := range cfg.Backends {
		env, err := cfg.sharedNativeEnvironment167(b, []string{"HOME=" + b.Home, "USERPROFILE=" + b.Home, cfg.SharedNativeRoute.GatewayEnvKey + "=SYNTHETIC_GATEWAY_CANARY", "OPENAI_API_KEY=SYNTHETIC_UNRELATED_CANARY"})
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for _, entry := range env {
			if strings.Contains(entry, "SYNTHETIC_UNRELATED_CANARY") {
				t.Fatal("unrequested API credential reached a child")
			}
			if entry == cfg.SharedNativeRoute.GatewayEnvKey+"=SYNTHETIC_GATEWAY_CANARY" {
				seen = true
			}
		}
		if seen != (i == 1) {
			t.Fatal("gateway key inheritance crossed provider boundary")
		}
		if strings.Contains(strings.Join(cfg.nativeArgs167(LaunchOptions{}, b), "\n"), "SYNTHETIC_GATEWAY_CANARY") {
			t.Fatal("credential value entered argv")
		}
	}
}

func TestSharedNative167StartupRejectsNonNullOrMalformedPolicy(t *testing.T) {
	for _, encoded := range []string{`{"requirements":{},"supportsIndependentSpeedModes":true}`, `{"requirements":null}`, `{"requirements":null,"supportsIndependentSpeedModes":true,"unknown":"PRIVATE_CANARY"}`} {
		r := sharedNativeRouterFixture167(t)
		checks := sharedNativeInitializePolicy167(t, r)
		out := reply(t, r, checks[0], encoded)
		expectError(t, out)
		if r.phase != "failed" || len(r.pending) != 0 || len(r.active) != 0 || strings.Contains(string(out[0].Message), "PRIVATE_CANARY") {
			t.Fatal("startup policy failure leaked data or retained admission")
		}
	}
}
