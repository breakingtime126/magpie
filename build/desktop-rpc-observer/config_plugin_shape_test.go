package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/proc"
)

func TestPluginPathShapesAndPrivateRedaction(t *testing.T) {
	const prefix = "plugins.SYNTHETIC_PRIVATE_PLUGIN@PRIVATE_MARKET."
	for _, row := range []struct {
		suffix string
		shape  string
	}{
		{"enabled", "direct_enabled"},
		{"auto_provision", "direct_auto_provision"},
		{"mcp_servers.PRIVATE_SERVER.enabled", "mcp_server_enabled"},
		{"apps.PRIVATE_APP.enabled", "app_enabled"},
		{"apps.PRIVATE_APP.destructive_enabled", "app_destructive_enabled"},
		{"apps.PRIVATE_APP.open_world_enabled", "app_open_world_enabled"},
		{"apps.PRIVATE_APP.default_tools_enabled", "app_default_tools_enabled"},
		{"apps.PRIVATE_APP.tools.PRIVATE_TOOL.enabled", "app_tool_enabled"},
	} {
		for _, rhs := range []string{"true", "false", "", " true ", "'true'", "true=PRIVATE_VALUE", "PRIVATE_VALUE"} {
			key := " \t\r\n\v\f" + prefix + row.suffix + "\f\v\n\r\t "
			args := []string{"--config=" + key + "=" + rhs}
			before := append([]string(nil), args...)
			var log bytes.Buffer
			o := newObserver(&log)
			o.argv(args)
			es := events(t, log.Bytes())
			if len(es) != 2 || es[1].ConfigPluginShape != row.shape {
				t.Fatal("unknown plugin path missing its fixed shape")
			}
			got := es[1]
			got.ConfigPluginShape, got.Sequence, got.PID = "", 0, 0
			if !reflect.DeepEqual(got, observedConfigArgument(key+"="+rhs)) {
				t.Fatal("plugin shape changed existing redacted metadata")
			}
			if !reflect.DeepEqual(args, before) {
				t.Fatal("plugin shape observation changed argv")
			}
			for _, secret := range []string{"SYNTHETIC_PRIVATE_PLUGIN", "PRIVATE_MARKET", "PRIVATE_SERVER", "PRIVATE_APP", "PRIVATE_TOOL", "PRIVATE_VALUE"} {
				if strings.Contains(log.String(), secret) {
					t.Fatal("plugin shape metadata exposed a private sentinel")
				}
			}
		}
	}
}

