package gateway

import (
	"net/http"

	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/provider"
)

// The native Codex build must not relay a caller's own ChatGPT authorization,
// including routes that do not use provider.Provider.Sign. AIHub uses the normal
// /v1 provider route; ordinary builds retain these legacy entry points.
func refuseLegacyCodexGateway(w http.ResponseWriter) bool {
	if !buildvariant.NativeCodex {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, provider.Responses, http.StatusForbidden, "this build leaves the built-in Codex route to the official Codex app")
	return true
}
