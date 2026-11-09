package appserverbridge

import (
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func localGatewayFixture167(t *testing.T) Config {
	t.Helper()
	cfg := sharedNativeConfigFixture167(t)
	cfg.SharedNativeRoute.GatewayAuth = SharedNativeLocalGateway167
	cfg.Backends[1].InheritEnv = nil
	return cfg
}

func TestSharedNativeLocalGateway167SecondaryOnly(t *testing.T) {
	cfg := localGatewayFixture167(t)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	key := cfg.SharedNativeRoute.GatewayEnvKey
	previous, present := os.LookupEnv(key)
	inherited := []string{
		"HOME=" + cfg.Backends[0].Home,
		"HTTPS_PROXY=http://synthetic-proxy.invalid:8443",
		"NO_PROXY=127.0.0.1",
		"CODEX_CA_CERTIFICATE=synthetic-ca-path",
		"OPENAI_API_KEY=SYNTHETIC_UNRELATED_CANARY",
	}
	before := append([]string(nil), inherited...)
	for i, b := range cfg.Backends {
		env, err := cfg.sharedNativeEnvironment167(b, inherited)
		if err != nil {
			t.Fatal(err)
		}
		m := sharedEnvironmentMap167(t, env)
		value, found := m[key]
		if found != (i == 1) || found && value != "magpie-codex" {
			t.Fatal("public gateway label was missing or crossed primary boundary")
		}
		if m["HTTPS_PROXY"] != "http://synthetic-proxy.invalid:8443" || m["NO_PROXY"] != "127.0.0.1" || m["CODEX_CA_CERTIFICATE"] != "synthetic-ca-path" {
			t.Fatal("local mode changed proxy or CA settings")
		}
		if strings.Contains(strings.Join(env, "\n"), "SYNTHETIC_UNRELATED_CANARY") {
			t.Fatal("unrelated credential crossed environment boundary")
		}
	}
	if after, exists := os.LookupEnv(key); after != previous || exists != present || !reflect.DeepEqual(before, inherited) {
		t.Fatal("environment construction mutated caller state")
	}
	b, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(b), "magpie-codex") || strings.Contains(string(b), "SYNTHETIC_UNRELATED_CANARY") {
		t.Fatal("profile contains a credential or injected caller label")
	}
}

func TestSharedNativeLocalGateway167RejectsAmbiguousInput(t *testing.T) {
	cfg := localGatewayFixture167(t)
	key := cfg.SharedNativeRoute.GatewayEnvKey
	for _, value := range []string{"", "SYNTHETIC_EXISTING_KEY", "magpie-codex"} {
		for _, b := range cfg.Backends {
			env, err := cfg.sharedNativeEnvironment167(b, []string{"HOME=" + b.Home, key + "=" + value})
			if err == nil || env != nil || err.Error() != "shared native environment unavailable" {
				t.Fatal("existing gateway environment silently replaced or exposed")
			}
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := cfg.sharedNativeEnvironment167(cfg.Backends[1], []string{"HOME=" + cfg.Backends[1].Home, strings.ToLower(key) + "=SYNTHETIC_KEY"}); err == nil {
			t.Fatal("case alias bypassed existing gateway environment rejection")
		}
	}
	for _, endpoint := range []string{"https://supplier.invalid/v1", "http://localhost:19431/v1", "http://127.0.0.1:19431/other", "http://127.0.0.1:0/v1"} {
		c := localGatewayFixture167(t)
		c.SharedNativeRoute.GatewayURL = endpoint
		if c.Validate() == nil {
			t.Fatal("local gateway mode accepted an unsupported destination")
		}
		if _, err := c.sharedNativeEnvironment167(c.Backends[1], []string{"HOME=" + c.Backends[1].Home}); err == nil {
			t.Fatal("environment builder injected label at an unvalidated destination")
		}
	}
	cfg.Backends[1].InheritEnv = []string{key}
	if cfg.Validate() == nil {
		t.Fatal("local gateway mode accepted credential inheritance")
	}
	cfg.Backends[1].InheritEnv = nil
	cfg.SharedNativeRoute.GatewayAuth = "unknown"
	if cfg.Validate() == nil {
		t.Fatal("unknown gateway authentication mode accepted")
	}
}

func TestSharedNativeLocalGateway167LeavesExistingMode(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	key := cfg.SharedNativeRoute.GatewayEnvKey
	inherited := []string{"HOME=" + cfg.Backends[0].Home, key + "=SYNTHETIC_EXISTING_KEY"}
	env, err := cfg.sharedNativeEnvironment167(cfg.Backends[1], inherited)
	if err != nil || sharedEnvironmentMap167(t, env)[key] != "SYNTHETIC_EXISTING_KEY" {
		t.Fatal("default shared environment mode changed")
	}
}
