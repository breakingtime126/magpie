package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/settings"
)

// SupplierMetadataSource distinguishes a saved declaration from a user override.
// An empty source means unknown, not a negative capability declaration.
type SupplierMetadataSource string

const (
	SupplierDeclared SupplierMetadataSource = "supplier-list"
	SupplierUser     SupplierMetadataSource = "user"
)

var (
	ErrConfiguredSupplierUnavailable   = errors.New("configured supplier metadata is unavailable; check the existing provider settings")
	ErrConfiguredSupplierNoModels      = errors.New("no configured supplier models; select a model or use Fetch models in the existing provider editor")
	ErrConfiguredSupplierIndirectRoute = errors.New("configured supplier needs a direct route; use the existing provider and routing settings without fallback or model redirection")
)

// ConfiguredSupplierModel is the nonsecret portion of an ordinary provider's
// saved configuration. It contains no native catalog, instructions or inferred
// tool capability. Zero limits and nil lists mean the source did not say.
// RouteID keeps Magpie's local alias; ModelID is the upstream name after the
// user's ModelIDs and ModelWires settings, for native-model classification.
type ConfiguredSupplierModel struct {
	RouteID, ModelID, Name              string
	Context, Output                     int
	Inputs, Outputs, Efforts            []string
	DefaultEffort                       string
	ContextSource, OutputSource         SupplierMetadataSource
	InputSource, OutputModalitiesSource SupplierMetadataSource
	EffortSource                        SupplierMetadataSource
}

type ConfiguredSupplierSnapshot struct {
	Models   []ConfiguredSupplierModel
	Revision ConfiguredSupplierRevision
}

// ConfiguredSupplierRevision is an opaque source generation, not proof that a
// vendor currently accepts requests. It retains no credentials or raw files.
type ConfiguredSupplierRevision struct{ state *configuredSupplierRevision }

type configuredSupplierRevision struct {
	endpoint string
	digest   [sha256.Size]byte
}

// Current rereads the selected ordinary sources, including lists edited by
// another process with unchanged size and timestamps. The caller owns watching
// and revocation; this package starts no goroutine and makes no network request.
func (r ConfiguredSupplierRevision) Current(ctx context.Context) bool {
	if r.state == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	_, digest, _, err := configuredSupplierModels(ctx, r.state.endpoint)
	return err == nil && ctx.Err() == nil && digest == r.state.digest
}

// ConfiguredSupplierModels projects an exact HTTPS endpoint's ordinary saved
// providers. Unlike Catalog, it never enumerates accounts or plugins, reads a
// native model cache, borrows models.dev capabilities, or fetches a model list.
// Provider secrets remain inside the existing provider configuration boundary.
func ConfiguredSupplierModels(ctx context.Context, endpoint string) (ConfiguredSupplierSnapshot, error) {
	endpoint, ok := supplierEndpoint(endpoint)
	if !ok || ctx == nil || ctx.Err() != nil {
		return ConfiguredSupplierSnapshot{}, ErrConfiguredSupplierUnavailable
	}
	models, digest, _, err := configuredSupplierModels(ctx, endpoint)
	if err != nil {
		return ConfiguredSupplierSnapshot{}, err
	}
	return ConfiguredSupplierSnapshot{Models: models, Revision: ConfiguredSupplierRevision{&configuredSupplierRevision{endpoint, digest}}}, nil
}

// The revision hashes only this allowlist, never Provider or settings as a
// whole. In particular, keys, custom headers and arbitrary extra bodies are not
// hashed, logged, retained in the revision, or returned to its consumer.
type supplierRoute struct {
	ID, Chat, Responses, Anthropic, Gemini, BaseAPI string
	Models                                          []ConfiguredSupplierModel
	Listed                                          []catalog.Model
	APIs                                            map[string]string
	Fallback                                        []string
	Routing                                         string
	Sink                                            bool
	ListPresent                                     bool
}

