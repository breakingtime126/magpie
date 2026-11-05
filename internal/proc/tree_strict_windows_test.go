package proc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The fixture has no account or network access. Its only files are supplied
// temporary paths. Its descendants do not exit on EOF, so only the job ends
// them during these tests. The leaf deadline is a last-resort test-runner guard.
func TestStrictTreeFixture(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-3] != "strict-tree-fixture" {
		return
	}
	mode, marker := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if mode == "leaf" {
		fmt.Println(os.Getpid())
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	if err := os.WriteFile(marker, []byte("started"), 0600); err != nil {
		os.Exit(2)
	}
	if mode == "tree" {
		leaf := exec.Command(os.Args[0], "-test.run=^TestStrictTreeFixture$", "strict-tree-fixture", "leaf", marker)
		leaf.Stdout = os.Stdout
		if err := leaf.Start(); err != nil {
			os.Exit(3)
		}
	}
	if mode == "marker" {
		fmt.Println("ready")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func strictFixture(t *testing.T, mode string) (*exec.Cmd, io.WriteCloser, *bufio.Reader, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStrictTreeFixture$", "strict-tree-fixture", mode, marker)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = stdout.Close()
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, stdin, bufio.NewReader(stdout), marker
}

func strictReadLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	line, err := strictFixtureLine(reader)
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// Return errors rather than calling Fatal inside a WithHandle callback, so
// startup can run its cleanup before the test reports a failed fixture.
func strictFixtureLine(reader *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			return "", got.err
		}
		return got.line[:len(got.line)-1], nil
	case <-time.After(30 * time.Second):
		return "", errors.New("owned fixture did not report readiness")
	}
}

func strictDuplicateProcess(process windows.Handle) (windows.Handle, error) {
	var duplicate windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), process, windows.CurrentProcess(), &duplicate,
		0, false, windows.DUPLICATE_SAME_ACCESS)
	return duplicate, err
}

func strictAssertExited(t *testing.T, process windows.Handle) {
	t.Helper()
	state, err := windows.WaitForSingleObject(process, 30000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("owned process survived cleanup: state=%d err=%v", state, err)
	}
}

func TestStartTreeStrictAssignedBeforeExecution(t *testing.T) {
	cmd, stdin, reader, marker := strictFixture(t, "marker")
	assigned := false
	tree, err := startTreeStrict(cmd, func(job, process windows.Handle) error {
		thread, err := ownedInitialThread(process)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(thread)
		// This samples the OS suspend count, not elapsed time. With the
		// CREATE_SUSPENDED mutation removed it cannot report one.
		previous, _, suspendErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("SuspendThread").Call(uintptr(thread))
		if uint32(previous) == ^uint32(0) {
			return suspendErr
		}
		if previous != 1 {
			return fmt.Errorf("child was executable before job assignment: suspend count %d", previous)
		}
		if previous, err := windows.ResumeThread(thread); err != nil || previous != 2 {
			return fmt.Errorf("restore initial suspension: count=%d err=%v", previous, err)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("child executed before job assignment: %v", err)
		}
		if err := windows.AssignProcessToJobObject(job, process); err != nil {
			return err
		}
		assigned = true
		return nil
	}, resumeOwnedThread)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tree.Kill()
		_ = cmd.Process.Kill()
		_ = tree.Wait()
	})
	if !assigned || strictReadLine(t, reader) != "ready" {
		t.Fatal("child did not run after assignment")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
}

func TestStartTreeStrictCleansDescendant(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop=%v", stop), func(t *testing.T) {
			cmd, stdin, reader, _ := strictFixture(t, "tree")
			tree, err := StartTreeStrict(cmd)
			if err != nil {
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				tree.Kill()
				_ = cmd.Process.Kill()
				if !waited {
					_ = tree.Wait()
				}
			})
			pid, err := strconv.ParseUint(strictReadLine(t, reader), 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			// The fixture is alive and waiting. Capture a stable wait handle
			// before letting its parent exit, never poll a possibly reused PID.
			leaf, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = windows.TerminateProcess(leaf, 1)
				_, _ = windows.WaitForSingleObject(leaf, 30000)
				_ = windows.CloseHandle(leaf)
			}()
			if stop {
				<-tree.Stop(time.Hour)
			} else {
				_ = stdin.Close()
			}
			err = tree.Wait()
			waited = true
			if !stop && err != nil {
				t.Fatal(err)
			}
			strictAssertExited(t, leaf)
			if tree.job != 0 {
				t.Fatal("job handle retained after Wait")
			}
		})
	}
}

