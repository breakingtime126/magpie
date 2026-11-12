//go:build windows

package appserverbridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativecatalog"
	"github.com/yetone/magpie/internal/nativeendpoint"
	"github.com/yetone/magpie/internal/nativehandshake"
	"github.com/yetone/magpie/internal/proc"
)

// Disposable official-runtime diagnostic only. No thread, turn, login or
// inference request is sent. A completed experiment is not chat acceptance.
func TestOfficialSecondaryInitializeControl(t *testing.T) {
	if os.Getenv("MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST") != "1" {
		t.Skip("explicit hosted official-runtime opt-in required")
	}
	exe := os.Getenv("MAGPIE_PREVIEW_OFFICIAL_NATIVE")
	if !filepath.IsAbs(exe) {
		t.Fatal("verified official executable unavailable")
	}
	concurrentPrimary, concurrentSecondary := secondaryInitializeControl(t, exe, false)
	orderedPrimary, orderedSecondary := secondaryInitializeControl(t, exe, true)
	fmt.Printf("native-preview-diagnostic stage=initialize-order-%s-%s-%s-%s class=passed\n", concurrentPrimary, concurrentSecondary, orderedPrimary, orderedSecondary)
}

func secondaryInitializeControl(t *testing.T, exe string, ordered bool) (primaryOutcome, secondaryOutcome string) {
	t.Helper()
	// Each case has fresh equivalent state. Both use identical secondary options,
	// catalog, environment and shared HOME/CODEX_HOME. The only treatment is
	// waiting for the primary initialize reply before starting the secondary.
	// No initialized notification, thread, turn or model-selection override.
	root := t.TempDir()
	home, state := filepath.Join(root, "home"), filepath.Join(root, "state")
	for _, dir := range []string{home, state} {
		if os.Mkdir(dir, 0700) != nil {
			t.Fatal("control directory unavailable")
		}
	}
	const model = "original-ui-fixture/original-ui-fixture-model"
	catalog, err := nativecatalog.Render([]nativecatalog.Model{{ID: model, Name: "original-ui-fixture-model"}})
	if err != nil {
		t.Fatal("control catalog unavailable")
	}
	path := filepath.Join(root, "catalog.json")
	if os.WriteFile(path, catalog, 0600) != nil {
		t.Fatal("control catalog write failed")
	}
	ep, err := nativeendpoint.Listen()
	if err != nil {
		t.Fatal("control endpoint unavailable")
	}
	defer func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if ep.Shutdown(ctx) != nil {
			t.Error("control descendant cleanup unverified")
		}
	}()
	primary := Backend{ID: "openai", Provider: "openai", Home: home, CodexHome: state, Command: exe}
	secondary := Backend{ID: "aihub", Provider: sharedSecondaryProvider167, Home: home, CodexHome: state, Command: exe}
	cfg := Config{Backends: []Backend{primary, secondary}, SharedNativeRoute: &SharedNativeRoute167{
		Mode: SharedNativeHome167, GatewayURL: "http://" + ep.Addr().String() + "/v1",
		GatewayEnvKey: "MAGPIE_INITIALIZE_CONTROL_GATEWAY", GatewayAuth: SharedNativeLocalGateway167, SecondaryCatalog: path,
	}}
	// Preserve transport/trust variables through the production filter. Only
	// homes are replaced with this experiment's own fresh directories.
	inherited := append([]string{}, os.Environ()...)
	filtered := inherited[:0]
	for _, entry := range inherited {
		key := entry
		for i, c := range entry {
			if c == '=' {
				key = entry[:i]
				break
			}
		}
		if strings.EqualFold(key, "HOME") || strings.EqualFold(key, "USERPROFILE") || strings.EqualFold(key, "CODEX_HOME") {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, "HOME="+home, "USERPROFILE="+home, "CODEX_HOME="+state)
	environment, err := cfg.sharedNativeEnvironment167(secondary, filtered)
	if err != nil {
		t.Fatal("control environment unavailable")
	}
	args := cfg.nativeArgs167(LaunchOptions{}, secondary)
	cmd := proc.Command(exe, args...)
	cmd.Env, cmd.Dir = environment, home
	// Null stderr, as in production: no raw native content or private errors.
	input, output, closeChild, err := ownedCommandPipes(cmd)
	if err != nil {
		t.Fatal("control pipes unavailable")
	}
	defer input.Close()
	defer output.Close()
	defer closeChild()
	var primaryInput io.WriteCloser
	var primaryOutput io.ReadCloser
	{
		primaryEnv, err := cfg.sharedNativeEnvironment167(primary, filtered)
		if err != nil {
			t.Fatal("primary control environment unavailable")
		}
		primaryCmd := proc.Command(exe, cfg.nativeArgs167(LaunchOptions{}, primary)...)
		primaryCmd.Env, primaryCmd.Dir = primaryEnv, home
		var closePrimaryChild func()
		primaryInput, primaryOutput, closePrimaryChild, err = ownedCommandPipes(primaryCmd)
		if err != nil {
			t.Fatal("primary control pipes unavailable")
		}
		defer primaryInput.Close()
		defer primaryOutput.Close()
		// Retain the Job until whole-tree drainage, not only root exit.
		tree, startErr := proc.StartTreeDrained(primaryCmd)
		closePrimaryChild()
		if tree != nil {
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				stopErr := tree.Stop()
				drainErr := tree.Wait(cleanup)
				if drainErr != nil {
					go retainProbeDrain(tree)
				}
				if stopErr != nil || drainErr != nil {
					t.Error("primary control cleanup unverified")
				}
			}()
		}
		if startErr != nil || tree == nil {
			t.Fatal("primary control child unavailable")
		}
	}
	if ordered {
		primaryContext, stopPrimary := context.WithTimeout(context.Background(), 35*time.Second)
		primaryOutcome = initializeControlExchange(primaryContext, primaryInput, primaryOutput)
		stopPrimary()
		if primaryOutcome != "reply" {
			return primaryOutcome, "notstarted"
		}
	}
	if err := ep.Start(cmd); err != nil {
		closeChild()
		t.Fatal("control child unavailable")
	}
	closeChild()
	ctx, stop := context.WithTimeout(context.Background(), 35*time.Second)
	defer stop()
	if ordered {
		return primaryOutcome, initializeControlExchange(ctx, input, output)
	}
	primaryResult := make(chan string, 1)
	secondaryResult := make(chan string, 1)
	go func() { primaryResult <- initializeControlExchange(ctx, primaryInput, primaryOutput) }()
	go func() { secondaryResult <- initializeControlExchange(ctx, input, output) }()
	// Both exchanges join before either child is retired. In particular, a
	// quick primary reply must not close its stdin while secondary starts.
	return <-primaryResult, <-secondaryResult
}