func configuredSupplierModels(ctx context.Context, endpoint string) ([]ConfiguredSupplierModel, [sha256.Size]byte, Stamp, error) {
	var zero [sha256.Size]byte
	before := StampNow()
	var f file
	if present, err := supplierReadJSON(Path(), &f, true); err != nil {
		return nil, zero, before, ErrConfiguredSupplierUnavailable
	} else if !present {
		return nil, zero, before, ErrConfiguredSupplierNoModels
	}
	var s settings.Settings
	if _, err := supplierReadJSON(settings.Path(), &s, true); err != nil {
		return nil, zero, before, ErrConfiguredSupplierUnavailable
	}
	providers, _ := ownOf(f)
	var models []ConfiguredSupplierModel
	var routes []supplierRoute
	seen := map[string]bool{}
	indirect := false
	for _, p := range providers {
		if ctx.Err() != nil {
			return nil, zero, before, ErrConfiguredSupplierUnavailable
		}
		// Account-only rows have no endpoint and ownOf excludes them. Native
		// Codex and special runtimes are never supplier candidates here.
		if p.ID == "codex" || p.Account != nil || p.IsVertex() || p.IsRemoteMagpie() || p.DecideOnly() || p.Off || p.Unlisted || !p.Ready() || p.ServesNone() || p.waitsForPicks() {
			continue
		}
		chat, chatOK := supplierEndpoint(p.Chat)
		responses, responsesOK := supplierEndpoint(p.Responses)
		if (!chatOK || chat != endpoint) && (!responsesOK || responses != endpoint) {
			continue
		}
		if len(p.Fallback) > 0 || PluginRouting(p.Routing) || slices.Contains([]string{"group", "auto", "magpie"}, p.ID) {
			indirect = true
			continue
		}
		if p.ID == "" || p.ID != Slug(p.ID) || seen[p.ID] {
			return nil, zero, before, ErrConfiguredSupplierUnavailable
		}
		seen[p.ID] = true
		listed, present, err := supplierSavedModels(p.ID, endpoint, len(p.Models) > 0)
		if err != nil {
			return nil, zero, before, err
		}
		saved := listed
		// The alias helper is pure with these source-declared models: none
		// carries EffortsBorrowed, so it cannot consult another catalog.
		ids := idsIn(s.ModelIDs, p.ID)
		excluded := map[string]bool{}
		declared := make(map[string]catalog.Model, len(listed))
		for _, m := range listed {
			declared[m.ID] = m
			if !supplierChatModel(m) {
				excluded[m.ID] = true
				if local := ids.local[m.ID]; local != "" {
					excluded[local] = true
				}
			}
		}
		listed = ids.list(listed)
		selected := supplierSelected(p, listed, declared, s, endpoint, excluded)
		// Invalid unused endpoints are represented only by a marker: a URL
		// containing userinfo or query credentials must never be hashed.
		canonical := func(raw string) string {
			if raw == "" {
				return ""
			}
			if normalized, ok := supplierEndpoint(raw); ok {
				return normalized
			}
			return "invalid"
		}
		route := supplierRoute{ID: p.ID, Chat: canonical(p.Chat), Responses: canonical(p.Responses), Anthropic: canonical(p.Anthropic), Gemini: canonical(p.Gemini), BaseAPI: p.BaseAPI, ListPresent: present, Listed: saved, APIs: map[string]string{}, Fallback: p.Fallback, Routing: p.Routing, Sink: p.Sink}
		for _, m := range selected {
			id := p.ID + "/" + m.ID
			if !supplierVisible(s, p, id, m.ID) || supplierGroupUnlists(f.Groups, id, m.ID) {
				continue
			}
			// Privacy routing and Codex's subagent override can replace an
			// already-qualified model. Do not resolve them through accounts.
			if to := strings.TrimSpace(s.RedactRouteTo); to != "" && to != id && !p.SkipsRedaction() {
				indirect = true
				continue
			}
			if to := strings.TrimSpace(s.CodexSubagentModel); to != "" && to != id {
				indirect = true
				continue
			}
			v := supplierModel(p, m, s, UpstreamNameIn(s.Wires(), p.ID, m.ID))
			if !supplierModelValid(v) {
				return nil, zero, before, ErrConfiguredSupplierUnavailable
			}
			route.Models = append(route.Models, v)
			if proto, ok := p.modelAPIAmong(len(p.Speaks()), Protocol(s.ModelAPIs[id])); ok {
				route.APIs[id] = string(proto)
			}
			models = append(models, v)
		}
		routes = append(routes, route)
	}
	if ctx.Err() != nil || StampNow() != before {
		return nil, zero, before, ErrConfiguredSupplierUnavailable
	}
	if len(models) == 0 {
		if indirect {
			return nil, zero, before, ErrConfiguredSupplierIndirectRoute
		}
		return nil, zero, before, ErrConfiguredSupplierNoModels
	}
	b, err := json.Marshal(routes)
	if err != nil {
		return nil, zero, before, ErrConfiguredSupplierUnavailable
	}
	return models, sha256.Sum256(b), before, nil
}

