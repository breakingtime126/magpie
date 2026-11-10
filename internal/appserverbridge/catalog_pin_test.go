package appserverbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestSharedNativeCatalogPinRejectsTampering(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	path := cfg.SharedNativeRoute.SecondaryCatalog
	data := []byte("opaque synthetic supplier artifact, not native model records")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	cfg.SharedNativeRoute.SecondaryCatalogSHA256 = hex.EncodeToString(sum[:])
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed synthetic artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg.Validate() == nil {
		t.Fatal("changed supplier artifact passed pinned Config validation")
	}
	cfg.SharedNativeRoute.SecondaryCatalogSHA256 = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal("legacy unpinned config changed", err)
	}
}

func TestPinnedCatalogRejectsMalformedLinksAndOversize(t *testing.T) {
	cfg := sharedNativeConfigFixture167(t)
	path := cfg.SharedNativeRoute.SecondaryCatalog
	for _, pin := range []string{"bad", strings.Repeat("A", 64), strings.Repeat("0", 64)} {
		if pinnedCatalogMatches(path, pin) {
			t.Fatal("invalid or mismatched digest admitted")
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(pinnedCatalogLimit + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if pinnedCatalogMatches(path, strings.Repeat("a", 64)) {
		t.Fatal("oversized catalog admitted")
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlink unavailable")
	}
	if pinnedCatalogMatches(link, strings.Repeat("a", 64)) {
		t.Fatal("linked catalog admitted")
	}
}
