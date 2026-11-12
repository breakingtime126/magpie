package nativepreview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativeprepdiag"
	"github.com/yetone/magpie/internal/proc"
)

func TestInstalledQueryProgressBeforeStop(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		cancelled, release := make(chan struct{}), make(chan struct{})
		var lines []string
		ctx, finish := nativeprepdiag.Begin(queryFixtureContext{Context: context.Background(), done: cancelled, cause: cause}, func(s string) { lines = append(lines, s) })
		cmd := &exec.Cmd{Path: "never-started"}
		writes := func(s string) {
			if cmd.Stderr != nil {
				_, _ = cmd.Stderr.Write([]byte(s))
			}
		}
		found, err := runInstalledQuery(ctx, cmd, func(*exec.Cmd) (queryProcess, error) {
			return queryFixtureProcess{wait: func() error {
				writes("PRIVATE-CANARY\nMAGPIE_QUERY_V1:entry\r\nMAGPIE_QUERY_V1:setup\r\nMAGPIE_QUERY_V1:management\r\nMAGPIE_QUERY_V1:utility\r\nMAGPIE_QUERY_V1:appx-import\r\nMAGPIE_QUERY_V1:appx-return\r\nMAGPIE_QUERY_V1:appx\r\n")
				close(cancelled)
				<-release
				return nil
			}, stop: func(grace time.Duration) <-chan struct{} {
				if grace != time.Second {
					t.Error("grace changed")
				}
				writes("MAGPIE_QUERY_V1:packages\n")
				close(release)
				done := make(chan struct{})
				close(done)
				return done
			}}, nil
		}, nil)
		finish(false)
		text := strings.Join(lines, "\n")
		if !strings.Contains(text, "stage=query-progress-appx outcome=completed") {
			t.Fatal("missing pre-Stop fixed progress snapshot", text)
		}
		if strings.Contains(text, "PRIVATE") || strings.Contains(text, "query-progress-packages") || found != "" || err != ErrUnavailable {
			t.Fatal("progress changed result or leaked", text)
		}
		want := "query-cancelled"
		if cause == context.DeadlineExceeded {
			want = "query-deadline"
		}
		for _, line := range lines {
			if strings.HasSuffix(line, "outcome=failed") {
				if !strings.Contains(line, "stage="+want+" ") {
					t.Fatal("first failure changed", line)
				}
				break
			}
		}
	}
}

func TestInstalledQueryProgressParser(t *testing.T) {
	prefix := "MAGPIE_QUERY_V1:"
	for _, tc := range []struct{ name, input, want string }{
		{"none", "PRIVATE-PATH-KEY\n", "unknown"},
		{"entry", prefix + "entry\n", "entry"},
		{"crlf", prefix + "entry\r\n" + prefix + "setup\r\n" + prefix + "management\r\n" + prefix + "utility\r\n" + prefix + "appx-import\r\n" + prefix + "appx-return\r\n" + prefix + "appx\r\n", "appx"},
		{"partial", prefix + "entry", "unknown"},
		{"next-partial", prefix + "entry\n" + prefix + "app", "unknown"},
		{"old-appx-first", prefix + "entry\n" + prefix + "setup\n" + prefix + "appx-import\n", "unknown"},
		{"skipped", prefix + "appx\n", "unknown"},
		{"duplicate", prefix + "entry\n" + prefix + "entry\n", "unknown"},
		{"bad-suffix", prefix + "entry\n" + prefix + "appxPRIVATE\n", "unknown"},
		{"unknown-marker", prefix + "entry\n" + prefix + "bogus\n", "unknown"},
		{"bad-cr", prefix + "entry\rX\n", "unknown"},
		{"noise", strings.Repeat("PRIVATE", 10000) + "\n" + prefix + "entry\nSECRET\n", "entry"},
		{"embedded", "SECRET" + prefix + "entry\n", "unknown"},
		{"invalid-stays", prefix + "appx\n" + prefix + "entry\n", "unknown"},
		{"all", prefix + "entry\n" + prefix + "setup\n" + prefix + "management\n" + prefix + "utility\n" + prefix + "appx-import\n" + prefix + "appx-return\n" + prefix + "appx\n" + prefix + "packages\n" + prefix + "path\n" + prefix + "json\n", "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{1, 2, 7, 256, len(tc.input) + 1} {
				p := &queryProgress{}
				for data := []byte(tc.input); len(data) > 0; {
					n := min(size, len(data))
					written, err := p.Write(data[:n])
					if written != n || err != nil {
						t.Fatal("stderr writer stopped consuming")
					}
					data = data[n:]
				}
				var lines []string
				ctx, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s) })
				p.snapshot(ctx)
				finish(false)
				text := strings.Join(lines, "\n")
				if !strings.Contains(text, "stage=query-progress-"+tc.want+" outcome=completed") || strings.Contains(text, "PRIVATE") || strings.Contains(text, "SECRET") {
					t.Fatalf("chunk=%d: wrong or unredacted snapshot: %s", size, text)
				}
			}
		})
	}
}

func TestInstalledQueryProgressConcurrentSnapshot(t *testing.T) {
	p := &queryProgress{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, token := range queryProgressMarkers {
			for _, b := range []byte(token + "\n") {
				_, _ = p.Write([]byte{b})
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		p.snapshot(context.Background())
	}
	<-done
	var lines []string
	ctx, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s) })
	p.snapshot(ctx)
	finish(false)
	if !strings.Contains(strings.Join(lines, "\n"), "query-progress-json outcome=completed") {
		t.Fatal("completed stream lost")
	}
}

