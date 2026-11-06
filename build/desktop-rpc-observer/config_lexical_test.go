package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestConfigKnownKeysDoNotUnquoteOrNormalizeSegments(t *testing.T) {
	for _, key := range []string{
		"\"model\"", "'model'", "\"features\".code_mode_host",
		"features.\"code_mode_host\"", "features.'code_mode_host'",
		"\"features.code_mode_host\"", "features .code_mode_host",
		"features. code_mode_host", "features\t.code_mode_host",
		"features.\tcode_mode_host", "features.\ncode_mode_host",
		"features.\rcode_mode_host", "features.\vcode_mode_host",
		"features.\fcode_mode_host", "features..code_mode_host",
		".features.code_mode_host", "features.code_mode_host.",
		"features.code mode_host", "\"featu\\u0072es\".code_mode_host",
		"\u00a0model", "model\u00a0", "\u2003model", "model\u2003",
		"\x00model", "model\x7f", "ｆeatures.code_mode_host",
		"notice.\"hide_gpt-5.1-codex-max_migration_prompt\"",
	} {
		e := observedConfigArgument(key + "=true")
		if e.Class != "unknown" || e.ConfigKey != "" || e.ConfigValue != "" ||
			e.ConfigFamily != "other" || e.ConfigLiteral != "bool_true" {
			t.Fatal("native-inequivalent spelling became a known key or parsed family")
		}
	}
	for _, key := range observedSchemaKeys {
		if strings.Contains(key, "\"") {
			continue
		}
		for _, padding := range []string{"", " ", "\t", "\r", "\n", "\v", "\f", " \t\r\n\v\f"} {
			e := observedConfigArgument(padding + key + padding + "=SYNTHETIC_SECRET")
			if e.ConfigKey != key || e.ConfigValue != "other" || e.Class != "" ||
				e.ConfigShape != "" || e.ConfigFamily != "" || e.ConfigLiteral != "" ||
				e.ConfigLexicalRoot != "" || len(e.ConfigKeyClasses) != 0 {
				t.Fatal("whole-key ASCII whitespace trim was not conservatively recognized")
			}
		}
	}
}

func TestConfigLexicalRootHasExactLiteralBoundary(t *testing.T) {
	for _, row := range [][2]string{
		{"plugins.SYNTHETIC_SECRET@market.enabled", "plugins"},
		{"plugins.", "plugins"},
		{"plugins..SYNTHETIC_SECRET", "plugins"},
		{" \t\r\n\v\fplugins.SYNTHETIC_SECRET \t\r\n\v\f", "plugins"},
		{"plugins.\nSYNTHETIC_SECRET", "plugins"},
		{"plugins.\"SYNTHETIC_SECRET\"", "plugins"},
		{"plugins.\\SYNTHETIC_SECRET", "plugins"},
		{"plugins", "other"},
		{"pluginsSYNTHETIC_SECRET.enabled", "other"},
		{"plugins@SYNTHETIC_SECRET.enabled", "other"},
		{"plugins/SYNTHETIC_SECRET.enabled", "other"},
		{"plugins\\SYNTHETIC_SECRET.enabled", "other"},
		{"plugins:SYNTHETIC_SECRET.enabled", "other"},
		{"plugins .SYNTHETIC_SECRET", "other"},
		{"plugins\t.SYNTHETIC_SECRET", "other"},
		{"plugins\n.SYNTHETIC_SECRET", "other"},
		{"\"plugins\".SYNTHETIC_SECRET", "other"},
		{"'plugins'.SYNTHETIC_SECRET", "other"},
		{"\"plugins.SYNTHETIC_SECRET\"", "other"},
		{"\"plu\\u0067ins\".SYNTHETIC_SECRET", "other"},
		{"PLUGINS.SYNTHETIC_SECRET", "other"},
		{"\u00a0plugins.SYNTHETIC_SECRET", "other"},
		{"ｐlugins.SYNTHETIC_SECRET", "other"},
		{".plugins.SYNTHETIC_SECRET", "other"},
		{"SYNTHETIC_SECRET.plugins", "other"},
	} {
		if observedConfigLexicalRoot(row[0]) != row[1] {
			t.Fatal("lexical root guessed beyond a fixed literal boundary")
		}
	}
	for _, root := range observedSchemaRoots {
		if observedConfigLexicalRoot(root+".SYNTHETIC_SECRET@market") != root ||
			observedConfigLexicalRoot(root+"SYNTHETIC_SECRET.private") != "other" ||
			observedConfigLexicalRoot(root+" .SYNTHETIC_SECRET") != "other" {
			t.Fatal("fixed root boundary mismatch")
		}
	}
}

