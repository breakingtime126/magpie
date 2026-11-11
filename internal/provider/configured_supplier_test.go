package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
)

const supplierFixtureEndpoint = "https://supplier.invalid/v1"

func supplierFixture(t *testing.T) Provider {
	t.Helper()
	home := t.TempDir()
	testenv.SetHome(t, home)
	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA", "CODEX_HOME"} {
		t.Setenv(variable, filepath.Join(home, variable))
	}
	return Provider{ID: "fixture-supplier", Chat: supplierFixtureEndpoint, Key: "SYNTHETIC_SECRET_CANARY", Models: []string{"model-a"}}
}

func supplierWrite(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	supplierWriteBytes(t, path, b)
}

func supplierWriteBytes(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func supplierWriteList(t *testing.T, p Provider, ms []catalog.Model) {
	t.Helper()
	supplierWrite(t, catalog.LivePath(p.ID), struct {
		Base   string
		Models []catalog.Model
	}{supplierFixtureEndpoint, ms})
}

func supplierReadFixture(t *testing.T) ConfiguredSupplierSnapshot {
	t.Helper()
	s, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConfiguredSupplierDeclaredAndUserMetadata(t *testing.T) {
	p := supplierFixture(t)
	p.Models, p.Contexts = []string{"local-model"}, map[string]int{"local-model": 32000}
	p.Headers = map[string]string{"Authorization": "SYNTHETIC_HEADER_CANARY"}
	p.ExtraBody = ExtraBody{"private": json.RawMessage(`"SYNTHETIC_BODY_CANARY"`)}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWrite(t, settings.Path(), settings.Settings{
		ModelIDs:     map[string]string{p.ID + "/local-model": "vendor/model-a"},
		ModelOutputs: map[string]int{p.ID + "/*": 9000},
		ModelImages:  map[string]bool{p.ID + "/local-model": false},
		ModelNames:   map[string]string{p.ID + "/local-model": "My model"},
		ModelEfforts: map[string][]string{p.ID + "/local-model": {"high"}},
	})
	supplierWriteList(t, p, []catalog.Model{{ID: "vendor/model-a", Name: "Declared model", Context: 64000, Output: 12000,
		InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, Efforts: []string{"low", "high"}, DefaultEffort: "high",
		Codex: json.RawMessage(`{"never_copy":"SYNTHETIC_NATIVE_SHAPED_CANARY"}`), Keys: []string{"SYNTHETIC_KEY_ID_CANARY"}}})
	before := AllBuilt.Load()
	s := supplierReadFixture(t)
	if AllBuilt.Load() != before {
		t.Fatal("extractor enumerated accounts")
	}
	if len(s.Models) != 1 {
		t.Fatal(s.Models)
	}
	m := s.Models[0]
	if m.RouteID != p.ID+"/local-model" || m.ModelID != "vendor/model-a" || m.Name != "My model" || m.Context != 32000 || m.Output != 9000 || m.DefaultEffort != "high" || !reflect.DeepEqual(m.Efforts, []string{"high"}) || !reflect.DeepEqual(m.Inputs, []string{"text"}) {
		t.Fatalf("wrong metadata: %+v", m)
	}
	if m.ContextSource != SupplierUser || m.OutputSource != SupplierUser || m.InputSource != SupplierUser || m.EffortSource != SupplierUser || m.OutputModalitiesSource != SupplierDeclared {
		t.Fatalf("wrong provenance: %+v", m)
	}
	b, err := json.Marshal(s)
	if err != nil || strings.Contains(string(b), "CANARY") || strings.Contains(string(b), "supplier.invalid") || strings.Contains(string(b), "Authorization") {
		t.Fatalf("source escaped boundary: %s %v", b, err)
	}
	if !s.Revision.Current(context.Background()) {
		t.Fatal("unchanged source expired")
	}
	if _, err := os.Stat(os.Getenv("CODEX_HOME")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("extractor touched native home")
	}
}

func TestConfiguredSupplierExplicitPicksNeedNoList(t *testing.T) {
	p := supplierFixture(t)
	p.Models = []string{"unknown-model", "gpt-5.5"}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	s := supplierReadFixture(t)
	if len(s.Models) != 2 {
		t.Fatal(s.Models)
	}
	for _, m := range s.Models {
		if m.Context != 0 || m.Output != 0 || m.Inputs != nil || m.Efforts != nil || m.ContextSource != "" || m.EffortSource != "" {
			t.Fatalf("invented capabilities: %+v", m)
		}
	}
	p.Models = nil
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierNoModels) {
		t.Fatal(err)
	}
	if s.Revision.Current(context.Background()) {
		t.Fatal("removed picks retained generation")
	}
}

func TestConfiguredSupplierFreshInstallHasNoModels(t *testing.T) {
	_ = supplierFixture(t)
	if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierNoModels) {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty source created a provider file")
	}
}

