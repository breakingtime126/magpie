package main

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Keep this expected set independent of the production lookup construction.
var publicPluginTestIDs = []string{
	"browser@openai-bundled",
	"chrome@openai-bundled",
	"chrome-dev@openai-bundled",
	"chrome-internal@openai-bundled",
	"computer-use@openai-bundled",
	"unified-computer-use@openai-bundled",
	"sites@openai-bundled",
	"visualize@openai-bundled",
}

func publicPluginTestKeys() []string {
	var keys []string
	for _, id := range publicPluginTestIDs {
		for _, field := range []string{"enabled", "auto_provision"} {
			keys = append(keys, "plugins."+id+"."+field)
		}
	}
	return keys
}

func TestPublicPluginMetadataSetIsSeparateAndFinite(t *testing.T) {
	if len(observedPublicPluginKeys) != 16 || len(observedConfigTypes) != 830 {
		t.Fatal("frozen metadata set changed")
	}
	for _, key := range publicPluginTestKeys() {
		fixed, known := observedPublicPluginKeys[key]
		if !known || fixed != key {
			t.Fatal("public metadata key does not map to its fixed literal")
		}
		if _, exists := observedConfigTypes[key]; exists {
			t.Fatal("public plugin keys changed the frozen schema table")
		}
	}
}

func TestPublicPluginMetadataWholeKeyTrimAndLiteralValues(t *testing.T) {
	for _, key := range publicPluginTestKeys() {
		for _, spelling := range []string{key, " \t" + key + "\r\n", "\v\f" + key + "\v\f"} {
			for _, row := range []struct {
				value string
				want  string
			}{
				{"true", "bool_true"},
				{"false", "bool_false"},
				{"", "other"},
				{"True", "other"},
				{"TRUE", "other"},
				{" true ", "other"},
				{"true\n", "other"},
				{"\"true\"", "other"},
				{"'false'", "other"},
				{"true # SYNTHETIC_PRIVATE_VALUE", "other"},
				{"true=SYNTHETIC_PRIVATE_VALUE", "other"},
				{"{enabled=true,token=\"SYNTHETIC_PRIVATE_VALUE\"}", "other"},
				{"[true,\"SYNTHETIC_PRIVATE_VALUE\"]", "other"},
				{"SYNTHETIC_PRIVATE_VALUE", "other"},
			} {
				arg := spelling + "=" + row.value
				for _, args := range [][]string{
					{"-c", arg}, {"--config", arg}, {"--config=" + arg},
				} {
					before := append([]string(nil), args...)
					var log bytes.Buffer
					newObserver(&log).argv(args)
					es := events(t, log.Bytes())
					if len(es) != 2 || es[1].Kind != "argv_config" || es[1].Class != "" ||
						es[1].ConfigKey != key || es[1].ConfigValue != row.want {
						t.Fatal("public plugin metadata was not classified exactly")
					}
					if es[1].ConfigShape != "" || es[1].ConfigFamily != "" ||
						es[1].ConfigLiteral != "" || es[1].ConfigLexicalRoot != "" ||
						len(es[1].ConfigKeyClasses) != 0 {
						t.Fatal("known key mixed with unknown metadata")
					}
					if !reflect.DeepEqual(args, before) {
						t.Fatal("observation changed the forwarded arguments")
					}
					if strings.Contains(log.String(), "SYNTHETIC_PRIVATE_VALUE") {
						t.Fatal("public plugin raw value leaked")
					}
				}
			}
		}
	}
}

