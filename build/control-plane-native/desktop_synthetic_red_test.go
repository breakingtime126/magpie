package main

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

// This old-symbol-only guard can run in the exact prior checkout. Linux stops
// before environment, filesystem or binary access, after parsing the profile.
func TestDesktopSelfAuthoredProfileWithoutCatalog(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the non-Windows admission guard runs on Linux CI")
	}
	var out bytes.Buffer
	err := runDesktopFixtures([]string{"-fixture-profile", "synthetic-models-4239", "-reviewed-build", "desktop-4239"},
		io.NopCloser(strings.NewReader("")), &out, &desktopAudit{})
	if err != failure("platform_rejected") || out.Len() != 0 {
		t.Fatal("explicit self-authored Desktop profile was rejected before the platform guard")
	}
}