func TestConfiguredSupplierDoesNotResurrectNonchatPicks(t *testing.T) {
	p := supplierFixture(t)
	p.Models = []string{"draw", "draw-alias", "film", "decide", "nontext", "good"}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWrite(t, settings.Path(), settings.Settings{ModelIDs: map[string]string{p.ID + "/draw-alias": "draw"}})
	supplierWriteList(t, p, []catalog.Model{{ID: "draw", Draws: true}, {ID: "film", Films: true}, {ID: "decide", Decides: true}, {ID: "nontext", OutputModalities: []string{"image"}}, {ID: "good", OutputModalities: []string{"text"}}})
	if ms := supplierReadFixture(t).Models; len(ms) != 1 || ms[0].RouteID != p.ID+"/good" {
		t.Fatal("nonchat picks became unknown chat models", ms)
	}
}

func TestConfiguredSupplierDoesNotWireChatPicksToNonchatModels(t *testing.T) {
	for _, kind := range []string{"exact", "wildcard", "wildcard-name"} {
		t.Run(kind, func(t *testing.T) {
			p := supplierFixture(t)
			p.Models = []string{"local-model", "good"}
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			wires := map[string]string{p.ID + "/good": "good"}
			target := "draw"
			switch kind {
			case "exact":
				wires[p.ID+"/local-model"] = target
			case "wildcard":
				wires[p.ID+"/*"] = target
			case "wildcard-name":
				target = "vendor/local-model"
				wires[p.ID+"/*"] = "vendor/*"
			}
			supplierWriteList(t, p, []catalog.Model{{ID: target, Draws: true}, {ID: "good", OutputModalities: []string{"text"}}})
			before := supplierReadFixture(t)
			if len(before.Models) != 2 {
				t.Fatal(before.Models)
			}
			supplierWrite(t, settings.Path(), settings.Settings{ModelWires: wires})
			if ms := supplierReadFixture(t).Models; len(ms) != 1 || ms[0].RouteID != p.ID+"/good" || ms[0].ModelID != "good" {
				t.Fatal("nonchat wire target became a chat model", ms)
			}
			if before.Revision.Current(context.Background()) {
				t.Fatal("nonchat wire edit retained generation")
			}
		})
	}
}