func initializeControlExchange(ctx context.Context, input io.WriteCloser, output io.ReadCloser) string {
	written := make(chan error, 1)
	go func() {
		_, err := input.Write([]byte("{\"id\":1,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"magpie_initialize_control\",\"version\":\"1\"},\"capabilities\":{\"experimentalApi\":true}}}\n"))
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			return "write"
		}
	case <-ctx.Done():
		input.Close()
		<-written
		return "timeout"
	}
	result := make(chan string, 1)
	readDone := make(chan struct{})
	defer func() {
		if ctx.Err() != nil {
			output.Close()
		}
		<-readDone
	}()
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64<<10), MaxMessageBytes+1)
		for scanner.Scan() {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
				result <- "invalid"
				return
			}
			if string(envelope["id"]) != "1" {
				continue
			}
			if _, ok := envelope["result"]; ok {
				if _, bad := envelope["error"]; bad {
					result <- "invalid"
				} else {
					result <- "reply"
				}
				return
			}
			if _, ok := envelope["error"]; ok {
				result <- "error"
				return
			}
			result <- "invalid"
			return
		}
		if scanner.Err() != nil {
			result <- "invalid"
		} else {
			result <- "eof"
		}
	}()
	select {
	case value := <-result:
		return value
	case <-ctx.Done():
		return "timeout"
	}
}

func secondaryInitializeControlArgs(cfg Config, b Backend, explicit bool, model string) []string {
	args := cfg.nativeArgs167(LaunchOptions{}, b)
	if explicit {
		args = append(args, "-c", "model="+strconv.Quote(model))
	}
	return args
}

func TestSecondaryInitializeControlChangesOnlyModel(t *testing.T) {
	cfg := Config{SharedNativeRoute: &SharedNativeRoute167{GatewayURL: "http://127.0.0.1:1234/v1", GatewayEnvKey: "MAGPIE_INITIALIZE_CONTROL_GATEWAY", SecondaryCatalog: "catalog.json"}}
	b := Backend{Provider: sharedSecondaryProvider167}
	base := secondaryInitializeControlArgs(cfg, b, false, "fixture/model")
	picked := secondaryInitializeControlArgs(cfg, b, true, "fixture/model")
	expected := cfg.nativeArgs167(LaunchOptions{}, b)
	if len(base) != len(expected) || len(picked) != len(base)+2 {
		t.Fatal("control changed argument shape")
	}
	for i := range base {
		if base[i] != expected[i] || picked[i] != base[i] {
			t.Fatal("control changed a production argument")
		}
	}
	if picked[len(base)] != "-c" || picked[len(base)+1] != "model="+strconv.Quote("fixture/model") {
		t.Fatal("control changed more than explicit model")
	}
}

