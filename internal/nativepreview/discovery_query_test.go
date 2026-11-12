package nativepreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativeprepdiag"
	"github.com/yetone/magpie/internal/proc"
)

type queryFixtureProcess struct {
	wait func() error
	stop func(time.Duration) <-chan struct{}
}

func (p queryFixtureProcess) Wait() error                          { return p.wait() }
func (p queryFixtureProcess) Stop(d time.Duration) <-chan struct{} { return p.stop(d) }

type queryFixtureContext struct {
	context.Context
	done  chan struct{}
	cause error
}

func (c queryFixtureContext) Done() <-chan struct{} { return c.done }
func (c queryFixtureContext) Err() error {
	select {
	case <-c.done:
		return c.cause
	default:
		return nil
	}
}

// This helper exits only this authored Go test binary. It never invokes the
// package query, PowerShell, a native executable, or an old runtime fixture.
func TestInstalledQueryExitHelper(t *testing.T) {
	value := os.Getenv("MAGPIE_QUERY_EXIT_FIXTURE")
	if value == "" {
		return
	}
	code, err := strconv.Atoi(value)
	if err != nil || code < 2 || code > 7 {
		t.Fatal("invalid authored exit")
	}
	os.Exit(code)
}
func queryFixtureExit(t *testing.T, code int) error {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, executable, "-test.run=^TestInstalledQueryExitHelper$")
	cmd.Env = append(os.Environ(), fmt.Sprintf("MAGPIE_QUERY_EXIT_FIXTURE=%d", code))
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != code {
		t.Fatal("authored exit fixture failed", err)
	}
	return err
}
func TestInstalledQueryBranches(t *testing.T) {
	for _, kind := range []string{"success", "pre-cancel", "pre-deadline", "start", "cancel", "deadline", "exit-two", "exit-three", "exit-other", "wait-error", "overflow", "exit-overflow", "syntax", "decode", "identity"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			note := func(s string) { mu.Lock(); defer mu.Unlock(); calls = append(calls, s) }
			cancellation := make(chan struct{})
			waitRelease := make(chan struct{})
			cause := context.Canceled
			if kind == "deadline" || kind == "pre-deadline" {
				cause = context.DeadlineExceeded
			}
			parent := queryFixtureContext{Context: context.Background(), done: cancellation, cause: cause}
			if strings.HasPrefix(kind, "pre-") {
				close(cancellation)
			}
			path := filepath.Join(t.TempDir(), "never-executed.exe")
			data, _ := json.Marshal(map[string]any{"path": path, "family": "OpenAI.Codex_2p2nqsd0c76g0", "signatureKind": "Store"})
			if kind == "syntax" {
				data = []byte("SECRET-CANARY")
			}
			if kind == "decode" {
				data = []byte(`{"path":42,"family":"SECRET-CANARY"}`)
			}
			if kind == "identity" {
				data = []byte(`{"path":"relative-secret","family":"SECRET-CANARY","signatureKind":"Store"}`)
			}
			if kind == "overflow" || kind == "exit-overflow" {
				data = bytes.Repeat([]byte("X"), 16385)
			}
			var waitErr error
			switch kind {
			case "exit-two":
				waitErr = queryFixtureExit(t, 2)
			case "exit-three":
				waitErr = queryFixtureExit(t, 3)
			case "exit-other", "exit-overflow":
				waitErr = queryFixtureExit(t, 7)
			case "wait-error":
				waitErr = errors.New("SECRET-CANARY")
			}
			var records []string
			var replay bytes.Buffer
			logger := log.New(&replay, "", log.LstdFlags)
			ctx, finish := nativeprepdiag.Begin(parent, func(s string) { records = append(records, s); logger.Print(s) })
			cmd := &exec.Cmd{Path: "inert-never-started", Args: []string{"inert", "fixed-argument"}}
			start := func(got *exec.Cmd) (queryProcess, error) {
				note("start")
				if got != cmd || got.Env != nil || got.Dir != "" || got.Stderr != nil || !reflect.DeepEqual(got.Args, []string{"inert", "fixed-argument"}) {
					t.Error("command/environment changed")
				}
				if kind == "start" {
					return nil, errors.New("SECRET-CANARY")
				}
				return queryFixtureProcess{wait: func() error {
					note("wait")
					if kind == "cancel" || kind == "deadline" {
						close(cancellation)
						<-waitRelease
					}
					_, _ = got.Stdout.Write(data)
					note("wait-return")
					return waitErr
				}, stop: func(grace time.Duration) <-chan struct{} {
					note("stop")
					if grace != time.Second {
						t.Error("stop grace changed")
					}
					close(waitRelease)
					done := make(chan struct{})
					close(done)
					return done
				}}, nil
			}
			found, err := runInstalledQuery(ctx, cmd, start, nil)
			finish(err == nil)
			wantCalls := "start,wait,wait-return"
			if kind == "start" {
				wantCalls = "start"
			}
			if strings.HasPrefix(kind, "pre-") {
				wantCalls = ""
			}
			if kind == "cancel" || kind == "deadline" {
				wantCalls = "start,wait,stop,wait-return"
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatal("operation order/count changed", calls)
			}
			if kind == "success" {
				if err != nil || found != path {
					t.Fatal("success changed", err)
				}
			} else if err != ErrUnavailable || found != "" {
				t.Fatal("refusal changed", err)
			}
			stage := map[string]string{"pre-cancel": "query-cancelled", "pre-deadline": "query-deadline", "start": "query-start", "cancel": "query-cancelled", "deadline": "query-deadline", "exit-two": "query-exit-two", "exit-three": "query-exit-three", "exit-other": "query-exit-nonzero", "wait-error": "query-wait-failure", "overflow": "query-output-bound", "exit-overflow": "query-exit-nonzero", "syntax": "query-json", "decode": "query-json", "identity": "query-identity"}[kind]
			text := strings.Join(records, "\n")
			if stage != "" && !strings.Contains(text, "stage="+stage+" outcome=failed") {
				t.Fatal("wrong classification", text)
			}
			if kind == "exit-overflow" && strings.Contains(text, "stage=query-output-bound") {
				t.Fatal("output guard overtook wait error")
			}
			if strings.Contains(text, "SECRET") || strings.Contains(text, path) || strings.Contains(text, "relative-secret") {
				t.Fatal("data leaked")
			}
			var stack []string
			for _, record := range records {
				fields := strings.Fields(record)
				stage := fields[3]
				if fields[4] == "outcome=entered" {
					stack = append(stack, stage)
				} else {
					if len(stack) == 0 || stack[len(stack)-1] != stage {
						t.Fatal("not nested", records)
					}
					stack = stack[:len(stack)-1]
				}
			}
			if len(stack) != 0 || len(records) >= nativeprepdiag.MaxEvents {
				t.Fatal("unbounded/incomplete observation")
			}
			if kind == "success" && len(records) != 12 {
				t.Fatal("query event budget changed", len(records))
			}
			if kind == "wait-error" {
				t.Log("\n" + replay.String())
			}
		})
	}
}

