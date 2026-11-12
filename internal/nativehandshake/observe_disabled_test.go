//go:build !nativehandshakediag

package nativehandshake

import (
	"context"
	"testing"
)

func TestHandshakeOrdinaryBuildHasNoProcessObserver(t *testing.T) {
	if processEnabled() || Enabled(context.Background()) {
		t.Fatal("ordinary build enabled process observer")
	}
	for s := Stage(0); s < 255; s++ {
		processEmit(s)
		Emit(context.Background(), s)
	}
}
