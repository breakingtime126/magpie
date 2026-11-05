package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartTreeStrict starts the command suspended, assigns it to a kill-on-close
// job, then resumes its sole initial thread. Unlike StartTree, any failure
// returns an error after ending and reaping the owned child, never a fallback.
// This is lifecycle ownership, not a security boundary against hostile peers.
func StartTreeStrict(cmd *exec.Cmd) (*Tree, error) {
	return startTreeStrict(cmd, windows.AssignProcessToJobObject, resumeOwnedThread)
}

// Per-call operations let tests fail assignment or resume without replacing
// shared platform functions while other commands are starting.
func startTreeStrict(cmd *exec.Cmd, assign func(windows.Handle, windows.Handle) error, resume func(windows.Handle) error) (*Tree, error) {
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	const conflicting = windows.CREATE_SUSPENDED | windows.DEBUG_PROCESS | windows.DEBUG_ONLY_THIS_PROCESS | windows.CREATE_BREAKAWAY_FROM_JOB
	if attr.CreationFlags&conflicting != 0 {
		return nil, errors.New("strict process tree: incompatible creation flags")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("strict process tree: create job: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = windows.CloseHandle(job)
		}
	}()
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
		LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, fmt.Errorf("strict process tree: configure job: %w", err)
	}
	original := cmd.SysProcAttr
	attr.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr = &attr
	err = cmd.Start()
	cmd.SysProcAttr = original
	if err != nil {
		return nil, err
	}
	// WithHandle pins os.Process's actual handle. Never reopen the process by
	// PID: a cancellation could have ended it and the PID could be reused.
	var operationErr error
	handleErr := cmd.Process.WithHandle(func(handle uintptr) {
		process := windows.Handle(handle)
		if err := assign(job, process); err != nil {
			operationErr = fmt.Errorf("assign job: %w", err)
			return
		}
		operationErr = resume(process)
	})
	err = errors.Join(handleErr, operationErr)
	if err != nil {
		killErr := cmd.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		// Closing the job also ends any child if resume partly succeeded.
		closeErr := windows.CloseHandle(job)
		owned = false
		waitErr := cmd.Wait()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			waitErr = nil // Termination's exit status is expected; reaping is not optional.
		}
		return nil, fmt.Errorf("strict process tree: %w", errors.Join(err, killErr, closeErr, waitErr))
	}
	owned = false
	return &Tree{cmd: cmd, waited: make(chan struct{}), group: group{job: job}}, nil
}

// Go closes CreateProcess's primary-thread handle. Toolhelp is documented and
// can recover that sole thread while the process is still suspended. More than
// one thread (including injected threads) is ambiguous and fails closed.
func ownedInitialThread(process windows.Handle) (windows.Handle, error) {
	pid, err := windows.GetProcessId(process)
	if err != nil {
		return 0, err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	var tid uint32
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			if tid != 0 {
				return 0, errors.New("strict process tree: multiple initial threads")
			}
			tid = entry.ThreadID
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, fmt.Errorf("strict process tree: enumerate threads: %w", err)
	}
	if tid == 0 {
		return 0, errors.New("strict process tree: initial thread missing")
	}
	thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME|windows.THREAD_QUERY_LIMITED_INFORMATION, false, tid)
	if err != nil {
		return 0, err
	}
	getOwner := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessIdOfThread")
	if err := getOwner.Find(); err != nil {
		_ = windows.CloseHandle(thread)
		return 0, err
	}
	owner, _, ownerErr := getOwner.Call(uintptr(thread))
	if owner == 0 {
		err = ownerErr
	}
	if err != nil || uint32(owner) != pid {
		_ = windows.CloseHandle(thread)
		return 0, fmt.Errorf("strict process tree: thread ownership mismatch: %w", errors.Join(err, errors.New("thread is not owned")))
	}
	state, err := windows.WaitForSingleObject(process, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		_ = windows.CloseHandle(thread)
		return 0, errors.Join(errors.New("strict process tree: child no longer running"), err)
	}
	return thread, nil
}

func resumeOwnedThread(process windows.Handle) error {
	thread, err := ownedInitialThread(process)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(thread)
	previous, err := windows.ResumeThread(thread)
	if err != nil {
		return fmt.Errorf("strict process tree: resume: %w", err)
	}
	if previous != 1 {
		return fmt.Errorf("strict process tree: unexpected suspend count %d", previous)
	}
	return nil
}
