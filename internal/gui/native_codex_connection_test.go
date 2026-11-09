package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/nativeconnect"
)

type nativeConnectionOwner struct {
	Windows
	controller *nativeconnect.Controller
}

func (w nativeConnectionOwner) NativeCodexConnection() *nativeconnect.Controller {
	return w.controller
}

func TestNativeCodexOriginalConnectMissingCatalog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	owner := nativeConnectionOwner{controller: nativeconnect.New(root, nil, nil)}
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
	if buildvariant.NativeCodex && !strings.Contains(recorder.Body.String(), "verified AIHub catalog") {
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
