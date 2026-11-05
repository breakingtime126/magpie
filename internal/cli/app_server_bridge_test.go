package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestAppServerBridgeHelpAndInvalidConfigDoNotStartMagpie(t *testing.T) {
	root := helpHome(t)
	before := tree(t, root)
	called := false
	saved := startUp
	startUp = func() { called = true }
	defer func() { startUp = saved }()
	out, err := runOut(t, "app-server-bridge", "--help")
	if err != nil || !strings.Contains(out, "Experimental stdio bridge") {
		t.Fatalf("help: %q %v", out, err)
	}
	if err := run([]string{"app-server-bridge", "--config", "relative.json"}); err == nil {
		t.Fatal("relative config accepted")
	}
	if called {
		t.Fatal("bridge ran normal startup")
	}
	if after := tree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("help or invalid config modified agent files")
	}
}
