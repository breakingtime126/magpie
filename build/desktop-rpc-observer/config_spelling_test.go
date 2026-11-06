package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigKeySpellingsRemainMetadataOnly(t *testing.T) {
	for _, row := range [][3]string{
		{" features.code_mode_host =true", "features.code_mode_host", "bool_true"},
		{"\tfeatures.code_mode_host\t=true", "features.code_mode_host", "bool_true"},
		{"\r\n\v\fforced_login_method \t\r\n\v\f='chatgpt'", "forced_login_method", "enum_chatgpt"},
		{" cli_auth_credentials_store =\"keyring\"", "cli_auth_credentials_store", "enum_keyring"},
		{"plugins._default.enabled=false", "plugins._default.enabled", "bool_false"},
		{"features.code_mode_host= true", "features.code_mode_host", "other"},
		{"features.code_mode_host=true ", "features.code_mode_host", "other"},
		{"model=\"SYNTHETIC_SECRET\"", "model", "other"},
		{" model =true", "model", "other"},
		{"approval_policy.granular.rules=false", "approval_policy.granular.rules", "other"},
		{"features.current_time_reminder.clock_source='model'", "features.current_time_reminder.clock_source", "other"},
		{"otel.exporter.otlp-http.tls.client-private-key=\"SYNTHETIC_SECRET\"", "otel.exporter.otlp-http.tls.client-private-key", "other"},
		{"features.tool_registry.error_on_tool_collisions=true", "features.tool_registry.error_on_tool_collisions", "other"},
		{"desktop={credential=\"SYNTHETIC_SECRET\"}", "desktop", "other"},
	} {
		for _, form := range []string{"-c", "--config", "-c=", "--config="} {
			args := []string{form, row[0]}
			if strings.HasSuffix(form, "=") {
				args = []string{form + row[0]}
			}
			before := append([]string(nil), args...)
			var log bytes.Buffer
			newObserver(&log).argv(args)
			es := events(t, log.Bytes())
			if len(es) != 2 || es[1].ConfigKey != row[1] || es[1].ConfigValue != row[2] ||
				es[1].Class != "" || es[1].ConfigShape != "" || es[1].ConfigFamily != "" ||
				es[1].ConfigLiteral != "" || es[1].ConfigLexicalRoot != "" || len(es[1].ConfigKeyClasses) != 0 {
				t.Fatal("known key spelling was not classified safely")
			}
			if strings.Join(args, "\x00") != strings.Join(before, "\x00") {
				t.Fatal("key observation changed forwarded arguments")
			}
			if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
				t.Fatal("raw value leaked")
			}
		}
	}
}

func TestUnknownConfigShapesNeverExposeInput(t *testing.T) {
	for _, row := range [][2]string{
		{"", "no_equals"},
		{"model", "no_equals"},
		{"SYNTHETIC_SECRET", "no_equals"},
		{"\"SYNTHETIC_SECRET\"", "no_equals"},
		{"=SYNTHETIC_SECRET", "empty_key"},
		{" \t =SYNTHETIC_SECRET", "empty_key"},
		{"SYNTHETIC_SECRET=true", "plain_key"},
		{"features.SYNTHETIC_SECRET=false", "plain_key"},
		{" desktop . SYNTHETIC_SECRET =true", "other_key"},
		{"FEATURES.code_mode_host=true", "plain_key"},
		{"notice.hide_gpt-5.1-codex-max_migration_prompt=true", "plain_key"},
		{"\"features.code_mode_host\"=true", "quoted_key"},
		{"features.\"code_mode_host.enabled\"=true", "quoted_key"},
		{"notice.\"hide_gpt-5.1-codex-max_migration_prompt\"=true", "quoted_key"},
		{"'SYNTHETIC_SECRET'=true", "quoted_key"},
		{"\"features\".\"code_mode_host\\u002eenabled\"=true", "quoted_key"},
		{"\"featu\\u0072es\".code_mode_host=true", "quoted_key"},
		{"\"features.code_mode_host=true", "quoted_key"},
		{"\"\"=true", "quoted_key"},
		{"features.'code_mode_host'junk=true", "quoted_key"},
		{"features.\"code_mode_host\n\"=true", "quoted_key"},
		{"features..code_mode_host=true", "other_key"},
		{"features.code_mode_host.=true", "other_key"},
		{"features.\ncode_mode_host=true", "other_key"},
		{"features . code mode host=true", "other_key"},
		{"features.\x00code_mode_host=true", "other_key"},
		{"features.\xffcode_mode_host=true", "other_key"},
		{"ｆeatures.code_mode_host=true", "other_key"},
		{"features.\u00a0code_mode_host=true", "other_key"},
		{"C:\\SYNTHETIC_SECRET=true", "other_key"},
		{"https://SYNTHETIC_SECRET=true", "other_key"},
	} {
		args := []string{"--config", row[0]}
		before := append([]string(nil), args...)
		var log bytes.Buffer
		newObserver(&log).argv(args)
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Kind != "argv_config" || es[1].Class != "unknown" ||
			es[1].ConfigShape != row[1] || es[1].ConfigKey != "" || es[1].ConfigValue != "" {
			t.Fatal("unknown config shape was not safely classified")
		}
		if strings.Join(args, "\x00") != strings.Join(before, "\x00") {
			t.Fatal("unknown key observation changed forwarded arguments")
		}
		if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
			t.Fatal("raw unknown key or value leaked")
		}
	}
	var log bytes.Buffer
	newObserver(&log).argv([]string{"--SYNTHETIC_SECRET"})
	es := events(t, log.Bytes())
	if len(es) != 1 || es[0].Kind != "argv" || es[0].Class != "unknown" || es[0].ConfigShape != "" ||
		es[0].ConfigKey != "" || es[0].ConfigValue != "" || es[0].ConfigFamily != "" ||
		es[0].ConfigLiteral != "" || es[0].ConfigLexicalRoot != "" || len(es[0].ConfigKeyClasses) != 0 ||
		strings.Contains(log.String(), "SYNTHETIC_SECRET") {
		t.Fatal("ordinary unknown argv acquired config metadata")
	}
}

