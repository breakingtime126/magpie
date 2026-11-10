package appserverbridge

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/yetone/magpie/internal/nativeconfigguard"
)

const SharedNativeHome167 = "shared-native-home-v0.167.0-alpha.3.2"
const SharedNativeLocalGateway167 = "magpie-loopback-placeholder-v1"
const sharedSecondaryProvider167 = "magpie_shared_aihub"
const sharedSecondaryName167 = "Magpie AIHub"
const sharedInferenceURL167 = "https://chatgpt.com/backend-api/codex"
const sharedChatGPTURL167 = "https://chatgpt.com/backend-api/"
const sharedRouteUnavailable167 = "shared native route admission unavailable"

// SharedNativeRoute167 explicitly declares a shared native state namespace.
// It does not grant login/proof support or isolate the secondary from that state.
type SharedNativeRoute167 struct {
	Mode                   string `json:"mode"`
	GatewayURL             string `json:"gateway_url"`
	GatewayEnvKey          string `json:"gateway_env_key"`
	GatewayAuth            string `json:"gateway_auth,omitempty"`
	SecondaryCatalog       string `json:"secondary_catalog"`
	SecondaryCatalogSHA256 string `json:"secondary_catalog_sha256,omitempty"`
}

func sameNativeDirectory167(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil || !x.IsDir() {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && y.IsDir() && os.SameFile(x, y)
}

func (cfg Config) validateSharedNative167() error {
	s := cfg.SharedNativeRoute
	if s == nil {
		return nil
	}
	if s.Mode != SharedNativeHome167 || len(cfg.Backends) != 2 || cfg.ControlPlane != ControlPlaneRouting167 || (cfg.StartupProbe != "" && cfg.StartupProbe != StartupProbeCompareUnrestricted167) || cfg.Attestation != "" || cfg.GatewayOAuth != "" {
		return errors.New("unsupported shared native route combination")
	}
	a, b := cfg.Backends[0], cfg.Backends[1]
	if a.Provider != "openai" || b.Provider != sharedSecondaryProvider167 || a.Models != nil || b.Models != nil || len(a.InheritEnv) != 0 {
		return errors.New("shared native route requires explicit primary and secondary identities")
	}
	switch s.GatewayAuth {
	case "":
		if len(b.InheritEnv) != 1 || b.InheritEnv[0] != s.GatewayEnvKey {
			return errors.New("shared native route requires explicit gateway environment")
		}
	case SharedNativeLocalGateway167:
		if len(b.InheritEnv) != 0 {
			return errors.New("shared native local gateway does not inherit credentials")
		}
	default:
		return errors.New("unsupported shared native gateway authentication mode")
	}
	if !environmentName.MatchString(s.GatewayEnvKey) || sharedNativeReservedGateway167(s.GatewayEnvKey) {
		return errors.New("shared native route requires a dedicated gateway environment name")
	}
	u, err := url.Parse(s.GatewayURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/v1" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.RawPath != "" {
		return errors.New("shared native route requires an explicit loopback gateway URL")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid shared native gateway port")
	}
	if !filepath.IsAbs(s.SecondaryCatalog) {
		return errors.New("shared native secondary catalog must be absolute")
	}
	info, err := os.Stat(s.SecondaryCatalog)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("shared native secondary catalog must exist")
	}
	if s.SecondaryCatalogSHA256 != "" && !pinnedCatalogMatches(s.SecondaryCatalog, s.SecondaryCatalogSHA256) {
		return errors.New("shared native secondary catalog identity unavailable")
	}
	if !sameNativeDirectory167(a.Home, b.Home) || !sameNativeDirectory167(a.CodexHome, b.CodexHome) {
		return errors.New("shared native route requires identical native home identities")
	}
	return nil
}

func (cfg Config) nativeArgs167(options LaunchOptions, b Backend) []string {
	args := options.backendArgs(b.Provider)
	if cfg.SharedNativeRoute == nil {
		return args
	}
	args = append(args, "--listen", "stdio://", "-c", "chatgpt_base_url="+strconv.Quote(sharedChatGPTURL167))
	if b.Provider == "openai" {
		return append(args, "-c", "openai_base_url="+strconv.Quote(sharedInferenceURL167))
	}
	s := cfg.SharedNativeRoute
	definition := "model_providers." + sharedSecondaryProvider167 + "={name=" + strconv.Quote(sharedSecondaryName167) + ",base_url=" + strconv.Quote(s.GatewayURL) + ",env_key=" + strconv.Quote(s.GatewayEnvKey) + ",wire_api=\"responses\",requires_openai_auth=false}"
	return append(args, "-c", definition, "-c", "model_catalog_json="+strconv.Quote(s.SecondaryCatalog))
}

// One-shot startup flags conflict with listen in pinned native clap. Preserve
// the identical typed configuration overrides, but never add a serve transport.
func (cfg Config) nativeProbeArgs167(options LaunchOptions, b Backend) []string {
	args := cfg.nativeArgs167(options, b)
	if cfg.SharedNativeRoute == nil {
		return args
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--listen" && args[i+1] == "stdio://" {
			return append(args[:i:i], args[i+2:]...)
		}
	}
	return args
}

