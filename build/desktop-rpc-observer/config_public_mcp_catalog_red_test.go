package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// Only APIs already present at e1c8fb52 are used: the base must fail the
// behavioral catalog assertion, not compilation against a new symbol.
func TestPublicMCPManifestCatalogRecognizedWithoutMutation(t *testing.T) {
	key := "plugins.fixture@public-market.mcp_servers.fixture-server.enabled"
	catalog, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key}))
	if err != nil {
		t.Fatal("source-bound nested MCP catalog key was rejected")
	}
	for _, row := range []struct {
		rhs  string
		want string
	}{
		{"true", "bool_true"}, {"false", "bool_false"}, {" true ", "other"},
		{"PRIVATE_VALUE", "other"}, {"true=PRIVATE_VALUE", "other"},
	} {
		args := []string{"-c", " \t" + key + "\r\n=" + row.rhs}
		before := append([]string(nil), args...)
		var log bytes.Buffer
		o := newObserver(&log)
		o.publicPlugins = catalog
		o.argv(args)
		es := events(t, log.Bytes())
		if len(es) != 2 {
			t.Fatal("nested catalog config event missing")
		}
		got := es[1]
		got.Sequence, got.PID = 0, 0
		if !reflect.DeepEqual(got, event{Kind: "argv_config", PublicPluginRef: 1, ConfigValue: row.want}) {
			t.Fatal("nested catalog key did not emit only exact ref and boolean classification")
		}
		if !reflect.DeepEqual(args, before) || strings.Contains(log.String(), "fixture") ||
			strings.Contains(log.String(), "public-market") || strings.Contains(log.String(), "PRIVATE_VALUE") {
			t.Fatal("nested catalog observation mutated argv or exposed names or values")
		}
	}
}
