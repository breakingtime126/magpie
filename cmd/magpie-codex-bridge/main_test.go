package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		os.Exit(fixtureServer())
	}
	testenv.Main(m)
}

// Only a copied native test executable reaches this path; no model is called.
func fixtureServer() int {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 3
		}
		if request.Method == "initialize" {
			response := map[string]any{
				"id":     request.ID,
				"result": map[string]string{
					"userAgent":      "native-adapter-fixture",
					"platformFamily": "fixture",
					"platformOs":     "fixture",
				},
			}
			if json.NewEncoder(os.Stdout).Encode(response) != nil {
				return 4
			}
		}
	}
	if scanner.Err() != nil {
		return 5
	}
	if os.WriteFile(filepath.Join(os.Getenv("HOME"), "clean-exit"), []byte("EOF"), 0600) != nil {
		return 6
	}
	return 0
}

type bufferCloser struct{ bytes.Buffer }

func (*bufferCloser) Close() error { return nil }

func TestAcceptedServerArguments(t *testing.T) {
	for _, args := range [][]string{
		{"app-server"},
		{"app-server", "--listen", "stdio://"},
		{"app-server", "--listen=stdio://"},
		{"app-server", "--stdio"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bridge.json")
			cfg := appserverbridge.Config{WorkingDirectory: "sentinel"}
			input := io.NopCloser(strings.NewReader("request\n"))
			var output bufferCloser
			var diagnostics bytes.Buffer
			var calls []string
			ctx := context.WithValue(context.Background(), struct{}{}, "sentinel")
			code := execute(ctx, args, input, &output, &diagnostics, dependencies{
				getenv: func(key string) string {
					if key != configEnv {
						t.Fatalf("unexpected environment read %q", key)
					}
					calls = append(calls, "env")
					return path
				},
				load: func(got string) (appserverbridge.Config, error) {
					if got != path {
						t.Fatal("configuration path changed")
					}
					calls = append(calls, "load")
					return cfg, nil
				},
				run: func(got context.Context, config appserverbridge.Config, in io.ReadCloser, out io.WriteCloser) error {
					calls = append(calls, "run")
					if got != ctx || !reflect.DeepEqual(config, cfg) || in != input || out != &output {
						t.Fatal("bridge inputs changed")
					}
					_, err := io.WriteString(out, "{\"id\":1,\"result\":{}}\n")
					return err
				},
			})
			if code != 0 || diagnostics.Len() != 0 || output.String() != "{\"id\":1,\"result\":{}}\n" || !reflect.DeepEqual(calls, []string{"env", "load", "run"}) {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%v", code, output.String(), diagnostics.String(), calls)
			}
		})
	}
}

func TestRejectedArgumentsDoNotReadConfigOrStartChildren(t *testing.T) {
	cases := [][]string{
		nil, {"serve"}, {"--help", "--unknown"}, {"--version", "app-server"},
		{"app-server", "--help", "--unknown"}, {"app-server", "--listen"},
		{"app-server", "--listen", "tcp://127.0.0.1:1234"},
		{"app-server", "--listen=stdio://", "--listen=stdio://"},
		{"app-server", "--listen", "stdio://", "trailing"},
		{"app-server", "--listen=STDIO://"}, {"app-server", "--listen=stdio:///"},
		{"app-server", "--stdio", "--stdio"},
		{"app-server", "--stdio", "--listen=stdio://"},
		{"app-server", "--listen", "stdio://", "--stdio"},
		{"app-server", "--"}, {"app-server", "--check"},
	}
	for _, flag := range []string{"--unknown", "--config", "-c", "--model", "-m", "--provider", "--model-provider", "--sandbox", "--ask-for-approval", "--dangerously-bypass-approvals-and-sandbox", "--full-auto", "--enable", "--disable", "--analytics-default-enabled", "--remote-control", "--managed-daemon", "--strict-config", "--telemetry", "--cwd", "-C"} {
		cases = append(cases, []string{"app-server", flag}, []string{"app-server", flag, "secret"}, []string{"app-server", flag + "=secret"})
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bufferCloser
			var diagnostics bytes.Buffer
			// Nil dependencies deliberately panic if even an environment read occurs.
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
			if code != 2 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "unsupported arguments") || strings.Contains(diagnostics.String(), "secret") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
			}
		})
	}
}

