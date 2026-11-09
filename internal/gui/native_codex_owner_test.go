package gui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativepreview"
)

type nativeOwnerFixture struct {
	Windows
	api *NativeCodexAPI
}

func (w nativeOwnerFixture) NativeCodexPreview() *NativeCodexAPI { return w.api }

type nativeAPIFixture struct {
	calls int
}

func (f *nativeAPIFixture) State() nativepreview.State {
	return nativepreview.State{}
}
func (f *nativeAPIFixture) SelectExecutable(string) error {
	f.calls++
	return nil
}
func (f *nativeAPIFixture) AutoSelectExecutable(context.Context) error {
	f.calls++
	return nil
}
func (f *nativeAPIFixture) ConfigureAIHub(string, string) error {
	f.calls++
	return nil
}
func (f *nativeAPIFixture) Discover(context.Context, string) ([]nativepreview.Model, error) {
	f.calls++
	return nil, nil
}
func (f *nativeAPIFixture) Login(context.Context) error {
	f.calls++
	return nil
}
func (f *nativeAPIFixture) Send(context.Context, string, string, string) (nativepreview.Reply, error) {
	f.calls++
	return nativepreview.Reply{}, nil
}
func (f *nativeAPIFixture) Cancel() {
	f.calls++
}
func (f *nativeAPIFixture) Close() error {
	return nil
}

func TestNativeCodexAPIIsOnlyOnItsOwningWindow(t *testing.T) {
	controller := &nativeAPIFixture{}
	api, err := NewNativeCodexAPI(controller, nil)
	if err != nil {
		t.Fatal("API construction failed")
	}
	owner := nativeOwnerFixture{api: api}
	if nativeCodexForWindow(owner) != nil {
		t.Fatal("product exposed the separate account/test sheet")
	}
	productBoot := map[string]any{}
	productRequest := httptest.NewRequest("GET", "/boot.js", nil)
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addNativeCodexBoot(productBoot, nativeCodexForWindow(owner), r)
	})).ServeHTTP(httptest.NewRecorder(), productRequest)
	if len(productBoot) != 0 {
		t.Fatal("product boot advertised the old test button or account token")
	}
	// Explicit API mounting below tests the retained internal component only.
	if nativeCodexForWindow(nil) != nil || nativeCodexForWindow(webHost{}) != nil || nativeCodexForWindow(appWebHost{app: owner}) != nil {
		t.Fatal("nonowner inherited native API")
	}
	mux := http.NewServeMux()
	mountNativeCodex(mux, api)
	request := func(own bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/codex-native/cancel", strings.NewReader(`{}`))
		r.Header.Set("Origin", "http://wails.localhost")
		r.Header.Set("X-Preview-Token", api.Boot().NativeCodexToken)
		r.RemoteAddr = "127.0.0.1:0"
		w := httptest.NewRecorder()
		if own {
			ownWindow(mux).ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
		return w
	}
	if request(false).Code != 404 || controller.calls != 0 {
		t.Fatal("forged loopback/origin/token bypassed owner context")
	}
	if request(true).Code != 200 || controller.calls != 1 {
		t.Fatal("own-window action unavailable")
	}
	boot := map[string]any{"version": "public"}
	r := httptest.NewRequest("GET", "/boot.js", nil)
	addNativeCodexBoot(boot, api, r)
	if len(boot) != 1 {
		t.Fatal("boot token leaked to nonowner")
	}
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addNativeCodexBoot(boot, api, r)
	})).ServeHTTP(httptest.NewRecorder(), r)
	if boot["nativeCodexPreview"] != true || boot["nativeCodexToken"] != api.Boot().NativeCodexToken {
		t.Fatal("owned boot missing capability")
	}
}
