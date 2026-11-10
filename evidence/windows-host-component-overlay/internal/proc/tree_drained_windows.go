package proc

import (
	"errors"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartTreeDrained reuses strict suspended startup and retains the assigned Job
// before the initial thread can run. Duplicating a Job handle is intentional;
// this function never duplicates a socket. A nonnil tree returned with an error
// still owns cleanup after a partially resumed startup and MUST be drained.
func StartTreeDrained(cmd *exec.Cmd) (*DrainedTree, error) {
	if cmd == nil || cmd.Process != nil || cmd.ProcessState != nil {
		return nil, errors.New("drained tree requires an unstarted command")
	}
	var retained windows.Handle
	var failedRootReaped bool
	var failedRootWait error
	tree, startErr := startTreeStrictObserved(cmd, func(job, process windows.Handle) error {
		return retainBeforeAssign(func() error {
			var copy windows.Handle
			current := windows.CurrentProcess()
			if err := windows.DuplicateHandle(current, job, current, &copy, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
				return err
			}
			retained = copy
			return nil
		}, func() error { return windows.AssignProcessToJobObject(job, process) })
	}, resumeOwnedThread, func(reaped bool, waitErr error) { failedRootReaped = reaped; failedRootWait = waitErr })
	if retained == 0 && (cmd.Process == nil || failedRootReaped) {
		return nil, startErr
	}
	rootDone := make(chan struct{})
	var rootErr error
	var uncertainRoot *os.Process
	if tree != nil {
		go func() { rootErr = tree.Wait(); close(rootDone) }()
	} else {
		if !failedRootReaped {
			rootErr = errors.Join(errors.New("strict startup did not prove owned root reaped"), failedRootWait)
			uncertainRoot = cmd.Process
		}
		// This signals completion of the wait ATTEMPT, not successful reaping.
		// rootError gates release even if an unassigned Job reports zero processes.
		close(rootDone)
	}
	drained := newDrainedTree(rootDone, func() error { return rootErr },
		func() error {
			var jobErr, rootKillErr error
			if retained != 0 {
				jobErr = windows.TerminateJobObject(retained, 1)
			}
			if uncertainRoot != nil {
				rootKillErr = uncertainRoot.Kill()
				if errors.Is(rootKillErr, os.ErrProcessDone) {
					rootKillErr = nil
				}
			}
			return errors.Join(jobErr, rootKillErr)
		},
		func() (uint32, error) {
			if retained == 0 {
				return 0, errors.New("failed startup has no retained Job drain proof")
			}
			var accounting drainedJobAccounting
			err := windows.QueryInformationJobObject(retained, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
			return accounting.ActiveProcesses, err
		}, func() error {
			if retained == 0 {
				return errors.New("cannot release unproven startup")
			}
			return windows.CloseHandle(retained)
		})
	if startErr != nil {
		return drained, errors.Join(startErr, drained.Stop())
	}
	return drained, nil
}

// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION has a fixed Windows ABI layout.
type drainedJobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}
