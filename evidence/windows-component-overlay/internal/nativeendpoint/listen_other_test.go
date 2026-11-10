//go:build !windows

package nativeendpoint

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatformNeverFallsBack(t *testing.T) {
	e, err := Listen()
	if e != nil || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatal("unsupported platform created endpoint", err)
	}
}
