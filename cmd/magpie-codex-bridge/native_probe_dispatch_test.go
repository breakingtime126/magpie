package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/yetone/magpie/internal/appserverbridge"
	"io"
	"path/filepath"
	"testing"
)

func TestTrustedStartupProbeUsesSeparateAuthenticatedOperation(t *testing.T) {
	data, _ := stagedProfile(t)
	for _, failed := range []bool{false, true} {
		var out bufferCloser
		var diag bytes.Buffer
		calls := 0
		d := dependencies{getenv: func(string) string { return filepath.Join(t.TempDir(), "profile.json") }, read: func(string) ([]byte, error) { return data, nil }, trustedProbe: func(_ context.Context, r io.Reader, o appserverbridge.LaunchOptions, info appserverbridge.StartupClientInfo) ([]byte, error) {
			calls++
			got, _ := io.ReadAll(r)
			if !bytes.Equal(got, data) || !o.CodeModeHost || !o.AnalyticsDefaultEnabled || info.Name == "" {
				t.Fatal("inspection inputs changed")
			}
			if failed {
				return []byte("must not expose"), errors.New("private probe failure")
			}
			return []byte(fixtureStartupSuccess167), nil
		}, trustedRun: func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			t.Fatal("inspection consumed JSONL lease")
			return nil
		}, load: func(string) (appserverbridge.Config, error) {
			t.Fatal("legacy fallback")
			return appserverbridge.Config{}, nil
		}}
		code := execute(context.Background(), desktopStartupArgs(), nil, &out, &diag, d)
		if calls != 1 || (code == 0) == failed {
			t.Fatal("wrong dispatch", code, calls)
		}
		if !failed && out.String() != fixtureStartupSuccess167+"\n" {
			t.Fatal("genuine result changed")
		}
		if failed && (bytes.Contains(out.Bytes(), []byte("must not expose")) || bytes.Contains(diag.Bytes(), []byte("private"))) {
			t.Fatal("failed probe leaked")
		}
	}
}