// A new immutable source must not quietly fall back to yesterday's .bak or a
// memoized list when today's file is malformed. Missing settings are known
// defaults; a missing list is usable only through explicitly saved model picks.
func supplierReadJSON(path string, into any, optional bool) (bool, error) {
	info, err := os.Lstat(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return false, ErrConfiguredSupplierUnavailable
	}
	f, err := os.Open(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ErrConfiguredSupplierUnavailable
	}
	defer f.Close()
	const limit = 8 << 20
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	b = bytes.TrimSpace(bytes.TrimPrefix(b, utf8BOM))
	if err != nil || len(b) == 0 || len(b) > limit || b[0] != '{' || json.Unmarshal(b, into) != nil {
		return false, ErrConfiguredSupplierUnavailable
	}
	return true, nil
}

func supplierEndpoint(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" || strings.Contains(path, "//") || strings.Contains(path, "/./") || strings.Contains(path, "/../") || strings.HasSuffix(path, "/.") || strings.HasSuffix(path, "/..") {
		return "", false
	}
	return "https://" + host + path, true
}

func supplierSavedModels(id, endpoint string, manual bool) ([]catalog.Model, bool, error) {
	// This deliberately omits Codex, Keys, price records and arbitrary source
	// objects. A supplier's native-shaped record cannot enter the DTO by copy.
	var list struct {
		Base   string
		Models []struct {
			ID, Name, Released, DefaultEffort                string
			APIs, InputModalities, OutputModalities, Efforts []string
			Context, MaxContext, Output                      int
			ImageInput                                       *bool
			Images, Draws, Films, Decides                    bool
		}
	}
	present, err := supplierReadJSON(catalog.LivePath(id), &list, true)
	if err != nil || !present {
		return nil, present, err
	}
	base, ok := supplierEndpoint(list.Base)
	if list.Models == nil {
		return nil, true, ErrConfiguredSupplierUnavailable
	}
	var out []catalog.Model
	seen := map[string]bool{}
	for _, m := range list.Models {
		if !supplierString(m.ID, 1024) || m.ID == "" || seen[m.ID] || m.Context < 0 || m.MaxContext < 0 || m.Output < 0 {
			return nil, true, ErrConfiguredSupplierUnavailable
		}
		seen[m.ID] = true
		inputs, outputs := catalog.Kinds(m.InputModalities), catalog.Kinds(m.OutputModalities)
		if m.ImageInput != nil && !*m.ImageInput && slices.Contains(inputs, "image") {
			m.ImageInput = new(true) // the same old-list repair as catalog.Live
		}
		out = append(out, catalog.Model{ID: m.ID, Name: m.Name, Released: m.Released, DefaultEffort: m.DefaultEffort, APIs: m.APIs, InputModalities: inputs, OutputModalities: outputs, Efforts: m.Efforts, Context: m.Context, MaxContext: m.MaxContext, Output: m.Output, ImageInput: m.ImageInput, Images: m.Images, Draws: m.Draws, Films: m.Films, Decides: m.Decides})
	}
	if !ok || base != endpoint {
		if manual {
			return nil, true, nil
		}
		return nil, true, ErrConfiguredSupplierUnavailable
	}
	return out, true, nil
}

