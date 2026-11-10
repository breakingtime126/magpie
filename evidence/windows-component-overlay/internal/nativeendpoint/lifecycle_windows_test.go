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
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

const endpointFixtureArgument = "nativeendpoint-synthetic-fixture"

// The helper is this Go test binary, not a native backend. It keeps inherited
// handles untouched and exits by its watchdog if the test owner disappears.
func TestNativeEndpointFixture(t *testing.T) {
	if len(os.Args) < 5 || os.Args[len(os.Args)-4] != endpointFixtureArgument {
		return
	}
	dir, token, stamp := os.Args[len(os.Args)-3], os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	ms, err := strconv.ParseInt(stamp, 10, 64)
	deadline := time.UnixMilli(ms)
	saved, readErr := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil || readErr != nil || !filepath.IsAbs(dir) || token == "" || string(saved) != token || !deadline.After(time.Now()) || time.Until(deadline) > 30*time.Second {
		t.Fatal("invalid synthetic fixture guard")
	}
	if err := os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(deadline))
	os.Exit(0)
}

func TestWindowsEndpointStartsOwnedHelperAndDrainsBeforeRebind(t *testing.T) {
	e, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := e.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	address := e.Addr().String()
	dir := t.TempDir()
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(tokenBytes)
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := proc.Command(executable, "-test.run=^TestNativeEndpointFixture$", "-test.timeout=35s", endpointFixtureArgument, dir, token, strconv.FormatInt(time.Now().Add(25*time.Second).UnixMilli(), 10))
	// Nil stdin/stdout/stderr connect directly to NUL. No copier goroutine can
	// keep Wait alive; no inherited account or network state is used by helper.
	originalAttributes := command.SysProcAttr
	if err := e.Start(command); err != nil {
		t.Fatal(err)
	}
	if command.SysProcAttr != originalAttributes {
		t.Fatal("startup did not restore command attributes")
	}
	until := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(until) {
			t.Fatal("owned helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	competitor, err := net.Listen("tcp4", address)
	if err == nil {
		_ = competitor.Close()
		t.Fatal("live original listener was rebound")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		t.Fatal("owned helper tree did not drain", err)
	}
	replacement, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("listener was not released after verified tree drain", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}
