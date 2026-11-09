package appserverbridge

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// sharedNativeEnvironment167 is shared-mode only. The runner retains its
// existing isolated environment builder when this explicit mode is absent.
// Paths and transport settings come only from the supplied process environment.
// The explicit local-gateway mode adds a public caller label to the secondary,
// never a saved credential. Nothing is logged or written to the host environment.
func (cfg Config) sharedNativeEnvironment167(b Backend, inherited []string) ([]string, error) {
	unavailable := errors.New("shared native environment unavailable")
	s := cfg.SharedNativeRoute
	if s == nil || s.Mode != SharedNativeHome167 || len(cfg.Backends) != 2 {
		return nil, unavailable
	}
	localGateway := s.GatewayAuth == SharedNativeLocalGateway167
	if s.GatewayAuth != "" && !localGateway {
		return nil, unavailable
	}
	if localGateway {
		u, err := url.Parse(s.GatewayURL)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/v1" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.RawPath != "" {
			return nil, unavailable
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 || len(cfg.Backends[1].InheritEnv) != 0 {
			return nil, unavailable
		}
	}
	secondary := b.ID == cfg.Backends[1].ID
	index := 0
	if secondary {
		index = 1
	}
	expected := cfg.Backends[index]
	if b.ID != expected.ID || b.Provider != expected.Provider || b.Home != expected.Home || b.CodexHome != expected.CodexHome ||
		!filepath.IsAbs(b.Home) || !filepath.IsAbs(b.CodexHome) || !sameNativeDirectory167(b.Home, b.Home) || !sameNativeDirectory167(b.CodexHome, b.CodexHome) {
		return nil, unavailable
	}
	if !environmentName.MatchString(s.GatewayEnvKey) || sharedNativeReservedGateway167(s.GatewayEnvKey) {
		return nil, unavailable
	}
	keyIdentity := func(key string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(key)
		}
		return key
	}
	allowed := map[string]bool{}
	for _, key := range []string{
		"PATH", "PATHEXT", "ComSpec", "SystemRoot",
		"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR",
		"TMP", "TEMP", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"CODEX_CA_CERTIFICATE", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS",
	} {
		allowed[keyIdentity(key)] = true
	}
	gatewayKey := keyIdentity(s.GatewayEnvKey)
	if allowed[gatewayKey] {
		return nil, unavailable
	}
	seen := map[string]string{}
	var out []string
	homeSeen, profileSeen, gatewaySeen := false, false, false
	for _, entry := range inherited {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue // Includes Windows's private drive-current-directory entries.
		}
		identity := keyIdentity(key)
		if localGateway && identity == gatewayKey {
			return nil, unavailable
		}
		if identity == keyIdentity("CODEX_WINDOWS_REGISTERED_CORE") {
			return nil, unavailable
		}
		if identity == keyIdentity("CODEX_HOME") {
			continue // The explicit native state namespace is pinned below.
		}
		isGateway := secondary && identity == gatewayKey
		if !allowed[identity] && !isGateway {
			continue
		}
		if strings.ContainsRune(value, 0) {
			return nil, unavailable
		}
		if previous, duplicate := seen[identity]; duplicate {
			if previous != value {
				return nil, unavailable
			}
			continue
		}
		seen[identity] = value
		if identity == keyIdentity("HOME") || identity == keyIdentity("USERPROFILE") {
			if !filepath.IsAbs(value) || !sameNativeDirectory167(value, b.Home) {
				return nil, unavailable
			}
			homeSeen = homeSeen || identity == keyIdentity("HOME")
			profileSeen = profileSeen || identity == keyIdentity("USERPROFILE")
		}
		if isGateway {
			if value == "" {
				return nil, unavailable
			}
			gatewaySeen = true
			key = s.GatewayEnvKey
		}
		out = append(out, key+"="+value)
	}
	if localGateway && secondary {
		// This is gateway.TokenFor("codex"), a public loopback caller label,
		// not a gateway key or proof that the listener is this Magpie instance.
		out = append(out, s.GatewayEnvKey+"=magpie-codex")
		gatewaySeen = true
	}
	if !(homeSeen || (runtime.GOOS == "windows" && profileSeen)) || (secondary && !gatewaySeen) {
		return nil, unavailable
	}
	return append(out, "CODEX_HOME="+b.CodexHome), nil
}

// sharedNativeReservedGateway167 keeps inherited trust configuration separate
// from the dedicated secondary credential, including Windows case aliases.
func sharedNativeReservedGateway167(key string) bool {
	upper := strings.ToUpper(key)
	if reservedEnv(upper) || strings.HasPrefix(upper, "OPENAI_") {
		return true
	}
	switch upper {
	case "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS":
		return true
	default:
		return false
	}
}
