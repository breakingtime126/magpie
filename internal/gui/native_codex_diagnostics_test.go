package gui

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativesupplier"
)

func TestOriginalConnectDiagnosticsBoundToOriginalInvocation(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	root := filepath.Join(t.TempDir(), "SECRET-CANARY")
	m := nativeconnect.NewManager(context.Background(), root, nil, nativesupplier.NewFactory(nil, root, nil))
	defer m.Close(context.Background())
	owner := nativeConnectionOwner{controller: m}
	request := httptest.NewRequest("POST", "/api/agents/connect/codex", nil)
	request.SetPathValue("action", "connect")
	response := httptest.NewRecorder()
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleNativeCodexConnection(owner, &agent.Agent{ID: "codex"}, w, r)
	})).ServeHTTP(response, request)
	text := output.String()
	if !buildvariant.NativeCodex {
		if strings.Contains(text, "native-connect-preparation-v1") {
			t.Fatal("ordinary build emitted native attempt")
		}
		return
	}
	if response.Code != 400 || !strings.Contains(response.Body.String(), "select or fetch a model in Providers") {
		t.Fatal("original error changed", response.Code, response.Body.String())
	}
	if strings.Count(text, "stage=connect outcome=entered") != 1 || strings.Count(text, "stage=connect outcome=failed") != 1 || !strings.Contains(text, "stage=supplier-metadata outcome=failed") {
		t.Fatal("original invocation not observed", text)
	}
	if strings.Contains(text, "SECRET") || strings.Contains(text, "AIHub") || strings.Contains(text, root) {
		t.Fatal("operation data leaked", text)
	}
	if strings.Contains(response.Body.String(), "native-connect-preparation-v1") {
		t.Fatal("new public response surface")
	}
	output.Reset()
	request = httptest.NewRequest("POST", "/api/agents/disconnect/codex", nil)
	request.SetPathValue("action", "disconnect")
	ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleNativeCodexConnection(owner, &agent.Agent{ID: "codex"}, w, r)
	})).ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(output.String(), "native-connect-preparation-v1") {
		t.Fatal("disconnect created Connect diagnostics")
	}
}
