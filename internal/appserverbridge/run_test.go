package appserverbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) {
	if startupProbeFixture() {
		os.Exit(0)
	}

	if len(os.Args) > 1 && os.Args[1] == "bridge-fixture-descendant" {
		fmt.Println("ready")
		for {
			time.Sleep(time.Hour)
		}
	}

	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		fixtureServer()
		os.Exit(0)
	}
	testenv.Main(m)
}

type fixtureReport struct {
	Args, Environment []string
	Directory         string
	PID               int
}

// This is a copied native test executable, not a shell script or a model call.
func fixtureServer() {
	home := os.Getenv("HOME")
	mode, _ := os.ReadFile(filepath.Join(home, "fixture-mode"))
	dir, _ := os.Getwd()
	report, _ := json.Marshal(fixtureReport{Args: os.Args[1:], Environment: os.Environ(), Directory: dir, PID: os.Getpid()})
	if os.WriteFile(filepath.Join(home, "fixture-report"), report, 0600) != nil {
		os.Exit(3)
	}
	if handled, code := experimentalFeatureFixture(); handled {
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	if handled, code := authStatusFixture(); handled {
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	if handled, code := authCompatFixture(); handled {
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "secret stderr must never reach the client")
	switch string(mode) {
	case "eof":
		return
	case "malformed":
		fmt.Println("not JSON: secret payload")
		io.Copy(io.Discard, os.Stdin)
		return
	case "oversized":
		fmt.Println(strings.Repeat("x", MaxMessageBytes+1))
		io.Copy(io.Discard, os.Stdin)
		return
	}
	if strings.HasPrefix(string(mode), "descendant") {
		self, _ := os.Executable()
		command := proc.CommandContext(context.Background(), self, "bridge-fixture-descendant")
		command.Env = os.Environ()
		ready, err := command.StdoutPipe()
		if err != nil {
			os.Exit(5)
		}
		if command.Start() != nil {
			os.Exit(6)
		}
		if _, err := bufio.NewReader(ready).ReadString('\n'); err != nil {
			os.Exit(7)
		}
		if os.WriteFile(filepath.Join(home, "fixture-descendant"), []byte(strconv.Itoa(command.Process.Pid)), 0600) != nil {
			os.Exit(8)
		}
	}
	var requestLog *os.File
	provider := ""
	turnNumber := 0
	activeTurn := ""
	if string(mode) == "conversations" || string(mode) == "interruptions" || string(mode) == "requirements" {
		var err error
		provider, err = strconv.Unquote(strings.TrimPrefix(os.Args[3], "model_provider="))
		if err != nil {
			os.Exit(9)
		}
		requestLog, err = os.OpenFile(filepath.Join(home, "fixture-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(10)
		}
		defer requestLog.Close()
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), MaxMessageBytes+1)
	for scanner.Scan() {
		if string(mode) == "descendant-malformed" {
			fmt.Println("not JSON")
			io.Copy(io.Discard, os.Stdin)
			return
		}
		if string(mode) == "descendant-exit" {
			return
		}
		if requestLog != nil {
			if _, err := fmt.Fprintln(requestLog, scanner.Text()); err != nil {
				os.Exit(11)
			}
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(4)
		}
		var method string
		json.Unmarshal(msg["method"], &method)
		if len(msg["id"]) == 0 {
			continue
		}
		result := json.RawMessage(`{}`)
		if method == "initialize" {
			result = json.RawMessage(`{"userAgent":"fixture","platformFamily":"unix","platformOs":"linux"}`)
		}
		if method == "configRequirements/read" && string(mode) == "requirements" {
			data, err := os.ReadFile(filepath.Join(home, "fixture-requirements"))
			if err != nil {
				os.Exit(12)
			}
			result = json.RawMessage(data)
		}
		if method == "model/list" {
			result = json.RawMessage(`{"data":[],"nextCursor":null}`)
		}
		if method == "environment/status" {
			result = json.RawMessage(`{"status":"ready"}`)
		}
		if method == "environment/info" {
			path := filepath.ToSlash(dir)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			shell := "fixture"
			if value, err := os.ReadFile(filepath.Join(home, "fixture-shell")); err == nil {
				shell = string(value)
			}
			result = raw(map[string]any{"shell": map[string]string{"name": shell, "path": shell}, "cwd": (&url.URL{Scheme: "file", Path: path}).String()})
		}
		var completed object
		var delta object
		if requestLog != nil {
			params, _ := child(msg, "params")
			switch method {
			case "thread/start":
				result = raw(map[string]any{"thread": map[string]any{"id": provider + "-thread", "modelProvider": provider, "model": text(params, "model"), "turns": []any{}}, "model": text(params, "model"), "modelProvider": provider})
			case "turn/start":
				if string(mode) == "interruptions" && activeTurn != "" {
					fmt.Println(string(raw(object{"id": msg["id"], "error": raw(map[string]any{"code": -32001, "message": "fixture turn is still active"})})))
					continue
				}
				turnNumber++
				turnID := fmt.Sprintf("%s-turn-%d", provider, turnNumber)
				result = raw(map[string]any{"turn": map[string]any{"id": turnID, "items": []any{}, "status": "inProgress", "error": nil}})
				if string(mode) == "interruptions" {
					activeTurn = turnID
					delta = object{"threadId": params["threadId"], "turnId": raw(turnID), "itemId": raw(turnID + "-message"), "delta": raw("synthetic-" + turnID)}
				}
				if string(mode) == "conversations" && turnNumber == 1 {
					completed = object{"threadId": params["threadId"], "turn": raw(map[string]any{"id": turnID, "items": []any{}, "status": "completed", "error": nil})}
				}
			case "turn/interrupt":
				if string(mode) == "interruptions" {
					if text(params, "turnId") != activeTurn {
						fmt.Println(string(raw(object{"id": msg["id"], "error": raw(map[string]any{"code": -32002, "message": "fixture turn is not active"})})))
						continue
					}
					activeTurn = ""
				}
				completed = object{"threadId": params["threadId"], "turn": raw(map[string]any{"id": text(params, "turnId"), "items": []any{}, "status": "interrupted", "error": nil})}
			}
		}
		// Completion may precede the interrupt acknowledgement in the real protocol.
		if completed != nil && string(mode) == "interruptions" {
			fmt.Println(string(raw(object{"method": raw("turn/completed"), "params": raw(completed)})))
			completed = nil
		}
		out, _ := json.Marshal(map[string]json.RawMessage{"id": msg["id"], "result": result})
		fmt.Println(string(out))
		if delta != nil {
			fmt.Println(string(raw(object{"method": raw("item/agentMessage/delta"), "params": raw(delta)})))
		}
		if completed != nil {
			fmt.Println(string(raw(object{"method": raw("turn/completed"), "params": raw(completed)})))
		}
	}
	if string(mode) == "descendant-stubborn" {
		for {
			time.Sleep(time.Hour)
		}
	}
	_ = os.WriteFile(filepath.Join(home, "fixture-clean-exit"), []byte("done"), 0600)
}

type runningBridge struct {
	input  *io.PipeWriter
	output *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func startFixtureBridge(t *testing.T, cfg Config) *runningBridge {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	r := &runningBridge{input: inputWriter, output: outputReader, cancel: cancel, done: make(chan struct{})}
	go func() { r.err = Run(ctx, cfg, inputReader, outputWriter); close(r.done) }()
	t.Cleanup(func() {
		cancel()
		inputWriter.Close()
		outputReader.Close()
		select {
		case <-r.done:
		case <-time.After(30 * time.Second):
			t.Error("bridge failed to stop and reap its children")
		}
	})
	return r
}

func (r *runningBridge) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(30 * time.Second):
		t.Fatal("bridge did not terminate")
		return nil
	}
}

func TestRunStartsIsolatedPinnedBackendsAndStopsOnClientEOF(t *testing.T) {
	cfg := fixtureConfig(t)
	cfg.Backends[1].Provider = "custom_provider"
	t.Setenv("OPENAI_API_KEY", "must-not-inherit")
	t.Setenv("HTTPS_PROXY", "https://must-not-inherit.invalid")
	r := startFixtureBridge(t, cfg)
	if _, err := fmt.Fprintln(r.input, `{"id":42,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`); err != nil {
		t.Fatal(err)
	}
	result := make(chan []byte, 1)
	go func() { line, _ := bufio.NewReader(r.output).ReadBytes('\n'); result <- line }()
	select {
	case line := <-result:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(line, &msg); err != nil || string(msg["id"]) != "42" || len(msg["result"]) == 0 {
			t.Fatalf("initialize response %s, %v", line, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("initialization did not complete")
	}
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-report"))
		if err != nil {
			t.Fatal(err)
		}
		var report fixtureReport
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		expected := []string{"app-server", "-c", `model_provider="` + b.Provider + `"`}
		if !reflect.DeepEqual(report.Args, expected) {
			t.Fatalf("args %q, want %q", report.Args, expected)
		}
		realHome, _ := filepath.EvalSymlinks(b.Home)
		realDir, _ := filepath.EvalSymlinks(report.Directory)
		if realDir != realHome {
			t.Fatalf("working directory %s, want %s", report.Directory, b.Home)
		}
		env := strings.Join(report.Environment, "\n")
		if strings.Contains(env, "must-not-inherit") || !strings.Contains(env, "CODEX_HOME="+b.CodexHome) || !strings.Contains(env, "HOME="+b.Home) {
			t.Fatalf("incorrect isolated environment: %s", env)
		}
	}
	r.input.Close()
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	for _, b := range cfg.Backends {
		if _, err := os.Stat(filepath.Join(b.Home, "fixture-clean-exit")); err != nil {
			t.Fatalf("backend %s did not receive EOF cleanup opportunity: %v", b.ID, err)
		}
	}
}

func TestRunFailsClosedWhenEitherBackendDiesOrViolatesProtocol(t *testing.T) {
	for _, mode := range []string{"eof", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			cfg := fixtureConfig(t)
			if err := os.WriteFile(filepath.Join(cfg.Backends[1].Home, "fixture-mode"), []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}
			r := startFixtureBridge(t, cfg)
			err := r.wait(t)
			if err == nil {
				t.Fatal("backend failure was treated as success")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("leaked backend content: %v", err)
			}
		})
	}
}

