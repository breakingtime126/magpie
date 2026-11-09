package nativepreview

import (
	"bytes"
	"encoding/json"
	"strings"
)

// This reviewed c407 feature set restricts tool actions without changing the
// Windows sandbox, approval policy or managed requirements. It is verified in
// the same native process's effective config before thread/start.
func textOverrides() map[string]string {
	values := map[string]string{
		"web_search": `"disabled"`, "agents.enabled": "false", "cloud.skills.enabled": "false", "orchestrator.mcp.enabled": "false",
	}
	for _, name := range []string{"code_mode", "code_mode_only", "code_mode_host", "code_mode_prewarm", "apps", "plugins", "multi_env_copy_file", "memories", "token_budget", "hooks", "plugin_hooks", "mcp_connection_sharing", "shell_tool", "multi_agent_v2", "image_generation", "executor_capability_discovery_v2", "request_permissions_tool", "deferred_executor", "tool_suggest", "goals"} {
		values["features."+name] = "false"
	}
	return values
}

func configPath(root map[string]json.RawMessage, path string) json.RawMessage {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		value := root[part]
		if i == len(parts)-1 {
			return value
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(value, &child) != nil || child == nil {
			return nil
		}
		root = child
	}
	return nil
}
func emptyObjectOrNull(value json.RawMessage) bool {
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return true
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && len(object) == 0
}
func noHooksOrNotify(config map[string]json.RawMessage) bool {
	if !emptyObjectOrNull(config["hooks"]) {
		return false
	}
	value := config["notify"]
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return true
	}
	var commands []json.RawMessage
	return json.Unmarshal(value, &commands) == nil && len(commands) == 0
}
func matchesTextGuard(data json.RawMessage, spec peerSpec) bool {
	if !matchesProvider(data, spec) {
		return false
	}
	var response struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if json.Unmarshal(data, &response) != nil || response.Config == nil {
		return false
	}
	for path, want := range textOverrides() {
		if !bytes.Equal(bytes.TrimSpace(configPath(response.Config, path)), []byte(want)) {
			return false
		}
	}
	// An empty requested map does not override every managed/project layer. Read
	// the real effective server set and permit only absent or explicitly disabled
	// entries; missing/invalid set is not evidence of an empty one.
	var servers map[string]map[string]json.RawMessage
	if value := response.Config["mcp_servers"]; len(value) == 0 || json.Unmarshal(value, &servers) != nil || servers == nil {
		return false
	}
	for _, server := range servers {
		if !bytes.Equal(bytes.TrimSpace(server["enabled"]), []byte("false")) {
			return false
		}
	}
	return noHooksOrNotify(response.Config)
}
