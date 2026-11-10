package nativebootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativechannel"
)

func testRecord(t *testing.T) Record {
	t.Helper()
	return Record{Version: 1, Generation: "0123456789abcdef0123456789abcdef", ProfileSHA256: strings.Repeat("b", 64), Reference: nativechannel.Reference{Path: filepath.Join(t.TempDir(), "descriptor.json"), SHA256: strings.Repeat("a", 64)}}
}
func TestRecordStrict(t *testing.T) {
	r := testRecord(t)
	b, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(b, r.Generation)
	if err != nil || got != r {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{nil, append(b, ' '), append(b, b...), []byte(`{}`), bytes.Repeat([]byte("x"), payloadLimit+1), bytes.Replace(b, []byte(`"version":1`), []byte(`"version":2`), 1), bytes.Replace(b, []byte(`"version":1`), []byte(`"version":1,"unknown":1`), 1), bytes.Replace(b, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)} {
		if _, err := decode(bad, r.Generation); err == nil {
			t.Fatal("accepted malformed payload")
		}
	}
	if _, err := decode(b, strings.Repeat("1", 32)); err == nil {
		t.Fatal("accepted another generation")
	}
	r.Reference.Path = "relative"
	raw, _ := json.Marshal(r)
	if _, err := decode(raw, r.Generation); err == nil {
		t.Fatal("accepted relative publication")
	}
}
func TestPackageResolver(t *testing.T) {
	root := t.TempDir()
	gui := filepath.Join(root, "Magpie-Native-Codex-Experimental.exe")
	bridge := filepath.Join(root, "magpie-codex-bridge.exe")
	for _, path := range []string{gui, bridge} {
		if err := os.WriteFile(path, []byte("synthetic identity fixture; never executed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for self, want := range map[string]string{gui: bridge, bridge: gui} {
		got, err := resolvePackagedPeer(self)
		if err != nil || got.Path != want {
			t.Fatal(got, err)
		}
	}
	if _, err := resolvePackagedPeer(filepath.Join(root, "other.exe")); err == nil {
		t.Fatal("accepted arbitrary entry")
	}
	if err := os.Remove(bridge); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePackagedPeer(gui); err == nil {
		t.Fatal("accepted missing peer")
	}
}

func TestProfileRawBytesBoundToOwner(t *testing.T) {
	r := testRecord(t)
	data := []byte(`{"bootstrap_generation":"` + r.Generation + `"}`)
	digest := sha256.Sum256(data)
	r.ProfileSHA256 = hex.EncodeToString(digest[:])
	if err := r.VerifyProfile(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, append(append([]byte{}, data...), '\n'), []byte(`{}`), bytes.Repeat([]byte("x"), (1<<20)+1)} {
		if r.VerifyProfile(bad) == nil {
			t.Fatal("accepted different or oversized profile bytes")
		}
	}
	for _, badHash := range []string{"", strings.Repeat("B", 64), strings.Repeat("b", 62), strings.Repeat("z", 64)} {
		invalid := r
		invalid.ProfileSHA256 = badHash
		if _, err := encode(invalid); err == nil {
			t.Fatal("accepted missing/noncanonical profile digest")
		}
	}
	b, _ := encode(r)
	missing := bytes.Replace(b, []byte(`,"profile_sha256":"`+r.ProfileSHA256+`"`), nil, 1)
	if bytes.Equal(missing, b) {
		t.Fatal("test did not remove required digest")
	}
	if _, err := decode(missing, r.Generation); err == nil {
		t.Fatal("wire accepted missing profile digest")
	}
}