func TestUnknownConfigShapesDoNotFingerprintLengths(t *testing.T) {
	for _, n := range []int{1, 16, 256, 4096} {
		key := strings.Repeat("s", n)
		value := strings.Repeat("v", n)
		e := observedConfigArgument(key + "=" + value)
		if e.Kind != "argv_config" || e.Class != "unknown" || e.ConfigShape != "plain_key" ||
			e.ConfigKey != "" || e.ConfigValue != "" || e.ConfigFamily != "other" {
			t.Fatal("unknown key content or length changed fixed metadata")
		}
	}
}

func TestUnknownConfigFamiliesAreFixedAndDoNotGuessSegments(t *testing.T) {
	for _, row := range [][2]string{
		{"desktop.SYNTHETIC_SECRET=true", "desktop"},
		{"plugins.SYNTHETIC_SECRET.enabled=true", "plugins"},
		{"features.SYNTHETIC_SECRET=false", "features"},
		{"model_providers.SYNTHETIC_SECRET.base_url=\"SYNTHETIC_SECRET\"", "model_providers"},
		{" \"desktop\" . 'SYNTHETIC_SECRET' =true", "other"},
		{"'plugins' . \"SYNTHETIC_SECRET\" . enabled=false", "other"},
		{"DESKTOP.SYNTHETIC_SECRET=true", "other"},
		{"desktopSYNTHETIC_SECRET.private=true", "other"},
		{"SYNTHETIC_SECRET.desktop=true", "other"},
		{"\"desktop.SYNTHETIC_SECRET\"=true", "other"},
		{"desktop.\"SYNTHETIC_SECRET.private\"=true", "other"},
		{"\"de\\u0073ktop\".SYNTHETIC_SECRET=true", "other"},
		{"desktop.\nSYNTHETIC_SECRET=true", "other"},
		{"desktop.\x00SYNTHETIC_SECRET=true", "other"},
		{"desktop.\xffSYNTHETIC_SECRET=true", "other"},
		{"desktop.\u00a0SYNTHETIC_SECRET=true", "other"},
		{"ｄesktop.SYNTHETIC_SECRET=true", "other"},
		{"desktop.SYNTHETIC_SECRET", "other"},
		{"=SYNTHETIC_SECRET", "other"},
	} {
		var log bytes.Buffer
		newObserver(&log).argv([]string{"-c", row[0]})
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Class != "unknown" || es[1].ConfigFamily != row[1] ||
			es[1].ConfigKey != "" || es[1].ConfigValue != "" {
			t.Fatal("unknown family was guessed or exposed input")
		}
		if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
			t.Fatal("dynamic family suffix leaked")
		}
	}
	for _, root := range observedSchemaRoots {
		e := observedConfigArgument(root + ".SYNTHETIC_SECRET=true")
		if e.Class != "unknown" || e.ConfigFamily != root || e.ConfigShape != "plain_key" ||
			e.ConfigKey != "" || e.ConfigValue != "" {
			t.Fatal("frozen root family was not recognized")
		}
	}
}
