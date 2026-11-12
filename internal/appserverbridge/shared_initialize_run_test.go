package appserverbridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativehandshake"
)

// Runs only inside the copied synthetic test executable, never a native account.
func sharedSerialFixture() bool {
	home := os.Getenv("HOME")
	mode, err := os.ReadFile(filepath.Join(home, "serial-runtime-mode"))
	if err != nil {
		return false
	}
	role := "primary"
	for _, arg := range os.Args {
		if arg == "model_provider="+strconv.Quote(sharedSecondaryProvider167) {
			role = "secondary"
		}
	}
	_ = os.WriteFile(filepath.Join(home, "serial-start-"+role), []byte("started"), 0600)
	if role == "secondary" {
		if _, err := os.Stat(filepath.Join(home, "serial-primary-replied")); err != nil {
			_ = os.WriteFile(filepath.Join(home, "serial-started-early"), []byte("bad"), 0600)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		message, err := parseObject(scanner.Bytes())
		if err != nil {
			return true
		}
		method := text(message, "method")
		if method == "initialized" {
			f, err := os.OpenFile(filepath.Join(home, "serial-notifications-"+role), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				_, _ = f.WriteString("initialized\n")
				_ = f.Close()
			}
			continue
		}
		var result []byte
		switch method {
		case "initialize":
			if role == "primary" {
				if string(mode) == "error" {
					fmt.Println(string(raw(object{"id": message["id"], "error": raw(object{"code": raw(-32000), "message": raw("synthetic error")})})))
					continue
				}
				if string(mode) == "eof" {
					return true
				}
				if string(mode) == "malformed" {
					fmt.Println("not-json")
					return true
				}
				if string(mode) == "hold" {
					for {
						if _, err := os.Stat(filepath.Join(home, "serial-release")); err == nil {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				_ = os.WriteFile(filepath.Join(home, "serial-primary-replied"), []byte("ready"), 0600)
			}
			result = raw(object{"userAgent": raw("synthetic"), "codexHome": raw(os.Getenv("CODEX_HOME")), "platformFamily": raw("test"), "platformOs": raw("test")})
		case "configRequirements/read":
			result = []byte(controlPlaneNullRequirements)
		case "config/read":
			result, _ = os.ReadFile(filepath.Join(home, "serial-config-"+role))
		case "model/list":
			result = raw(object{"data": raw([]any{fixtureModel("synthetic-model", true)}), "nextCursor": raw(nil)})
		default:
			return true
		}
		fmt.Println(string(raw(object{"id": message["id"], "result": result})))
	}
	return true
}

func serialRuntimeConfig(t *testing.T, mode string) Config {
	t.Helper()
	cfg := sharedNativeConfigFixture167(t)
	commands := startupProbeFixtureConfig(t)
	for i := range cfg.Backends {
		cfg.Backends[i].Command = commands.Backends[i].Command
		cfg.Backends[i].StartupProbeSHA256 = commands.Backends[i].StartupProbeSHA256
		cfg.Backends[i].InheritEnv = nil
	}
	cfg.SharedNativeRoute.GatewayAuth = SharedNativeLocalGateway167
	home := cfg.Backends[0].Home
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", cfg.Backends[0].CodexHome)
	if err := os.WriteFile(filepath.Join(home, "serial-runtime-mode"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(cfg.Backends)
	if err != nil {
		t.Fatal(err)
	}
	r.sharedNativeRoute167 = cfg.SharedNativeRoute
	for i, role := range []string{"primary", "secondary"} {
		if err := os.WriteFile(filepath.Join(home, "serial-config-"+role), raw(sharedNativeConfigResponse167(r, cfg.Backends[i].ID)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func startSerialRuntime(t *testing.T, cfg Config) (*runningBridge, *handshakeEvents) {
	t.Helper()
	input, client := io.Pipe()
	output, writer := io.Pipe()
	events := &handshakeEvents{changed: make(chan struct{}, 64)}
	ctx, cancel := context.WithCancel(nativehandshake.WithObserver(context.Background(), events.record))
	bridge := &runningBridge{input: client, output: output, cancel: cancel, done: make(chan struct{})}
	go func() { bridge.err = Run(ctx, cfg, input, writer); close(bridge.done) }()
	t.Cleanup(func() { cancel(); client.Close(); output.Close(); _ = bridge.wait(t) })
	return bridge, events
}

func assertSerialNotStarted(t *testing.T, cfg Config) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(cfg.Backends[0].Home, "serial-start-secondary")); !os.IsNotExist(err) {
		t.Fatal("secondary started before validated primary initialize")
	}
}

func TestSharedSerialRunDelaysStartAndCompletesHandshake(t *testing.T) {
	cfg := serialRuntimeConfig(t, "hold")
	bridge, events := startSerialRuntime(t, cfg)
	stream := readRequirementsStream(t, bridge)
	// Preserve early notifications without letting them start the secondary.
	fmt.Fprintln(bridge.input, `{"method":"initialized"}`)
	fmt.Fprintln(bridge.input, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"synthetic","version":"1"},"capabilities":{"experimentalApi":true}}}`)
	events.wait(t, nativehandshake.PrimaryInitializeWritten)
	assertSerialNotStarted(t, cfg)
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "serial-release"), []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	events.wait(t, nativehandshake.InitializeOutputWritten)
	if !events.has(nativehandshake.SecondaryInitializeReply) {
		t.Fatal("missing genuine secondary initialize")
	}
	bridge.input.Close()
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 1 {
		t.Fatal("full shared handshake failed")
	}
	if _, err := os.Stat(filepath.Join(cfg.Backends[0].Home, "serial-started-early")); !os.IsNotExist(err) {
		t.Fatal("secondary raced primary")
	}
	for _, role := range []string{"primary", "secondary"} {
		data, err := os.ReadFile(filepath.Join(cfg.Backends[0].Home, "serial-notifications-"+role))
		if err != nil || string(data) != "initialized\n" {
			t.Fatal("early initialized notification lost or duplicated")
		}
	}
}

func TestSharedSerialRunFailureAndCancellationNeverStartSecondary(t *testing.T) {
	for _, mode := range []string{"error", "malformed", "eof", "cancel", "client-eof"} {
		t.Run(mode, func(t *testing.T) {
			fixtureMode := mode
			if mode == "cancel" || mode == "client-eof" {
				fixtureMode = "hold"
			}
			cfg := serialRuntimeConfig(t, fixtureMode)
			bridge, events := startSerialRuntime(t, cfg)
			stream := readRequirementsStream(t, bridge)
			fmt.Fprintln(bridge.input, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"synthetic","version":"1"},"capabilities":{"experimentalApi":true}}}`)
			events.wait(t, nativehandshake.PrimaryInitializeWritten)
			if mode == "error" {
				events.wait(t, nativehandshake.PrimaryInitializeError)
				bridge.input.Close()
			} else if mode == "cancel" {
				bridge.cancel()
			} else if mode == "client-eof" {
				bridge.input.Close()
			}
			_ = bridge.wait(t)
			<-stream.done
			assertSerialNotStarted(t, cfg)
		})
	}
}