func TestConfiguredSupplierWireTargetOwnsDeclarations(t *testing.T) {
	for _, targetSaved := range []bool{false, true} {
		t.Run(fmt.Sprint(targetSaved), func(t *testing.T) {
			p := supplierFixture(t)
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			supplierWrite(t, settings.Path(), settings.Settings{ModelWires: map[string]string{p.ID + "/model-a": "target"}})
			listed := []catalog.Model{{ID: "model-a", Name: "Local label", Context: 64000, Output: 8000, Efforts: []string{"high"}, DefaultEffort: "high", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}}}
			if targetSaved {
				listed = append(listed, catalog.Model{ID: "target", Context: 16000, Output: 2000, Efforts: []string{"low"}, DefaultEffort: "low", InputModalities: []string{"text"}, OutputModalities: []string{"text"}})
			}
			supplierWriteList(t, p, listed)
			m := supplierReadFixture(t).Models[0]
			if m.RouteID != p.ID+"/model-a" || m.ModelID != "target" || m.Name != "Local label" {
				t.Fatal(m)
			}
			if targetSaved {
				if m.Context != 16000 || m.Output != 2000 || !slices.Equal(m.Efforts, []string{"low"}) || m.DefaultEffort != "low" || !slices.Equal(m.Inputs, []string{"text"}) || m.ContextSource != SupplierDeclared {
					t.Fatal("wire target lost its declarations", m)
				}
			} else if m.Context != 0 || m.Output != 0 || m.Efforts != nil || m.DefaultEffort != "" || m.Inputs != nil || m.Outputs != nil || m.ContextSource != "" || m.EffortSource != "" {
				t.Fatal("unknown wire target borrowed old declarations", m)
			}
			p.Contexts = map[string]int{"model-a": 12000}
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			supplierWrite(t, settings.Path(), settings.Settings{ModelWires: map[string]string{p.ID + "/model-a": "target"}, ModelNames: map[string]string{p.ID + "/model-a": "User label"}, ModelOutputs: map[string]int{p.ID + "/model-a": 1000}})
			m = supplierReadFixture(t).Models[0]
			if m.Context != 12000 || m.ContextSource != SupplierUser || m.Output != 1000 || m.OutputSource != SupplierUser || m.Name != "User label" {
				t.Fatal("local user overrides were lost", m)
			}
		})
	}
}

func TestConfiguredSupplierSavedAPIsDoNotConfineRoute(t *testing.T) {
	for _, tc := range []struct {
		name, allowed, localAPI, targetAPI string
		wired                              bool
	}{
		{"local-list", "chat", "chat", "", false},
		{"wire-list", "chat", "responses", "chat", true},
		{"wire-opposite", "responses", "chat", "responses", true},
		{"wire-unknown", "chat", "chat", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := supplierFixture(t)
			p.Responses = "https://different.invalid/v1"
			if tc.allowed == "responses" {
				p.Chat, p.Responses = p.Responses, p.Chat
			}
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			listed := []catalog.Model{{ID: "model-a", APIs: []string{tc.localAPI}}}
			if tc.targetAPI != "" {
				listed = append(listed, catalog.Model{ID: "target", APIs: []string{tc.targetAPI}})
			}
			supplierWriteList(t, p, listed)
			for _, pin := range []string{"", "chat", "responses", "unknown"} {
				s := settings.Settings{ModelAPIs: map[string]string{p.ID + "/model-a": pin}}
				if tc.wired {
					s.ModelWires = map[string]string{p.ID + "/model-a": "target"}
				}
				supplierWrite(t, settings.Path(), s)
				got, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint)
				if pin == tc.allowed {
					if err != nil || len(got.Models) != 1 {
						t.Fatalf("explicit %s pin rejected: %v %v", pin, got.Models, err)
					}
				} else if !errors.Is(err, ErrConfiguredSupplierNoModels) {
					t.Fatalf("saved APIs confined mixed endpoints with pin %q: %v %v", pin, got.Models, err)
				}
			}
		})
	}
}

func TestConfiguredSupplierWireTargetRevisionUsesRawDeclarations(t *testing.T) {
	p := supplierFixture(t)
	p.Responses = supplierFixtureEndpoint
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	// ModelIDs hides the original model-a list row behind the other alias,
	// but an explicit wire can still target that original upstream ID.
	supplierWrite(t, settings.Path(), settings.Settings{ModelIDs: map[string]string{p.ID + "/model-a": "other"}, ModelWires: map[string]string{p.ID + "/model-a": "model-a"}})
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", APIs: []string{"chat"}}, {ID: "other", APIs: []string{"chat"}}})
	before := supplierReadFixture(t)
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", APIs: []string{"responses"}}, {ID: "other", APIs: []string{"chat"}}})
	if before.Revision.Current(context.Background()) {
		t.Fatal("changed raw declaration retained generation")
	}
	if ms := supplierReadFixture(t).Models; len(ms) != 1 || ms[0].ModelID != "model-a" {
		t.Fatal(ms)
	}
}