// These are ExposedOf's ordinary-provider selection rules, without its name,
// effort and protocol lookups outside the captured supplier list.
func supplierSelected(p Provider, listed []catalog.Model, declared map[string]catalog.Model, s settings.Settings, endpoint string, excluded map[string]bool) []catalog.Model {
	listed = slices.DeleteFunc(slices.Clone(listed), func(m catalog.Model) bool { return !supplierChatModel(m) })
	// A wire may change the model behind a local name. Only its effective
	// upstream target can declare capabilities. Preserve the local label,
	// and leave an unlisted target's declarations unknown.
	wires := s.Wires()
	effective := func(m catalog.Model) catalog.Model {
		target := declared[UpstreamNameIn(wires, p.ID, m.ID)]
		target.ID, target.Name = m.ID, m.Name
		return target
	}
	var out []catalog.Model
	if len(p.Models) > 0 {
		for _, id := range p.PicksOf(listed) {
			if excluded[id] {
				continue
			}
			m := catalog.Model{ID: id, Name: id}
			if i := slices.IndexFunc(listed, func(m catalog.Model) bool { return m.ID == id }); i >= 0 {
				m = listed[i]
			}
			m = effective(m)
			if supplierChatModel(m) && supplierServes(p, m.ID, s, endpoint) {
				out = append(out, m)
			}
		}
		return out
	}
	for _, m := range listed {
		m = effective(m)
		if supplierChatModel(m) && supplierServes(p, m.ID, s, endpoint) {
			out = append(out, m)
		}
	}
	if p.BestInFamily {
		out = catalog.BestInFamily(out)
	}
	return out[:min(len(out), manyModels)]
}

func supplierChatModel(m catalog.Model) bool {
	return !m.Draws && !m.Films && !m.Decides && (len(m.OutputModalities) == 0 || slices.Contains(m.OutputModalities, "text"))
}

// The gateway can retry every configured protocol when a saved list's APIs
// have all failed. Only a valid explicit local model pin narrows that fallback;
// neither the local nor the wire target's saved APIs confine the route.
func supplierServes(p Provider, model string, s settings.Settings, endpoint string) bool {
	speaks := p.Speaks()
	if proto, ok := p.modelAPIAmong(len(speaks), Protocol(s.ModelAPIs[p.ID+"/"+model])); ok {
		speaks = []Protocol{proto}
	}
	any := false
	for _, proto := range speaks {
		base, ok := supplierEndpoint(p.Base(proto))
		if !ok || base != endpoint {
			return false
		}
		any = true
	}
	return any
}

func supplierVisible(s settings.Settings, p Provider, id, model string) bool {
	if _, off := matchDisabled(s.DisabledModels, id, model, model[strings.LastIndex(model, "/")+1:]); off {
		return false
	}
	if names, narrowed := s.Visible["codex"]; narrowed && !Shows(names, Entry{Provider: p, Family: p.Family}) {
		return false
	}
	if picks, only := s.PickedModels["codex"]; only {
		return slices.Contains(picks, id)
	}
	return !slices.Contains(s.HiddenModels["codex"], id)
}

// A group's listing-only choice hides its direct members; the group itself is
// never offered here. Pattern matching is pure and does not enumerate groups
// or accounts to discover additional members.
func supplierGroupUnlists(groups []Group, id, model string) bool {
	for _, g := range groups {
		if !g.Unlists || g.Hidden || g.Disabled {
			continue
		}
		for _, member := range g.Members {
			if member == id || member == model {
				return true
			}
			if suffix, ok := strings.CutPrefix(member, id+":"); ok && memberLevelsSpelled(suffix) != "" {
				return true
			}
		}
		for _, pattern := range g.Match {
			if PatternMatches(pattern, id) {
				return true
			}
		}
	}
	return false
}

