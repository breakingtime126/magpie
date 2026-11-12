package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// Authored subprocess only. Never imports Appx or launches official PowerShell.
func TestOwnedHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "appx-diagnostic-helper" {
		return
	}
	for _, line := range markerOrder("A") {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Exit(0)
}
func TestOwnedAuthoredCompletion(t *testing.T) {
	cmd := proc.Command(os.Args[0], "-test.run=^TestOwnedHelper$", "appx-diagnostic-helper")
	p := newProgress("A")
	cmd.Stderr = p
	cmd.Stdout = p
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := runCommand(ctx, cmd, p, func(c *exec.Cmd) (owner, error) {
		tree, e := proc.StartTreeDrained(c)
		if tree == nil {
			return nil, e
		}
		return tree, e
	})
	if r.Outcome != "completed" || !r.Cleanup {
		t.Fatalf("authored strict child: %+v", r)
	}
}
func TestAdmission(t *testing.T) {
	env := map[string]string{"GITHUB_SHA": strings.Repeat("a", 40), "APPX_DIAGNOSTIC_VERIFIED_SHA": strings.Repeat("a", 40), "GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted", "RUNNER_OS": "Windows", "GITHUB_REPOSITORY": "breakingtime126/magpie", "GITHUB_REF": "refs/heads/experiment/app-server-bridge-v0.1.3035", "GITHUB_EVENT_NAME": "push", "APPX_DIAGNOSTIC_OPT_IN": optIn}
	get := func(s string) string { return env[s] }
	if !admitted(get) {
		t.Fatal("explicit context rejected")
	}
	for k, v := range env {
		env[k] = "untrusted"
		if admitted(get) {
			t.Fatal("missing admission", k)
		}
		env[k] = v
	}
}
