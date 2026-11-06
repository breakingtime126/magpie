package appserverbridge

import "strconv"

// DesktopPluginCodexAppTools167 permits only the reviewed normal-start override
// for Codex 0.167.0-alpha.3.2. Empty configuration keeps this mode disabled.
const DesktopPluginCodexAppTools167 = "codex-app-tools-v0.167.0-alpha.3.2"

// CodexAppToolsMCPOverride is a single literal, not a generic plugin/TOML path.
const CodexAppToolsMCPOverride = "plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true"

// LaunchOptions contains only reviewed, process-scoped Desktop controls. A
// false field means absent, not an explicit false configuration override.
// Arbitrary config, credential storage, other plugin controls, sandbox selection
// and remote hosts are intentionally not representable here.
type LaunchOptions struct {
	CodeModeHost            bool
	AnalyticsDefaultEnabled bool
	CodexAppToolsMCPEnabled bool
}

func (options LaunchOptions) backendArgs(provider string) []string {
	var args []string
	if options.CodeModeHost {
		// The upstream scalar override can replace a structured feature
		// table and erase disable_in_process_fallback. Toggle only enabled;
		// this intentional normalization preserves existing sibling policy.
		args = append(args, "-c", "features.code_mode_host.enabled=true")
	}
	args = append(args, "app-server")
	if options.AnalyticsDefaultEnabled {
		// Preserve the official default-only flag. Do not convert it into
		// analytics.enabled=true, which would override a backend's opt-out.
		args = append(args, "--analytics-default-enabled")
	}
	if options.CodexAppToolsMCPEnabled {
		// Preserve the exact observed leaf. Official plugin policy remains
		// authoritative; this can enable an installed plugin and its MCP server.
		args = append(args, "-c", CodexAppToolsMCPOverride)
	}
	// The provider is selected by validated bridge configuration, never by
	// untrusted Desktop argv. Keep it last and never invoke a shell.
	return append(args, "-c", "model_provider="+strconv.Quote(provider))
}