func TestRunCancellationClosesBlockedClientRead(t *testing.T) {
	r := startFixtureBridge(t, fixtureConfig(t))
	r.cancel()
	if err := r.wait(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}

func TestWriteMessageCancellationClosesBlockedPipe(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writeMessage(ctx, writer, []byte(`{"id":1}`)) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("blocked write outlived cancellation")
	}
}

func TestScanMessagesBoundsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan bridgeEvent, 1)
	scanMessages(ctx, strings.NewReader(strings.Repeat("x", MaxMessageBytes+1)+"\n"), "backend", events)
	event := <-events
	if event.err == nil {
		t.Fatal("oversized message accepted")
	}
}

func TestRunSweepsBackendProcessGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process liveness uses Linux procfs; other OS builds are checked separately")
	}
	cfg := fixtureConfig(t)
	b := cfg.Backends[0]
	if err := os.WriteFile(filepath.Join(b.Home, "fixture-mode"), []byte("descendant"), 0600); err != nil {
		t.Fatal(err)
	}
	r := startFixtureBridge(t, cfg)
	pid := 0
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for pid == 0 {
		data, _ := os.ReadFile(filepath.Join(b.Home, "fixture-descendant"))
		pid, _ = strconv.Atoi(string(data))
		if pid != 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("grandchild did not start")
		}
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Kill(); child.Release() })
	r.input.Close()
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		// A zombie has stopped executing and awaits its adoptive parent's reap.
		_, rest, _ := strings.Cut(string(data), ") ")
		if os.IsNotExist(err) || strings.HasPrefix(rest, "Z ") {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("grandchild survived backend shutdown")
		}
	}
}

