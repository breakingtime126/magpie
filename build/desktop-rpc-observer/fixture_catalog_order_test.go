package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFixtureGuardStartFollowsActualPublicCatalog(t *testing.T) {
	for _, guard := range []bool{false, true} {
		for _, catalog := range []bool{false, true} {
			var output bytes.Buffer
			o := newObserver(&output)
			if guard {
				o.fixtureGuard = guardFixture(t)
			}
			if catalog {
				o.publicPlugins = &publicPluginCatalog{digest: strings.Repeat("a", 64)}
			}
			o.emitStart()
			got := events(t, output.Bytes())
			want := []string{"observer_start"}
			if catalog {
				want = append(want, "public_plugin_catalog")
			}
			if guard {
				want = append(want, "fixture_readonly_guard_started")
			}
			if len(got) != len(want) {
				t.Fatal("unexpected startup event count", len(got), len(want))
			}
			for i, kind := range want {
				if got[i].Kind != kind || got[i].Sequence != uint64(i+1) {
					t.Fatal("catalog must precede guard activation evidence", got)
				}
				if kind == "public_plugin_catalog" && got[i].PublicPluginCatalogSHA256 != o.publicPlugins.digest {
					t.Fatal("actual catalog digest was not emitted")
				}
			}
		}
	}
}
