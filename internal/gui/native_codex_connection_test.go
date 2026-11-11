package gui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativesupplier"
)

type nativeConnectionOwner struct {
	Windows
	controller nativeCodexConnection
}

func (w nativeConnectionOwner) NativeCodexConnection() nativeCodexConnection {
	return w.controller
}

func TestNativeCodexOriginalConnectMissingCatalog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	owner := nativeConnectionOwner{controller: nativeconnect.NewManager(context.Background(), root, nil, nativesupplier.NewFactory(nil, root, nil))}
	a := &agent.Agent{ID: "codex"}
	r := httptest.NewRequest("POST", "/api/agents/connect/codex", nil)
	r.SetPathValue("action", "connect")
	recorder := httptest.NewRecorder()
	handled := false
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = handleNativeCodexConnection(owner, a, w, r)
	})).ServeHTTP(recorder, r)
	if handled != buildvariant.NativeCodex {
		t.Fatal("ordinary build changed Connect dispatch")
	}
	if buildvariant.NativeCodex && !strings.Contains(recorder.Body.String(), "select or fetch a model in Providers") {
		t.Fatal("missing metadata was hidden as successful connection")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("missing metadata created transaction files")
	}
	for _, other := range []*agent.Agent{{ID: "claude"}, {ID: "codex", WSL: "fixture-distro"}, {ID: "codex-copy"}} {
		if handleNativeCodexConnection(owner, other, httptest.NewRecorder(), r) {
			t.Fatal("intercepted another agent or WSL/profile copy")
		}
	}
	if buildvariant.NativeCodex {
		outside := httptest.NewRecorder()
		if !handleNativeCodexConnection(owner, a, outside, r) || outside.Code != 404 {
			t.Fatal("nonowner request reached native Connect")
		}
	}
}

type syntheticNativeConnection struct {
	connects, disconnects, closes int
	closeErr                      error
	statusErr                     error
}

func (s *syntheticNativeConnection) Status(context.Context) (nativeconnect.Status, error) {
	return nativeconnect.Status{SelectionConfigured: s.connects > s.disconnects, JournalPresent: s.connects > s.disconnects}, s.statusErr
}
func (s *syntheticNativeConnection) Connect(context.Context) (nativeconnect.Status, error) {
	s.connects++
	return nativeconnect.Status{SelectionConfigured: true, JournalPresent: true}, nil
}
func (s *syntheticNativeConnection) Disconnect(context.Context) (nativeconnect.Status, error) {
	s.disconnects++
	return nativeconnect.Status{}, nil
}
func (s *syntheticNativeConnection) Close(ctx context.Context) error {
	s.closes++
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded host cleanup")
	}
	return s.closeErr
}

func TestNativeCodexOriginalEndpointUsesHostLifecycle(t *testing.T) {
	c := &syntheticNativeConnection{}
	owner := nativeConnectionOwner{controller: c}
	for _, action := range []string{"connect", "disconnect"} {
		request := httptest.NewRequest("POST", "/api/agents/"+action+"/codex", nil)
		request.SetPathValue("action", action)
		response := httptest.NewRecorder()
		ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleNativeCodexConnection(owner, &agent.Agent{ID: "codex"}, w, r)
		})).ServeHTTP(response, request)
		if buildvariant.NativeCodex && (response.Code != 200 || !strings.Contains(response.Body.String(), `"chatVerified":false`)) {
			t.Fatal("selection confused with chat", response.Code, response.Body.String())
		}
	}
	want := 0
	if buildvariant.NativeCodex {
		want = 1
	}
	if c.connects != want || c.disconnects != want || c.closes != 0 {
		t.Fatal("wrong original endpoint lifecycle", c)
	}
	c.closeErr = nativeconnect.ErrUnavailable
	if err := closeNativeCodexConnection(c); err == nil {
		t.Fatal("host hid failed cleanup")
	}
	c.closeErr = nil
	if err := closeNativeCodexConnection(c); err != nil || c.closes != 2 {
		t.Fatal("host cannot retry cleanup", err)
	}
}

