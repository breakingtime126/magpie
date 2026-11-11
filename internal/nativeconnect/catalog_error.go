package nativeconnect

import "errors"

// CatalogRequirement names a fixed, non-secret preparation failure. Supplier
// errors and arbitrary model records must never become original-GUI messages.
type CatalogRequirement uint8

const (
	CatalogMetadataUnavailable CatalogRequirement = iota + 1
	CatalogEvidenceExpired
	CatalogRouteChanged
	CatalogConfigurationMissing
	CatalogIndirectRoute
)

type missingCatalogError struct{ requirement CatalogRequirement }

func (e missingCatalogError) Unwrap() error { return ErrMissingCatalog }
func (e missingCatalogError) Error() string {
	detail := ""
	switch e.requirement {
	case CatalogMetadataUnavailable:
		detail = ": current provider settings or saved models could not be read; check Providers"
	case CatalogEvidenceExpired:
		detail = ": supplier capability evidence is stale or changed"
	case CatalogRouteChanged:
		detail = ": the current supplier route no longer matches its prepared profile"
	case CatalogConfigurationMissing:
		detail = ": enable a direct AIHub provider, then select or fetch a model in Providers"
	case CatalogIndirectRoute:
		detail = ": use a direct AIHub model route without cross-provider fallback or model redirection"
	}
	return ErrMissingCatalog.Error() + detail
}

// MissingCatalog returns only one of the reviewed public explanations.
func MissingCatalog(requirement CatalogRequirement) error {
	if requirement < CatalogMetadataUnavailable || requirement > CatalogIndirectRoute {
		return ErrMissingCatalog
	}
	return missingCatalogError{requirement}
}

func safeMissingCatalog(err error) error {
	var detail missingCatalogError
	if errors.As(err, &detail) {
		return MissingCatalog(detail.requirement)
	}
	return ErrMissingCatalog
}
