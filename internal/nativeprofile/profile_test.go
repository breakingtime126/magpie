package nativeprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

func configFixture(t *testing.T) appserverbridge.Config {
	t.Helper()
	command := filepath.Join(t.TempDir(), "never-run.exe")
	if err := os.WriteFile(command, []byte("inert synthetic executable fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	return appserverbridge.Config{Backends: []appserverbridge.Backend{
		{ID: "a", Provider: "openai", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()},
		{ID: "b", Provider: "synthetic", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()},
	}}
}
func TestPreparedImmutableExactCodec(t *testing.T) {
	cfg := configFixture(t)
	p, err := New(strings.Repeat("1", 32), cfg)
	if err != nil {
		t.Fatal(err)
	}
	data := p.Bytes()
	sum := sha256.Sum256(data)
	if p.SHA256() != hex.EncodeToString(sum[:]) || p.Generation() != strings.Repeat("1", 32) || p.ValidateFor(cfg) != nil {
		t.Fatal("prepared identity mismatch")
	}
	data[0] = 'X'
	if p.Bytes()[0] != '{' {
		t.Fatal("exposed mutable bytes")
	}
	cfg.Backends[0].Provider = "changed"
	if p.ValidateFor(cfg) == nil {
		t.Fatal("accepted divergent plan")
	}
	if (&Prepared{}).ValidateFor(cfg) == nil || (*Prepared)(nil).SHA256() != "" {
		t.Fatal("zero prepared profile accepted")
	}
}
func TestStrictEnvelopeAndInspectionNoConfigReads(t *testing.T) {
	cfg := configFixture(t)
	p, err := New(strings.Repeat("2", 32), cfg)
	if err != nil {
		t.Fatal(err)
	}
	data := p.Bytes()
	if err := os.Remove(cfg.Backends[0].Command); err != nil {
		t.Fatal(err)
	}
	// Locator and Inspect still work after command removal: no Config.Validate.
	if g, err := Locator(data); err != nil || g != p.Generation() {
		t.Fatal(g, err)
	}
	if selected, err := Inspect(data); !selected || err != nil {
		t.Fatal(selected, err)
	}
	if _, err := LoadConfig(data); err == nil {
		t.Fatal("validated a missing executable")
	}
	for _, bad := range [][]byte{nil, append(append([]byte{}, data...), ' '), append(append([]byte{}, data...), data...), bytes.Repeat([]byte("x"), Limit+1), bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1), bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"unknown":1`), 1), []byte(`{"native_bootstrap":null,"config":{}}`)} {
		if _, err := Locator(bad); err == nil {
			t.Fatal("accepted invalid explicit envelope")
		}
	}
	raw, _ := json.Marshal(cfg)
	if selected, err := Inspect(raw); selected || err != nil {
		t.Fatal("legacy config was changed")
	}
	if _, err := Locator(raw); err == nil {
		t.Fatal("explicit launcher fell back to legacy")
	}
}
func TestReadBoundedAndInvalidPreparedBytes(t *testing.T) {
	if _, err := Read(bytes.NewReader(bytes.Repeat([]byte("x"), Limit+1))); err == nil {
		t.Fatal("accepted oversized profile")
	}
	if _, err := Read(nil); err == nil {
		t.Fatal("accepted absent reader")
	}
	cfg := configFixture(t)
	if (&Prepared{data: []byte(`{"native_bootstrap":null}`)}).ValidateFor(cfg) == nil {
		t.Fatal("accepted malformed private state")
	}
}