func TestNativeCodexSelectionStatusOwnerAndUnknown(t *testing.T) {
	c := &syntheticNativeConnection{connects: 1}
	owner := nativeConnectionOwner{controller: c}
	r := httptest.NewRequest("GET", "/api/state", nil)
	if got := readNativeCodexSelection(owner, r); got != nil {
		t.Fatal("foreign request read desktop selection", got)
	}
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := readNativeCodexSelection(owner, r)
		if !buildvariant.NativeCodex {
			if got != nil {
				t.Fatal("ordinary build exposed selection")
			}
			return
		}
		if got == nil || !got.SelectionConfigured || got.Unknown || got.ChatVerified || got.BackendHandshakeVerified {
			t.Fatal("selection status dishonest", got)
		}
		c.statusErr = nativeconnect.ErrConflict
		got = readNativeCodexSelection(owner, r)
		if got == nil || !got.Unknown {
			t.Fatal("failed inspection looked disconnected", got)
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
}

type nativeConnectionWebOwner struct{ nativeConnectionOwner }

func (nativeConnectionWebOwner) browserTab() {}

func TestNativeCodexSelectionRoutesAndCache(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native-only original host routes")
	}
	c := &syntheticNativeConnection{connects: 1}
	owner := nativeConnectionOwner{controller: c}
	handler := Handler(owner, nil)
	request := func(h http.Handler, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		return response
	}
	owned := ownWindow(handler)
	for _, path := range []string{"/api/state", "/api/state?last=1"} {
		response := request(owned, "GET", path)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"selectionConfigured":true`) {
			t.Fatal("owning host lost selection on reload", path, response.Code, response.Body.String())
		}
		cached, ok := lastKnown()
		if !ok || cached.NativeCodexSelection != nil {
			t.Fatal("host selection entered shared cache")
		}
		foreign := request(handler, "GET", path)
		if strings.Contains(foreign.Body.String(), `nativeCodexSelection`) {
			t.Fatal("foreign request got host selection")
		}
	}
	boot := request(owned, "GET", "/boot.js")
	if !strings.Contains(boot.Body.String(), `"nativeCodexConnection":true`) {
		t.Fatal("owning boot omitted original native control")
	}
	foreignBoot := request(handler, "GET", "/boot.js")
	if strings.Contains(foreignBoot.Body.String(), `"nativeCodexConnection":true`) {
		t.Fatal("foreign boot enabled native control")
	}
	web := ownWindow(Handler(nativeConnectionWebOwner{owner}, nil))
	webState := request(web, "GET", "/api/state")
	if strings.Contains(webState.Body.String(), `nativeCodexSelection`) {
		t.Fatal("web window got owner selection")
	}
	webRestore := request(web, "POST", "/api/agents/disconnect/codex")
	if webRestore.Code != 404 || c.disconnects != 0 {
		t.Fatal("web window reached restore")
	}
	c.statusErr = nativeconnect.ErrConflict
	uncertain := request(owned, "GET", "/api/state?last=1")
	if !strings.Contains(uncertain.Body.String(), `"unknown":true`) {
		t.Fatal("failed read was reported as known", uncertain.Body.String())
	}
	c.statusErr = nil
	foreign := request(handler, "POST", "/api/agents/disconnect/codex")
	if foreign.Code != 404 || c.disconnects != 0 {
		t.Fatal("foreign transport reached restore", foreign.Code)
	}
	// Empty test home has no installed Codex. Native restoration is dispatched
	// before ordinary discovery and does not need a runtime or supplier.
	response := request(owned, "POST", "/api/agents/disconnect/codex")
	if response.Code != 200 || c.disconnects != 1 || strings.Contains(response.Body.String(), `"connected":`) {
		t.Fatal("original restore required discovery or claimed legacy connection", response.Code, response.Body.String())
	}
}

func TestNativeCodexSelectionNotInKeptDigest(t *testing.T) {
	s := stateJSON{Agents: []agentJSON{}, NativeCodexSelection: &nativeCodexSelectionJSON{Status: nativeconnect.Status{JournalPresent: true, SelectionConfigured: true}}}
	response := httptest.NewRecorder()
	got, ok := writeStateJSON(response, s)
	if !ok || !strings.Contains(response.Body.String(), `"selectionConfigured":true`) {
		t.Fatal("response omitted selection")
	}
	if keptFile(s).State.NativeCodexSelection != nil {
		t.Fatal("selection entered shared file")
	}
	s.NativeCodexSelection = nil
	want, err := encodedSum(keptFile(s))
	if err != nil || got != want {
		t.Fatal("shared digest contains host-specific selection", err)
	}
}
