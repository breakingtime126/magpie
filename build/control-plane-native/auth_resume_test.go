package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resumeReader struct {
	result json.RawMessage
	err    error
	method string
	params any
}

func (r *resumeReader) rpc(method string, params any) (json.RawMessage, error) {
	r.method, r.params = method, params
	return r.result, r.err
}

func TestSyntheticResumeKeepsExactThreadProviderAndModel(t *testing.T) {
	good := `{"model":"magpie/alpha/test-alpha","modelProvider":"magpie_bridge","thread":{"id":"saved-alpha","modelProvider":"magpie_bridge"}}`
	r := &resumeReader{result: json.RawMessage(good)}
	if err := resumeSyntheticThread(r, "saved-alpha", "alpha"); err != nil || r.method != "thread/resume" {
		t.Fatal("valid saved thread rejected")
	}
	params, ok := r.params.(map[string]any)
	if !ok || len(params) != 3 || params["threadId"] != "saved-alpha" ||
		params["modelProvider"] != "magpie_bridge" || params["model"] != "magpie/alpha/test-alpha" {
		t.Fatal("resume ownership request changed")
	}
	for _, bad := range []string{
		strings.Replace(good, "saved-alpha", "other", 1),
		strings.Replace(good, "magpie/alpha/test-alpha", "magpie/beta/test-beta", 1),
		strings.Replace(good, "magpie_bridge", "alpha_provider", 1),
		strings.ReplaceAll(good, "magpie_bridge", "beta_provider"),
		strings.Replace(good, `"model":"magpie/alpha/test-alpha"`, `"model":null`, 1),
	} {
		if resumeSyntheticThread(&resumeReader{result: json.RawMessage(bad)}, "saved-alpha", "alpha") == nil {
			t.Fatal("changed resume identity accepted")
		}
	}
	if resumeSyntheticThread(&resumeReader{err: failure("native_error")}, "saved-alpha", "alpha") != failure("native_error") {
		t.Fatal("native resume failure hidden")
	}
}

func TestSyntheticRestartAuditRequiresOneCompleteRoundPerOwner(t *testing.T) {
	var first assistantAudit
	auditOwners(t, &first)
	for _, name := range []string{"alpha", "beta"} {
		auditedTurn(t, &first, name, 1, true)
	}
	if n, err := first.finishRounds(1); err != nil || n != 2 {
		t.Fatal("complete first generation rejected")
	}
	// A different process has a fresh audit; no mutex/map/state copy is used.
	var second assistantAudit
	auditOwners(t, &second)
	for _, name := range []string{"alpha", "beta"} {
		auditedTurn(t, &second, name, 2, false)
	}
	if n, err := second.finishRounds(1); err != nil || n != 2 {
		t.Fatal("complete second generation rejected")
	}
	if _, err := second.finish(); err == nil {
		t.Fatal("legacy four-turn audit weakened")
	}
	for _, rounds := range []int{0, 3} {
		var a assistantAudit
		fourAuditedTurns(t, &a)
		if _, err := a.finishRounds(rounds); err == nil {
			t.Fatal("unbounded checkpoint accepted")
		}
	}
	var incomplete assistantAudit
	auditOwners(t, &incomplete)
	auditedTurn(t, &incomplete, "alpha", 1, false)
	if _, err := incomplete.finishRounds(1); err == nil {
		t.Fatal("missing owner's completion accepted")
	}
}

