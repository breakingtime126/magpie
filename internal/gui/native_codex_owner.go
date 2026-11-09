package gui

import "net/http"

// The separate account/test sheet is retained only as an internal component.
// Product boot must not advertise it as the original Codex Connect workflow.
func nativeCodexForWindow(w Windows) *NativeCodexAPI {
	return nil
}

// Only ownWindow can attach this unexported transport context key. Origin or
// RemoteAddr alone cannot grant ownership to a browser/network request.
func nativeCodexOwnedRequest(r *http.Request) bool {
	scheme, ok := r.Context().Value(windowSchemeKey{}).(string)
	return ok && (scheme == "http" || scheme == "wails")
}

func mountNativeCodex(mux *http.ServeMux, api *NativeCodexAPI) {
	mux.HandleFunc("/api/codex-native/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if api == nil || !nativeCodexOwnedRequest(r) {
			http.NotFound(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}

func addNativeCodexBoot(boot map[string]any, api *NativeCodexAPI, r *http.Request) {
	if api == nil || !nativeCodexOwnedRequest(r) {
		return
	}
	nativeBoot := api.Boot()
	boot["nativeCodexPreview"] = nativeBoot.NativeCodexPreview
	boot["nativeCodexToken"] = nativeBoot.NativeCodexToken
}
