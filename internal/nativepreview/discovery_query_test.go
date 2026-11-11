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
			found, err := runInstalledQuery(ctx, cmd, start)
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
			if fmt.Sprintf("%x", sha256.Sum256([]byte(query))) != "481e415b2d3e6c13acfd8f93b430299701a51be262f55ee10cbccb027e8c607e" {
				t.Fatal("original query bytes changed")
			}
			queryFound = true
		}
		if call, ok := node.(*ast.CallExpr); ok {
			var rendered bytes.Buffer
			if err := printer.Fprint(&rendered, token.NewFileSet(), call); err != nil {
				t.Fatal(err)
			}
			calls[rendered.String()]++
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
