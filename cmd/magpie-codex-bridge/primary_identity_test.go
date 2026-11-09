package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
)

const identityFlag = "--inspect-primary-identity-v0.167.0-alpha.3.2"

func TestPrimaryIdentityCLIIsSeparateFromCheckAndServe(t *testing.T) {
	for _, flag := range []string{identityFlag, "--check"} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		calls := 0
		deps := dependencies{
			getenv: func(string) string {
				return filepath.Join(t.TempDir(), "profile.json")
			},
			load: func(string) (appserverbridge.Config, error) {
				return appserverbridge.Config{}, nil
			},
			identity: func(context.Context, appserverbridge.Config) ([]byte, error) {
				calls++
				return []byte(`{"schema":1,"scope":"native-reported-identity-only","authMethod":null,"requiresOpenaiAuth":true}`), nil
			},
		}
		if code := execute(context.Background(), []string{flag}, nil, &output, &diagnostics, deps); code != 0 {
			t.Fatal("explicit operation failed")
		}
		if flag == "--check" {
			if calls != 0 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "no children started") {
				t.Fatal("check gained identity side effects")
			}
		} else if calls != 1 || !strings.Contains(output.String(), "native-reported-identity-only") || diagnostics.Len() != 0 {
			t.Fatal("identity operation did not use only the explicit dependency")
		}
	}
	for _, args := range [][]string{{"app-server", identityFlag}, {identityFlag, "--check"}, {identityFlag, "--help"}, {identityFlag, "--startup-sign-out"}} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		if code := execute(context.Background(), args, nil, &output, &diagnostics, dependencies{}); code != 2 || output.Len() != 0 {
			t.Fatal("mixed identity arguments reached configuration")
		}
	}
}

func TestPrimaryIdentityCLIFailureNeverEchoesNativeError(t *testing.T) {
	for _, source := range []error{errors.New("PUBLIC-SECRET-CANARY"), context.Canceled, context.DeadlineExceeded} {
		var output bufferCloser
		var diagnostics bytes.Buffer
		code := execute(context.Background(), []string{identityFlag}, nil, &output, &diagnostics, dependencies{
			getenv: func(string) string {
				return filepath.Join(t.TempDir(), "profile.json")
			},
			load: func(string) (appserverbridge.Config, error) {
				return appserverbridge.Config{}, nil
			},
			identity: func(context.Context, appserverbridge.Config) ([]byte, error) {
				return nil, source
			},
		})
		if code != 1 || strings.Contains(output.String()+diagnostics.String(), "PUBLIC-") || !strings.Contains(output.String(), `"error"`) {
			t.Fatal("native error was not a fixed failure")
		}
	}
}
