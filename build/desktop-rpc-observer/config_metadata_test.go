package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestObservedConfigMetadataIsFiniteAndRedacted(t *testing.T) {
	if len(observedConfigTypes) != 830 {
		t.Fatal("unexpected frozen metadata set")
	}
	var log bytes.Buffer
	o := newObserver(&log)
	for key, kind := range observedConfigTypes {
		for _, input := range []string{"true", "false", "SECRET", "{secret=\"SECRET\"}", "\"SECRET\""} {
			log.Reset()
			o = newObserver(&log)
			o.argv([]string{"-c", key + "=" + input})
			es := events(t, log.Bytes())
			if len(es) != 2 {
				t.Fatal("missing config metadata")
			}
			if key == "notice.\"hide_gpt-5.1-codex-max_migration_prompt\"" {
				if es[1].Class != "unknown" || es[1].ConfigKey != "" || es[1].ConfigValue != "" {
					t.Fatal("dotted quoted segment was interpreted")
				}
			} else if es[1].ConfigKey != key || es[1].ConfigValue != observedConfigValue(kind, input) {
				t.Fatal("known key was not classified")
			}
			if strings.Contains(log.String(), "SECRET") {
				t.Fatal("raw config value leaked")
			}
		}
	}
	for _, key := range []string{"features.SECRET", "features.plugins.SECRET", "plugins.SECRET.enabled",
		"apps.SECRET.enabled", "model_providers.SECRET", "desktop.SECRET", "FEATURES.plugins"} {
		log.Reset()
		o = newObserver(&log)
		o.argv([]string{"--config", key + "=true"})
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Class != "unknown" || es[1].ConfigKey != "" || es[1].ConfigValue != "" {
			t.Fatal("dynamic or unknown key was exposed")
		}
		if strings.Contains(log.String(), "SECRET") {
			t.Fatal("raw config key leaked")
		}
	}
}

func TestObservedConfigLiteralClassification(t *testing.T) {
	for _, kind := range []string{"object", "opaque", "unknown"} {
		for _, value := range []string{"true", "false", "file", "keyring", "{enabled=true}"} {
			if observedConfigValue(kind, value) != "other" {
				t.Fatal("opaque value was interpreted")
			}
		}
	}
	for _, kind := range []string{"cli_store", "mcp_store", "sandbox", "login_method"} {
		if observedConfigValue(kind, "true") != "other" {
			t.Fatal("enum interpreted as a boolean")
		}
		for _, value := range []string{"SECRET", "keyringSECRET", "SECRETkeyring", "{key=\"SECRET\"}"} {
			if observedConfigValue(kind, value) != "other" {
				t.Fatal("nonliteral enum accepted")
			}
		}
	}
	for _, row := range []struct {
		kind  string
		value string
		want  string
	}{
		{"bool", "true", "bool_true"},
		{"bool", "false", "bool_false"},
		{"bool", "\"true\"", "other"},
		{"bool", " true ", "other"},
		{"cli_store", "keyring", "enum_keyring"},
		{"cli_store", "'keyring'", "enum_keyring"},
		{"cli_store", "\"keyring\"", "enum_keyring"},
		{"cli_store", "ephemeral", "enum_ephemeral"},
		{"mcp_store", "ephemeral", "other"},
		{"sandbox", "\"mxc\"", "enum_mxc"},
		{"sandbox", "keyring", "other"},
		{"login_method", "\"chatgpt\"", "enum_chatgpt"},
		{"login_method", "'api'", "enum_api"},
		{"login_method", "api", "enum_api"},
		{"login_method", "keyring", "other"},
		{"cli_store", "chatgpt", "other"},
	} {
		if observedConfigValue(row.kind, row.value) != row.want {
			t.Fatal("literal classification mismatch")
		}
	}
}

func TestForcedLoginMetadataNeverChangesArguments(t *testing.T) {
	for _, value := range []string{"\"chatgpt\"", "'api'", "SYNTHETIC_SECRET", "{mode=\"SYNTHETIC_SECRET\"}"} {
		args := []string{"-c", "forced_login_method=" + value}
		before := append([]string(nil), args...)
		var log bytes.Buffer
		o := newObserver(&log)
		o.argv(args)
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].ConfigKey != "forced_login_method" ||
			es[1].ConfigValue != observedConfigValue("login_method", value) {
			t.Fatal("forced login method metadata was not classified")
		}
		if strings.Join(args, "\x00") != strings.Join(before, "\x00") {
			t.Fatal("observation changed the forwarded arguments")
		}
		if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
			t.Fatal("forced login raw value leaked")
		}
	}
}
