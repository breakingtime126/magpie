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
	noShell(t)
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "/bin/sh", "-c", `echo ready; : > "$1"; exec sleep 300`, "sh", ready)
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
