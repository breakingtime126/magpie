//go:build nativehandshakediag

package nativehandshake

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestHandshakeOwnedDirectoryRejectsWindowsShortAlias(t *testing.T) {
	base, root, exe := handshakeOwnedFixture(t)
	long, err := syscall.UTF16PtrFromString(base)
	if err != nil {
		t.Fatal("short-name fixture encoding failed")
	}
	buffer := make([]uint16, 32768)
	n, err := syscall.GetShortPathName(long, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || n >= uint32(len(buffer)) {
		t.Fatal("short-name fixture lookup failed")
	}
	short := syscall.UTF16ToString(buffer[:n])
	if strings.EqualFold(short, base) {
		t.Skip("test volume has no distinct short-name alias; no filesystem setting changed")
	}
	// Alias only the test-owned ancestor, preserving the role and owned-run
	// basename checks so this exercises the unchanged resolved-path guard.
	alias := filepath.Join(short, filepath.Base(filepath.Dir(root)), "app", filepath.Base(exe))
	assertHandshakeAliasRejected(t, alias, exe)
}
