package appserverbridge

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Config names exactly two independently configured official app servers.
// Loading it never creates directories, copies credentials or changes settings.
type Config struct {
	Backends []Backend `json:"backends"`

	// ControlPlane exposes only bridge-owned routing defaults. Native execution
	// policy and all account/permission contracts remain separately owned.
	ControlPlane string `json:"control_plane,omitempty"`
	ControlHome  string `json:"control_home,omitempty"`

	// Auth compatibility modes are independent, explicit and disabled by default.
	// They never authorize generating proof tokens or starting an OAuth login.
	AuthStatus               string `json:"auth_status,omitempty"`
	Attestation              string `json:"attestation,omitempty"`
	GatewayOAuth             string `json:"gateway_oauth,omitempty"`
	GatewayOAuthProviderName string `json:"gateway_oauth_provider_name,omitempty"`

	// RequirementsRead permits both reviewed backends to refresh policy state.
	// It is separate from the one-shot startup inspection opt-in.
	RequirementsRead string `json:"config_requirements_read,omitempty"`

	// ExperimentalFeatureList compares fresh unscoped native pages. Each child
	// reloads its configuration; this opt-in is not a network isolation promise.
	ExperimentalFeatureList string `json:"experimental_feature_list,omitempty"`

	// StartupProbe is disabled unless explicitly enabled after review of both
	// authorities and the official probe's credential/network/cache effects.
	StartupProbe string `json:"startup_probe,omitempty"`

	// DesktopPluginOverride authorizes one exact normal-start plugin leaf.
	// It never installs a plugin or adds the override when Desktop omits it.
	DesktopPluginOverride string `json:"desktop_plugin_override,omitempty"`

	// WorkingDirectory explicitly selects one shared startup context. Empty
	// preserves each child's private home as its process working directory.
	WorkingDirectory string `json:"working_directory,omitempty"`
}

// Backend keeps credentials and state in explicit private directories. Models,
// when set, contains official model/list descriptors for a custom provider.
// InheritEnv forwards only named variables; values do not belong in this file.
type Backend struct {
	ID         string            `json:"id"`
	Provider   string            `json:"provider"`
	Command    string            `json:"command"`
	Home       string            `json:"home"`
	CodexHome  string            `json:"codex_home"`
	Models     []json.RawMessage `json:"models,omitempty"`
	InheritEnv []string          `json:"inherit_env,omitempty"`

	// StartupProbeSHA256 pins the reviewed executable for startup inspection
	// and the separately enabled requirements/auth/launch compatibility modes.
	StartupProbeSHA256 string `json:"startup_probe_sha256,omitempty"`
}

