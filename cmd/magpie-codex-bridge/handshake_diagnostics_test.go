package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativehandshake"
)

func TestHandshakeIdentityCommandsNeverObserve(t *testing.T) {
	for _, arg := range []string{"--version", "--help", "--startup-sign-out"} {
		events := 0
		ctx := nativehandshake.WithObserver(context.Background(), func(nativehandshake.Stage) { events++ })
		var out bufferCloser
		var diag bytes.Buffer
		execute(ctx, []string{arg}, io.NopCloser(bytes.NewReader(nil)), &out, &diag, dependencies{getenv: func(string) string { t.Fatal("identity accessed environment"); return "" }})
		if events != 0 {
			t.Fatal("identity or unsupported signout created trace")
		}
	}
}

func TestHandshakeAdapterDispatchPrecedesBootstrap(t *testing.T) {
	data, _ := stagedProfile(t)
	events := map[nativehandshake.Stage]int{}
	ctx := nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) { events[s]++ })
	var out bufferCloser
	var diag bytes.Buffer
	d := dependencies{getenv: func(string) string { return filepath.Join(t.TempDir(), "profile.json") }, read: func(string) ([]byte, error) { return data, nil }, trustedRun: func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error {
		if events[nativehandshake.AdapterEntered] != 1 || events[nativehandshake.JSONLRequested] != 1 {
			t.Fatal("bootstrap entered without invocation milestones")
		}
		return nil
	}}
	if execute(ctx, []string{"app-server"}, io.NopCloser(bytes.NewReader(nil)), &out, &diag, d) != 0 {
		t.Fatal("dispatch changed")
	}
	if events[nativehandshake.InitializeOutputWritten] != 0 || events[nativehandshake.ProfileAuthenticated] != 0 {
		t.Fatal("dispatch fabricated backend or bootstrap proof")
	}
}