// Pin the production query/constructor/owner wiring separately from inert
// process fixtures, which never invoke the Windows package query.
func TestInstalledQuerySourceContract(t *testing.T) {
	source, err := os.ReadFile("discovery_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "discovery_windows.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	queryFound := false
	calls := map[string]int{}
	ast.Inspect(file, func(node ast.Node) bool {
		if value, ok := node.(*ast.ValueSpec); ok && len(value.Names) == 1 && value.Names[0].Name == "installedQuery" {
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok {
				t.Fatal("query literal changed")
			}
			query, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(query))) != "22d3baf0d80dd8d5b7446e88414afa7d929c1b70dcfb89f98d943a9cc6c3140b" {
				t.Fatal("reviewed query bytes changed")
			}
			queryFound = true
		}
		if call, ok := node.(*ast.CallExpr); ok {
			var rendered bytes.Buffer
			if err := printer.Fprint(&rendered, token.NewFileSet(), call); err != nil {
				t.Fatal(err)
			}
			calls[rendered.String()]++
			if function, ok := call.Fun.(*ast.Ident); ok && function.Name == "runInstalledQuery" {
				if len(call.Args) != 4 {
					t.Fatal("missing production root observer")
				}
				observer, ok := call.Args[3].(*ast.Ident)
				if !ok || observer.Name != "observeInstalledQueryRoot" {
					t.Fatal("wrong production root observer")
				}
			}
		}
		if assign, ok := node.(*ast.AssignStmt); ok {
			for _, left := range assign.Lhs {
				if selector, ok := left.(*ast.SelectorExpr); ok {
					if base, ok := selector.X.(*ast.Ident); ok && base.Name == "command" {
						t.Fatal("production command fields changed")
					}
				}
			}
		}
		return true
	})
	if !queryFound {
		t.Fatal("missing query")
	}
	for _, call := range []string{`context.WithTimeout(parent, 10*time.Second)`, `os.Getenv("SystemRoot")`, `proc.CommandContext(context.WithoutCancel(ctx), powershell, "-NoProfile", "-NonInteractive", "-Command", installedQuery)`, `proc.StartTreeStrict(cmd)`} {
		if calls[call] != 1 {
			t.Fatal("production wiring changed", call, calls[call])
		}
	}
}