func TestPluginPathShapeRejectsNeighbors(t *testing.T) {
	for _, key := range []string{
		"", "plugins", "plugins.a@b", "plugins.a.enabled",
		"Plugins.a@b.enabled", "plugins.a@b.Enabled", "plugins.a@b.auto_Provision",
		"plugins.a@b.Mcp_servers.s.enabled", "plugins.a@b.Apps.a.enabled",
		"plugins.a@b.apps.a.Tools.t.enabled", "plugins.a@b.apps.a.tools.t.Enabled",
		"plugins.a@b.apps.a.default_Tools_enabled",
		"plugins.a@b.mcp_servers.s.tools.t.enabled",
		"plugins.a@b.mcp_servers.s.destructive_enabled",
		"plugins.a@b.apps.a.tools.t.destructive_enabled",
		"plugins.a@b.apps.a.default_tools.enabled",
		"plugins.a@b.apps.a.tools.enabled",
		"plugins.a@b.enabled.extra", "plugins.a@b.apps.a.enabled.extra",
		"plugins.a@b.apps.a.tools.t.enabled.extra",
		"plugins.a@@b.enabled", "plugins.@b.enabled", "plugins.a@.enabled",
		"plugins.a@b..enabled", "plugins.a@b.apps..enabled",
		"plugins.a@b.apps.a.tools..enabled", "plugins.a@b.mcp_servers..enabled",
		"plugins._a@b.enabled", "plugins.a@-b.enabled", "plugins.a@b.apps._a.enabled",
		"plugins.a@b.mcp_servers.-s.enabled", "plugins.a@b.apps.a.tools._t.enabled",
		"plugins.a@b.enabled.", ".plugins.a@b.enabled",
		" plugins.a @b.enabled ", "plugins.a@b. enabled", "plugins.a@b.apps. a.enabled",
		"plugins.a@b.apps.a.tools.t .enabled", "plugins.a@b.enabled\x00",
		"plugins.a@b.enabled\u00a0", "\u00a0plugins.a@b.enabled",
		"plugins.é@b.enabled", "plugins.a@b.apps.é.enabled",
		"plugins.a/b@c.enabled", "plugins.a\\b@c.enabled",
		"plugins.a@b.apps.a/b.enabled", "plugins.a@b.apps.a.tools.t@b.enabled",
		"plugins.\"a@b\".enabled", "'plugins.a@b.enabled'",
		"plugins.a@b.apps.\"a\".enabled",
		"plugins." + strings.Repeat("a", 65) + "@b.enabled",
		"plugins.a@" + strings.Repeat("b", 65) + ".enabled",
		"plugins.a@b.mcp_servers." + strings.Repeat("s", 65) + ".enabled",
		"plugins.a@b.apps." + strings.Repeat("a", 65) + ".enabled",
		"plugins.a@b.apps.a.tools." + strings.Repeat("t", 65) + ".enabled",
	} {
		if observedPluginPathShape(key) != "" {
			t.Fatal("unsupported neighboring path received a plugin shape")
		}
		var log bytes.Buffer
		newObserver(&log).argv([]string{"-c", key + "=true"})
		if strings.Contains(log.String(), "configPluginShape") {
			t.Fatal("invalid plugin shape was serialized")
		}
	}
	for _, key := range []string{
		"plugins.A1_b-c@M2_k-l.enabled",
		"plugins." + strings.Repeat("a", 64) + "@" + strings.Repeat("b", 64) + ".enabled",
		"plugins.a@b.apps." + strings.Repeat("a", 64) + ".tools." + strings.Repeat("t", 64) + ".enabled",
		"plugins.a@b.mcp_servers." + strings.Repeat("s", 64) + ".enabled",
	} {
		if observedPluginPathShape(key) == "" {
			t.Fatal("bounded safe plugin path was rejected")
		}
	}
}

func TestPluginShapeOmittedForKnownAndMissingAssignments(t *testing.T) {
	key := "plugins.browser@openai-curated-remote.enabled"
	catalog, err := parsePublicPluginCatalog(publicCatalogTestValue(t, []string{key}))
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{
		key + "=true", "plugins.chrome@openai-bundled.enabled=true",
		"features.plugins=true", "plugins._default.enabled=false",
		"plugins.private@market.enabled", "=true",
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.publicPlugins = catalog
		o.argv([]string{"-c", arg})
		if strings.Contains(log.String(), "configPluginShape") {
			t.Fatal("plugin shape replaced known recognition or a missing assignment")
		}
	}
}

