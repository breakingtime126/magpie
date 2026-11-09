package codexsetup

import "net/http"

// ActionsHandler exposes only the fixed original-GUI native preview namespace.
// It cannot serve standalone assets or mount actions under caller-supplied paths.
type ActionsHandler struct {
	handler *Handler
}

func NewActionsHandler(controller Controller, choose func() (string, error)) (*ActionsHandler, error) {
	handler, err := NewHandler(controller, choose)
	if err != nil {
		return nil, err
	}
	return &ActionsHandler{handler: handler}, nil
}

// Token is for the owning Wails window's boot JSON, never logs or external pages.
func (h *ActionsHandler) Token() string {
	return h.handler.token
}

func (h *ActionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	action := ""
	switch r.URL.Path {
	case "/api/codex-native/state":
		action = "/api/state"
	case "/api/codex-native/auto":
		action = "/api/auto"
	case "/api/codex-native/choose":
		action = "/api/choose"
	case "/api/codex-native/configure":
		action = "/api/configure"
	case "/api/codex-native/discover":
		action = "/api/discover"
	case "/api/codex-native/login":
		action = "/api/login"
	case "/api/codex-native/send":
		action = "/api/send"
	case "/api/codex-native/cancel":
		action = "/api/cancel"
	default:
		http.NotFound(w, r)
		return
	}
	h.handler.serveAction(w, r, action)
}
