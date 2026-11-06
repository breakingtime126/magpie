package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func publicCatalogTestValue(t *testing.T, keys []string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	wire := publicPluginCatalogWire{
		Schema: 1, PackageSHA256: strings.Repeat("a", 64), ArchiveSHA256: strings.Repeat("b", 64),
		KeysSHA256: hex.EncodeToString(digest[:]), Keys: keys,
	}
	value, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}

func TestPublicCatalogCanonicalBindingAndLimits(t *testing.T) {
	key := "plugins.browser@openai-curated-remote.enabled"
	value := publicCatalogTestValue(t, []string{key})
	catalog, err := parsePublicPluginCatalog(value)
	if err != nil || catalog.keys[key] != 1 {
		t.Fatal("valid public catalog was not loaded")
	}
	for _, bad := range []string{
		"", "null", "{}", value + "\n", strings.Repeat(" ", 16385),
		strings.Replace(value, "\"schema\":1", "\"schema\":1,\"schema\":1", 1),
		strings.Replace(value, "\"schema\":1", "\"Schema\":1", 1),
		strings.Replace(value, "\"schema\":1", "\"schema\":1,\"ignored\":true", 1),
		strings.Replace(value, "\"schema\":1", "\"schema\":2", 1),
		strings.Replace(value, "\"schema\":1", "\"schema\":1.0", 1),
		strings.Replace(value, "\"packageSha256\":\"a", "\"packageSha256\":\"A", 1),
		strings.Replace(value, "\"archiveSha256\":\"b", "\"archiveSha256\":\"z", 1),
		strings.Replace(value, "\"plugins.", "\"plugins\\u002e", 1),
		strings.Replace(value, catalog.digest, strings.Repeat("0", 64), 1),
		publicCatalogTestValue(t, nil), publicCatalogTestValue(t, []string{}),
		publicCatalogTestValue(t, []string{key, key}),
		publicCatalogTestValue(t, []string{"plugins.z@market.enabled", "plugins.a@market.enabled"}),
	} {
		loaded, err := parsePublicPluginCatalog(bad)
		if err == nil || loaded != nil || err.Error() != "invalid public plugin catalog" {
			t.Fatal("invalid public catalog did not fail closed")
		}
	}
	var keys []string
	for i := 0; i < 128; i++ {
		keys = append(keys, "plugins.fixture"+strconv.Itoa(i)+"@market.enabled")
	}
	sort.Strings(keys)
	if _, err := parsePublicPluginCatalog(publicCatalogTestValue(t, keys)); err != nil {
		t.Fatal("bounded maximum catalog rejected")
	}
	keys = append(keys, "plugins.zzz@market.enabled")
	if _, err := parsePublicPluginCatalog(publicCatalogTestValue(t, keys)); err == nil {
		t.Fatal("oversized catalog accepted")
	}
}

func TestPublicCatalogKeyGrammarIsExact(t *testing.T) {
	for _, key := range []string{
		"plugins.browser@openai-curated-remote.enabled",
		"plugins.A1_b-c@M2_k-l.auto_provision",
	} {
		if !publicPluginKey(key) {
			t.Fatal("valid ASCII public key rejected")
		}
	}
	for _, key := range []string{
		"plugins.unknown.enabled", "plugins.a@b.enabled.extra", "plugins.a@b.mcp_servers",
		"plugins.a@b.enabled ", "plugins.a @b.enabled", "plugins.a@ b.enabled",
		"plugins.\"a@b\".enabled", "'plugins.a@b.enabled'", "Plugins.a@b.enabled",
		"plugins.a@@b.enabled", "plugins..a@b.enabled", "plugins.a.b@c.enabled",
		"plugins.a@b.Enabled", "plugins.a@b.apps", "plugins.a@b.enabled\x00",
		"plugins.a@b.enabled\u00a0", "plugins.a/b@c.enabled", "plugins.a\\b@c.enabled",
		"plugins._a@b.enabled", "plugins.a@-b.enabled",
		"plugins." + strings.Repeat("a", 65) + "@b.enabled",
		"plugins." + strings.Repeat("a", 64) + "@" + strings.Repeat("b", 64) + ".enabled",
	} {
		if publicPluginKey(key) {
			t.Fatal("unsupported public key accepted")
		}
		if _, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key})); err == nil {
			t.Fatal("catalog accepted an unsupported key")
		}
	}
}

