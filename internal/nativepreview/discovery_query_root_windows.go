package nativepreview

import (
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

// Retain a noninheritable, wait-only handle before Cmd.Wait can release its
// original handle. It confers no process/job termination or PID-reopen authority.
// The query caller serializes sampling and release; no callback escapes it.
func observeInstalledQueryRoot(command *exec.Cmd) queryRootObservation {
	if command == nil || command.Process == nil {
		return queryRootObservation{}
	}
	retained := retainInstalledQueryRoot(command.Process)
	if retained == 0 {
		return queryRootObservation{}
	}
	return queryRootObservation{
		state: func() queryRootState {
			if retained == 0 {
				return queryRootUnknown
			}
			state, err := windows.WaitForSingleObject(retained, 0)
			return classifyQueryRootWait(state, err)
		},
		close: func() {
			if retained != 0 {
				_ = windows.CloseHandle(retained)
				retained = 0
			}
		},
	}
}

func classifyQueryRootWait(state uint32, err error) queryRootState {
	if err != nil {
		return queryRootUnknown
	}
	switch state {
	case uint32(windows.WAIT_TIMEOUT):
		return queryRootRunning
	case windows.WAIT_OBJECT_0:
		return queryRootExited
	default:
		return queryRootUnknown
	}
}

func retainInstalledQueryRoot(process *os.Process) windows.Handle {
	var retained windows.Handle
	var captureErr error
	err := process.WithHandle(func(handle uintptr) {
		current := windows.CurrentProcess()
		captureErr = windows.DuplicateHandle(current, windows.Handle(handle), current,
			&retained, windows.SYNCHRONIZE, false, 0)
	})
	if err != nil || captureErr != nil || retained == 0 {
		if retained != 0 {
			_ = windows.CloseHandle(retained)
		}
		return 0
	}
	return retained
}