// Authored child only: exercises the real os/exec stderr pipe on Windows and
// Linux, with no installed-package lookup, account, native runtime or network.
func TestInstalledQueryProgressPipeHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "query-progress-helper" {
		return
	}
	fmt.Fprintln(os.Stderr, strings.Repeat("PRIVATE-CANARY", 4096))
	for _, token := range queryProgressMarkers {
		fmt.Fprint(os.Stderr, token+"\r\n")
	}
	fmt.Fprint(os.Stdout, "fixed-json-output")
	os.Exit(0)
}
func TestInstalledQueryProgressPipeDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, os.Args[0], "-test.run=^TestInstalledQueryProgressPipeHelper$", "query-progress-helper")
	p := &queryProgress{}
	var output bytes.Buffer
	cmd.Stderr = p
	cmd.Stdout = &output
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err = tree.Wait(); err != nil {
		t.Fatal(err)
	}
	var lines []string
	observed, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s) })
	p.snapshot(observed)
	finish(false)
	text := strings.Join(lines, "\n")
	if output.String() != "fixed-json-output" || !strings.Contains(text, "query-progress-json outcome=completed") || strings.Contains(text, "PRIVATE") {
		t.Fatal("pipe corrupted output or progress", text)
	}
}

func TestInstalledQueryProgressCancelPipeHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "query-progress-cancel-helper" {
		return
	}
	fmt.Fprint(os.Stderr, "MAGPIE_QUERY_V1:entry\r\n")
	// Parent deliberately retains stdin. Root retains stderr until strict Stop.
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

type queryCancelWriter struct {
	writer io.Writer
	cancel context.CancelFunc
	once   sync.Once
}

func (w *queryCancelWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	// Marker may arrive in multiple reads. Cancellation occurs only once the
	// underlying bounded parser has received its complete first line.
	progress := w.writer.(*queryProgress)
	progress.mu.Lock()
	ready := progress.stage == 1
	progress.mu.Unlock()
	if ready {
		w.once.Do(w.cancel)
	}
	return n, err
}
func TestInstalledQueryProgressCancellationDrainsPipe(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	var lines []string
	ctx, finish := nativeprepdiag.Begin(parent, func(s string) { lines = append(lines, s) })
	cmd := proc.Command(os.Args[0], "-test.run=^TestInstalledQueryProgressCancelPipeHelper$", "query-progress-cancel-helper")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var mu sync.Mutex
	var calls []string
	note := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	// Watchdog only reports a stuck fixture; it is not the cancellation stimulus.
	var watchdogFired atomic.Bool
	watchdog := time.AfterFunc(30*time.Second, func() { watchdogFired.Store(true); cancel(); _ = stdin.Close() })
	defer watchdog.Stop()
	found, err := runInstalledQuery(ctx, cmd, func(command *exec.Cmd) (queryProcess, error) {
		command.Stderr = &queryCancelWriter{writer: command.Stderr, cancel: cancel}
		tree, startErr := proc.StartTreeStrict(command)
		if startErr != nil {
			return nil, startErr
		}
		return queryFixtureProcess{wait: func() error { note("wait"); err := tree.Wait(); note("drained"); return err }, stop: func(grace time.Duration) <-chan struct{} {
			note("stop")
			if grace != time.Second {
				t.Error("grace changed")
			}
			return tree.Stop(grace)
		}}, nil
	}, nil)
	finish(false)
	if found != "" || err != ErrUnavailable {
		t.Fatal("cancelled pipe accepted output")
	}
	if !watchdog.Stop() || watchdogFired.Load() {
		t.Fatal("watchdog fired instead of marker-driven cancellation")
	}
	order := strings.Join(calls, ",")
	if order != "wait,stop,drained" && order != "stop,wait,drained" {
		t.Fatal("sole Wait/Stop/drain order changed", calls)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "query-progress-entry outcome=completed") || !strings.Contains(text, "query-cancelled outcome=failed") {
		t.Fatal("marker-triggered cancellation missing", text)
	}
}

func TestInstalledQueryProgressAppxBoundaries(t *testing.T) {
	// Independent literals pin all ten boundaries, including mask bits 8 and 9.
	names := []string{"entry", "setup", "management", "utility", "appx-import", "appx-return", "appx", "packages", "path", "json"}
	var prefix string
	for _, name := range names {
		line := "MAGPIE_QUERY_V1:" + name + "\r\n"
		for _, suffix := range []string{line, strings.TrimSuffix(line, "\r\n"), line[:len(line)-2] + "PRIVATE\n", line + line} {
			for _, size := range []int{1, 2, 17, 256} {
				p := &queryProgress{}
				data := []byte(prefix + suffix)
				for len(data) > 0 {
					n := min(size, len(data))
					_, _ = p.Write(data[:n])
					data = data[n:]
				}
				var lines []string
				ctx, finish := nativeprepdiag.Begin(context.Background(), func(s string) { lines = append(lines, s) })
				p.snapshot(ctx)
				finish(false)
				want := "unknown"
				if suffix == line {
					want = name
				}
				if !strings.Contains(strings.Join(lines, "\n"), "stage=query-progress-"+want+" outcome=completed") {
					t.Fatalf("boundary=%s chunk=%d want=%s: %v", name, size, want, lines)
				}
			}
		}
		prefix += line
	}
}
