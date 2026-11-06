package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

const startupProbe167Mode = "compare-unrestricted-v0.167.0-alpha.3.2"
const desktopStartupClientInfo167 = `{"name":"codex_desktop","title":"Codex Desktop","version":"26.1103.3517.0"}`
const fixtureStartupSuccess167 = `{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true}}`

func TestStartupProbe167Dispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	args := desktopStartupArgs()
	args[len(args)-1] = desktopStartupClientInfo167
	cfg := appserverbridge.Config{StartupProbe: startupProbe167Mode}
	title := "Codex Desktop"
	wantInfo := appserverbridge.StartupClientInfo{Name: "codex_desktop", Title: &title, Version: "26.1103.3517.0"}
	wantOptions := appserverbridge.LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	var output bufferCloser
	var diagnostics bytes.Buffer
	var calls []string
	code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{
		getenv: func(key string) string {
			calls = append(calls, "env")
			if key != configEnv {
				t.Fatal("read unexpected environment")
			}
			return path
		},
		load: func(got string) (appserverbridge.Config, error) {
			calls = append(calls, "load")
			if got != path {
				t.Fatal("changed selected configuration")
			}
			return cfg, nil
		},
		probe: func(ctx context.Context, got appserverbridge.Config, options appserverbridge.LaunchOptions, info appserverbridge.StartupClientInfo) ([]byte, error) {
			calls = append(calls, "probe")
			if !reflect.DeepEqual(got, cfg) || options != wantOptions || !reflect.DeepEqual(info, wantInfo) {
				t.Fatal("new profile changed one-shot dispatch or Desktop identity")
			}
			return []byte(fixtureStartupSuccess167), nil
		},
		run: func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
			t.Fatal("one-shot profile entered JSONL")
			return nil
		},
	})
	if code != 0 || diagnostics.Len() != 0 || output.String() != fixtureStartupSuccess167+"\n" || !reflect.DeepEqual(calls, []string{"env", "load", "probe"}) {
		t.Fatal("reviewed new profile did not reach one-shot inspection")
	}
}

func TestStartupProbeHelpNamesExactProfilesAndBoundaries(t *testing.T) {
	var output bufferCloser
	var diagnostics bytes.Buffer
	code := execute(context.Background(), []string{"--help"}, nil, &output, &diagnostics, dependencies{
		getenv: func(string) string {
			t.Fatal("help read configuration")
			return ""
		},
	})
	if code != 0 || diagnostics.Len() != 0 {
		t.Fatal("help failed")
	}
	for _, text := range []string{
		"compare-unrestricted-v0.166 (exact 0.166.0-alpha.6)",
		"compare-unrestricted-v0.167.0-alpha.3.2 (exact 0.167.0-alpha.3.2)",
		"reject every non-null requirements result",
		"cannot be combined with the v0.166 JSONL requirements/auth modes",
		"Sign-out is unsupported",
	} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("help omitted profile boundary %q", text)
		}
	}
}