func TestStartTreeStrictFailureReapsOwnedChild(t *testing.T) {
	for _, stage := range []string{"assign", "resume"} {
		t.Run(stage, func(t *testing.T) {
			cmd, _, _, marker := strictFixture(t, "marker")
			injected := errors.New("injected " + stage + " failure")
			var process windows.Handle
			assign := func(job, child windows.Handle) error {
				var err error
				process, err = strictDuplicateProcess(child)
				if err != nil {
					return err
				}
				if stage == "assign" {
					return injected
				}
				return windows.AssignProcessToJobObject(job, child)
			}
			tree, err := startTreeStrict(cmd, assign, func(windows.Handle) error { return injected })
			if process != 0 {
				defer func() {
					_ = windows.TerminateProcess(process, 1)
					_, _ = windows.WaitForSingleObject(process, 30000)
					_ = windows.CloseHandle(process)
					if cmd.ProcessState == nil {
						_ = cmd.Wait()
					}
				}()
			}
			if tree != nil {
				defer tree.sweep(cmd)
			}
			if tree != nil || !errors.Is(err, injected) {
				t.Fatalf("unexpected startup result: tree=%v err=%v", tree, err)
			}
			strictAssertExited(t, process)
			if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
				t.Fatal("failed child was not reaped")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed startup executed fixture: %v", err)
			}
		})
	}
}

func TestStartTreeStrictRejectsConflictingFlags(t *testing.T) {
	for _, flag := range []uint32{windows.CREATE_SUSPENDED, windows.DEBUG_PROCESS, windows.DEBUG_ONLY_THIS_PROCESS, windows.CREATE_BREAKAWAY_FROM_JOB} {
		cmd, _, _, _ := strictFixture(t, "marker")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flag}
		tree, err := StartTreeStrict(cmd)
		if tree != nil {
			t.Cleanup(func() {
				tree.Kill()
				tree.sweep(cmd)
			})
		}
		if tree != nil || err == nil || cmd.Process != nil {
			t.Fatalf("conflicting flag %#x started a process: tree=%v err=%v", flag, tree, err)
		}
	}
}

func TestStartTreeStrictPartialResumeFailureEndsDescendant(t *testing.T) {
	cmd, _, reader, _ := strictFixture(t, "tree")
	injected := errors.New("injected failure after resume")
	var process, leaf windows.Handle
	defer func() {
		for _, handle := range []windows.Handle{leaf, process} {
			if handle != 0 {
				_ = windows.TerminateProcess(handle, 1)
				_, _ = windows.WaitForSingleObject(handle, 30000)
				_ = windows.CloseHandle(handle)
			}
		}
	}()
	assign := func(job, child windows.Handle) error {
		var err error
		process, err = strictDuplicateProcess(child)
		if err != nil {
			return err
		}
		return windows.AssignProcessToJobObject(job, child)
	}
	resume := func(child windows.Handle) error {
		if err := resumeOwnedThread(child); err != nil {
			return err
		}
		line, err := strictFixtureLine(reader)
		if err != nil {
			return err
		}
		pid, err := strconv.ParseUint(line, 10, 32)
		if err != nil {
			return err
		}
		leaf, err = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
		if err != nil {
			return err
		}
		return injected
	}
	tree, err := startTreeStrict(cmd, assign, resume)
	if tree != nil {
		defer tree.sweep(cmd)
	}
	if tree != nil || !errors.Is(err, injected) {
		t.Fatalf("unexpected startup result: tree=%v err=%v", tree, err)
	}
	strictAssertExited(t, process)
	strictAssertExited(t, leaf)
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("partially resumed child was not reaped")
	}
}
