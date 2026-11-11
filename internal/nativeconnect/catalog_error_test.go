package nativeconnect

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMissingCatalogDoesNotExposeUpstreamDetails(t *testing.T) {
	for _, reason := range []CatalogRequirement{CatalogMetadataUnavailable, CatalogEvidenceExpired, CatalogRouteChanged, CatalogConfigurationMissing, CatalogIndirectRoute} {
		want := MissingCatalog(reason)
		got := safeMissingCatalog(fmt.Errorf("private supplier detail: %w", want))
		if !errors.Is(got, ErrMissingCatalog) || got.Error() != want.Error() || strings.Contains(got.Error(), "private") {
			t.Fatal("catalog failure lost safe detail or leaked upstream text")
		}
	}
	if got := safeMissingCatalog(fmt.Errorf("private supplier detail: %w", ErrMissingCatalog)); got != ErrMissingCatalog {
		t.Fatal("unreviewed supplier error escaped")
	}
	if MissingCatalog(255) != ErrMissingCatalog {
		t.Fatal("unknown requirement accepted")
	}
}
