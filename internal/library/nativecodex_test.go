package library

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/buildvariant"
)

func TestNativeCodexTargetsGuard(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	for _, a := range []*agent.Agent{{ID: "codex"}, {ID: "codex@wsl:Ubuntu", WSL: "Ubuntu"}, {ID: "codex#work"}} {
		if targetOf(a) != nil || wslTargetOf(a) != nil {
			t.Fatalf("protected target available: %s", a.ID)
		}
	}
}

func TestNativeCodexProjectMCPGuardPreservesRemovedEntries(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	d := t.TempDir()
	rel := ".codex/config.toml"
	p := filepath.Join(d, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte("[mcp_servers.kept]\ncommand = \"never-run\"\n")
	if err := os.WriteFile(p, before, 0o600); err != nil {
		t.Fatal(err)
	}
	project := &Project{Dir: d, Wrote: map[string][]string{rel: {"kept"}}, Startup: map[string]map[string]float64{rel: {"kept": 42}}}
	l := &Library{}
	l.syncProjectMCP(project, func(what string, err error) { t.Fatalf("%s: %v", what, err) })
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("sync removed or changed protected MCP config")
	}
	if !reflect.DeepEqual(project.Wrote[rel], []string{"kept"}) || project.Startup[rel]["kept"] != 42 {
		t.Fatal("sync lost protected tracking")
	}
}

func TestNativeCodexProjectMCPExplicitReject(t *testing.T) {
	if !buildvariant.NativeCodex {
		t.Skip("native variant only")
	}
	if _, err := ProjectServer("not-opened", "not-read", []string{"codex"}); !errors.Is(err, agent.ErrNativeCodexLegacyWrite) {
		t.Fatalf("explicit project target: %v", err)
	}
}