func TestSyntheticColdDiscoveryAllowsOnlyUnavailableModel(t *testing.T) {
	threads := [2]string{"saved-alpha", "saved-beta"}
	good := `{"data":[{"id":"saved-alpha","modelProvider":"magpie_bridge","model":null,"preview":"synthetic","ephemeral":false,"source":"vscode"}],"nextCursor":null}`
	if _, _, err := checkThreadListPageAvailability([]byte(good), threads, map[string]bool{}, true, acceptanceSynthetic4239, true); err != nil {
		t.Fatal("explicit native unavailable model rejected during cold discovery")
	}
	if _, _, err := checkThreadListPageForProfile([]byte(good), threads, map[string]bool{}, true, acceptanceSynthetic4239); err == nil {
		t.Fatal("loaded model requirement weakened")
	}
	for _, bad := range []string{
		strings.Replace(good, `"model":null,`, "", 1),
		strings.Replace(good, `"model":null`, `"model":"test-alpha"`, 1),
		strings.Replace(good, `"model":null`, `"model":"magpie/beta/test-beta"`, 1),
		strings.Replace(good, "magpie_bridge", "beta_provider", 1),
		strings.Replace(good, "saved-alpha", "unknown", 1),
	} {
		if _, _, err := checkThreadListPageAvailability([]byte(bad), threads, map[string]bool{}, true, acceptanceSynthetic4239, true); err == nil {
			t.Fatal("foreign or missing metadata treated as unavailable")
		}
	}
}

func TestSyntheticCredentialRotationRequiresHistoryAndNewBearer(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	post := func(f *fixture, auth, input string) int {
		t.Helper()
		body := jsonBytes(map[string]any{"model": "test-alpha", "stream": true, "input": json.RawMessage(input)})
		req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/responses", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", auth)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	_, value, _ := syntheticCredential("alpha")
	old, renewed := "Bearer "+value, "Bearer "+value+"-RENEWED"
	history := `[{"type":"message","role":"user","content":[{"type":"input_text","text":"synthetic-alpha-turn-1"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fixture-answer-alpha"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"synthetic-alpha-turn-2"}]}]`
	for _, tc := range []struct {
		name, auth, input string
		accepted          bool
	}{
		{"complete", renewed, history, true},
		{"old-token", old, history, false},
		{"foreign-answer", renewed, strings.Replace(history, "fixture-answer-alpha", "fixture-answer-beta", 1), false},
		{"wrong-role", renewed, strings.Replace(history, `"role":"assistant"`, `"role":"user"`, 1), false},
		{"wrong-content-type", renewed, strings.Replace(history, "output_text", "input_text", 1), false},
		{"unstructured-substring", renewed, `"synthetic-alpha-turn-1 fixture-answer-alpha synthetic-alpha-turn-2"`, false},
		{"missing-user-history", renewed, strings.Replace(history, "synthetic-alpha-turn-1", "missing", 1), false},
		{"missing-assistant-history", renewed, strings.Replace(history, "fixture-answer-alpha", "missing", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWithAuth("alpha", "test-alpha", "", old)
			defer f.close()
			if f.rotateSyntheticAuth() == nil {
				t.Fatal("rotation before first response accepted")
			}
			if post(f, old, `"synthetic-alpha-turn-1"`) != http.StatusOK || f.rotateSyntheticAuth() != nil {
				t.Fatal("first response or single rotation failed")
			}
			if f.rotateSyntheticAuth() == nil {
				t.Fatal("repeated rotation accepted")
			}
			status := post(f, tc.auth, tc.input)
			requests, failures := f.counts()
			if tc.accepted {
				if status != http.StatusOK || requests != 2 || failures != 0 {
					t.Fatal("renewed credential and persisted context rejected")
				}
			} else if status != http.StatusBadRequest || requests != 1 || failures != 1 {
				t.Fatal("old credential or missing context accepted")
			}
		})
	}
}

func TestSyntheticResumeCountersCannotEnterLegacySummary(t *testing.T) {
	s := passedSummary()
	s.RestartCycles, s.ResumedThreads = 1, 2
	if validSummary(s) || validSummaryForProfile(s, acceptance4239) {
		t.Fatal("synthetic lifecycle accepted by legacy profile")
	}
	s.Scope, s.NativeSHA256 = selfAuthoredScope, native4239SHA
	s.Status, s.Stage, s.Failure = "failed", "resume", "native_error"
	s.RestartCycles = 0
	if validSummaryForProfile(s, acceptanceSynthetic4239) {
		t.Fatal("resume progress accepted without a restart")
	}
}
