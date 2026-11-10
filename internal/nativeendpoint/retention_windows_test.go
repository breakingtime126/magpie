package nativeendpoint

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
	"golang.org/x/sys/windows"
)

// This helper operates only on guarded disposable files. It never operates on
// the inherited socket: retaining the original handle is the behavior under test.
func TestEndpointRetentionFixture(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-4] != "nativeendpoint-retention-fixture" {
		return
	}
	dir, token, stamp := os.Args[len(os.Args)-3], os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	ms, err := strconv.ParseInt(stamp, 10, 64)
	deadline := time.UnixMilli(ms)
	saved, readErr := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil || readErr != nil || !filepath.IsAbs(dir) || token == "" || string(saved) != token || !deadline.After(time.Now()) || time.Until(deadline) > 30*time.Second {
		t.Fatal("invalid retention fixture guard")
	}
	for time.Now().Before(deadline) {
		challenge, err := os.ReadFile(filepath.Join(dir, "challenge"))
		if err == nil {
			if err := os.WriteFile(filepath.Join(dir, "response"), challenge, 0600); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(0)
}

func TestWindowsEndpointInheritedOriginalSurvivesParentSocketClose(t *testing.T) {
	e, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	// Closing the parent listener deliberately violates normal endpoint ownership
	// only in this in-package acceptance test. Cleanup must still prove Job drain.
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := e.Shutdown(ctx)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error("shutdown", err)
		}
		select {
		case <-e.done:
		default:
			t.Error("endpoint cleanup did not finish")
			return
		}
		e.mu.Lock()
		drainErr := e.childDrainErr
		waitDone := e.firstWaitDone
		e.mu.Unlock()
		if drainErr != nil {
			t.Error("child tree drain was not proved", drainErr)
		}
		if waitDone == nil {
			return
		}
		select {
		case <-waitDone:
		default:
			t.Error("owned child was not reaped")
		}
	}
	defer shutdown()
	address := e.Addr().String()
	dir := t.TempDir()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(random)
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := proc.Command(executable, "-test.run=^TestEndpointRetentionFixture$", "-test.timeout=35s", "nativeendpoint-retention-fixture", dir, token, strconv.FormatInt(time.Now().Add(25*time.Second).UnixMilli(), 10))
	if err := e.Start(cmd); err != nil {
		t.Fatal(err)
	}
	proveLive := func(value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "challenge"), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			response, err := os.ReadFile(filepath.Join(dir, "response"))
			if err == nil && string(response) == value {
				return
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("owned helper failed fresh liveness challenge")
	}
	proveLive(token + "-ready")
	if err := e.socket.(*windowsSocket).TCPListener.Close(); err != nil {
		t.Fatal(err)
	}
	proveLive(token + "-parent-closed")
	for _, reuse := range []bool{false, true} {
		lc := net.ListenConfig{}
		if reuse {
			lc.Control = func(_, _ string, raw syscall.RawConn) error {
				var operation error
				err := raw.Control(func(h uintptr) {
					operation = syscall.SetsockoptInt(syscall.Handle(h), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				})
				return errors.Join(err, operation)
			}
		}
		listener, err := lc.Listen(context.Background(), "tcp4", address)
		if err == nil {
			_ = listener.Close()
			t.Fatalf("original socket was not retained by owned child (SO_REUSEADDR=%v)", reuse)
		}
		if !errors.Is(err, windows.WSAEADDRINUSE) && !errors.Is(err, windows.WSAEACCES) {
			t.Fatalf("unexpected bind failure: %v", err)
		}
	}
	proveLive(token + "-bind-checked")
	shutdown()
	replacement, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("address not reusable after verified drain", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}