func TestPublicPluginMetadataRejectsNeighboringKeys(t *testing.T) {
	for _, key := range publicPluginTestKeys() {
		for _, neighbor := range []string{
			strings.ToUpper(key),
			strings.Replace(key, "plugins.", "Plugins.", 1),
			strings.Replace(key, "@openai-bundled", "@other-market", 1),
			strings.Replace(key, "@openai-bundled", "@openai-curated-remote", 1),
			strings.Replace(key, "@openai-bundled", "@openai-bundled-alpha", 1),
			strings.Replace(key, "@openai-bundled", "@OPENAI-BUNDLED", 1),
			strings.Replace(key, "@openai-bundled", "-SYNTHETIC_PRIVATE_ID@openai-bundled", 1),
			"plugins.SYNTHETIC_PRIVATE_ID@openai-bundled.enabled",
			strings.Replace(key, "plugins.", "plugins. ", 1),
			strings.Replace(key, "plugins.", "plugins .", 1),
			strings.Replace(key, "@", " @", 1),
			strings.Replace(key, "@", "@ ", 1),
			strings.Replace(key, "@openai-bundled.", "@openai-bundled. ", 1),
			"\"" + key + "\"",
			"'" + key + "'",
			strings.Replace(key, "plugins.", "plugins.\"", 1) + "\"",
			key + ".extra",
			key + "_extra",
			key + "\x00",
			key + "\u00a0",
			"\u00a0" + key,
			strings.Replace(key, ".", "..", 1),
			strings.Replace(key, ".", "/", 1),
			strings.Replace(key, ".", "\\", 1),
		} {
			for _, value := range []string{"true", "SYNTHETIC_PRIVATE_VALUE"} {
				var log bytes.Buffer
				args := []string{"-c", neighbor + "=" + value}
				before := append([]string(nil), args...)
				newObserver(&log).argv(args)
				es := events(t, log.Bytes())
				if len(es) != 2 || es[1].Class != "unknown" ||
					es[1].ConfigKey != "" || es[1].ConfigValue != "" ||
					es[1].ConfigShape == "" || es[1].ConfigFamily == "" ||
					es[1].ConfigLexicalRoot == "" || len(es[1].ConfigKeyClasses) == 0 {
					t.Fatal("unknown plugin key was exposed or lost existing redaction")
				}
				want := "other"
				if value == "true" {
					want = "bool_true"
				}
				if es[1].ConfigLiteral != want {
					t.Fatal("unknown plugin value changed its existing classification")
				}
				if !reflect.DeepEqual(args, before) {
					t.Fatal("unknown key observation changed arguments")
				}
				if strings.Contains(log.String(), "SYNTHETIC_PRIVATE") ||
					strings.Contains(log.String(), "openai-bundled") {
					t.Fatal("unknown plugin identity or raw value leaked")
				}
			}
		}
		var log bytes.Buffer
		newObserver(&log).argv([]string{"-c", key})
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Class != "unknown" || es[1].ConfigShape != "no_equals" ||
			es[1].ConfigKey != "" || es[1].ConfigValue != "" {
			t.Fatal("key without assignment was recognized")
		}
	}
}

func publicPluginForwardingFixtureArguments() []string {
	var args []string
	for _, key := range publicPluginTestKeys() {
		args = append(args, "-c", " \t"+key+"=true",
			"--config="+key+"=false", "--config", key+"=SYNTHETIC_PRIVATE_VALUE")
	}
	return args
}

func TestPublicPluginArgumentChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_PUBLIC_PLUGIN_CHILD") != "1" {
		return
	}
	want := publicPluginForwardingFixtureArguments()
	if len(os.Args) < len(want) || !reflect.DeepEqual(os.Args[len(os.Args)-len(want):], want) {
		os.Exit(41)
	}
	if os.Getenv(activeEnv) != "1" {
		os.Exit(42)
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		os.Exit(43)
	}
	os.Exit(17)
}

func TestPublicPluginArgumentsReachChildUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MAGPIE_OBSERVER_PUBLIC_PLUGIN_CHILD", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-test.run=^TestPublicPluginArgumentChildFixture$", "--"},
		publicPluginForwardingFixtureArguments()...)
	before := append([]string(nil), args...)
	payload := []byte("SYNTHETIC_PRIVATE_PAYLOAD\x00\xff\r\n")
	var output, diagnostics captureCloser
	var log bytes.Buffer
	o := newObserver(&log)
	o.argv(args)
	code := runChild(self, args, io.NopCloser(bytes.NewReader(payload)), &output, &diagnostics, o)
	if code != 17 {
		t.Fatalf("fixture exit = %d, want 17", code)
	}
	if !reflect.DeepEqual(args, before) || !bytes.Equal(output.Bytes(), payload) ||
		diagnostics.Len() != 0 || !output.closed || !diagnostics.closed {
		t.Fatal("public plugin observation changed child arguments or transport")
	}
	if strings.Contains(log.String(), "SYNTHETIC_PRIVATE") {
		t.Fatal("child argument or transport content entered metadata")
	}
}
