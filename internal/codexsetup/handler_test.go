package codexsetup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativepreview"
)

type fakeController struct {
	calls int
	state nativepreview.State
	fail  bool
}

func (f *fakeController) State() nativepreview.State                 { return f.state }
func (f *fakeController) AutoSelectExecutable(context.Context) error { f.calls++; return nil }
func (f *fakeController) SelectExecutable(string) error              { f.calls++; return nil }
func (f *fakeController) ConfigureAIHub(string, string) error {
	f.calls++
	if f.fail {
		return errors.New("PUBLIC-SECRET-CANARY")
	}
	return nil
}
func (f *fakeController) Discover(context.Context, string) ([]nativepreview.Model, error) {
	f.calls++
	return []nativepreview.Model{{ID: "public-model", Name: "Public model"}}, nil
}
func (f *fakeController) Login(context.Context) error { f.calls++; return nil }
func (f *fakeController) Send(context.Context, string, string, string) (nativepreview.Reply, error) {
	f.calls++
	return nativepreview.Reply{Text: "public synthetic reply"}, nil
}
func (f *fakeController) Cancel()      { f.calls++ }
func (f *fakeController) Close() error { return nil }

func TestPreviewHandlerRequiresExplicitSameOriginAction(t *testing.T) {
	c := &fakeController{}
	h, err := NewHandler(c, func() (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, body, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("X-Preview-Token", token)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct{ path, body, token, origin string }{
		{"/api/configure", `{"confirm":true}`, "", ""},
		{"/api/configure", `{"confirm":true}`, h.token, "https://outside.invalid"},
		{"/api/configure", `{"confirm":true}`, h.token, "https://wails.localhost"},
		{"/api/configure", `{"confirm":true}`, h.token, "http://wails.localhost/path"},
		{"/api/configure", `{"confirm":false}`, h.token, ""},
		{"/api/send", `{"confirm":true}`, h.token, ""},
		{"/api/login", `{"confirm":true}`, h.token, ""},
		{"/api/discover", `{"confirm":true}`, h.token, ""},
		{"/api/configure", `{"confirm":true,"extra":1}`, h.token, ""},
	} {
		if request(tc.path, tc.body, tc.token, tc.origin).Code < 400 {
			t.Fatal("unapproved action accepted")
		}
	}
	if c.calls != 0 {
		t.Fatal("rejected request reached native controller")
	}
	c.fail = true
	w := request("/api/configure", `{"endpoint":"https://example.invalid","key":"PUBLIC-KEY","confirm":true}`, h.token, "")
	if w.Code != 400 || strings.Contains(w.Body.String(), "PUBLIC-") {
		t.Fatal("source error or secret exposed")
	}
	c.state.CanDiscover = true
	if request("/api/discover", `{"provider":"openai","confirm":true}`, h.token, "").Code != 200 {
		t.Fatal("explicit discovery failed")
	}
}

func TestPreviewPageIsIndependentAndDoesNotStartNative(t *testing.T) {
	c := &fakeController{}
	h, err := NewHandler(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatal("asset unavailable")
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing content restrictions")
		}
		if strings.Contains(w.Body.String(), "__PREVIEW_TOKEN__") {
			t.Fatal("missing request token")
		}
	}
	if c.calls != 0 {
		t.Fatal("opening page started native work")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if w.Code != 403 {
		t.Fatal("unprotected state route")
	}
}