func TestPublicCatalogRefsAreBoundedAndNeverExposeKeysOrValues(t *testing.T) {
	key := "plugins.browser@openai-curated-remote.enabled"
	value := publicCatalogTestValue(t, []string{key})
	catalog, err := parsePublicPluginCatalog(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		value string
		want  string
	}{
		{"true", "bool_true"}, {"false", "bool_false"}, {" true ", "other"},
		{"true=SYNTHETIC_PRIVATE_VALUE", "other"}, {"\"true\"", "other"},
		{"SYNTHETIC_PRIVATE_VALUE", "other"},
	} {
		args := []string{"-c", " \t" + key + "\r\n=" + row.value}
		before := append([]string(nil), args...)
		var log bytes.Buffer
		o := newObserver(&log)
		o.publicPlugins = catalog
		o.emit(event{Kind: "public_plugin_catalog", PublicPluginCatalogSHA256: catalog.digest})
		o.argv(args)
		es := events(t, log.Bytes())
		if len(es) != 3 || es[0].PublicPluginCatalogSHA256 != catalog.digest ||
			es[2].PublicPluginRef != 1 || es[2].ConfigValue != row.want ||
			es[2].ConfigKey != "" || es[2].Class != "" || es[2].ConfigShape != "" ||
			es[2].ConfigFamily != "" || es[2].ConfigLiteral != "" ||
			es[2].ConfigLexicalRoot != "" || len(es[2].ConfigKeyClasses) != 0 {
			t.Fatal("public index metadata did not preserve its contract")
		}
		if !reflect.DeepEqual(args, before) || strings.Contains(log.String(), "browser") ||
			strings.Contains(log.String(), "SYNTHETIC_PRIVATE") {
			t.Fatal("public observation changed arguments or exposed strings")
		}
	}
	for _, arg := range []string{
		key, key + ".extra=true", "\"" + key + "\"=true",
		"plugins.SYNTHETIC_PRIVATE_ID@private-market.enabled=true",
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.publicPlugins = catalog
		o.argv([]string{"--config=" + arg})
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Class != "unknown" || es[1].PublicPluginRef != 0 ||
			es[1].ConfigKey != "" || strings.Contains(log.String(), "SYNTHETIC_PRIVATE") {
			t.Fatal("unknown identity was promoted or leaked")
		}
	}
}

func TestPublicCatalogDoesNotReplaceFrozenRecognitionOrDefaultBehavior(t *testing.T) {
	key := "plugins.chrome@openai-bundled.enabled"
	catalog, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key}))
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.publicPlugins = catalog
	o.argv([]string{"-c", key + "=true"})
	es := events(t, log.Bytes())
	if es[1].ConfigKey != key || es[1].PublicPluginRef != 0 {
		t.Fatal("catalog replaced frozen public metadata")
	}
	log.Reset()
	newObserver(&log).argv([]string{"-c", "plugins.unlisted@market.enabled=true"})
	if events(t, log.Bytes())[1].Class != "unknown" {
		t.Fatal("absent catalog changed unknown behavior")
	}
}

func TestPublicCatalogEnvironmentNeverReachesProductionChild(t *testing.T) {
	t.Setenv(publicPluginCatalogEnv, "SYNTHETIC_PRIVATE_CATALOG")
	t.Setenv(strings.ToLower(publicPluginCatalogEnv), "SYNTHETIC_PRIVATE_LOWERCASE")
	t.Setenv("MAGPIE_CATALOG_FIXTURE_UNRELATED", "keep")
	found := false
	for _, value := range childEnvironment() {
		key, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, publicPluginCatalogEnv) || strings.Contains(value, "SYNTHETIC_PRIVATE") {
			t.Fatal("public catalog environment reached child")
		}
		found = found || value == "MAGPIE_CATALOG_FIXTURE_UNRELATED=keep"
	}
	if !found {
		t.Fatal("unrelated child environment changed")
	}
}
