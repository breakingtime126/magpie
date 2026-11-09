package gui

import (
	"net/http"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/nativeconnect"
)

// Only the original owning desktop window may configure future native starts.
// Keep legacy ConnectHow/Sync guards: never enter their configuration/history
// migration or Rewired side effects from this branch.
func handleNativeCodexConnection(w Windows, a *agent.Agent, rw http.ResponseWriter, r *http.Request) bool {
	action := r.PathValue("action")
	if !buildvariant.NativeCodex || a.ID != "codex" || a.WSL != "" || (action != "connect" && action != "disconnect") {
		return false
	}
	rw.Header().Set("Cache-Control", "no-store")
	owner, ok := w.(interface{ NativeCodexConnection() *nativeconnect.Controller })
	if !ok || isWeb(w) || !nativeCodexOwnedRequest(r) || owner.NativeCodexConnection() == nil {
		http.NotFound(rw, r)
		return true
	}
	var result nativeconnect.Status
	var err error
	if action == "connect" {
		result, err = owner.NativeCodexConnection().Connect(r.Context())
	} else {
		result, err = owner.NativeCodexConnection().Disconnect()
	}
	if err != nil {
		fail(rw, err)
		return true
	}
	// Do not populate Connected or call the old agent.Notice/Rewired hooks.
	// The normal state remains honest about legacy wiring. A separate field
	// explicitly reports only the selection transaction, not backend success.
	s := state()
	if result.SelectionConfigured {
		s.Notice = "Codex launch selection configured. Backend startup and chat have not been verified."
	} else {
		s.Notice = "Previous Codex launch selection restored. Already running windows have not been changed."
	}
	writeJSON(rw, struct {
		stateJSON
		NativeCodexSelection nativeconnect.Status `json:"nativeCodexSelection"`
	}{s, result})
	return true
}
