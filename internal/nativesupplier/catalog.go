// Package nativesupplier prepares original Connect from an in-process supplier
// source. It does not treat a model listing, historical test or JSON file as an
// independently validated native catalog, and never obtains credentials.
package nativesupplier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativepreview"
)

const catalogLimit = 1 << 20

// snapshot can only be made by a supplier adapter in this package. Its adapter
// must independently validate the native schema, capabilities and current
// gateway-qualified model routes before constructing it. A hash proves byte
// identity, not those facts. No HTTP, environment or self-signed-file adapter
// exists. Keep supplier keys and auth data out of every field.
type snapshot struct {
	nativeVersion string
	catalog       []byte
	digest        string
	source        string
	observed      time.Time
	expires       time.Time
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
	if s.observed.IsZero() || s.expires.IsZero() || s.observed.After(now) ||
		!s.expires.After(s.observed) || !now.Before(s.expires) {
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

// configuredCatalog is the one production source boundary. The November 2026
// historical tool-call result and directory declaration establish neither a
// current route nor the complete native catalog. Do not fill missing fields
// from illustrative examples or cached native records. An authorized supplier
// adapter must replace this boundary using independent, attributable metadata.
func configuredCatalog(context.Context) (snapshot, error) {
	return snapshot{}, nativeconnect.MissingCatalog(nativeconnect.CatalogMetadataUnavailable)
}
