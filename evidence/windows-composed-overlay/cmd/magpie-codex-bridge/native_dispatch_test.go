package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativeprofile"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagedProfile(t *testing.T) ([]byte, appserverbridge.Config) {
	t.Helper()
	command := filepath.Join(t.TempDir(), "inert.exe")
	if e := os.WriteFile(command, []byte("inert never executed"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := appserverbridge.Config{Backends: []appserverbridge.Backend{{ID: "a", Provider: "openai", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()}, {ID: "b", Provider: "synthetic", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()}}}
	p, e := nativeprofile.New(strings.Repeat("1", 32), cfg)
	if e != nil {
		t.Fatal(e)
	}
	return p.Bytes(), cfg
}
func TestTrustedDispatchBeforeValidationSingleSnapshot(t *testing.T) {
	data, cfg := stagedProfile(t)
	path := filepath.Join(t.TempDir(), "p.json")
	mustAdapterWrite(t, path, data)
	mustAdapterRemove(t, cfg.Backends[0].Command)
	reads, runs := 0, 0
	var out bufferCloser
	var diag bytes.Buffer
	in := io.NopCloser(strings.NewReader("untouched"))
	d := dependencies{getenv: func(string) string { return path }, read: func(p string) ([]byte, error) {
		reads++
		b, e := readProfile(p)
		if e == nil {
			e = os.WriteFile(p, []byte("changed after read"), 0600)
		}
		return b, e
	}, load: func(string) (appserverbridge.Config, error) { t.Fatal("legacy loader reached"); return cfg, nil }, trustedRun: func(_ context.Context, r io.Reader, o appserverbridge.LaunchOptions, i io.ReadCloser, w io.WriteCloser) error {
		runs++
		b, e := io.ReadAll(r)
		if e != nil || !bytes.Equal(b, data) || !o.CodeModeHost || !o.AnalyticsDefaultEnabled || i != in || w != &out {
			t.Fatal("inputs changed")
		}
		return nil
	}}
	code := execute(context.Background(), []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", "--stdio"}, in, &out, &diag, d)
	if code != 0 || reads != 1 || runs != 1 || diag.Len() != 0 {
		t.Fatalf("code=%d reads=%d runs=%d diag=%s", code, reads, runs, &diag)
	}
}
func TestTrustedDiagnosticsDoNotConsumeLease(t *testing.T) {
	data, _ := stagedProfile(t)
	for _, args := range [][]string{{"--check"}, {"--inspect-primary-identity-v0.167.0-alpha.3.2"}, desktopStartupArgs()} {
		var out bufferCloser
		var diag bytes.Buffer
		d := dependencies{getenv: func(string) string { return filepath.Join(t.TempDir(), "p.json") }, read: func(string) ([]byte, error) { return data, nil }, trustedRun: func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			t.Fatal("diagnostic consumed lease")
			return nil
		}}
		if execute(context.Background(), args, nil, &out, &diag, d) == 0 {
			t.Fatal("diagnostic claimed success")
		}
	}
}
func TestTrustedMalformedNeverFallback(t *testing.T) {
	data, _ := stagedProfile(t)
	for _, bad := range [][]byte{[]byte(`{"native_bootstrap":null}`), []byte(`{"native_bootstrap":`), []byte(`null`), bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1), bytes.Replace(data, []byte(`"native_bootstrap"`), []byte(`"NATIVE_BOOTSTRAP"`), 1), bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), append(append([]byte{}, data...), data...)} {
		var out bufferCloser
		var diag bytes.Buffer
		d := dependencies{getenv: func(string) string { return filepath.Join(t.TempDir(), "p.json") }, read: func(string) ([]byte, error) { return bad, nil }, load: func(string) (appserverbridge.Config, error) {
			t.Fatal("legacy fallback")
			return appserverbridge.Config{}, nil
		}, trustedRun: func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			t.Fatal("bad envelope connected")
			return nil
		}}
		if execute(context.Background(), []string{"app-server"}, nil, &out, &diag, d) == 0 {
			t.Fatal("accepted bad envelope")
		}
	}
}
func TestTrustedFailurePreservesCleanupFailure(t *testing.T) {
	data, _ := stagedProfile(t)
	for _, failure := range []error{errors.New("dead owner"), context.Canceled, errors.Join(context.Canceled, errors.New("cleanup failure"))} {
		var out bufferCloser
		var diag bytes.Buffer
		d := dependencies{getenv: func(string) string { return filepath.Join(t.TempDir(), "p.json") }, read: func(string) ([]byte, error) { return data, nil }, trustedRun: func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			return failure
		}}
		if code := execute(context.Background(), []string{"app-server"}, nil, &out, &diag, d); code != 1 || out.Len() != 0 || strings.Contains(diag.String(), failure.Error()) {
			t.Fatalf("code=%d diag=%s", code, &diag)
		}
	}
}
func TestSnapshotLegacyAndBounds(t *testing.T) {
	_, cfg := stagedProfile(t)
	data, _ := json.Marshal(cfg)
	path := filepath.Join(t.TempDir(), "p.json")
	mustAdapterWrite(t, path, data)
	var out bufferCloser
	var diag bytes.Buffer
	ran := false
	d := dependencies{getenv: func(string) string { return path }, read: readProfile, run: func(_ context.Context, c appserverbridge.Config, _ appserverbridge.LaunchOptions, _ io.ReadCloser, _ io.WriteCloser) error {
		ran = true
		if c.Backends[0].Command != cfg.Backends[0].Command {
			t.Fatal("legacy config changed")
		}
		return nil
	}}
	if execute(context.Background(), []string{"app-server"}, nil, &out, &diag, d) != 0 || !ran {
		t.Fatal("legacy dispatch lost", &diag)
	}
	mustAdapterWrite(t, path, bytes.Repeat([]byte("x"), nativeprofile.Limit+1))
	if _, e := readProfile(path); e == nil {
		t.Fatal("oversize accepted")
	}
	if _, e := readProfile(t.TempDir()); e == nil {
		t.Fatal("directory accepted")
	}
}

func mustAdapterWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func mustAdapterRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestProfileSnapshotRejectsMidReadChanges(t *testing.T) {
	for _, kind := range []string{"grow", "truncate"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.json")
			original := []byte(`{"synthetic":true}`)
			mustAdapterWrite(t, path, original)
			data, err := readProfileWith(path, func(r io.Reader) ([]byte, error) {
				snapshot, err := nativeprofile.Read(r)
				if err != nil {
					return nil, err
				}
				switch kind {
				case "grow":
					mustAdapterWrite(t, path, append(bytes.Clone(original), ' '))
				case "truncate":
					mustAdapterWrite(t, path, original[:len(original)-1])
				}
				return snapshot, nil
			})
			if !errors.Is(err, nativeprofile.ErrProfile) || data != nil {
				t.Fatal("changed profile snapshot was accepted", err)
			}
		})
	}
}
func TestProfileSnapshotStableIdentityAndByteLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	expected := []byte(`{"synthetic":true}`)
	mustAdapterWrite(t, path, expected)
	data, err := readProfile(path)
	if err != nil || !bytes.Equal(data, expected) {
		t.Fatal("stable regular file rejected", err)
	}
	mustAdapterWrite(t, path, bytes.Repeat([]byte("x"), nativeprofile.Limit+1))
	called := false
	if _, err := readProfileWith(path, func(io.Reader) ([]byte, error) { called = true; return nil, nil }); err == nil || called {
		t.Fatal("oversized file reached reader")
	}
}

func TestProfileSnapshotRejectsDifferentFileWithSameMetadata(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "first.json"), filepath.Join(root, "replacement.json")
	data := []byte(`{"synthetic":true}`)
	mustAdapterWrite(t, a, data)
	mustAdapterWrite(t, b, data)
	first, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, first.ModTime(), first.ModTime()); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ModTime().Equal(replacement.ModTime()) || first.Size() != replacement.Size() {
		t.Fatal("fixture metadata not equal")
	}
	if sameProfileFile(first, replacement) || sameProfileFile(first, nil) {
		t.Fatal("metadata-only identity admitted replacement or missing path")
	}
}
