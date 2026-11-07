package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandaloneAcceptancePinsAreSeparateAndExact(t *testing.T) {
	old := "40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39"
	current := "4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382"
	if nativeSHA != old || nativeVersion != "codex-cli 0.167.0-alpha.3.2" ||
		catalogBlob != "489ebe3f2483348641a16160ce8491dd077eabca" {
		t.Fatal("default runtime, version or catalog proof changed")
	}
	root := t.TempDir()
	args := []string{"-bridge", filepath.Join(root, "bridge.exe"), "-codex", filepath.Join(root, "codex.exe"),
		"-catalog", filepath.Join(root, "models.json"), "-scratch-parent", root}
	for _, tc := range []struct{ profile, want string }{{"", old}, {"app-server-4239", current}} {
		opts, err := arguments(append(append([]string(nil), args...), "-acceptance-profile", tc.profile))
		if err != nil || opts.acceptanceProfile != tc.profile {
			t.Fatal("explicit profile was not preserved", err)
		}
		pin, err := acceptanceSHA(opts.acceptanceProfile)
		if err != nil || pin != tc.want {
			t.Fatal("profile selected the wrong fixed pin", err)
		}
	}
	opts, err := arguments(args)
	if err != nil || opts.acceptanceProfile != "" {
		t.Fatal("omitted option changed default acceptance")
	}
	for _, invalid := range []string{"4239", "desktop-4239", "app-server-4240", "APP-SERVER-4239", "app-server-4239 ", old, current, strings.Repeat("0", 64)} {
		if pin, err := acceptanceSHA(invalid); pin != "" || err != failure("input_invalid") {
			t.Fatal("unreviewed profile or caller-supplied hash was accepted")
		}
		if _, err := arguments(append(append([]string(nil), args...), "-acceptance-profile", invalid)); err != failure("input_invalid") {
			t.Fatal("invalid profile reached input setup")
		}
		report := summary{}
		if err := run(context.Background(), options{acceptanceProfile: invalid}, &report); err != failure("input_invalid") || report.BinaryVerified {
			t.Fatal("invalid profile reached runtime or binary inspection")
		}
	}
	if _, err := arguments(append(append([]string(nil), args...), "-reviewed-build", "desktop-4239")); err != failure("input_invalid") {
		t.Fatal("Desktop fixture opt-in enabled standalone acceptance")
	}
	var out bytes.Buffer
	if err := runDesktopFixtures([]string{"-acceptance-profile", "app-server-4239"}, io.NopCloser(strings.NewReader("")), &out, &desktopAudit{}); err != failure("input_invalid") || out.Len() != 0 {
		t.Fatal("standalone opt-in enabled Desktop fixture setup")
	}
}

func TestStandaloneAcceptanceSummaryIsBoundToRequestedProfile(t *testing.T) {
	old := passedSummary()
	current := old
	current.NativeSHA256 = "4b3286e35d7fe866c6b0eb3f07d7a4cdb5e96489576170dfd6b3a15aa1c67382"
	if !validSummary(old) || validSummary(current) || !validSummaryForProfile(current, "app-server-4239") ||
		validSummaryForProfile(old, "app-server-4239") || validSummaryForProfile(current, "desktop-4239") {
		t.Fatal("summary accepted a different runtime from the explicit profile")
	}
	for _, change := range []func(*summary){
		func(s *summary) { s.LayerVersionChanges = 1 },
		func(s *summary) { s.ThreadListQueries = 0 },
		func(s *summary) { s.ThreadListPages = 3 },
		func(s *summary) { s.AssistantMessages = 3 },
		func(s *summary) { s.OwnershipRejections = 0 },
		func(s *summary) { s.VersionVerified = false },
		func(s *summary) { s.CleanupComplete = false },
		func(s *summary) { s.UnexpectedServerRequests = 1 },
	} {
		bad := current
		change(&bad)
		if validSummaryForProfile(bad, "app-server-4239") {
			t.Fatal("4239 profile weakened an existing acceptance oracle")
		}
	}
}
