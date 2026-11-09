package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/buildvariant"
)

type nativeCodexUnreadBody struct{ reads int }

func (b *nativeCodexUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("synthetic body must not be read")
}

func (*nativeCodexUnreadBody) Close() error { return nil }

type nativeCodexNoTransport struct{ calls int }

func (n *nativeCodexNoTransport) RoundTrip(*http.Request) (*http.Response, error) {
	n.calls++
	return nil, errors.New("synthetic transport must not be called")
}

func TestNativeGPTLegacyGatewayBuildBoundary(t *testing.T) {
	w := httptest.NewRecorder()
	if refuseLegacyCodexGateway(w) != buildvariant.NativeCodex {
		t.Fatal("incorrect gateway build boundary")
	}
	if !buildvariant.NativeCodex && (w.Body.Len() != 0 || len(w.Header()) != 0) {
		t.Fatal("ordinary build response changed")
	}
}

func TestNativeGPTLegacyGatewayRejectsBeforeReadOrTransport(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("nativecodex build only")
	}
	n := &nativeCodexNoTransport{}
	oldClient, oldTransport := http.DefaultClient, http.DefaultTransport
	http.DefaultClient, http.DefaultTransport = &http.Client{Transport: n}, n
	t.Cleanup(func() { http.DefaultClient, http.DefaultTransport = oldClient, oldTransport })
	// A nil server also proves rejection precedes server state, account/catalog
	// lookup and WebSocket negotiation. All credentials below are inert canaries.
	var s *Server
	for _, tc := range []struct {
		name, method, path string
		socket             bool
		handle             http.HandlerFunc
	}{
		{"responses", "POST", CodexPath + "/responses", false, s.codexBackend},
		{"models", "GET", CodexPath + "/models", false, s.codexBackend},
		{"socket", "GET", CodexPath + "/responses", true, s.codexBackend},
		{"catalog", "GET", CodexCatalogPath, false, s.codexCatalog},
		{"upstream", "POST", "/v1/responses", false, func(w http.ResponseWriter, r *http.Request) {
			s.codexUpstreamOn(w, r, "/responses", []byte(`{"model":"synthetic"}`), nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &nativeCodexUnreadBody{}
			r := httptest.NewRequest(tc.method, tc.path, b)
			r.Header.Set("Authorization", "Bearer PUBLIC-CANARY-NOT-A-TOKEN")
			if tc.socket {
				r.Header.Set("Upgrade", "websocket")
			}
			w := httptest.NewRecorder()
			tc.handle(w, r)
			if w.Code != http.StatusForbidden || b.reads != 0 || n.calls != 0 || strings.Contains(w.Body.String(), "PUBLIC-CANARY") {
				t.Fatal("legacy route crossed the local refusal boundary")
			}
		})
	}
}