func TestConfigKeyClassesAreFixedPresenceLabels(t *testing.T) {
	wantOrder := []string{
		"at_sign", "slash", "backslash", "colon", "ascii_space",
		"ascii_control", "non_ascii", "other_ascii_punctuation",
	}
	if !reflect.DeepEqual(observedConfigKeyClassOrder[:], wantOrder) {
		t.Fatal("unexpected fixed key class vocabulary or order")
	}
	for _, row := range []struct {
		key  string
		want []string
	}{
		{"Aa019_.-", []string{"none"}},
		{"", []string{"none"}},
		{"@@@", []string{"at_sign"}},
		{"///", []string{"slash"}},
		{"\\\\", []string{"backslash"}},
		{":::", []string{"colon"}},
		{" \t\t ", []string{"ascii_space"}},
		{"\x00\r\n\v\f\x1f\x7f", []string{"ascii_control"}},
		{"\xff\u00a0中", []string{"non_ascii"}},
		{"\"'[]{}(),;!?+=*%$&#<>|~^\x60", []string{"other_ascii_punctuation"}},
		{"[SYNTHETIC_SECRET]@@/:\\ \t\r\n\xff", wantOrder},
		{"\xff\n\t\\:/@@[SYNTHETIC_SECRET]", wantOrder},
	} {
		if !reflect.DeepEqual(observedConfigKeyClasses(row.key), row.want) {
			t.Fatal("key classes expose ordering, count or unsupported categories")
		}
	}
}

func TestUnknownConfigLexicalFieldsDoNotInspectValues(t *testing.T) {
	for _, value := range []string{
		"SYNTHETIC_SECRET", "@/:\\ []{}(),;\t\r\n\xff",
		"true ", " true", "\"true\"", "'false'", "TRUE", "false=SECRET", "",
	} {
		e := observedConfigArgument("desktop.SYNTHETIC_SECRET=" + value)
		if e.ConfigKey != "" || e.ConfigValue != "" || e.ConfigLiteral != "other" ||
			e.ConfigLexicalRoot != "desktop" || e.ConfigFamily != "desktop" ||
			!reflect.DeepEqual(e.ConfigKeyClasses, []string{"none"}) {
			t.Fatal("RHS changed fixed key classification or was interpreted")
		}
	}
	for _, row := range [][2]string{
		{"true", "bool_true"}, {"false", "bool_false"},
		{" true", "other"}, {"true\t", "other"}, {"false\n", "other"},
		{"\"true\"", "other"}, {"'false'", "other"}, {"False", "other"},
		{"true=false", "other"}, {"", "other"},
	} {
		e := observedConfigArgument("plugins.SYNTHETIC_SECRET@market.enabled=" + row[0])
		if e.ConfigLiteral != row[1] || e.ConfigShape != "other_key" ||
			e.ConfigFamily != "other" || e.ConfigLexicalRoot != "plugins" ||
			e.ConfigKey != "" || e.ConfigValue != "" ||
			!reflect.DeepEqual(e.ConfigKeyClasses, []string{"at_sign"}) {
			t.Fatal("unknown literal or lexical metadata was not exact")
		}
	}
	for _, argument := range []string{"", "desktop.SYNTHETIC_SECRET@market", "\"SYNTHETIC_SECRET\""} {
		e := observedConfigArgument(argument)
		if e.ConfigShape != "no_equals" || e.ConfigFamily != "other" ||
			e.ConfigLiteral != "other" || e.ConfigLexicalRoot != "other" ||
			!reflect.DeepEqual(e.ConfigKeyClasses, []string{"none"}) {
			t.Fatal("missing assignment acquired inferred key or value metadata")
		}
	}
	e := observedConfigArgument(" \t\r\n\v\f=true")
	if e.ConfigShape != "empty_key" || e.ConfigFamily != "other" || e.ConfigLiteral != "bool_true" ||
		e.ConfigLexicalRoot != "other" ||
		!reflect.DeepEqual(e.ConfigKeyClasses, []string{"ascii_space", "ascii_control"}) {
		t.Fatal("empty trimmed key metadata was not bounded")
	}
}