func TestConfiguredSupplierOptionalEffortsDoNotGateModels(t *testing.T) {
	p := supplierFixture(t)
	p.Models = []string{"model-a", "model-b"}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", Efforts: []string{"future", "high", "ultra", "persistent", "unsafe\n"}, DefaultEffort: "ultra"}, {ID: "model-b", Efforts: []string{"future"}, DefaultEffort: "future"}})
	ms := supplierReadFixture(t).Models
	if len(ms) != 2 || !slices.Equal(ms[0].Efforts, []string{"high", "ultra", "persistent"}) || ms[0].DefaultEffort != "ultra" || ms[0].EffortSource != SupplierDeclared {
		t.Fatal(ms)
	}
	if ms[1].Efforts != nil || ms[1].DefaultEffort != "" || ms[1].EffortSource != "" {
		t.Fatal("unknown optional metadata was invented", ms[1])
	}
}

func TestConfiguredSupplierDeclaredBoundsAndModalities(t *testing.T) {
	p := supplierFixture(t)
	p.Contexts = map[string]int{"*": 128000}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", Context: 32000, Output: 64000, ImageInput: new(false), InputModalities: []string{"text/plain", "image/png", "image/jpeg"}, OutputModalities: []string{"text/plain"}}})
	m := supplierReadFixture(t).Models[0]
	if m.Context != 32000 || m.Output != 32000 || m.ContextSource != SupplierUser || m.OutputSource != SupplierDeclared || !slices.Equal(m.Inputs, []string{"text", "image"}) || !slices.Equal(m.Outputs, []string{"text"}) {
		t.Fatal(m)
	}
}

func TestConfiguredSupplierExactEndpointAndProtocol(t *testing.T) {
	p := supplierFixture(t)
	p.Chat = "HTTPS://SUPPLIER.INVALID:443/v1/"
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	if len(supplierReadFixture(t).Models) != 1 {
		t.Fatal("normalized endpoint did not match")
	}
	for _, endpoint := range []string{"http://supplier.invalid/v1", "https://supplier.invalid.evil/v1", "https://user:CANARY@supplier.invalid/v1", "https://supplier.invalid/v1?key=CANARY", "https://supplier.invalid/v1#fragment", "https://supplier.invalid/%76%31", "https://supplier.invalid/v2", "https://supplier.invalid/v1/../v1"} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := ConfiguredSupplierModels(context.Background(), endpoint); err == nil {
				t.Fatal("accepted different or ambiguous endpoint")
			}
		})
	}
	p.Responses = "https://different.invalid/v1"
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierNoModels) {
		t.Fatalf("mixed runtime route accepted: %v", err)
	}
	supplierWrite(t, settings.Path(), settings.Settings{ModelAPIs: map[string]string{p.ID + "/model-a": "chat"}})
	s := supplierReadFixture(t)
	if len(s.Models) != 1 {
		t.Fatal(s.Models)
	}
	supplierWrite(t, settings.Path(), settings.Settings{ModelAPIs: map[string]string{p.ID + "/model-a": "responses"}})
	if s.Revision.Current(context.Background()) {
		t.Fatal("changed actual protocol retained generation")
	}
}

