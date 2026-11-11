package gui

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativeprepdiag"
)

// nativeCodexConnection keeps the original endpoint independent of preparation.
type nativeCodexConnection interface {
	Connect(context.Context) (nativeconnect.Status, error)
	Disconnect(context.Context) (nativeconnect.Status, error)
	Status(context.Context) (nativeconnect.Status, error)
	Close(context.Context) error
}

func closeNativeCodexConnection(connection nativeCodexConnection) error {
	if connection == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return connection.Close(ctx)
}

// Selection status belongs to this desktop host, never the shared state cache.
// Unknown reads keep recovery reachable; they must not imply disconnected.
type nativeCodexSelectionJSON struct {
	nativeconnect.Status
	Unknown bool `json:"unknown"`
}

func ownedNativeCodexConnection(w Windows, r *http.Request) nativeCodexConnection {
	if !buildvariant.NativeCodex || isWeb(w) || !nativeCodexOwnedRequest(r) {
		return nil
	}
	owner, ok := w.(interface{ NativeCodexConnection() nativeCodexConnection })
	if !ok {
		return nil
	}
	return owner.NativeCodexConnection()
}

func readNativeCodexSelection(w Windows, r *http.Request) *nativeCodexSelectionJSON {
	connection := ownedNativeCodexConnection(w, r)
	if connection == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	status, err := connection.Status(ctx)
	return &nativeCodexSelectionJSON{Status: status, Unknown: err != nil}
}

// Only the original owning desktop window may configure future native starts.
// Keep legacy ConnectHow/Sync guards: never enter their configuration/history
// migration or Rewired side effects from this branch.
func handleNativeCodexConnection(w Windows, a *agent.Agent, rw http.ResponseWriter, r *http.Request) bool {
	action := r.PathValue("action")
	if !buildvariant.NativeCodex || a.ID != "codex" || a.WSL != "" || (action != "connect" && action != "disconnect") {
		return false
	}
	rw.Header().Set("Cache-Control", "no-store")
	connection := ownedNativeCodexConnection(w, r)
	if connection == nil {
		http.NotFound(rw, r)
		return true
	}
	var result nativeconnect.Status
	var err error
	if action == "connect" {
		ctx, finish := nativeprepdiag.Begin(r.Context(), func(line string) { log.Print(line) })
		func() {
			success := false
			defer func() { finish(success) }()
			result, err = connection.Connect(ctx)
			success = err == nil
		}()
	} else {
		result, err = connection.Disconnect(r.Context())
	}
	if err != nil {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusBadRequest)
		writeJSON(rw, struct {
			Error                string                    `json:"error"`
			NativeCodexSelection *nativeCodexSelectionJSON `json:"nativeCodexSelection"`
		}{err.Error(), readNativeCodexSelection(w, r)})
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
	s.NativeCodexSelection = &nativeCodexSelectionJSON{Status: result}
	writeJSON(rw, s)
	return true
}
