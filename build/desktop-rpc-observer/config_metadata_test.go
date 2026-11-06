package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestObservedConfigMetadataIsFiniteAndRedacted(t *testing.T) {
	if len(observedConfigTypes) != 233 {
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
			if len(es) != 2 || es[1].ConfigKey != key || es[1].ConfigValue != observedConfigValue(kind, input) {
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
	for _, kind := range []string{"object", "unknown"} {
		for _, value := range []string{"true", "false", "file", "keyring", "{enabled=true}"} {
			if observedConfigValue(kind, value) != "other" {
				t.Fatal("opaque value was interpreted")
			}
		}
	}
	for _, kind := range []string{"cli_store", "mcp_store", "sandbox"} {
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
	} {
		if observedConfigValue(row.kind, row.value) != row.want {
			t.Fatal("literal classification mismatch")
		}
	}
}
