package nativehost

import (
	"context"
	"net/http"
)

// admittedCapability checks current supplier evidence for each channel request,
// then delegates unchanged to the captured gateway and its existing guards.
// This is not a destination-security boundary: gateway middleware, redirects
// and same-user configuration races remain governed by the gateway's policy.
type admittedCapability struct {
	captured  capability
	admission func(context.Context) error
}

func (a admittedCapability) Valid() bool { return a.captured != nil && a.captured.Valid() }

func (a admittedCapability) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !a.Valid() || checkAdmission(r.Context(), a.admission) != nil || !a.Valid() || r.Context().Err() != nil {
		http.Error(w, "native supplier admission unavailable", http.StatusServiceUnavailable)
		return
	}
	a.captured.ServeHTTP(w, r)
}
