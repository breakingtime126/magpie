package nativeendpoint

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/yetone/magpie/internal/proc"
	"golang.org/x/sys/windows"
)

// Serializes this package's inheritance transactions. Go 1.26.8 uses an
// explicit handle list. Unrelated non-Go launchers are NOT serialized by this
// mutex; integration must audit every launcher in the owning process.
var inheritanceMu sync.Mutex
var getHandleInformation = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation")

// Listen sets SO_EXCLUSIVEADDRUSE before binding the original IPv4 socket.
// It is staged only: there are no production callers before Windows and exact
// native-runtime acceptance proves handle retention and lifecycle behavior.
func Listen() (*Endpoint, error) {
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var operation error
		err := raw.Control(func(handle uintptr) {
			operation = syscall.SetsockoptInt(syscall.Handle(handle), syscall.SOL_SOCKET, ^syscall.SO_REUSEADDR, 1)
		})
		return errors.Join(err, operation)
	}}
	l, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tcp, ok := l.(*net.TCPListener)
	if !ok {
		_ = l.Close()
		return nil, errors.New("native endpoint listener has no original TCP socket")
	}
	return newEndpoint(&windowsSocket{TCPListener: tcp})
}

type windowsSocket struct{ *net.TCPListener }

func checkedAttributes(cmd *exec.Cmd) (syscall.SysProcAttr, error) {
	a := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		a = *cmd.SysProcAttr
	}
	if a.CmdLine != "" || a.CreationFlags&^windows.CREATE_NO_WINDOW != 0 || a.Token != 0 || a.ProcessAttributes != nil || a.ThreadAttributes != nil || a.NoInheritHandles || len(a.AdditionalInheritedHandles) != 0 || a.ParentProcess != 0 {
		return a, errors.New("native endpoint rejects conflicting process attributes")
	}
	return a, nil // HideWindow and proc.Command's CREATE_NO_WINDOW are compatible.
}

func (s *windowsSocket) start(cmd *exec.Cmd) (p child, result error) {
	attr, err := checkedAttributes(cmd)
	if err != nil {
		return nil, err
	}
	raw, err := s.SyscallConn()
	if err != nil {
		return nil, err
	}
	inheritanceMu.Lock()
	defer inheritanceMu.Unlock()
	var operation error
	err = raw.Control(func(value uintptr) {
		handle := syscall.Handle(value)
		if handle == 0 || handle == syscall.InvalidHandle {
			operation = errors.New("native endpoint has invalid original socket")
			return
		}
		original := cmd.SysProcAttr
		attr.AdditionalInheritedHandles = []syscall.Handle{handle}
		cmd.SysProcAttr = &attr
		defer func() { cmd.SysProcAttr = original }()
		p, operation = inheritOriginal(func() (child, error) {
			tree, startErr := proc.StartTreeDrained(cmd)
			if tree == nil {
				return nil, startErr
			}
			return drainedChild{tree}, startErr
		}, func() (uint32, error) {
			var flags uint32
			ok, _, callErr := getHandleInformation.Call(uintptr(handle), uintptr(unsafe.Pointer(&flags)))
			if ok == 0 {
				return 0, callErr
			}
			return flags, nil
		}, func(flags uint32) error {
			return syscall.SetHandleInformation(handle, syscall.HANDLE_FLAG_INHERIT, flags)
		})
	})
	return p, errors.Join(err, operation)
}

// Wait returns only on a verified whole-Job drain; an error retains ownership.
type drainedChild struct{ tree *proc.DrainedTree }

func (c drainedChild) Wait() error { return c.tree.Wait(context.Background()) }
func (c drainedChild) Kill()       { _ = c.tree.Stop() }

func (s *windowsSocket) stopAccept() error { return s.SetDeadline(time.Now()) }