var backendName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadConfig reads one bounded JSON object and rejects misspelled fields.
func LoadConfig(input io.Reader) (Config, error) {
	var cfg Config
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil {
		return cfg, errors.New("cannot read bridge configuration")
	}
	if len(data) > 1<<20 {
		return cfg, errors.New("bridge configuration exceeds 1 MiB")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, errors.New("invalid bridge configuration JSON or unknown field")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, errors.New("bridge configuration must contain one JSON object")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks isolation before any subprocess starts. All paths must exist;
// symlinks are resolved so aliases cannot silently share account state.
func (cfg Config) Validate() error {
	if cfg.ControlPlane != "" && cfg.ControlPlane != ControlPlaneRouting167 {
		return errors.New("unsupported control_plane mode")
	}
	if cfg.ControlPlane == "" && cfg.ControlHome != "" {
		return errors.New("control_home requires the routing control plane")
	}
	if cfg.AuthStatus != "" && cfg.AuthStatus != AuthStatusPrimaryNoAuth167 {
		return errors.New("unsupported auth_status mode")
	}
	if cfg.Attestation != "" && cfg.Attestation != AttestationUnavailable && cfg.Attestation != AttestationUnavailable167 {
		return errors.New("unsupported attestation mode")
	}
	if cfg.GatewayOAuth != "" && cfg.GatewayOAuth != GatewayOAuthPrimaryRead && cfg.GatewayOAuth != GatewayOAuthPrimaryRead167 {
		return errors.New("unsupported gateway_oauth mode")
	}
	if cfg.GatewayOAuth != "" {
		if len(cfg.GatewayOAuthProviderName) == 0 || len(cfg.GatewayOAuthProviderName) > 128 || !startupText(cfg.GatewayOAuthProviderName) {
			return errors.New("gateway OAuth inspection requires a reviewed provider display name")
		}
	} else if cfg.GatewayOAuthProviderName != "" {
		return errors.New("gateway OAuth provider display name requires the read-only mode")
	}
	if cfg.RequirementsRead != "" && cfg.RequirementsRead != ConfigRequirementsCompareSafe && cfg.RequirementsRead != ConfigRequirementsCompareNull167 {
		return errors.New("unsupported config_requirements_read mode")
	}
	if cfg.ExperimentalFeatureList != "" && cfg.ExperimentalFeatureList != ExperimentalFeatureListCompare167 {
		return errors.New("unsupported experimental_feature_list mode")
	}
	if cfg.StartupProbe != "" && !StartupProbeSupported(cfg.StartupProbe) {
		return errors.New("unsupported startup_probe mode")
	}
	if cfg.DesktopPluginOverride != "" && cfg.DesktopPluginOverride != DesktopPluginCodexAppTools167 {
		return errors.New("unsupported desktop_plugin_override mode")
	}
	// All selected contracts describe the same executable pair. Each mode
	// remains an independent opt-in; none authorizes another version's mode.
	legacy := cfg.RequirementsRead == ConfigRequirementsCompareSafe ||
		cfg.Attestation == AttestationUnavailable || cfg.GatewayOAuth == GatewayOAuthPrimaryRead ||
		cfg.StartupProbe == StartupProbeCompareUnrestricted
	current := cfg.RequirementsRead == ConfigRequirementsCompareNull167 ||
		cfg.auth167() || cfg.StartupProbe == StartupProbeCompareUnrestricted167 ||
		cfg.DesktopPluginOverride == DesktopPluginCodexAppTools167 || cfg.ExperimentalFeatureList == ExperimentalFeatureListCompare167 || cfg.ControlPlane == ControlPlaneRouting167
	if legacy && current {
		return errors.New("incompatible versions in selected compatibility modes")
	}
	if len(cfg.Backends) != 2 {
		return errors.New("bridge requires exactly two backends")
	}
	self, err := os.Executable()
	if err != nil {
		return errors.New("cannot identify bridge executable")
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return errors.New("cannot inspect bridge executable")
	}
	ids := map[string]bool{}
	var homes []string
	for i, b := range cfg.Backends {
		if b.StartupProbeSHA256 != "" || cfg.StartupProbe != "" || cfg.RequirementsRead != "" || cfg.Attestation != "" || cfg.GatewayOAuth != "" || cfg.AuthStatus != "" || cfg.DesktopPluginOverride != "" || cfg.ExperimentalFeatureList != "" || cfg.ControlPlane != "" {
			digest, err := hex.DecodeString(b.StartupProbeSHA256)
			if err != nil || len(digest) != 32 {
				return errors.New("reviewed compatibility requires a SHA256 pin for each backend")
			}
		}
		if !backendName.MatchString(b.ID) || ids[b.ID] {
			return fmt.Errorf("backend %d needs a unique alphanumeric id", i+1)
		}
		ids[b.ID] = true
		if cfg.ControlPlane == ControlPlaneRouting167 && b.Provider == controlPlaneProvider167 {
			return errors.New("logical bridge provider is reserved for the frontend")
		}
		if !backendName.MatchString(b.Provider) {
			return fmt.Errorf("backend %s has an invalid provider", b.ID)
		}
		if !filepath.IsAbs(b.Command) {
			return fmt.Errorf("backend %s command must be absolute", b.ID)
		}
		info, err := os.Stat(b.Command)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("backend %s command must be an existing executable file", b.ID)
		}
		if os.SameFile(info, selfInfo) {
			return fmt.Errorf("backend %s command resolves to the bridge itself", b.ID)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("backend %s command is not executable", b.ID)
		}
		f, err := os.Open(b.Command)
		if err != nil {
			return fmt.Errorf("cannot inspect backend %s command", b.ID)
		}
		var prefix [2]byte
		_, err = io.ReadFull(f, prefix[:])
		f.Close()
		ext := strings.ToLower(filepath.Ext(b.Command))
		if err != nil || string(prefix[:]) == "#!" || ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
			return fmt.Errorf("backend %s requires a native executable, not a script", b.ID)
		}
		for _, p := range []string{b.Home, b.CodexHome} {
			if !filepath.IsAbs(p) {
				return fmt.Errorf("backend %s home paths must be absolute", b.ID)
			}
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return fmt.Errorf("backend %s home paths must already exist", b.ID)
			}
			st, err := os.Stat(real)
			if err != nil || !st.IsDir() {
				return fmt.Errorf("backend %s home paths must be directories", b.ID)
			}
			for _, previous := range homes {
				if pathsOverlap(previous, real) {
					return errors.New("all backend home paths must be distinct and non-overlapping")
				}
			}
			homes = append(homes, real)
		}
		for _, descriptor := range b.Models {
			model, err := parseObject(descriptor)
			if err != nil {
				return fmt.Errorf("backend %s has an invalid model descriptor", b.ID)
			}
			if err := validateModel(model); err != nil {
				return fmt.Errorf("backend %s has an invalid model descriptor: %w", b.ID, err)
			}
		}
		seen := map[string]bool{}
		for _, key := range b.InheritEnv {
			upper := strings.ToUpper(key)
			if !environmentName.MatchString(key) || reservedEnv(upper) || seen[upper] {
				return fmt.Errorf("backend %s has an invalid, reserved or duplicate inherited environment name", b.ID)
			}
			seen[upper] = true
		}
	}
	if cfg.ControlPlane == ControlPlaneRouting167 {
		if !filepath.IsAbs(cfg.ControlHome) {
			return errors.New("control_home must be an explicit absolute directory")
		}
		real, err := filepath.EvalSymlinks(cfg.ControlHome)
		if err != nil {
			return errors.New("control_home must already exist")
		}
		st, err := os.Stat(real)
		if err != nil || !st.IsDir() {
			return errors.New("control_home must be a directory")
		}
		for _, home := range homes {
			if pathsOverlap(home, real) {
				return errors.New("control_home must not overlap any backend home")
			}
		}
	}
	if cfg.WorkingDirectory != "" {
		if !filepath.IsAbs(cfg.WorkingDirectory) {
			return errors.New("working_directory must be absolute")
		}
		real, err := filepath.EvalSymlinks(cfg.WorkingDirectory)
		if err != nil {
			return errors.New("working_directory must already exist")
		}
		st, err := os.Stat(real)
		if err != nil || !st.IsDir() {
			return errors.New("working_directory must be a directory")
		}
		for _, home := range homes {
			if pathsOverlap(home, real) {
				return errors.New("working_directory must not overlap any backend home")
			}
		}
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	// Compare file identities too: case-insensitive filesystems may retain the
	// caller's spelling after EvalSymlinks, and mounted aliases can differ.
	ancestor := func(parent, child string) bool {
		st, err := os.Stat(parent)
		if err != nil {
			return false
		}
		for {
			other, err := os.Stat(child)
			if err == nil && os.SameFile(st, other) {
				return true
			}
			next := filepath.Dir(child)
			if next == child {
				return false
			}
			child = next
		}
	}
	if ancestor(a, b) || ancestor(b, a) {
		return true
	}
	if runtime.GOOS == "windows" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	inside := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return inside(a, b) || inside(b, a)
}

