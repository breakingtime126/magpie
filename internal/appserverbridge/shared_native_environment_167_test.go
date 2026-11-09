package appserverbridge

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func sharedEnvironmentMap167(t *testing.T, env []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("output contained malformed environment entry")
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		if _, exists := out[key]; exists {
			t.Fatal("output contained duplicate environment identity")
		}
		out[key] = value
	}
	return out
}

func TestSharedNativeEnvironment167PreservesExplicitPathsAndProxies(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	inherited := []string{
		"HOME=" + cfg.Backends[0].Home, "USERPROFILE=" + cfg.Backends[0].Home,
		"CODEX_HOME=ignored-foreign-state", "PATH=synthetic-path", "PATHEXT=.EXE;.CMD",
		"ComSpec=synthetic-shell", "SystemRoot=synthetic-system",
		"APPDATA=actual-appdata", "LOCALAPPDATA=actual-localappdata",
		"XDG_CONFIG_HOME=actual-config", "XDG_CACHE_HOME=actual-cache", "XDG_DATA_HOME=actual-data",
		"XDG_STATE_HOME=actual-state", "XDG_RUNTIME_DIR=actual-runtime",
		"TEMP=actual-temp", "TMP=actual-tmp", "TMPDIR=actual-tmpdir",
		"HTTP_PROXY=http://synthetic-user:synthetic-pass@proxy.invalid:8080", "HTTPS_PROXY=http://proxy.invalid:8443",
		"ALL_PROXY=socks5://proxy.invalid:1080", "NO_PROXY=localhost,127.0.0.1",
		"OPENAI_API_KEY=UNRELATED_SECRET_CANARY", "OPENAI_ORGANIZATION=UNRELATED_SECRET_CANARY",
		"NODE_OPTIONS=UNRELATED_SECRET_CANARY", "UNKNOWN=UNRELATED_SECRET_CANARY",
		cfg.SharedNativeRoute.GatewayEnvKey + "=GATEWAY_SECRET_CANARY",
	}
	before := append([]string(nil), inherited...)
	for i, backend := range cfg.Backends {
		out, err := cfg.sharedNativeEnvironment167(backend, inherited)
		if err != nil {
			t.Fatal(err)
		}
		values := sharedEnvironmentMap167(t, out)
		if values["CODEX_HOME"] != backend.CodexHome || values["HOME"] != backend.Home || values["APPDATA"] != "actual-appdata" || values["PATH"] != "synthetic-path" || values["HTTP_PROXY"] != "http://synthetic-user:synthetic-pass@proxy.invalid:8080" || values["XDG_RUNTIME_DIR"] != "actual-runtime" {
			t.Fatal("shared environment rewrote actual paths, namespace, or proxy")
		}
		if strings.Contains(strings.Join(out, "\n"), "UNRELATED_SECRET_CANARY") {
			t.Fatal("unrequested environment credential/control leaked")
		}
		_, hasGateway := values[cfg.SharedNativeRoute.GatewayEnvKey]
		if hasGateway != (i == 1) {
			t.Fatal("gateway credential crossed primary boundary")
		}
	}
	if !reflect.DeepEqual(before, inherited) {
		t.Fatal("inherited environment slice was mutated")
	}
}

func TestSharedNativeEnvironment167RejectsConflictingHomeIdentities(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	backend := cfg.Backends[0]
	for _, env := range [][]string{
		{}, {"HOME="}, {"HOME=relative"},
		{"HOME=" + t.TempDir()},
		{"HOME=" + backend.Home, "USERPROFILE=" + t.TempDir()},
		{"HOME=" + backend.Home, "HOME=" + t.TempDir()},
		{"HOME=" + backend.Home, "PATH=first", "PATH=second"},
		{"HOME=" + backend.Home, "PATH=PRIVATE_CANARY\x00"},
	} {
		out, err := cfg.sharedNativeEnvironment167(backend, env)
		if err == nil || out != nil || err.Error() != "shared native environment unavailable" {
			t.Fatal("conflicting environment was rewritten or raw detail exposed")
		}
	}
	out, err := cfg.sharedNativeEnvironment167(backend, []string{"HOME=" + backend.Home, "HOME=" + backend.Home})
	if err != nil || sharedEnvironmentMap167(t, out)["HOME"] != backend.Home {
		t.Fatal("identical duplicate environment values were not safely deduplicated")
	}
}

