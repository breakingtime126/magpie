package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// This fixture uses only the pre-change API so the base fails behaviorally,
// rather than failing to compile because a new symbol is unavailable.
func TestPluginShapeUnknownPathMetadataRed(t *testing.T) {
	var log bytes.Buffer
	newObserver(&log).argv([]string{"-c", "plugins.SYNTHETIC_PRIVATE_PLUGIN@private-market.apps.PrivateApp.tools.PrivateTool.enabled=true"})
	decoder := json.NewDecoder(&log)
	var flag, config map[string]any
	if err := decoder.Decode(&flag); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	if config["configPluginShape"] != "app_tool_enabled" {
		t.Fatal("unknown plugin path missing fixed app_tool_enabled shape")
	}
}