func (r *Router) sharedNativeAdmissionStart167(g *request) []Delivery {
	g.sharedNativeAdmission167 = true
	g.sharedNativePolicy167 = true
	g.remaining = len(r.backends)
	g.results = map[string][]json.RawMessage{}
	var out []Delivery
	for _, b := range r.backends {
		id := r.next("request")
		r.pending["s:"+id] = pending{g, b.ID}
		out = append(out, deliver(b.ID, object{"id": raw(id), "method": raw("configRequirements/read"), "params": raw(nil)}))
	}
	return out
}

func (r *Router) sharedNativeAdmissionResponse167(g *request, backend string, line []byte) ([]Delivery, error) {
	m, err := parseObject(line)
	if err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) {
		return r.failSharedNativeAdmission167(g)
	}
	if g.sharedNativePolicy167 {
		if validateRequirementsJSON(line) != nil || validateConfigRequirementsNull167(m["result"]) != nil {
			return r.failSharedNativeAdmission167(g)
		}
		g.remaining--
		if g.remaining != 0 {
			return nil, nil
		}
		g.sharedNativePolicy167 = false
		if g.sharedNativeTarget167 == "" {
			g.remaining = len(r.backends)
			var out []Delivery
			for _, b := range r.backends {
				id := r.next("request")
				r.pending["s:"+id] = pending{g, b.ID}
				out = append(out, deliver(b.ID, object{"id": raw(id), "method": raw("config/read"), "params": raw(object{"includeLayers": raw(false)})}))
			}
			return out, nil
		}
		g.remaining = 1
		id := r.next("request")
		r.pending["s:"+id] = pending{g, backend}
		return []Delivery{deliver(backend, object{"id": raw(id), "method": raw("config/read"), "params": raw(object{"includeLayers": raw(false), "cwd": g.params["cwd"]})})}, nil
	}
	s := r.sharedNativeRoute167
	valid := false
	if backend == r.backends[0].ID {
		valid = nativeconfigguard.MatchPrimary(m["result"], sharedInferenceURL167, sharedChatGPTURL167)
	} else {
		valid = nativeconfigguard.MatchSecondary(m["result"], nativeconfigguard.SecondarySpec{Provider: sharedSecondaryProvider167, Name: sharedSecondaryName167, Endpoint: s.GatewayURL, EnvKey: s.GatewayEnvKey, CatalogPath: s.SecondaryCatalog})
	}
	if valid && backend != r.backends[0].ID {
		result, _ := child(m, "result")
		config, _ := child(result, "config")
		valid = requirementsEnum(config["chatgpt_base_url"], sharedChatGPTURL167)
	}
	if !valid {
		return r.failSharedNativeAdmission167(g)
	}
	// Keep no native effective configuration, credentials, headers or raw errors.
	g.remaining--
	if g.remaining != 0 {
		return nil, nil
	}
	g.sharedNativeAdmission167 = false
	if g.sharedNativeTarget167 != "" {
		var out []Delivery
		if g.sharedNativeTarget167 == r.backends[0].ID {
			out = r.beginPrimaryCreation167(g)
		}
		return append(out, r.send(g, g.sharedNativeTarget167, g.params)), nil
	}
	return r.startList(g)
}

// This narrow path checks the actual target process before new native config is
// loaded for a start/resume. It neither infers missing cwd nor creates trust.
func (r *Router) sharedNativeThreadAdmission167(g *request, backend string) []Delivery {
	cwd := text(g.params, "cwd")
	if cwd == "" || !filepath.IsAbs(cwd) || len(cwd) > 4096 {
		return []Delivery{fail(g.id, -32602, "shared native route requires an explicit local project cwd")}
	}
	if len(g.params["serviceName"]) != 0 && !requirementsNull(g.params["serviceName"]) {
		return []Delivery{fail(g.id, -32602, "shared native route does not support service overrides")}
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() || pathsOverlap(cwd, r.controlHome167) || pathsOverlap(cwd, r.backends[0].CodexHome) {
		return []Delivery{fail(g.id, -32602, "shared native project context is unsupported")}
	}
	g.sharedNativeTarget167 = backend
	g.sharedNativeAdmission167 = true
	g.sharedNativePolicy167 = true
	g.remaining = 1
	r.active[g.key] = true
	r.activeBytes += g.size
	id := r.next("request")
	r.pending["s:"+id] = pending{g, backend}
	return []Delivery{deliver(backend, object{"id": raw(id), "method": raw("configRequirements/read"), "params": raw(nil)})}
}

// A failed reload preflight is observed route uncertainty, not a retryable proof
// permission. The runner closes both children on this fixed fatal error.
func (r *Router) failSharedNativeAdmission167(g *request) ([]Delivery, error) {
	if g.sharedNativeTarget167 != "" && r.auth.primaryProofCandidate {
		r.auth.primaryProofFailed = true
		r.auth.primaryProofAdmitted = false
		r.abandon(g)
		return nil, errAuthCompatibility
	}
	return r.reject(g, -32000, sharedRouteUnavailable167), nil
}
