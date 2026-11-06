package main

import "strings"

// These identities are public source metadata, not evidence that Desktop uses
// any particular override, that a plugin is installed, or that enabling it is safe.
// Frozen openai/codex source at c40729ea5cf8e947481b130c59807273eda2f8e9.
//
// ALLOWLISTED_BUNDLED_HOOKS in bundled_hooks.rs names browser, chrome,
// chrome-dev, chrome-internal, computer-use and unified-computer-use at
// openai-bundled. merge_configured_plugins_with_remote_installed in loader.rs
// names sites and visualize at openai-bundled. PluginConfig in types.rs defines
// enabled and auto_provision as distinct optional boolean fields.
//
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/plugin/src/bundled_hooks.rs
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/core-plugins/src/loader.rs
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/config/src/types.rs
//
// Lookup values are fixed literals, never caller-supplied bytes. No schema or
// plugin manifest is fetched at runtime. This map is separate from the frozen
// schema keys and changes only test-observer metadata, never CLI acceptance.
var observedPublicPluginKeys = map[string]string{
	"plugins.browser@openai-bundled.enabled":                     "plugins.browser@openai-bundled.enabled",
	"plugins.browser@openai-bundled.auto_provision":              "plugins.browser@openai-bundled.auto_provision",
	"plugins.chrome@openai-bundled.enabled":                      "plugins.chrome@openai-bundled.enabled",
	"plugins.chrome@openai-bundled.auto_provision":               "plugins.chrome@openai-bundled.auto_provision",
	"plugins.chrome-dev@openai-bundled.enabled":                  "plugins.chrome-dev@openai-bundled.enabled",
	"plugins.chrome-dev@openai-bundled.auto_provision":           "plugins.chrome-dev@openai-bundled.auto_provision",
	"plugins.chrome-internal@openai-bundled.enabled":             "plugins.chrome-internal@openai-bundled.enabled",
	"plugins.chrome-internal@openai-bundled.auto_provision":      "plugins.chrome-internal@openai-bundled.auto_provision",
	"plugins.computer-use@openai-bundled.enabled":                "plugins.computer-use@openai-bundled.enabled",
	"plugins.computer-use@openai-bundled.auto_provision":         "plugins.computer-use@openai-bundled.auto_provision",
	"plugins.unified-computer-use@openai-bundled.enabled":        "plugins.unified-computer-use@openai-bundled.enabled",
	"plugins.unified-computer-use@openai-bundled.auto_provision": "plugins.unified-computer-use@openai-bundled.auto_provision",
	"plugins.sites@openai-bundled.enabled":                       "plugins.sites@openai-bundled.enabled",
	"plugins.sites@openai-bundled.auto_provision":                "plugins.sites@openai-bundled.auto_provision",
	"plugins.visualize@openai-bundled.enabled":                   "plugins.visualize@openai-bundled.enabled",
	"plugins.visualize@openai-bundled.auto_provision":            "plugins.visualize@openai-bundled.auto_provision",
}

// Match the complete key after only whole-key ASCII trimming. Do not unquote,
// normalize case, trim individual segments, or recognize arbitrary plugin IDs.
// The native override layer splits literal dots rather than parsing TOML keys.
func observedPublicPluginConfigKey(key string) (string, bool) {
	fixed, known := observedPublicPluginKeys[strings.Trim(key, observedConfigASCIIWhitespace)]
	return fixed, known
}