func TestHelpAndVersionNeedNoConfiguration(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"-V"}, {"app-server", "--help"}, {"app-server", "-h"}} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{})
		if code != 0 || diagnostics.Len() != 0 || !strings.HasPrefix(output.String(), identity) {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, output.String(), diagnostics.String())
		}
	}
}

func TestConfigPathMustBeExplicitAndAbsolute(t *testing.T) {
	for _, path := range []string{"", "bridge.json", "./bridge.json", "../bridge.json", "~/bridge.json", "C:bridge.json"} {
		for _, args := range [][]string{{"app-server"}, {"--check"}} {
			var output bufferCloser
			var diagnostics bytes.Buffer
			code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{getenv: func(string) string { return path }})
			if code != 2 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "absolute") {
				t.Fatalf("path=%q code=%d", path, code)
			}
		}
	}
}

func TestLoadFailureNeverStartsChildrenOrLeaksDetails(t *testing.T) {
	for _, args := range [][]string{{"app-server"}, {"--check"}} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{
			getenv: func(string) string {
				return filepath.Join(t.TempDir(), "secret.json")
			},
			load: func(string) (appserverbridge.Config, error) {
				return appserverbridge.Config{}, errors.New("private secret")
			},
		})
		if code != 2 || output.Len() != 0 || strings.Contains(diagnostics.String(), "secret") {
			t.Fatalf("code=%d stderr=%q", code, diagnostics.String())
		}
	}
}

// These inert bytes satisfy the configuration's native-file inspection. They
// are never executed: check must stop before the run dependency is called.
func TestCheckLoadsRealConfigWithoutStartingChildrenOrChangingState(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "inert.exe")
	if err := os.WriteFile(command, []byte("MZinert-test-fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appserverbridge.Config{}
	for _, id := range []string{"first", "second"} {
		home := filepath.Join(root, id+"-home")
		codex := filepath.Join(root, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{ID: id, Provider: "openai", Command: command, Home: home, CodexHome: codex})
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(context.Background(), []string{"--check"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string { return path },
		load:   loadConfig,
	})
	if code != 0 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "no children started") {
		t.Fatalf("code=%d stderr=%q", code, diagnostics.String())
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("check changed fixture state")
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestInvalidRealConfigsAreReadOnly(t *testing.T) {
	for _, contents := range []string{"", "{}", "null", "{broken", `{"backends":[],"unknown":"secret"}`, strings.Repeat("x", (1<<20)+1)} {
		root := t.TempDir()
		path := filepath.Join(root, "bridge.json")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, root)
		if _, err := loadConfig(path); err == nil {
			t.Fatal("accepted invalid configuration")
		}
		if !reflect.DeepEqual(before, snapshot(t, root)) {
			t.Fatal("invalid configuration changed state")
		}
	}
	root := t.TempDir()
	for _, path := range []string{root, filepath.Join(root, "missing.json")} {
		if _, err := loadConfig(path); err == nil {
			t.Fatal("accepted non-file configuration")
		}
	}
}

func TestServerErrorsOnlyUseGenericStderr(t *testing.T) {
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(context.Background(), []string{"app-server"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string {
			return filepath.Join(t.TempDir(), "bridge.json")
		},
		load: func(string) (appserverbridge.Config, error) {
			return appserverbridge.Config{}, nil
		},
		run: func(context.Context, appserverbridge.Config, io.ReadCloser, io.WriteCloser) error {
			return errors.New("secret payload")
		},
	})
	if code != 1 || output.Len() != 0 || diagnostics.String() != "magpie-codex-bridge: app-server bridge failed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output.String(), diagnostics.String())
	}
}

func TestCanceledContextDoesNotStartChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(ctx, []string{"app-server"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string {
			return filepath.Join(t.TempDir(), "bridge.json")
		},
		load: func(string) (appserverbridge.Config, error) {
			return appserverbridge.Config{}, nil
		},
	})
	if code != 0 || output.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatal("cancellation produced an error or stdout")
	}
}

func TestNativeAdapterInitializesAndReapsBothChildrenOnEOF(t *testing.T) {
	testAdapterLifecycle(t, "")
}

