package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/proc"
)

func TestPublicMCPCatalogGrammarAndExactMatches(t *testing.T) {
	prefix := "plugins.a@b.mcp_servers."
	for _, key := range []string{
		prefix + "s.enabled", prefix + "A1_b-c.enabled",
		prefix + strings.Repeat("s", 64) + ".enabled",
		"plugins." + strings.Repeat("a", 64) + "@b.mcp_servers.s.enabled",
		"plugins.a@" + strings.Repeat("b", 64) + ".mcp_servers.s.enabled",
		"plugins." + strings.Repeat("a", 64) + "@b.mcp_servers." + strings.Repeat("s", 33) + ".enabled",
	} {
		if !publicPluginKey(key) {
			t.Fatal("bounded nested MCP catalog key rejected")
		}
		if _, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key})); err != nil {
			t.Fatal("valid nested MCP catalog rejected")
		}
	}
	for _, key := range []string{
		prefix + ".enabled", prefix + "_s.enabled", prefix + "-s.enabled",
		prefix + "s.enabled.extra", prefix + "s.auto_provision",
		prefix + "s.tools.t.enabled", prefix + "s.Enabled",
		prefix + "s.s.enabled", prefix + "\"s\".enabled",
		prefix + "s@x.enabled", prefix + "s/x.enabled", prefix + "s\\x.enabled",
		prefix + "s x.enabled", prefix + "s\t.enabled", prefix + "é.enabled",
		prefix + "s.enabled\x00", prefix + "s.enabled\u00a0",
		prefix + strings.Repeat("s", 65) + ".enabled",
		"plugins." + strings.Repeat("a", 64) + "@b.mcp_servers." + strings.Repeat("s", 34) + ".enabled",
		"plugins.a@@b.mcp_servers.s.enabled", "plugins.a@b.Mcp_servers.s.enabled",
		"plugins.a@b.apps.s.enabled", "plugins.a@b.mcp_servers.enabled",
		"plugins.\"a@b\".mcp_servers.s.enabled",
		"plugins.a@b.mcp_servers.s.enabled ", " plugins.a@b.mcp_servers.s.enabled",
	} {
		if publicPluginKey(key) {
			t.Fatal("neighboring MCP catalog key accepted")
		}
		if _, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key})); err == nil {
			t.Fatal("invalid MCP catalog did not fail closed")
		}
	}
	key := prefix + "Server.enabled"
	catalog, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key}))
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		prefix + "server.enabled=true", prefix + "Other.enabled=true",
		key, key + ".extra=true", prefix + "\"Server\".enabled=true",
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.publicPlugins = catalog
		o.argv([]string{"-c", arg})
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Class != "unknown" || es[1].PublicPluginRef != 0 {
			t.Fatal("non-exact server name matched catalog")
		}
	}
}

const publicMCPExecutableArg = " \tplugins.SYNTHETIC_PRIVATE_PLUGIN@PRIVATE_MARKET.mcp_servers.PRIVATE_SERVER.enabled=true"
const publicMCPExecutableWire = "{\"id\":\"PRIVATE_ID\",\"method\":\"account/read\",\"params\":{\"private\":\"PRIVATE_VALUE\"}}\r\n\x00\xff"

// The native test binary checks the complete argv and input, then echoes every
// input byte and exits nonzero. It uses no shell, network or live account.
func TestPublicMCPExecutableChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_PUBLIC_MCP_CHILD") != "1" {
		return
	}
	want := []string{"-test.run=^TestPublicMCPExecutableChildFixture$", "--", "-c", publicMCPExecutableArg}
	if !reflect.DeepEqual(os.Args[1:], want) || os.Getenv(activeEnv) != "1" {
		os.Exit(41)
	}
	if _, present := os.LookupEnv(publicPluginCatalogEnv); present {
		os.Exit(42)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil || string(input) != publicMCPExecutableWire {
		os.Exit(43)
	}
	if _, err := os.Stdout.Write(input); err != nil {
		os.Exit(44)
	}
	os.Stderr.WriteString("PRIVATE_STDERR\x00\xff\r\n")
	os.Exit(17)
}

func TestPublicMCPExecutablePreservesArgvWireAndExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("actual observer entry point is Windows-only")
	}
	work := t.TempDir()
	observerExe := filepath.Join(work, "public-mcp-observer.exe")
	build := proc.Command("go", "build", "-o", observerExe, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	var buildLog bytes.Buffer
	build.Stdout, build.Stderr = &buildLog, &buildLog
	if err := runOwnedTestCommand(t, build); err != nil {
		t.Fatalf("offline observer build failed: %v\n%s", err, buildLog.String())
	}
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv(activeEnv, "")
	t.Setenv("MAGPIE_OBSERVER_PUBLIC_MCP_CHILD", "1")
	// The dictionary is an immutable exact-match snapshot and is stripped before
	// the fixture child is launched.
	t.Setenv(publicPluginCatalogEnv, publicCatalogTestValue(t, []string{"plugins.SYNTHETIC_PRIVATE_PLUGIN@PRIVATE_MARKET.mcp_servers.PRIVATE_SERVER.enabled"}))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", self)
	prefix := filepath.Join(work, "trace")
	t.Setenv("MAGPIE_CI_TRACE_FILE", prefix)
	cmd := proc.Command(observerExe, "-test.run=^TestPublicMCPExecutableChildFixture$", "--", "-c", publicMCPExecutableArg)
	cmd.Stdin = strings.NewReader(publicMCPExecutableWire)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runOwnedTestCommand(t, cmd)
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17 ||
		stdout.String() != publicMCPExecutableWire || stderr.String() != "PRIVATE_STDERR\x00\xff\r\n" {
		t.Fatal("nested MCP catalog observation changed child argv, stdin, stdout, stderr or exit")
	}
	files, err := filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 1 {
		t.Fatal("missing nested MCP catalog executable trace")
	}
	log, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(log) > maxTrace || bytes.Contains(log, []byte("PRIVATE")) {
		t.Fatal("nested MCP catalog executable trace exposed private names or values")
	}
	configs := 0
	var exited, outputEOF, errorEOF bool
	for _, e := range events(t, log) {
		if e.Kind == "argv_config" {
			configs++
			if e.ConfigPluginShape != "" || e.Class != "" ||
				e.ConfigKey != "" || e.ConfigValue != "bool_true" || e.PublicPluginRef != 1 ||
				e.ConfigLiteral != "" || e.ConfigLexicalRoot != "" {
				t.Fatal("compiled observer missing exact nested MCP public catalog ref")
			}
		}
		exited = exited || e.Kind == "child_exit" && e.Exit != nil && *e.Exit == 17
		outputEOF = outputEOF || e.Kind == "stream_end" && e.Direction == "from_bridge" && e.Class == "eof"
		errorEOF = errorEOF || e.Kind == "stream_end" && e.Direction == "stderr" && e.Class == "eof"
	}
	if configs != 1 || !exited || !outputEOF || !errorEOF {
		t.Fatal("nested MCP catalog executable trace missing config, exit or output EOF")
	}
}
