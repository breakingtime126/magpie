//go:build !darwin || !cgo

package proc

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The ready file orders cancellation after Start, rather than relying on
// the scheduler to expose exec's killed-process error. The timer is only
// a watchdog; the child never exits on its own during a successful test.
func TestCombinedOutputOwnContextAfterStart(t *testing.T) {
	exe, exeErr := os.Executable()
	if exeErr != nil {
		t.Fatal(exeErr)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, exe, "-test.run=^TestCombinedOutputOwnCancelChild$", "-test.timeout=6m")
	// Keep testenv's sandbox marker and isolated homes, including TEMP/TMP
	// on Windows, while selecting only this child mode in the test binary.
	cmd.Env = append(os.Environ(), "MAGPIE_PROC_SLEEP=", "MAGPIE_PROC_RAN=",
		"MAGPIE_PROC_OWN_CANCEL_CHILD=1", "MAGPIE_PROC_OWN_CANCEL_READY="+ready)
	var out []byte
	var err error
	done := make(chan struct{})
	go func() {
		out, err = CombinedOutputOwn(ctx, cmd)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if _, statErr := os.Stat(ready); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal(statErr)
		}
		select {
		case <-done:
			t.Fatalf("command ended before cancellation: %q, %v", out, err)
		case <-deadline.C:
			t.Fatal("command did not become ready")
		case <-poll.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-deadline.C:
		t.Fatal("command did not finish after cancellation")
	}
	if err != context.Canceled {
		t.Fatalf("started command returned %v, want context.Canceled", err)
	}
	if string(out) != "ready\n" {
		t.Errorf("output %q, want ready line before cancellation", out)
	}
	if cmd.ProcessState == nil {
		t.Fatal("canceled command was not waited for")
	}
}

// This helper does nothing in an ordinary suite run. The explicitly
// selected child writes its output before readiness and starts no children.
func TestCombinedOutputOwnCancelChild(t *testing.T) {
	if os.Getenv("MAGPIE_PROC_OWN_CANCEL_CHILD") != "1" || len(os.Args) != 3 ||
		os.Args[1] != "-test.run=^TestCombinedOutputOwnCancelChild$" || os.Args[2] != "-test.timeout=6m" {
		return
	}
	ready := os.Getenv("MAGPIE_PROC_OWN_CANCEL_READY")
	if !filepath.IsAbs(ready) {
		t.Fatal("child readiness path must be absolute")
	}
	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Minute)
	t.Fatal("child was not canceled")
}

func TestCombinedOutputOwnKeepsCommandErrors(t *testing.T) {
	noShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := Command("/bin/sh", "-c", "exit 0")
	want := errors.New("command lookup failed")
	cmd.Err = want
	if _, err := CombinedOutputOwn(ctx, cmd); err != want {
		t.Fatalf("lookup error %v, want %v", err, want)
	}
	// A command that cannot start has no exit status to replace, even
	// when the supplied context is already canceled.
	cmd = Command("/bin/sh", "-c", "exit 0")
	cmd.Dir = filepath.Join(t.TempDir(), "missing")
	if _, err := CombinedOutputOwn(ctx, cmd); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("start error %v, want the missing-directory error", err)
	}
	cmd = CommandContext(context.Background(), "/bin/sh", "-c", "echo failed; exit 7")
	out, err := CombinedOutputOwn(context.Background(), cmd)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 || string(out) != "failed\n" {
		t.Fatalf("ordinary failure %q, %v, want output and exit status 7", out, err)
	}
}

func TestCombinedOutputOwnContextBeforeStart(t *testing.T) {
	noShell(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired} {
		cmd := CommandContext(ctx, "/bin/sh", "-c", "echo unexpected")
		out, err := CombinedOutputOwn(ctx, cmd)
		if err != ctx.Err() || len(out) != 0 || cmd.Process != nil {
			t.Errorf("ended context ran command: output %q, error %v, process %v", out, err, cmd.Process)
		}
	}
}

func TestCombinedOutputOwnKeepsReuseErrors(t *testing.T) {
	noShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, stdoutSet := range []bool{false, true} {
		cmd := Command("/bin/sh", "-c", "exit 0")
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		if cmd.Process == nil || cmd.ProcessState == nil {
			t.Fatal("first run did not start and wait for the command")
		}
		want := "exec: already started"
		if stdoutSet {
			cmd.Stdout = io.Discard
			want = "exec: Stdout already set"
		}
		if _, err := CombinedOutputOwn(ctx, cmd); err == nil || err.Error() != want {
			t.Errorf("reused command returned %v, want %s", err, want)
		}
	}
}
