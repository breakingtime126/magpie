// Package nativesupplier prepares original Connect from the user's current
// ordinary Magpie supplier configuration. Native metadata is independently
// authored from the public schema, not copied from native records or caches.
package nativesupplier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/nativecatalog"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativepreview"
	"github.com/yetone/magpie/internal/provider"
)

const catalogLimit = 1 << 20

// snapshot contains an authored native client profile and a current configured
// route revision. A hash proves byte identity, not upstream model capability.
// Keep supplier keys and auth data out of every field. expires may be zero for
// explicit local configuration, whose validity is determined by route revision.
type snapshot struct {
	nativeVersion string
	catalog       []byte
	digest        string
	source        string
	observed      time.Time
	expires       time.Time
	monitor       bool
	// The source cancels validity when its captured supplier route or capability
	// generation changes. This binds validity beyond the selection request.
	validity context.Context
	// current revalidates the exact supplier-route generation captured by the
	// adapter. A changed route requires new preparation, never silent rebinding.
	current func(context.Context) bool
}

func (s snapshot) freeze(ctx context.Context, now time.Time) (snapshot, error) {
	if s.nativeVersion != nativepreview.SharedRuntimeVersion || s.source == "" ||
		len(s.source) > 256 || len(s.catalog) == 0 || len(s.catalog) > catalogLimit ||
		!json.Valid(s.catalog) || s.current == nil || s.validity == nil {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogMetadataUnavailable)
	}
	s.catalog = bytes.Clone(s.catalog)
	if s.observed.IsZero() || s.observed.After(now) ||
		!s.expires.IsZero() && (!s.expires.After(s.observed) || !now.Before(s.expires)) {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogEvidenceExpired)
	}
	digest := sha256.Sum256(s.catalog)
	if hex.EncodeToString(digest[:]) != s.digest {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogEvidenceExpired)
	}
	if ctx.Err() != nil {
		return snapshot{}, nativeconnect.ErrUnavailable
	}
	if s.validity.Err() != nil || !s.current(ctx) {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogRouteChanged)
	}
	if ctx.Err() != nil || s.validity.Err() != nil {
		return snapshot{}, nativeconnect.ErrUnavailable
	}
	return s, nil
}

// This is only a selector for the user's already-configured supplier. It never
// writes a provider, supplies authentication or contacts that service.
const configuredAIHubEndpoint = "https://aihub.friendtimes.net/v1"

func configuredCatalog(ctx context.Context) (snapshot, error) {
	configured, err := provider.ConfiguredSupplierModels(ctx, configuredAIHubEndpoint)
	if err != nil {
		reason := nativeconnect.CatalogMetadataUnavailable
		if errors.Is(err, provider.ErrConfiguredSupplierNoModels) {
			reason = nativeconnect.CatalogConfigurationMissing
		}
		if errors.Is(err, provider.ErrConfiguredSupplierIndirectRoute) {
			reason = nativeconnect.CatalogIndirectRoute
		}
		return snapshot{}, nativeconnect.MissingCatalog(reason)
	}
	models := make([]nativecatalog.Model, 0, len(configured.Models))
	for _, model := range configured.Models {
		// Known OpenAI GPT/Codex/O-series names remain native-primary choices,
		// including a user's alias of such a vendor model. Do not route them
		// through the selected company endpoint just to fill the picker.
		if nativePrimaryModel(model.ModelID) {
			continue
		}
		models = append(models, nativecatalog.Model{ID: model.RouteID, Name: model.Name, Context: model.Context, Efforts: model.Efforts, DefaultEffort: model.DefaultEffort})
	}
	if len(models) == 0 {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogConfigurationMissing)
	}
	data, err := nativecatalog.Render(models)
	if err != nil || !configured.Revision.Current(ctx) {
		return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogRouteChanged)
	}
	digest := sha256.Sum256(data)
	return snapshot{nativeVersion: nativepreview.SharedRuntimeVersion, catalog: data,
		digest: hex.EncodeToString(digest[:]), source: "magpie-configured-aihub-v1",
		observed: time.Now(), validity: context.Background(), monitor: true,
		current: configured.Revision.Current}, nil
}

func nativePrimaryModel(id string) bool {
	id = strings.ToLower(id[strings.LastIndex(id, "/")+1:])
	for _, family := range []string{"gpt", "chatgpt", "codex", "o1", "o3", "o4"} {
		if id == family || strings.HasPrefix(id, family+"-") {
			return true
		}
	}
	return false
}
