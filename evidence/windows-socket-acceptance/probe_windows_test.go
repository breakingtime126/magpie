//go:build windows

// Synthetic experiment, not a production implementation or native acceptance result.
package socketacceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const prefix = "MAGPIE_SOCKET_PROBE_"

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var getHandleInformation = kernel32.NewProc("GetHandleInformation")
var isProcessInJob = kernel32.NewProc("IsProcessInJob")

func childInJob(child, job windows.Handle) (bool, error) {
	var v int32
	r, _, e := isProcessInJob.Call(uintptr(child), uintptr(job), uintptr(unsafe.Pointer(&v)))
	if r == 0 {
		return false, e
	}
	return v != 0, nil
}

func flags(h syscall.Handle) (uint32, error) {
	var v uint32
	r, _, e := getHandleInformation.Call(uintptr(h), uintptr(unsafe.Pointer(&v)))
	if r == 0 {
		return 0, e
	}
	return v, nil
}

// Masked restoration preserves every unrelated flag and the original inheritance bit.
// Injection tests exercise the startup-failure and restoration-error paths.
func inherit(start func() error, get func() (uint32, error), set func(uint32) error) error {
	original, e := get()
	if e != nil {
		return e
	}
	if e = set(syscall.HANDLE_FLAG_INHERIT); e != nil {
		return e
	}
	startErr := start()
	return errors.Join(startErr, set(original&syscall.HANDLE_FLAG_INHERIT))
}
func startWithOriginal(l *net.TCPListener, c *exec.Cmd) error {
	raw, e := l.SyscallConn()
	if e != nil {
		return e
	}
	var op error
	e = raw.Control(func(v uintptr) {
		h := syscall.Handle(v)
		c.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{h}}
		op = inherit(c.Start, func() (uint32, error) { return flags(h) }, func(f uint32) error { return syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, f) })
	})
	return errors.Join(e, op)
}
func listen(address string, option int) (net.Listener, error) {
	lc := net.ListenConfig{}
	if option != 0 {
		lc.Control = func(_, _ string, raw syscall.RawConn) error {
			var op error
			e := raw.Control(func(v uintptr) { op = syscall.SetsockoptInt(syscall.Handle(v), syscall.SOL_SOCKET, option, 1) })
			return errors.Join(e, op)
		}
	}
	return lc.Listen(context.Background(), "tcp4", address)
}
func mark(dir, name string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	p := filepath.Join(dir, name)
	if e = os.WriteFile(p+".new", b, 0600); e != nil {
		return e
	}
	return os.Rename(p+".new", p)
}
func exists(p string) bool { _, e := os.Stat(p); return e == nil }
func waitFile(dir, name string, deadline time.Time) error {
	for time.Now().Before(deadline) {
		if exists(filepath.Join(dir, name)) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", name)
}
func command(role, dir, token string, deadline time.Time) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestProbeHelper$", "-test.timeout=45s")
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, prefix) {
			c.Env = append(c.Env, e)
		}
	}
	c.Env = append(c.Env, prefix+"ROLE="+role, prefix+"DIR="+dir, prefix+"TOKEN="+token, prefix+"DEADLINE="+strconv.FormatInt(deadline.UnixMilli(), 10))
	// nil stdin/out/err use NUL, not pipes that could keep root waits open.
	return c
}

type ready struct {
	Address       string
	ChildPID      int
	Before, After uint32
}