func TestSecondaryInitializeControlExchangeKeepsStreamsOpen(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	fixture := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(server)
		_, err := reader.ReadString('\n')
		if err != nil {
			fixture <- false
			return
		}
		if _, err := server.Write([]byte("{\"id\":1,\"result\":{}}\n")); err != nil {
			fixture <- false
			return
		}
		value, err := reader.ReadByte()
		fixture <- err == nil && value == 'x'
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := initializeControlExchange(ctx, client, client); got != "reply" {
		t.Fatal("initialize response not classified")
	}
	client.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte{'x'}); err != nil || !<-fixture {
		t.Fatal("quick initialize reply retired shared primary")
	}
}

// Fast diagnostic of the complete real Router barrier, not Desktop acceptance.
// No thread/turn/inference RPC is sent. The unused loopback reservation is not
// an AIHub service or a model-response fixture.
func TestOfficialBridgeInitializeBarrier(t *testing.T) {
	if os.Getenv("MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST") != "1" {
		t.Skip("explicit hosted official-runtime opt-in required")
	}
	exe := os.Getenv("MAGPIE_PREVIEW_OFFICIAL_NATIVE")
	if !filepath.IsAbs(exe) {
		t.Fatal("verified official executable unavailable")
	}
	cfg := sharedNativeConfigFixture167(t)
	catalog, err := nativecatalog.Render([]nativecatalog.Model{{ID: "original-ui-fixture/original-ui-fixture-model", Name: "original-ui-fixture-model"}})
	if err != nil || os.WriteFile(cfg.SharedNativeRoute.SecondaryCatalog, catalog, 0600) != nil {
		t.Fatal("fresh synthetic catalog unavailable")
	}
	// Match the production native-settings contract rather than the legacy
	// no-owned-catalog path. This also requests genuine includeHidden lists.
	cfg.BridgeModelSettings = BridgeModelSettingsNative167
	cfg.StartupProbe = StartupProbeCompareUnrestricted167
	cfg.RequirementsRead = ConfigRequirementsCompareNull167
	cfg.ExperimentalFeatureList = ExperimentalFeatureListCompare167
	cfg.AuthStatus = AuthStatusPrimaryTrustedStdio167
	cfg.DesktopPluginOverride = DesktopPluginCodexAppTools167
	catalogSHA := sha256.Sum256(catalog)
	cfg.SharedNativeRoute.SecondaryCatalogSHA256 = hex.EncodeToString(catalogSHA[:])
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("loopback reservation unavailable")
	}
	defer reservation.Close()
	cfg.SharedNativeRoute.GatewayURL = "http://" + reservation.Addr().String() + "/v1"
	cfg.SharedNativeRoute.GatewayAuth = SharedNativeLocalGateway167
	for i := range cfg.Backends {
		cfg.Backends[i].Command = exe
		cfg.Backends[i].StartupProbeSHA256 = windowsReadinessSHA167
		cfg.Backends[i].InheritEnv = nil
	}
	t.Setenv("HOME", cfg.Backends[0].Home)
	t.Setenv("USERPROFILE", cfg.Backends[0].Home)
	t.Setenv("CODEX_HOME", cfg.Backends[0].CodexHome)
	var mu sync.Mutex
	var stages []nativehandshake.Stage
	observed := nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) {
		mu.Lock()
		defer mu.Unlock()
		if len(stages) < 128 {
			stages = append(stages, s)
		}
	})
	ctx, cancel := context.WithTimeout(observed, 45*time.Second)
	defer cancel()
	input, client := io.Pipe()
	output, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, input, writer) }()
	outcome := initializeControlExchange(ctx, client, output)
	client.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(5*time.Second):
		t.Fatal("bridge process cleanup unverified")
	}
	output.Close()
	mu.Lock()
	for _, s := range stages {
		fmt.Printf("native-bridge-stage stage=%s\n", s.String())
	}
	mu.Unlock()
	fmt.Printf("native-preview-diagnostic stage=bridge-initialize-%s class=passed\n", outcome)
}
