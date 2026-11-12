package nativepreview

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/yetone/magpie/internal/proc"
	"golang.org/x/sys/windows"
)

// The authored test child only writes a sentinel and waits for stdin EOF.
// It does not invoke any native runtime, package query, account or network.
func TestInstalledQueryRootHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "query-root-helper" {
		return
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

type queryHeldWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *queryHeldWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestInstalledQueryRootWindowsRetainedAfterReap(t *testing.T) {
	// The writer barrier, not a timing assumption, keeps Cmd.Wait pending after
	// the OS root wait releases Go's original handle. Repeat the exit boundary.
	for iteration := 0; iteration < 8; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			cmd := proc.Command(os.Args[0], "-test.run=^TestInstalledQueryRootHelper$", "query-root-helper")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			writer := &queryHeldWriter{entered: make(chan struct{}), release: make(chan struct{})}
			var unblock sync.Once
			release := func() { unblock.Do(func() { close(writer.release) }) }
			cmd.Stdout = writer
			tree, err := proc.StartTreeStrict(cmd)
			if err != nil {
				release()
				t.Fatal(err)
			}
			observation := observeInstalledQueryRoot(cmd)
			retained := retainInstalledQueryRoot(cmd.Process)
			done := make(chan error, 1)
			go func() { done <- tree.Wait() }()
			waited := false
			t.Cleanup(func() {
				release()
				if !waited {
					<-tree.Stop(time.Second)
					select {
					case <-done:
						waited = true
					case <-time.After(30 * time.Second):
						t.Error("owned test child wait not completed")
					}
				}
				if waited {
					if observation.close != nil {
						observation.close()
					}
					if retained != 0 {
						_ = windows.CloseHandle(retained)
					}
				}
			})
			if observation.state == nil || retained == 0 {
				t.Fatal("owned observation unavailable")
			}
			var flags uint32
			getFlags := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation")
			if err := getFlags.Find(); err != nil {
				t.Fatal(err)
			}
			ok, _, flagErr := getFlags.Call(uintptr(retained), uintptr(unsafe.Pointer(&flags)))
			if ok == 0 || flags&windows.HANDLE_FLAG_INHERIT != 0 {
				t.Fatal("observation handle inheritable", flagErr)
			}
			// Desired SYNCHRONIZE access must not confer termination authority.
			if err := windows.TerminateProcess(retained, 1); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Fatal("observer acquired termination authority", err)
			}
			select {
			case <-writer.entered:
			case <-time.After(30 * time.Second):
				t.Fatal("authored child readiness missing")
			}
			if observation.state() != queryRootRunning {
				t.Fatal("running root not observed")
			}
			if err := stdin.Close(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				// Failure establishes that Go released its original handle;
				// no error-text mapping and no unsynchronized ProcessState read.
				if err := cmd.Process.WithHandle(func(uintptr) {}); err != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("original root handle not released")
				}
				time.Sleep(time.Millisecond)
			}
			if observation.state() != queryRootExited {
				t.Fatal("retained observer lost exited root during output drain")
			}
			select {
			case <-done:
				waited = true
				t.Fatal("held stdout did not hold Cmd.Wait")
			default:
			}
			release()
			select {
			case err := <-done:
				waited = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("released stdout did not complete original Wait")
			}
			if observation.state() != queryRootExited {
				t.Fatal("observer changed after original Wait")
			}
			observation.close()
			if observation.state() != queryRootUnknown {
				t.Fatal("closed observation reused handle")
			}
		})
	}
}

func TestInstalledQueryRootWindowsUnknown(t *testing.T) {
	if got := observeInstalledQueryRoot(nil); got.state != nil || got.close != nil {
		t.Fatal("nil command fabricated observation")
	}
	for _, tc := range []struct {
		state uint32
		err   error
		want  queryRootState
	}{
		{windows.WAIT_OBJECT_0, nil, queryRootExited},
		{uint32(windows.WAIT_TIMEOUT), nil, queryRootRunning},
		{windows.WAIT_FAILED, nil, queryRootUnknown},
		{windows.WAIT_ABANDONED, nil, queryRootUnknown},
		{windows.WAIT_OBJECT_0, errors.New("PRIVATE-CANARY"), queryRootUnknown},
	} {
		if got := classifyQueryRootWait(tc.state, tc.err); got != tc.want {
			t.Fatal("wait failure/result mapping", got)
		}
	}
}
