package main

import (
	"bytes"
	"strings"
	"testing"
)

// This fixture uses only the observer API already present at eed7071a.
// Copy this file alone to that commit for the behavioral red test. Every
// expected key below is public pinned-source metadata, not a runtime claim.
func TestPublicPluginOverridesRecognizedWithoutMutation(t *testing.T) {
	for _, key := range []string{
		"plugins.browser@openai-bundled.enabled",
		"plugins.browser@openai-bundled.auto_provision",
		"plugins.chrome@openai-bundled.enabled",
		"plugins.chrome@openai-bundled.auto_provision",
		"plugins.chrome-dev@openai-bundled.enabled",
		"plugins.chrome-dev@openai-bundled.auto_provision",
		"plugins.chrome-internal@openai-bundled.enabled",
		"plugins.chrome-internal@openai-bundled.auto_provision",
		"plugins.computer-use@openai-bundled.enabled",
		"plugins.computer-use@openai-bundled.auto_provision",
		"plugins.unified-computer-use@openai-bundled.enabled",
		"plugins.unified-computer-use@openai-bundled.auto_provision",
		"plugins.sites@openai-bundled.enabled",
		"plugins.sites@openai-bundled.auto_provision",
		"plugins.visualize@openai-bundled.enabled",
		"plugins.visualize@openai-bundled.auto_provision",
	} {
		t.Run(key, func(t *testing.T) {
			var log bytes.Buffer
			args := []string{"-c", key + "=true"}
			newObserver(&log).argv(args)
			es := events(t, log.Bytes())
			if len(es) != 2 || es[1].Kind != "argv_config" || es[1].Class != "" ||
				es[1].ConfigKey != key || es[1].ConfigValue != "bool_true" {
				t.Fatal("public bundled-plugin exact key was not recognized")
			}
			if args[0] != "-c" || args[1] != key+"=true" {
				t.Fatal("diagnostic observation changed an argument")
			}
			log.Reset()
			newObserver(&log).argv([]string{"--config", key + "=SYNTHETIC_PRIVATE_VALUE"})
			es = events(t, log.Bytes())
			if len(es) != 2 || es[1].ConfigKey != key || es[1].ConfigValue != "other" {
				t.Fatal("public bundled-plugin value was not classified opaquely")
			}
			if strings.Contains(log.String(), "SYNTHETIC_PRIVATE_VALUE") {
				t.Fatal("diagnostic leaked a value")
			}
		})
	}
}