func TestBuiltAdapterStdio(t *testing.T) {
	adapter := os.Getenv("MAGPIE_BRIDGE_ADAPTER_TEST_EXE")
	if adapter == "" {
		t.Skip("set MAGPIE_BRIDGE_ADAPTER_TEST_EXE to a built native adapter to exercise main")
	}
	if !filepath.IsAbs(adapter) {
		t.Fatal("MAGPIE_BRIDGE_ADAPTER_TEST_EXE must be absolute")
	}
	info, err := os.Stat(adapter)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("MAGPIE_BRIDGE_ADAPTER_TEST_EXE must name an existing native adapter")
	}
	testAdapterLifecycle(t, adapter)
}

func testAdapterLifecycle(t *testing.T, adapter string) {
	t.Helper()
	root := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(root, "native-fixture.exe")
	if err := os.WriteFile(command, binary, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appserverbridge.Config{}
	for _, id := range []string{"first", "second"} {
		home := filepath.Join(root, id+"-home")
		codex := filepath.Join(root, id+"-codex")
		for _, path := range []string{home, codex} {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{ID: id, Provider: "openai", Command: command, Home: home, CodexHome: codex})
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(configEnv, path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	var diagnostics bytes.Buffer
	finished := make(chan struct{})
	code := -1
	go func() {
		defer close(finished)
		defer inputReader.Close()
		defer outputWriter.Close()
		if adapter == "" {
			code = execute(ctx, []string{"app-server", "--stdio"}, inputReader, outputWriter, &diagnostics, dependencies{
				getenv: os.Getenv,
				load:   loadConfig,
				run:    appserverbridge.Run,
			})
			return
		}
		command := proc.CommandContext(ctx, adapter, "app-server", "--stdio")
		command.Env = os.Environ()
		command.Dir = root
		command.Stdin = inputReader
		command.Stdout = outputWriter
		command.Stderr = &diagnostics
		tree, err := proc.StartTreeStrict(command)
		if err != nil {
			fmt.Fprintln(&diagnostics, "cannot start built adapter:", err)
			return
		}
		defer func() { <-tree.Stop(time.Second) }()
		if err := tree.Wait(); err != nil {
			fmt.Fprintln(&diagnostics, "built adapter failed:", err)
			return
		}
		code = 0
	}()
	// A dedicated reader drains stdout so EOF cleanup cannot deadlock on a
	// blocked output pipe. Both goroutines are joined on every failure path.
	readDone := make(chan struct{})
	lines := make(chan string, 1)
	var transcript bytes.Buffer
	var readErr error
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(outputReader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(&transcript, line)
			select {
			case lines <- line:
			default:
			}
		}
		readErr = scanner.Err()
	}()
	t.Cleanup(func() {
		cancel()
		inputWriter.Close()
		outputReader.Close()
		inputReader.Close()
		outputWriter.Close()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("adapter did not stop and reap its children")
		}
		<-readDone
	})
	if _, err := fmt.Fprintln(inputWriter, `{"id":42,"method":"initialize","params":{"clientInfo":{"name":"adapter-test","version":"1"}}}`); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-lines:
		var response struct {
			ID     int               `json:"id"`
			Result map[string]string `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout was not JSONL: %q: %v", line, err)
		}
		want := map[string]string{"userAgent": "native-adapter-fixture", "platformFamily": "fixture", "platformOs": "fixture"}
		if response.ID != 42 || !reflect.DeepEqual(response.Result, want) {
			t.Fatalf("unexpected initialize response: %q", line)
		}
	case <-ctx.Done():
		t.Fatal("adapter initialization timed out")
	}
	if _, err := fmt.Fprintln(inputWriter, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("adapter did not finish on client EOF")
	}
	<-readDone
	if code != 0 || diagnostics.Len() != 0 || readErr != nil || strings.Count(transcript.String(), "\n") != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q read=%v", code, transcript.String(), diagnostics.String(), readErr)
	}
	for _, backend := range cfg.Backends {
		marker, err := os.ReadFile(filepath.Join(backend.Home, "clean-exit"))
		if err != nil || string(marker) != "EOF" {
			t.Fatalf("backend %s did not exit cleanly on EOF: %q, %v", backend.ID, marker, err)
		}
	}
}
