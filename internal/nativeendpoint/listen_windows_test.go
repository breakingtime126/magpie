package nativeendpoint

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// Pure attribute checks only. No Windows child or socket is created by these tests.
func TestRejectsConflictingWindowsAttributes(t *testing.T) {
	attrs := []syscall.SysProcAttr{{CmdLine: "override"}, {CreationFlags: 1}, {Token: 1}, {ProcessAttributes: &syscall.SecurityAttributes{}}, {ThreadAttributes: &syscall.SecurityAttributes{}}, {NoInheritHandles: true}, {AdditionalInheritedHandles: []syscall.Handle{1}}, {ParentProcess: 1}}
	for _, a := range attrs {
		if _, err := checkedAttributes(&exec.Cmd{SysProcAttr: &a}); err == nil {
			t.Fatalf("accepted conflict: %+v", a)
		}
	}
	original := &syscall.SysProcAttr{HideWindow: true}
	a, err := checkedAttributes(&exec.Cmd{SysProcAttr: original})
	if err != nil || !a.HideWindow {
		t.Fatal(err)
	}
}

func TestAllowsProcNoWindowWithoutChangingAttributes(t *testing.T) {
	original := &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	a, err := checkedAttributes(&exec.Cmd{SysProcAttr: original})
	if err != nil || a.CreationFlags != windows.CREATE_NO_WINDOW || !a.HideWindow {
		t.Fatal("proc.Command attributes rejected or altered", err)
	}
}