func TestPluginShapeWireRemainsTransparent(t *testing.T) {
	wire := []byte("{\"method\":\"PRIVATE_RPC\",\"params\":\"plugins.PRIVATE@MARKET.enabled=PRIVATE_VALUE\"}\n")
	var log, forwarded bytes.Buffer
	o := newObserver(&log)
	o.argv([]string{"-c", "plugins.PRIVATE@MARKET.enabled=true"})
	if err := pump(&forwarded, bytes.NewReader(wire), &lineObserver{o: o, direction: "to_bridge"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwarded.Bytes(), wire) {
		t.Fatal("plugin shape observation changed wire bytes")
	}
	for _, e := range events(t, log.Bytes()) {
		if e.Kind != "argv_config" {
			b, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(b, []byte("configPluginShape")) {
				t.Fatal("plugin shape appeared outside config metadata")
			}
		}
	}
	if strings.Contains(log.String(), "PRIVATE") || strings.Contains(log.String(), "MARKET") {
		t.Fatal("plugin shape observation leaked wire or argv data")
	}
}

const pluginShapeExecutableArg = " \tplugins.SYNTHETIC_PRIVATE_PLUGIN@PRIVATE_MARKET.apps.PRIVATE_APP.tools.PRIVATE_TOOL.enabled=true"
const pluginShapeExecutableWire = "{\"id\":\"PRIVATE_ID\",\"method\":\"account/read\",\"params\":{\"private\":\"PRIVATE_VALUE\"}}\r\n\x00\xff"

// The native test binary checks the complete argv and input, then echoes every
// input byte and exits nonzero. It uses no shell, network or live account.
func TestPluginShapeExecutableChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_PLUGIN_SHAPE_CHILD") != "1" {
		return
	}
	want := []string{"-test.run=^TestPluginShapeExecutableChildFixture$", "--", "-c", pluginShapeExecutableArg}
	if !reflect.DeepEqual(os.Args[1:], want) || os.Getenv(activeEnv) != "1" {
		os.Exit(41)
	}
	if _, present := os.LookupEnv(publicPluginCatalogEnv); present {
		os.Exit(42)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil || string(input) != pluginShapeExecutableWire {
		os.Exit(43)
	}
	if _, err := os.Stdout.Write(input); err != nil {
		os.Exit(44)
	}
	os.Stderr.WriteString("PRIVATE_STDERR\x00\xff\r\n")
	os.Exit(17)
}

func TestPluginShapeExecutablePreservesArgvWireAndExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("actual observer entry point is Windows-only")
	}
	work := t.TempDir()
	observerExe := filepath.Join(work, "plugin-shape-observer.exe")
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
	t.Setenv("MAGPIE_OBSERVER_PLUGIN_SHAPE_CHILD", "1")
	// A valid unrelated dictionary exercises the unknown path after catalog
	// fallback, and is still stripped before the fixture child is launched.
	t.Setenv(publicPluginCatalogEnv, publicCatalogTestValue(t, []string{"plugins.fixture@public-market.enabled"}))
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", self)
	prefix := filepath.Join(work, "trace")
	t.Setenv("MAGPIE_CI_TRACE_FILE", prefix)
	cmd := proc.Command(observerExe, "-test.run=^TestPluginShapeExecutableChildFixture$", "--", "-c", pluginShapeExecutableArg)
	cmd.Stdin = strings.NewReader(pluginShapeExecutableWire)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runOwnedTestCommand(t, cmd)
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17 ||
		stdout.String() != pluginShapeExecutableWire || stderr.String() != "PRIVATE_STDERR\x00\xff\r\n" {
		t.Fatal("plugin shape observation changed child argv, stdin, stdout, stderr or exit")
	}
	files, err := filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 1 {
		t.Fatal("missing plugin shape executable trace")
	}
	log, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(log) > maxTrace || bytes.Contains(log, []byte("PRIVATE")) {
		t.Fatal("plugin shape executable trace exposed private names or values")
	}
	configs := 0
	var exited, outputEOF, errorEOF bool
	for _, e := range events(t, log) {
		if e.Kind == "argv_config" {
			configs++
			if e.ConfigPluginShape != "app_tool_enabled" || e.Class != "unknown" ||
				e.ConfigKey != "" || e.ConfigValue != "" || e.PublicPluginRef != 0 ||
				e.ConfigLiteral != "bool_true" || e.ConfigLexicalRoot != "plugins" {
				t.Fatal("compiled observer missing redacted unknown app_tool_enabled shape")
			}
		}
		exited = exited || e.Kind == "child_exit" && e.Exit != nil && *e.Exit == 17
		outputEOF = outputEOF || e.Kind == "stream_end" && e.Direction == "from_bridge" && e.Class == "eof"
		errorEOF = errorEOF || e.Kind == "stream_end" && e.Direction == "stderr" && e.Class == "eof"
	}
	if configs != 1 || !exited || !outputEOF || !errorEOF {
		t.Fatal("plugin shape executable trace missing config, exit or output EOF")
	}
}