func TestConfiguredSupplierFiltersAndExposure(t *testing.T) {
	p := supplierFixture(t)
	var ps []Provider
	for _, kind := range []string{"off", "none", "unlisted", "codex", "account", "plugin", "elsewhere"} {
		q := p
		q.ID = kind
		switch kind {
		case "off":
			q.Off = true
		case "none":
			q.Models, q.NoModels = nil, true
		case "unlisted":
			q.Unlisted = true
		case "account":
			q.Chat = ""
		case "plugin":
			q.Chat = "plugin://fixture"
		case "elsewhere":
			q.Chat = "https://elsewhere.invalid/v1"
		}
		ps = append(ps, q)
	}
	p.Models = nil
	ps = append(ps, p)
	supplierWrite(t, Path(), file{Providers: ps})
	var ms []catalog.Model
	for i := range 30 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("model-%02d", i)})
	}
	supplierWriteList(t, p, ms)
	s := supplierReadFixture(t)
	if len(s.Models) != 24 {
		t.Fatalf("exposure cap: %d", len(s.Models))
	}
	for _, m := range s.Models {
		if !strings.HasPrefix(m.RouteID, p.ID+"/") {
			t.Fatal(m)
		}
	}
	supplierWrite(t, settings.Path(), settings.Settings{DisabledModels: []string{"*model-00"}, HiddenModels: map[string][]string{"codex": {p.ID + "/model-01"}}})
	if got := len(supplierReadFixture(t).Models); got != 22 {
		t.Fatalf("hidden/disabled: %d", got)
	}
	supplierWrite(t, settings.Path(), settings.Settings{PickedModels: map[string][]string{"codex": {p.ID + "/model-02"}}})
	if got := supplierReadFixture(t).Models; len(got) != 1 || got[0].RouteID != p.ID+"/model-02" {
		t.Fatal(got)
	}
}

func TestConfiguredSupplierRevisionUsesSemanticSources(t *testing.T) {
	p := supplierFixture(t)
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", Context: 64000}})
	s := supplierReadFixture(t)
	// A credential rotation or unrelated presentation setting does not
	// rebind the route. Neither secret is retained or hashed by the revision.
	p.Key, p.Headers = "DIFFERENT_SYNTHETIC_SECRET", map[string]string{"Authorization": "DIFFERENT_SYNTHETIC_HEADER"}
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWrite(t, settings.Path(), map[string]any{"theme": "dark"})
	Changed()
	if !s.Revision.Current(context.Background()) {
		t.Fatal("unrelated settings or auth change revoked source")
	}
	path := catalog.LivePath(p.ID)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	supplierWriteList(t, p, []catalog.Model{{ID: "model-a", Context: 32000}})
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("fixture did not preserve stamp")
	}
	if s.Revision.Current(context.Background()) {
		t.Fatal("same-stamp model edit retained generation")
	}
	s = supplierReadFixture(t)
	p.Chat = "https://different.invalid/v1"
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	if s.Revision.Current(context.Background()) {
		t.Fatal("endpoint replacement retained generation")
	}
}

