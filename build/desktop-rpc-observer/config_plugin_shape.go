package main

import "strings"

// observedPluginPathShape emits only a fixed schema path label, not identity or
// activation authority. The conservative ASCII grammar intentionally excludes
// native spellings requiring quotes, escapes or Unicode. Values are not read.
// Source: openai/codex c40729ea5cf8e947481b130c59807273eda2f8e9,
// codex-rs/core/config.schema.json blob f5149c6efdf7ab9f1550aa0639dd5f2401205f25:
// PluginConfig, PluginMcpServerConfig, AppConfig, AppToolsConfig, AppToolConfig.
// McpServerToolConfig has no enabled field and is deliberately not recognized.
func observedPluginPathShape(key string) string {
	parts := strings.Split(strings.Trim(key, observedConfigASCIIWhitespace), ".")
	if len(parts) < 3 || parts[0] != "plugins" {
		return ""
	}
	plugin, market, found := strings.Cut(parts[1], "@")
	if !found || !observedPluginPathSegment(plugin) || !observedPluginPathSegment(market) {
		return ""
	}
	switch len(parts) {
	case 3:
		switch parts[2] {
		case "enabled":
			return "direct_enabled"
		case "auto_provision":
			return "direct_auto_provision"
		}
	case 5:
		if !observedPluginPathSegment(parts[3]) {
			return ""
		}
		if parts[2] == "mcp_servers" && parts[4] == "enabled" {
			return "mcp_server_enabled"
		}
		if parts[2] == "apps" {
			switch parts[4] {
			case "enabled":
				return "app_enabled"
			case "destructive_enabled":
				return "app_destructive_enabled"
			case "open_world_enabled":
				return "app_open_world_enabled"
			case "default_tools_enabled":
				return "app_default_tools_enabled"
			}
		}
	case 7:
		if parts[2] == "apps" && observedPluginPathSegment(parts[3]) &&
			parts[4] == "tools" && observedPluginPathSegment(parts[5]) && parts[6] == "enabled" {
			return "app_tool_enabled"
		}
	}
	return ""
}

func observedPluginPathSegment(segment string) bool {
	if len(segment) == 0 || len(segment) > 64 {
		return false
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '_' && c != '-') {
			return false
		}
	}
	return true
}
