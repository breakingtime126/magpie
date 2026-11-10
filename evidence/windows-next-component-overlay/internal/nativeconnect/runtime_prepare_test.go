package nativeconnect

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativepreview"
)

func TestRuntimeBackendsUseOneReviewedNativeRuntime(t *testing.T) {
	root := t.TempDir()
	runtime := nativepreview.PreparedRuntime{
		Command: filepath.Join(root, "official.exe"), Home: filepath.Join(root, "home"), CodexHome: filepath.Join(root, "native"),
		SHA256: strings.Repeat("a", 64), Version: nativepreview.SharedRuntimeVersion,
	}
	backends, err := runtimeBackends(runtime)
	if err != nil || len(backends) != 2 {
		t.Fatal("reviewed runtime did not produce the explicit backend pair")
	}
	if backends[0].ID != "openai" || backends[0].Provider != "openai" || backends[1].ID != "aihub" || backends[1].Provider != "magpie_shared_aihub" {
		t.Fatal("direct primary or Magpie secondary identity changed")
	}
	for _, backend := range backends {
		if backend.Command != runtime.Command || backend.Home != runtime.Home || backend.CodexHome != runtime.CodexHome || backend.StartupProbeSHA256 != runtime.SHA256 || backend.Models != nil || len(backend.InheritEnv) != 0 {
			t.Fatal("runtime pair copied models/credentials or changed native state identity")
		}
	}
	for _, invalid := range []nativepreview.PreparedRuntime{{}, {Command: runtime.Command, Home: runtime.Home, CodexHome: runtime.CodexHome, SHA256: runtime.SHA256, Version: "unsupported"}, {Command: "relative", Home: runtime.Home, CodexHome: runtime.CodexHome, SHA256: runtime.SHA256, Version: runtime.Version}} {
		if _, err := runtimeBackends(invalid); err == nil {
			t.Fatal("invalid runtime snapshot admitted")
		}
	}
}