func TestConfiguredSupplierRejectsUnknownSource(t *testing.T) {
	for _, target := range []string{"provider", "settings", "list", "list-null", "list-directory"} {
		t.Run(target, func(t *testing.T) {
			p := supplierFixture(t)
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			supplierWriteList(t, p, []catalog.Model{{ID: "model-a"}})
			s := supplierReadFixture(t)
			path := catalog.LivePath(p.ID)
			data := []byte(`{"malformed"`)
			switch target {
			case "provider":
				path = Path()
			case "settings":
				path = settings.Path()
			case "list-null":
				data = []byte(`{"Base":"https://supplier.invalid/v1","Models":null}`)
			case "list-directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if target != "list-directory" {
				supplierWriteBytes(t, path, data)
			}
			if s.Revision.Current(context.Background()) {
				t.Fatal("unknown source retained generation")
			}
			if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

func TestConfiguredSupplierManualPicksIgnoreStaleListMetadata(t *testing.T) {
	p := supplierFixture(t)
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWrite(t, catalog.LivePath(p.ID), map[string]any{"Base": "https://old.invalid/v1", "Models": []catalog.Model{{ID: "model-a", Context: 128000, Efforts: []string{"high"}}}})
	s := supplierReadFixture(t)
	if len(s.Models) != 1 || s.Models[0].Context != 0 || s.Models[0].Efforts != nil {
		t.Fatal("stale metadata used", s.Models)
	}
	p.Models = nil
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierUnavailable) {
		t.Fatal("stale list used without explicit picks", err)
	}
}

func TestConfiguredSupplierRequiresDirectRoutes(t *testing.T) {
	for _, kind := range []string{"fallback", "plugin-routing", "privacy", "subagent", "lead", "group", "auto", "magpie"} {
		t.Run(kind, func(t *testing.T) {
			p := supplierFixture(t)
			var s settings.Settings
			switch kind {
			case "fallback":
				p.Fallback = []string{"elsewhere/model"}
			case "plugin-routing":
				p.Routing = "plugin/custom"
			case "privacy":
				s.RedactRouteTo = "elsewhere/model"
			case "subagent":
				s.CodexSubagentModel = "elsewhere/model"
			case "lead":
				s.CodexSubagentModel = settings.CodexSubagentLead
			default:
				p.ID = kind
			}
			supplierWrite(t, Path(), file{Providers: []Provider{p}})
			supplierWrite(t, settings.Path(), s)
			if _, err := ConfiguredSupplierModels(context.Background(), supplierFixtureEndpoint); !errors.Is(err, ErrConfiguredSupplierIndirectRoute) {
				t.Fatal("indirect route accepted", err)
			}
		})
	}
	t.Run("harmless-key-order-and-same-model", func(t *testing.T) {
		p := supplierFixture(t)
		p.Routing, p.Sink = Rotate, true
		supplierWrite(t, Path(), file{Providers: []Provider{p}})
		supplierWrite(t, settings.Path(), settings.Settings{RedactRouteTo: p.ID + "/model-a", CodexSubagentModel: p.ID + "/model-a"})
		if len(supplierReadFixture(t).Models) != 1 {
			t.Fatal("same-route settings rejected")
		}
	})
}

func TestConfiguredSupplierRespectsGroupListingOnly(t *testing.T) {
	for _, pattern := range []bool{false, true} {
		t.Run(fmt.Sprint(pattern), func(t *testing.T) {
			p := supplierFixture(t)
			p.Models = []string{"model-a", "model-b"}
			g := Group{ID: "group-fixture", Unlists: true, Members: []string{p.ID + "/model-a:high"}}
			if pattern {
				g.Members, g.Match = nil, []string{p.ID + "/*-a"}
			}
			supplierWrite(t, Path(), file{Providers: []Provider{p}, Groups: []Group{g}})
			s := supplierReadFixture(t)
			if len(s.Models) != 1 || s.Models[0].RouteID != p.ID+"/model-b" {
				t.Fatal("listing-only group bypassed", s.Models)
			}
		})
	}
}

func TestConfiguredSupplierAliasesWiresAndCancellation(t *testing.T) {
	p := supplierFixture(t)
	supplierWrite(t, Path(), file{Providers: []Provider{p}})
	supplierWrite(t, settings.Path(), settings.Settings{ModelIDs: map[string]string{p.ID + "/model-a": "vendor/model-a"}, ModelWires: map[string]string{p.ID + "/*": "namespace/*"}})
	s := supplierReadFixture(t)
	if s.Models[0].ModelID != "namespace/vendor/model-a" || !slices.Equal(s.Models[0].Efforts, nil) {
		t.Fatal(s.Models)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Revision.Current(ctx) || (ConfiguredSupplierRevision{}).Current(context.Background()) {
		t.Fatal("cancelled or empty revision was current")
	}
	if _, err := ConfiguredSupplierModels(ctx, supplierFixtureEndpoint); err == nil {
		t.Fatal("cancelled extraction succeeded")
	}
}