func supplierModel(p Provider, m catalog.Model, s settings.Settings, own string) ConfiguredSupplierModel {
	v := ConfiguredSupplierModel{RouteID: p.ID + "/" + m.ID, ModelID: own, Name: m.Name, Context: catalog.Window(m.Context), Output: m.Output, Inputs: slices.Clone(m.InputModalities), Outputs: slices.Clone(m.OutputModalities), Efforts: supplierEfforts(m.Efforts), DefaultEffort: m.DefaultEffort}
	if v.Name == "" {
		v.Name = m.ID
	}
	if name, ok := modelNameIn(s.ModelNames, p.ID, m.ID); ok {
		v.Name = name
	}
	if v.Context > 0 {
		v.ContextSource = SupplierDeclared
	}
	if n := p.Contexts[m.ID]; n > 0 {
		v.Context, v.ContextSource = n, SupplierUser
	} else if n := p.Contexts["*"]; n > 0 {
		if most := max(catalog.Window(m.Context), m.MaxContext); most > 0 {
			n = min(n, most)
		}
		v.Context, v.ContextSource = n, SupplierUser
	}
	if v.Output > 0 {
		v.OutputSource = SupplierDeclared
	}
	if n := outputOf(s, p.ID, m.ID); n > 0 {
		v.Output, v.OutputSource = n, SupplierUser
	}
	v.Output = OutputWithin(v.Context, v.Output)
	if v.Inputs != nil {
		v.InputSource = SupplierDeclared
	}
	if v.Outputs != nil {
		v.OutputModalitiesSource = SupplierDeclared
	}
	image := m.ImageInput
	if image == nil && m.Images {
		image = new(true)
	}
	if override, ok := s.ModelImages[v.RouteID]; ok {
		image, v.InputSource = &override, SupplierUser
	} else if image != nil {
		v.InputSource = SupplierDeclared
	}
	if image != nil {
		if v.Inputs == nil {
			v.Inputs = []string{"text"}
		}
		v.Inputs = slices.DeleteFunc(v.Inputs, func(kind string) bool { return kind == "image" })
		if *image {
			v.Inputs = append(v.Inputs, "image")
		}
	}
	if v.Efforts != nil {
		v.EffortSource = SupplierDeclared
	}
	if kept := supplierEfforts(s.ModelEfforts[v.RouteID]); len(kept) > 0 {
		if len(v.Efforts) == 0 {
			v.Efforts = kept
		} else {
			v.Efforts = effortsKept(v.Efforts, kept)
		}
		v.EffortSource = SupplierUser
	}
	if s.ModelReasonless[v.RouteID] {
		v.Efforts, v.DefaultEffort, v.EffortSource = []string{}, "", SupplierUser
	}
	if !slices.Contains(v.Efforts, v.DefaultEffort) {
		v.DefaultEffort = ""
	}
	return v
}

// Optional effort vocabulary can grow independently of the provider editor.
// Preserve supported declarations, including the pinned native vocabulary,
// and leave entirely unknown metadata unknown rather than rejecting a model.
func supplierEfforts(declared []string) []string {
	if declared == nil {
		return nil
	}
	out := []string{}
	for _, effort := range declared {
		if (slices.Contains(Levels, effort) || effort == "ultra" || effort == "persistent") && !slices.Contains(out, effort) {
			out = append(out, effort)
		}
	}
	if len(declared) > 0 && len(out) == 0 {
		return nil
	}
	return out
}

func supplierString(s string, limit int) bool {
	return len(s) <= limit && !strings.ContainsFunc(s, unicode.IsControl)
}

func supplierModelValid(m ConfiguredSupplierModel) bool {
	if !supplierString(m.RouteID, 2048) || !supplierString(m.ModelID, 1024) || m.ModelID == "" || !supplierString(m.Name, 4096) || m.Context < 0 || m.Output < 0 {
		return false
	}
	for _, modes := range [][]string{m.Inputs, m.Outputs} {
		for _, mode := range modes {
			if !slices.Contains([]string{"text", "image", "audio", "video", "pdf"}, mode) {
				return false
			}
		}
	}
	return true
}