func TestProbeHelper(t *testing.T) {
	role := os.Getenv(prefix + "ROLE")
	if role == "" {
		t.Skip("private helper role")
	}
	dir, token := os.Getenv(prefix+"DIR"), os.Getenv(prefix+"TOKEN")
	b, e := os.ReadFile(filepath.Join(dir, "token"))
	ms, pe := strconv.ParseInt(os.Getenv(prefix+"DEADLINE"), 10, 64)
	deadline := time.UnixMilli(ms)
	if e != nil || pe != nil || token == "" || string(b) != token || !deadline.After(time.Now()) || time.Until(deadline) > 40*time.Second || (role != "parent" && role != "child") {
		t.Fatal("invalid helper guard")
	}
	// Independent watchdog bounds lifetime even when root orchestration is interrupted.
	go func() { time.Sleep(time.Until(deadline)); os.Exit(90) }()
	if role == "child" {
		// Deliberately never inspect, duplicate, close, listen, accept, or otherwise
		// operate the inherited socket. Process exit releases its reference.
		if e = mark(dir, "child-ready", true); e != nil {
			t.Fatal(e)
		}
		if e = waitFile(dir, "child-stop", deadline); e != nil {
			t.Fatal(e)
		}
		return
	}
	if e = waitFile(dir, "parent-go", deadline); e != nil {
		t.Fatal(e)
	}
	l, e := listen("127.0.0.1:0", ^syscall.SO_REUSEADDR)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	tcp := l.(*net.TCPListener)
	raw, e := tcp.SyscallConn()
	if e != nil {
		t.Fatal(e)
	}
	var before, after uint32
	var fe error
	if e = raw.Control(func(v uintptr) { before, fe = flags(syscall.Handle(v)) }); e != nil || fe != nil {
		t.Fatal(errors.Join(e, fe))
	}
	c := command("child", dir, token, deadline)
	if e = startWithOriginal(tcp, c); e != nil {
		t.Fatal(e)
	}
	if e = raw.Control(func(v uintptr) { after, fe = flags(syscall.Handle(v)) }); e != nil || fe != nil {
		t.Fatal(errors.Join(e, fe))
	}
	if e = waitFile(dir, "child-ready", deadline); e != nil {
		t.Fatal(e)
	}
	if e = mark(dir, "parent-ready", ready{l.Addr().String(), c.Process.Pid, before, after}); e != nil {
		t.Fatal(e)
	}
	// Root intentionally kills us without graceful socket closure.
	if e = c.Wait(); e != nil {
		t.Fatal(e)
	}
}

// Fixed Windows ABI layout from JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

func drainJob(job windows.Handle) error {
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var a jobAccounting
		if e := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&a)), uint32(unsafe.Sizeof(a)), nil); e != nil {
			return e
		}
		if a.ActiveProcesses == 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("job cleanup timed out with active processes")
}

