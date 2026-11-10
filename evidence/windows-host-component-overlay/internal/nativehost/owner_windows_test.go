package nativehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
)

func TestWindowsOwnerAwaitFailureReturnsNilInterface(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	record := nativebootstrap.Record{Version: 1, Generation: hex.EncodeToString(random[:]), ProfileSHA256: strings.Repeat("b", 64), Reference: nativechannel.Reference{Path: filepath.Join(t.TempDir(), "public.json"), SHA256: strings.Repeat("a", 64)}}
	raw, err := nativebootstrap.Listen(record, nativebootstrap.Peer{Path: self})
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	adapter := &windowsOwner{raw}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease, err := adapter.Await(ctx)
	if err == nil || lease != nil {
		t.Fatalf("canceled Await returned typed nil ownership: lease=%#v err=%v", lease, err)
	}
	// A second attempt is a genuine used-owner error, not another connection.
	lease, err = adapter.Await(context.Background())
	if err == nil || lease != nil {
		t.Fatalf("used owner returned typed nil ownership: lease=%#v err=%v", lease, err)
	}
}
