package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// The PowerShell caller owns this watchdog; it in turn owns the acceptance
// process and all descendants in a strict kill-on-close Windows Job Object.
// Killing the watchdog closes that job even if its Go cleanup cannot run.
type boundedCapture struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.data)+len(p) > 16384 {
		b.overflow = true
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *boundedCapture) bytes() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.overflow
}

func supervise(args []string) {
	scope := "synthetic-loopback-only"
	failed := func() {
		_ = json.NewEncoder(os.Stdout).Encode(summary{Schema: 1, Scope: scope,
			Status: "failed", Stage: "supervisor", Failure: "supervision_failed"})
	}
	opts, err := arguments(args)
	if err != nil {
		failed()
		os.Exit(1)
	}
	scope = acceptanceScope(opts.acceptanceProfile)
	if checkEnvironment(os.Environ()) != nil {
		failed()
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		failed()
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 265*time.Second)
	defer cancel()
	cmd := proc.Command(exe, args...)
	cmd.Env, cmd.Stderr = os.Environ(), nil
	cmd.WaitDelay = 2 * time.Second
	var output boundedCapture
	cmd.Stdout = &output
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil {
		failed()
		os.Exit(1)
	}
	done := make(chan error, 1)
	go func() { done <- tree.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		tree.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		failed()
		os.Exit(1)
	}
	data, overflow := output.bytes()
	var report summary
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if overflow || d.Decode(&report) != nil || !validSummaryForProfile(report, opts.acceptanceProfile) ||
		(err == nil) != (report.Status == "passed") {
		failed()
		os.Exit(1)
	}
	// Decode a second value to reject concatenated reports and trailing data.
	var extra any
	if d.Decode(&extra) != io.EOF || !bytes.Equal(data, append(jsonBytes(report), '\n')) {
		failed()
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if err != nil {
		os.Exit(1)
	}
}

func validSummary(s summary) bool {
	return validSummaryForProfile(s, "")
}

func validSummaryForProfile(s summary, profile string) bool {
	pinnedSHA, err := acceptanceSHA(profile)
	if err != nil {
		return false
	}
	if s.Schema != 1 || s.Scope != acceptanceScope(profile) ||
		(s.Status != "passed" && s.Status != "failed") ||
		(s.NativeSHA256 != "" && s.NativeSHA256 != pinnedSHA) {
		return false
	}
	stages := map[string]bool{}
	for _, v := range []string{"inputs", "version", "initialize", "initial_config", "catalog", "resolved_config", "threads", "turns", "thread_list", "ownership", "final_config", "shutdown", "restart", "resume", "complete"} {
		stages[v] = true
	}
	if !stages[s.Stage] {
		return false
	}
	failures := map[string]bool{"": true}
	for _, v := range []string{
		"internal_failure", "input_invalid", "environment_rejected", "scratch_invalid", "scratch_not_empty",
		"native_hash_mismatch", "catalog_hash_mismatch", "catalog_invalid", "platform_rejected",
		"process_start_failed", "version_invalid", "fixture_invalid", "fixture_failed",
		"protocol_invalid", "protocol_limit", "unexpected_server_request", "native_error",
		"protocol_write_failed", "deadline", "process_ended", "unexpected_item", "cleanup_failed",
		"shutdown_failed", "logical_provider_mismatch", "project_not_empty", "ownership_changed",
		"config_shape_mismatch", "turn_failed", "answer_crossed_route", "unexpected_auth_data",
		"control_home_mismatch", "thread_list_invalid",
	} {
		failures[v] = true
	}
	if !failures[s.Failure] {
		return false
	}
	counts := []int{s.ConfigReads, s.LayerReads, s.StableLayerRereads, s.LayerVersionChanges, s.CatalogModels, s.Threads, s.Turns, s.AssistantMessages, s.ThreadReads, s.ThreadListQueries, s.ThreadListPages, s.ThreadListItems, s.OwnershipRejections, s.AlphaRequests, s.BetaRequests, s.FixtureErrors, s.UnexpectedServerRequests}
	for _, n := range counts {
		if n < 0 || n > 2048 {
			return false
		}
	}
	if s.RestartCycles < 0 || s.RestartCycles > 1 || s.ResumedThreads < 0 || s.ResumedThreads > 2 ||
		(s.ResumedThreads > 0 && s.RestartCycles != 1) ||
		(profile != acceptanceSynthetic4239 && (s.RestartCycles != 0 || s.ResumedThreads != 0)) {
		return false
	}
	if s.Status == "failed" {
		return s.Failure != ""
	}
	if profile == acceptanceSynthetic4239 && (s.RestartCycles != 1 || s.ResumedThreads != 2) {
		return false
	}
	want := []int{9, 6, 3, 0, 2, 2, 4, 4, 2, 2, 4, 4, 2, 2, 2, 0, 0}
	for i, n := range counts {
		if n != want[i] {
			return false
		}
	}
	return s.Stage == "complete" && s.Failure == "" && s.NativeSHA256 == pinnedSHA &&
		s.BinaryVerified && s.VersionVerified && s.AdapterCleanExit && s.CleanupComplete
}