// Exercise the actual runner pipes and native child processes, not only Router.
func TestRunRoutesIndependentConversationsEndToEnd(t *testing.T) {
	cfg := fixtureConfig(t)
	for i := range cfg.Backends {
		b := &cfg.Backends[i]
		b.Provider = b.ID + "_provider"
		b.Models = []json.RawMessage{fixtureModel(b.ID+"_model", true)}
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-mode"), []byte("conversations"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := startFixtureBridge(t, cfg)
	replies := make(chan []byte, 64)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(replies)
		scanner := bufio.NewScanner(r.output)
		for scanner.Scan() {
			replies <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	t.Cleanup(func() {
		r.cancel()
		r.output.Close()
		select {
		case <-readDone:
		case <-time.After(30 * time.Second):
			t.Error("client reader did not stop")
		}
	})
	requestID := 0
	notices := []object{}
	ask := func(method string, params object, wantError bool) object {
		t.Helper()
		requestID++
		id := raw(requestID)
		request := object{"id": id, "method": raw(method), "params": raw(params)}
		if _, err := fmt.Fprintln(r.input, string(raw(request))); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case line, ok := <-replies:
				if !ok {
					t.Fatal("bridge ended before response")
				}
				reply, err := parseObject(line)
				if err != nil {
					t.Fatal(err)
				}
				if exists(reply, "method") {
					notices = append(notices, reply)
					continue
				}
				if string(reply["id"]) != string(id) {
					t.Fatalf("response ID %s, want %s", reply["id"], id)
				}
				if exists(reply, "error") != wantError {
					t.Fatalf("unexpected response: %s", line)
				}
				if wantError {
					return reply
				}
				result, err := child(reply, "result")
				if err != nil {
					t.Fatal(err)
				}
				return result
			case <-time.After(30 * time.Second):
				t.Fatal("bridge conversation response timed out")
			}
		}
	}
	ask("initialize", object{"clientInfo": raw(map[string]string{"name": "integrated-fixture", "version": "1"})}, false)
	if _, err := fmt.Fprintln(r.input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	models := ask("model/list", object{}, false)
	var listed []object
	if err := json.Unmarshal(models["data"], &listed); err != nil || len(listed) != 2 {
		t.Fatalf("model list %s: %v", models["data"], err)
	}
	for _, b := range cfg.Backends {
		expected := alias(b.ID, b.ID+"_model")
		found := false
		for _, m := range listed {
			if text(m, "model") == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing model %s", expected)
		}
	}
	for i, b := range cfg.Backends {
		model := alias(b.ID, b.ID+"_model")
		started := ask("thread/start", object{"model": raw(model)}, false)
		thread, _ := child(started, "thread")
		threadID := text(thread, "id")
		if threadID != b.Provider+"-thread" || text(started, "model") != model || text(started, "modelProvider") != b.Provider {
			t.Fatalf("wrong backend thread: %s", raw(started))
		}
		for turn := 1; turn <= 2; turn++ {
			input := []map[string]any{{"type": "text", "text": fmt.Sprintf("private-%s-input-%d", b.ID, turn), "text_elements": []any{}}}
			result := ask("turn/start", object{"threadId": raw(threadID), "input": raw(input)}, false)
			returned, _ := child(result, "turn")
			expected := fmt.Sprintf("%s-turn-%d", b.Provider, turn)
			if text(returned, "id") != expected {
				t.Fatalf("wrong turn: %s", raw(result))
			}
			if turn == 2 {
				ask("turn/interrupt", object{"threadId": raw(threadID), "turnId": raw(expected)}, false)
			}
		}
		foreign := cfg.Backends[1-i]
		ask("turn/start", object{"threadId": raw(threadID), "model": raw(alias(foreign.ID, foreign.ID+"_model")), "input": raw([]map[string]any{{"type": "text", "text": "forbidden-cross-provider-input", "text_elements": []any{}}})}, true)
	}
	// Backend notifications and local rejections may interleave. Wait on the
	// expected events, not on elapsed time or the local rejection's ordering.
	for len(notices) < 4 {
		select {
		case line, ok := <-replies:
			if !ok {
				t.Fatal("bridge ended before completion notices")
			}
			notice, err := parseObject(line)
			if err != nil || text(notice, "method") != "turn/completed" {
				t.Fatalf("unexpected completion message: %s, %v", line, err)
			}
			notices = append(notices, notice)
		case <-time.After(30 * time.Second):
			t.Fatal("completion notices did not arrive")
		}
	}
	if len(notices) != 4 {
		t.Fatalf("completion notices=%d, want four", len(notices))
	}
	statuses := map[string]int{}
	for _, notice := range notices {
		p, _ := child(notice, "params")
		turn, _ := child(p, "turn")
		statuses[text(turn, "status")]++
	}
	if statuses["completed"] != 2 || statuses["interrupted"] != 2 {
		t.Fatalf("completion statuses: %v", statuses)
	}
	r.input.Close()
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	for i, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-requests"))
		if err != nil {
			t.Fatal(err)
		}
		log := string(data)
		foreign := cfg.Backends[1-i]
		if strings.Contains(log, "private-"+foreign.ID+"-input") || strings.Contains(log, "forbidden-cross-provider-input") || strings.Contains(log, foreign.Provider+"-thread") {
			t.Fatalf("backend %s received foreign input: %s", b.ID, log)
		}
		for turn := 1; turn <= 2; turn++ {
			if !strings.Contains(log, fmt.Sprintf("private-%s-input-%d", b.ID, turn)) {
				t.Fatalf("backend %s missed turn %d", b.ID, turn)
			}
		}
		counts := map[string]int{}
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			m, err := parseObject([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			method := text(m, "method")
			counts[method]++
			if method == "thread/start" {
				params, err := child(m, "params")
				if err != nil || text(params, "model") != b.ID+"_model" || text(params, "modelProvider") != b.Provider {
					t.Fatalf("backend %s did not receive its pinned model/provider: %s", b.ID, line)
				}
			}
		}
		if counts["thread/start"] != 1 || counts["turn/start"] != 2 || counts["turn/interrupt"] != 1 || counts["model/list"] != 0 {
			t.Fatalf("backend %s request counts: %v", b.ID, counts)
		}
	}
}

func TestRunSharedWorkingDirectoryKeepsHomesAndRealEnvironmentInfo(t *testing.T) {
	for _, differingShell := range []bool{false, true} {
		t.Run(strconv.FormatBool(differingShell), func(t *testing.T) {
			cfg := fixtureConfig(t)
			cfg.Backends[1].Provider = "custom_provider"
			cfg.WorkingDirectory = filepath.Join(filepath.Dir(cfg.Backends[0].Home), "shared-start")
			if err := os.Mkdir(cfg.WorkingDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			if differingShell {
				if err := os.WriteFile(filepath.Join(cfg.Backends[1].Home, "fixture-shell"), []byte("different-shell"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			parentCWD, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			r := startFixtureBridge(t, cfg)
			lines := make(chan []byte, 8)
			go func() {
				defer close(lines)
				scanner := bufio.NewScanner(r.output)
				for scanner.Scan() {
					lines <- append([]byte(nil), scanner.Bytes()...)
				}
			}()
			ask := func(message string) object {
				t.Helper()
				if _, err := fmt.Fprintln(r.input, message); err != nil {
					t.Fatal(err)
				}
				select {
				case line := <-lines:
					m, err := parseObject(line)
					if err != nil {
						t.Fatal(err)
					}
					return m
				case <-time.After(30 * time.Second):
					t.Fatal("shared directory fixture timed out")
					return nil
				}
			}
			init := ask(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"},"capabilities":{"experimentalApi":true}}}`)
			if !exists(init, "result") {
				t.Fatal("shared working directory initialization failed", init)
			}
			fmt.Fprintln(r.input, `{"method":"initialized"}`)
			response := ask(`{"id":2,"method":"environment/info","params":{"environmentId":"local"}}`)
			if exists(response, "error") != differingShell {
				t.Fatalf("real environment equality changed: %s", raw(response))
			}
			if !differingShell {
				info, _ := child(response, "result")
				if text(info, "cwd") == "" || !strings.HasSuffix(text(info, "cwd"), "/shared-start") {
					t.Fatal("response did not report actual shared cwd", info)
				}
			}
			for _, b := range cfg.Backends {
				data, err := os.ReadFile(filepath.Join(b.Home, "fixture-report"))
				if err != nil {
					t.Fatal(err)
				}
				var report fixtureReport
				if err := json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				dir, _ := filepath.EvalSymlinks(report.Directory)
				shared, _ := filepath.EvalSymlinks(cfg.WorkingDirectory)
				if dir != shared {
					t.Fatalf("child cwd %s, want shared %s", dir, shared)
				}
				env := strings.Join(report.Environment, "\n")
				if !strings.Contains(env, "HOME="+b.Home) || !strings.Contains(env, "CODEX_HOME="+b.CodexHome) {
					t.Fatal("shared cwd changed private homes")
				}
				expectedArgs := []string{"app-server", "-c", `model_provider="` + b.Provider + `"`}
				if !reflect.DeepEqual(report.Args, expectedArgs) {
					t.Fatalf("startup added a config or permission override: %q", report.Args)
				}
			}
			current, err := os.Getwd()
			if err != nil || current != parentCWD {
				t.Fatal("bridge changed parent working directory")
			}
			r.input.Close()
			if err := r.wait(t); err != nil {
				t.Fatal(err)
			}
			if _, open := <-lines; open {
				t.Fatal("unexpected extra fixture message")
			}
		})
	}
}