// Runtime injection and implicit configuration locations cannot override the
// isolated homes, even through an explicitly listed environment variable.
func reservedEnv(key string) bool {
	switch key {
	case "HOME", "USERPROFILE", "CODEX_HOME", "PATH", "PATHEXT", "APPDATA", "LOCALAPPDATA", "TMPDIR", "TMP", "TEMP", "SYSTEMROOT", "WINDIR", "COMSPEC", "NODE_OPTIONS", "NODE_PATH", "BASH_ENV", "ENV", "SHELLOPTS", "PYTHONPATH", "PYTHONHOME", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "OPENAI_BASE_URL", "OPENAI_API_BASE":
		return true
	}
	return strings.HasPrefix(key, "CODEX_") || strings.HasPrefix(key, "XDG_") || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "DYLD_") || strings.HasSuffix(key, "_PROXY") || key == "ALL_PROXY" || key == "NO_PROXY"
}

// auth167 selects only the exact reviewed auth contract; it enables no probe.
func (cfg Config) auth167() bool {
	return cfg.Attestation == AttestationUnavailable167 || cfg.GatewayOAuth == GatewayOAuthPrimaryRead167 || cfg.AuthStatus == AuthStatusPrimaryNoAuth167
}

func (cfg Config) authCompatibility() authCompatibility {
	return authCompatibility{
		authStatusNoAuth:       cfg.AuthStatus == AuthStatusPrimaryNoAuth167,
		rejectHumanApproval:    cfg.auth167() || cfg.StartupProbe == StartupProbeCompareUnrestricted167 || cfg.RequirementsRead == ConfigRequirementsCompareNull167 || cfg.DesktopPluginOverride == DesktopPluginCodexAppTools167 || cfg.ExperimentalFeatureList == ExperimentalFeatureListCompare167 || cfg.ControlPlane == ControlPlaneRouting167,
		attestationUnavailable: cfg.Attestation == AttestationUnavailable || cfg.Attestation == AttestationUnavailable167,
		gatewayRead:            cfg.GatewayOAuth == GatewayOAuthPrimaryRead || cfg.GatewayOAuth == GatewayOAuthPrimaryRead167,
		gatewayProviderName:    cfg.GatewayOAuthProviderName,
	}
}