func TestUnknownConfigLexicalMetadataDoesNotFingerprintContent(t *testing.T) {
	want := observedConfigArgument("plugins.SYNTHETIC_SECRET@market.enabled=SYNTHETIC_SECRET")
	for _, n := range []int{1, 16, 256, 4096} {
		secret := strings.Repeat("S", n)
		for _, key := range []string{
			"plugins." + secret + "@market.enabled",
			"plugins.@" + secret + ".enabled",
			"plugins." + secret + strings.Repeat("@", n) + ".enabled",
		} {
			got := observedConfigArgument(key + "=" + strings.Repeat("V", n))
			if !reflect.DeepEqual(got, want) {
				t.Fatal("unknown content, position, count or length changed fixed metadata")
			}
		}
	}
}

func TestConfigLexicalFieldsAreUnknownOnlyAndArgumentsUnchanged(t *testing.T) {
	for _, argument := range []string{
		"plugins.SYNTHETIC_SECRET@market.enabled=true",
		"features .code_mode_host=false",
		"features.\"code_mode_host\"=true",
		"desktop.\x00SYNTHETIC_SECRET=@/:\\\xff",
		" \t\r\n\v\f=SYNTHETIC_SECRET",
		"SYNTHETIC_SECRET@market",
		" features.code_mode_host =true",
	} {
		for _, flag := range []string{"-c", "--config", "-c=", "--config="} {
			args := []string{flag, argument}
			if strings.HasSuffix(flag, "=") {
				args = []string{flag + argument}
			}
			before := append([]string(nil), args...)
			var log bytes.Buffer
			newObserver(&log).argv(args)
			es := events(t, log.Bytes())
			if len(es) != 2 || !reflect.DeepEqual(args, before) {
				t.Fatal("observation changed arguments or framing")
			}
			e := es[1]
			encoded, err := json.Marshal(e)
			if err != nil {
				t.Fatal("metadata could not be serialized")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal("metadata could not be read")
			}
			_, hasLiteral := fields["configLiteral"]
			_, hasRoot := fields["configLexicalRoot"]
			_, hasClasses := fields["configKeyClasses"]
			if e.Class == "unknown" {
				if e.ConfigKey != "" || e.ConfigValue != "" || e.ConfigLiteral == "" ||
					e.ConfigLexicalRoot == "" || len(e.ConfigKeyClasses) == 0 ||
					!hasLiteral || !hasRoot || !hasClasses {
					t.Fatal("unknown config lacks fixed metadata or exposes a key/value")
				}
			} else if e.ConfigLiteral != "" || e.ConfigLexicalRoot != "" ||
				len(e.ConfigKeyClasses) != 0 || hasLiteral || hasRoot || hasClasses {
				t.Fatal("known config acquired unknown-only metadata")
			}
			if strings.Contains(log.String(), "SYNTHETIC_SECRET") ||
				strings.Contains(log.String(), "market") {
				t.Fatal("raw key, dynamic suffix or value leaked")
			}
		}
	}
}
