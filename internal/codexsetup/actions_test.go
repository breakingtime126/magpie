package codexsetup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativepreview"
)

func TestOriginalGUIActionsReusePreviewBoundaries(t *testing.T) {
	c := &fakeController{state: nativepreview.State{CanDiscover: true, CanLogin: true, CanSend: true}}
	h, err := NewActionsHandler(c, func() (string, error) { return "", nil })
	if err != nil || len(h.Token()) != 64 || c.calls != 0 {
		t.Fatal("construction changed state or token missing")
	}
	request := func(method, path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Preview-Token", token)
		r.Header.Set("Origin", origin)
		original := r.URL.Path
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if r.URL.Path != original {
			t.Fatal("request path was rewritten")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("response may be cached")
		}
		return w
	}
	for _, path := range []string{"/", "/app.js", "/style.css", "/api/state", "/api/login", "/api/codex-native/", "/api/codex-native/../login", "/api/codex-native/unknown", "/api/codex-native/state/"} {
		if request("POST", path, `{}`, h.Token(), "http://wails.localhost").Code != 404 {
			t.Fatal("unmapped route or asset exposed")
		}
	}
	for _, tc := range []struct{ method, body, token, origin string }{
		{"GET", `{}`, h.Token(), "http://wails.localhost"},
		{"POST", `{}`, "", "http://wails.localhost"},
		{"POST", `{}`, h.Token(), "https://outside.invalid"},
		{"POST", `{"unexpected":true}`, h.Token(), "http://wails.localhost"},
		{"POST", `{}` + `{}`, h.Token(), "http://wails.localhost"},
		{"POST", `{"text":"` + strings.Repeat("x", 8192) + `"}`, h.Token(), "http://wails.localhost"},
	} {
		if request(tc.method, "/api/codex-native/cancel", tc.body, tc.token, tc.origin).Code < 400 {
			t.Fatal("invalid request accepted")
		}
	}
	for _, action := range []string{"configure", "discover", "login", "send"} {
		if request("POST", "/api/codex-native/"+action, `{}`, h.Token(), "http://wails.localhost").Code < 400 {
			t.Fatal("unconfirmed action accepted")
		}
	}
	if c.calls != 0 {
		t.Fatal("rejected action reached controller")
	}
	for _, action := range []string{"state", "auto", "choose", "configure", "discover", "login", "send", "cancel"} {
		if request("POST", "/api/codex-native/"+action, `{"confirm":true}`, h.Token(), "http://wails.localhost").Code != 200 {
			t.Fatal("mapped action failed")
		}
	}
	if c.calls != 6 {
		t.Fatal("unexpected action dispatch count")
	}
	c.state = nativepreview.State{}
	before := c.calls
	for _, action := range []string{"discover", "login", "send"} {
		if request("POST", "/api/codex-native/"+action, `{"confirm":true}`, h.Token(), "http://wails.localhost").Code < 400 {
			t.Fatal("disabled capability accepted")
		}
	}
	if c.calls != before {
		t.Fatal("disabled action reached controller")
	}
}