// A root snapshot adds no authority: all observations must still stop, fully
// wait and return the original refusal. The fixture injects cancellation, not
// elapsed-time thresholds, and never launches PowerShell or a native runtime.
func TestInstalledQueryRootObservation(t *testing.T) {
	for _, state := range []queryRootState{queryRootUnknown, queryRootRunning, queryRootExited, 255} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("state=%d/cause=%v", state, cause), func(t *testing.T) {
				var mu sync.Mutex
				var calls []string
				note := func(s string) { mu.Lock(); defer mu.Unlock(); calls = append(calls, s) }
				cancelled := make(chan struct{})
				release := make(chan struct{})
				parent := queryFixtureContext{Context: context.Background(), done: cancelled, cause: cause}
				var lines []string
				ctx, finish := nativeprepdiag.Begin(parent, func(s string) { lines = append(lines, s) })
				cmd := &exec.Cmd{Path: "never-started"}
				start := func(got *exec.Cmd) (queryProcess, error) {
					note("start")
					return queryFixtureProcess{wait: func() error {
						note("wait")
						close(cancelled)
						<-release
						note("wait-return")
						return errors.New("PRIVATE-CANARY")
					}, stop: func(grace time.Duration) <-chan struct{} {
						note("stop")
						if grace != time.Second {
							t.Error("stop grace changed")
						}
						close(release)
						done := make(chan struct{})
						close(done)
						return done
					}}, nil
				}
				observe := func(got *exec.Cmd) queryRootObservation {
					note("retain")
					if got != cmd {
						t.Error("observed foreign command")
					}
					return queryRootObservation{state: func() queryRootState { note("observe"); return state }, close: func() { note("close") }}
				}
				found, err := runInstalledQuery(ctx, cmd, start, observe)
				finish(err == nil)
				if found != "" || err != ErrUnavailable {
					t.Fatal("root observation changed refusal")
				}
				if strings.Join(calls, ",") != "start,retain,wait,observe,stop,wait-return,close" {
					t.Fatal("root snapshot lifecycle/order", calls)
				}
				want := "query-root-unknown"
				if state == queryRootRunning {
					want = "query-root-running"
				}
				if state == queryRootExited {
					want = "query-root-exited"
				}
				text := strings.Join(lines, "\n")
				if !strings.Contains(text, "stage="+want+" outcome=completed") {
					t.Fatal("missing fixed root observation", text)
				}
				firstFailure := "query-cancelled"
				if cause == context.DeadlineExceeded {
					firstFailure = "query-deadline"
				}
				for _, line := range lines {
					if strings.HasSuffix(line, "outcome=failed") {
						if !strings.Contains(line, "stage="+firstFailure+" ") {
							t.Fatal("observation obscured original failure", lines)
						}
						break
					}
				}
				if strings.Contains(text, "PRIVATE") || len(lines) != 10 {
					t.Fatal("root observation leaked or changed event budget", lines)
				}
			})
		}
	}
}

func TestInstalledQueryRootObservationOtherBranches(t *testing.T) {
	for _, kind := range []string{"success", "wait-error", "start-error", "pre-cancel", "capture-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			note := func(s string) { mu.Lock(); defer mu.Unlock(); calls = append(calls, s) }
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "pre-cancel" {
				cancel()
			}
			var lines []string
			ctx, finish := nativeprepdiag.Begin(parent, func(s string) { lines = append(lines, s) })
			cmd := &exec.Cmd{Path: "never-started"}
			path := filepath.Join(t.TempDir(), "never-executed.exe")
			data, _ := json.Marshal(map[string]any{"path": path, "family": "OpenAI.Codex_2p2nqsd0c76g0", "signatureKind": "Store"})
			release := make(chan struct{})
			start := func(got *exec.Cmd) (queryProcess, error) {
				note("start")
				if kind == "start-error" {
					return nil, errors.New("authored")
				}
				return queryFixtureProcess{wait: func() error {
					note("wait")
					if kind == "capture-unavailable" {
						cancel()
						<-release
					}
					_, _ = got.Stdout.Write(data)
					note("wait-return")
					if kind == "wait-error" {
						return errors.New("authored")
					}
					return nil
				}, stop: func(time.Duration) <-chan struct{} {
					note("stop")
					close(release)
					done := make(chan struct{})
					close(done)
					return done
				}}, nil
			}
			observe := func(*exec.Cmd) queryRootObservation {
				note("retain")
				if kind == "capture-unavailable" {
					return queryRootObservation{}
				}
				return queryRootObservation{state: func() queryRootState { t.Error("unexpected snapshot"); return queryRootRunning }, close: func() { note("close") }}
			}
			found, err := runInstalledQuery(ctx, cmd, start, observe)
			finish(err == nil)
			want := "start,retain,wait,wait-return,close"
			switch kind {
			case "start-error":
				want = "start"
			case "pre-cancel":
				want = ""
			case "capture-unavailable":
				want = "start,retain,wait,stop,wait-return"
			}
			if strings.Join(calls, ",") != want {
				t.Fatal("other branch observation lifecycle", calls)
			}
			if kind == "success" {
				if err != nil || found != path {
					t.Fatal("success changed", err)
				}
			} else if err != ErrUnavailable || found != "" {
				t.Fatal("refusal changed", err)
			}
			if kind == "capture-unavailable" && !strings.Contains(strings.Join(lines, "\n"), "stage=query-root-unknown outcome=completed") {
				t.Fatal("capture failure was not unknown", lines)
			}
		})
	}
}