func TestSharedNativeEnvironment167UsesHostKeyCaseSemantics(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	backend := cfg.Backends[0]
	env := []string{"HOME=" + backend.Home, "http_proxy=lower-proxy", "HTTP_PROXY=upper-proxy"}
	out, err := cfg.sharedNativeEnvironment167(backend, env)
	if runtime.GOOS == "windows" {
		if err == nil || out != nil {
			t.Fatal("conflicting Windows case aliases accepted")
		}
		out, err = cfg.sharedNativeEnvironment167(backend, []string{"userprofile=" + backend.Home, "Path=actual-path"})
		if err != nil || sharedEnvironmentMap167(t, out)["PATH"] != "actual-path" {
			t.Fatal("Windows user-profile identity or path case rejected")
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		values := sharedEnvironmentMap167(t, out)
		if values["http_proxy"] != "lower-proxy" || values["HTTP_PROXY"] != "upper-proxy" {
			t.Fatal("POSIX case-distinct proxy values changed")
		}
		if _, err := cfg.sharedNativeEnvironment167(backend, []string{"home=" + backend.Home}); err == nil {
			t.Fatal("POSIX lowercase home was substituted for HOME")
		}
	}
}

func TestSharedNativeEnvironment167RequiresDedicatedSecondaryKeyAndOptIn(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	env := []string{"HOME=" + cfg.Backends[0].Home}
	if _, err := cfg.sharedNativeEnvironment167(cfg.Backends[1], env); err == nil {
		t.Fatal("missing secondary credential silently selected unauthenticated route")
	}
	if _, err := cfg.sharedNativeEnvironment167(cfg.Backends[1], append(env, cfg.SharedNativeRoute.GatewayEnvKey+"=")); err == nil {
		t.Fatal("empty secondary credential accepted")
	}
	cfg.SharedNativeRoute.GatewayEnvKey = "OPENAI_OTHER_KEY"
	if _, err := cfg.sharedNativeEnvironment167(cfg.Backends[1], append(env, "OPENAI_OTHER_KEY=PRIVATE_CANARY")); err == nil {
		t.Fatal("dedicated key contract allowed OPENAI credential namespace")
	}
	cfg.SharedNativeRoute = nil
	if out, err := cfg.sharedNativeEnvironment167(cfg.Backends[0], env); err == nil || out != nil {
		t.Fatal("shared builder silently enabled outside explicit mode")
	}
}

func TestSharedNativeEnvironment167PreservesCertificateConfiguration(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	keys := []string{"CODEX_CA_CERTIFICATE", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"}
	env := []string{"HOME=" + cfg.Backends[0].Home, cfg.SharedNativeRoute.GatewayEnvKey + "=SECONDARY_CANARY", "NODE_OPTIONS=UNRELATED_CANARY"}
	for _, key := range keys {
		env = append(env, key+"=existing synthetic path/"+key)
	}
	for _, backend := range cfg.Backends {
		out, err := cfg.sharedNativeEnvironment167(backend, env)
		if err != nil {
			t.Fatal(err)
		}
		values := sharedEnvironmentMap167(t, out)
		for _, key := range keys {
			if values[key] != "existing synthetic path/"+key {
				t.Fatal("existing certificate configuration was changed or omitted")
			}
		}
		if strings.Contains(strings.Join(out, "\n"), "UNRELATED_CANARY") {
			t.Fatal("unrequested Node control was inherited")
		}
	}
	for _, key := range keys {
		for _, spelling := range []string{key, strings.ToLower(key)} {
			if !sharedNativeReservedGateway167(spelling) {
				t.Fatal("certificate key was not reserved")
			}
			cfg.SharedNativeRoute.GatewayEnvKey = spelling
			for _, backend := range cfg.Backends {
				out, err := cfg.sharedNativeEnvironment167(backend, append(env, spelling+"=PRIVATE_CANARY"))
				if err == nil || out != nil || err.Error() != "shared native environment unavailable" {
					t.Fatal("certificate key accepted as dedicated credential or error exposed raw value")
				}
			}
		}
	}
}

func TestSharedNativeEnvironment167RejectsRegisteredCorePresence(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	for _, value := range []string{"", "1", "PRIVATE_CANARY"} {
		for _, b := range cfg.Backends {
			env := []string{"HOME=" + b.Home, cfg.SharedNativeRoute.GatewayEnvKey + "=SECONDARY_CANARY", "CODEX_WINDOWS_REGISTERED_CORE=" + value}
			out, err := cfg.sharedNativeEnvironment167(b, env)
			if err == nil || len(out) != 0 || strings.Contains(err.Error(), "PRIVATE_CANARY") {
				t.Fatal("unsupported registered-core semantics were dropped or inherited")
			}
		}
	}
}
