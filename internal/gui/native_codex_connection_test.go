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
