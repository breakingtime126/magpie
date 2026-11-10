package buildvariant_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// Staged overlays are source inputs for their manifest runners, not packages
// in the product module. Enumerate exactly as the aggregate Windows vet does.
func TestEvidenceExcludedFromRootPackages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	goExe := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goExe += ".exe"
	}
	t.Setenv("GOOS", "windows")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOTELEMETRY", "off")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	cmd := proc.CommandContext(ctx, goExe, "list", "-e", "-find", "-tags", "nogui", "-f", "{{.ImportPath}}", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("enumerate product packages: %v\n%s", err, out)
	}
	packages := strings.Fields(string(out))
	if len(packages) == 0 {
		t.Fatal("product package enumeration returned no packages")
	}
	for _, path := range packages {
		if strings.HasPrefix(path, "github.com/yetone/magpie/evidence/") {
			t.Fatalf("staged evidence leaked into product package enumeration: %s", path)
		}
	}
}