func TestSyntheticOriginalSocketRetention(t *testing.T) {
	if os.Getenv(prefix+"ROLE") != "" {
		t.Fatal("root must not run in a helper role")
	}
	dir := t.TempDir()
	token := filepath.Base(dir) + strconv.FormatInt(time.Now().UnixNano(), 10)
	if e := os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(30 * time.Second)
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
		t.Fatal(e)
	}
	c := command("parent", dir, token, deadline)
	if e = c.Start(); e != nil {
		t.Fatal(e)
	}
	parentDone := make(chan error, 1)
	go func() { parentDone <- c.Wait() }()
	parentWaited := false
	defer func() {
		_ = mark(dir, "child-stop", true)
		_ = windows.TerminateJobObject(job, 99)
		_ = c.Process.Kill() // Also owns an unassigned parent if job assignment failed.
		if e := drainJob(job); e != nil {
			t.Error(e)
		}
		if !parentWaited {
			select {
			case <-parentDone:
			case <-time.After(3 * time.Second):
				t.Error("parent cleanup timed out")
			}
		}
	}()
	// Borrow the owned process handle, never reopen a mutable target by PID.
	// Concurrent Wait/early exit may make WithHandle fail, but cannot redirect
	// assignment to an unrelated process after PID reuse.
	var assignErr error
	e = c.Process.WithHandle(func(h uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(h))
	})
	e = errors.Join(e, assignErr)
	if e != nil {
		t.Fatalf("job assignment failed; no socket experiment started (nested job restrictions may apply): %v", e)
	}
	if e = mark(dir, "parent-go", true); e != nil {
		t.Fatal(e)
	}
	if e = waitFile(dir, "parent-ready", time.Now().Add(10*time.Second)); e != nil {
		t.Fatal(e)
	}
	var r ready
	b, e := os.ReadFile(filepath.Join(dir, "parent-ready"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &r); e != nil {
		t.Fatal(e)
	}
	if r.Before != r.After {
		t.Fatalf("original flags not restored: before=%#x after=%#x", r.Before, r.After)
	}
	child, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_INFORMATION, false, uint32(r.ChildPID))
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(child)
	// The PID-derived handle has query/synchronize rights only. Verify membership
	// before any waits; all termination remains against our owned job/process.
	var inJob bool
	if inJob, e = childInJob(child, job); e != nil || !inJob {
		t.Fatalf("child outside owned cleanup job: inJob=%v error=%v", inJob, e)
	}
	defer func() {
		_ = windows.TerminateJobObject(job, 99)
		s, e := windows.WaitForSingleObject(child, 3000)
		if e != nil || s != windows.WAIT_OBJECT_0 {
			t.Errorf("child cleanup not confirmed: status=%d error=%v", s, e)
		}
	}()
	if e = c.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-parentDone:
		parentWaited = true
	case <-time.After(3 * time.Second):
		t.Fatal("abrupt parent exit not confirmed")
	}
	for i := 0; i < 10; i++ {
		for _, opt := range []int{0, syscall.SO_REUSEADDR} {
			l, bindErr := listen(r.Address, opt)
			if bindErr == nil {
				l.Close()
				t.Fatalf("retention FAILED: bind succeeded with option %d after parent death", opt)
			}
			var errno syscall.Errno
			if !errors.As(bindErr, &errno) || (errno != windows.WSAEACCES && errno != windows.WSAEADDRINUSE) {
				t.Fatalf("unexpected bind failure is not evidence of retention: %v", bindErr)
			}
		}
		if s, e := windows.WaitForSingleObject(child, 0); e != nil || s != uint32(windows.WAIT_TIMEOUT) {
			t.Fatalf("child did not remain alive: status=%d error=%v", s, e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e = mark(dir, "child-stop", true); e != nil {
		t.Fatal(e)
	}
	if s, e := windows.WaitForSingleObject(child, 3000); e != nil || s != windows.WAIT_OBJECT_0 {
		t.Fatalf("child exit not confirmed: status=%d error=%v", s, e)
	}
	for _, opt := range []int{0, syscall.SO_REUSEADDR} {
		l, e := listen(r.Address, opt)
		if e != nil {
			t.Fatalf("address not released after last reference gone (option %d): %v", opt, e)
		}
		l.Close()
	}
	t.Log("SYNTHETIC PASS only: original listener retained across abrupt parent death; both bind modes denied until child exit; flags restored")
}

func TestInheritanceTransaction(t *testing.T) {
	startFailure := errors.New("start failed")
	restoreFailure := errors.New("restore failed")
	for _, original := range []uint32{0, syscall.HANDLE_FLAG_INHERIT, 2 /* HANDLE_FLAG_PROTECT_FROM_CLOSE */} {
		for _, failStart := range []bool{false, true} {
			var writes []uint32
			e := inherit(func() error {
				if failStart {
					return startFailure
				}
				return nil
			}, func() (uint32, error) { return original, nil }, func(v uint32) error { writes = append(writes, v); return nil })
			if len(writes) != 2 || writes[0] != syscall.HANDLE_FLAG_INHERIT || writes[1] != original&syscall.HANDLE_FLAG_INHERIT {
				t.Fatalf("restoration sequence: %v", writes)
			}
			if errors.Is(e, startFailure) != failStart {
				t.Fatalf("start result lost: %v", e)
			}
		}
	}
	calls := 0
	e := inherit(func() error { return startFailure }, func() (uint32, error) { return 0, nil }, func(uint32) error {
		calls++
		if calls == 2 {
			return restoreFailure
		}
		return nil
	})
	if !errors.Is(e, startFailure) || !errors.Is(e, restoreFailure) {
		t.Fatalf("joined errors lost: %v", e)
	}
	for _, getFails := range []bool{false, true} {
		started := false
		e = inherit(func() error { started = true; return nil }, func() (uint32, error) {
			if getFails {
				return 0, restoreFailure
			}
			return 0, nil
		}, func(uint32) error { return restoreFailure })
		if started || !errors.Is(e, restoreFailure) {
			t.Fatal("startup did not fail closed")
		}
	}
}
