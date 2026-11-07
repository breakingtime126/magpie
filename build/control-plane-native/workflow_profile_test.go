package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

const standalonePushExclusion = " && !(github.event_name == 'push' && contains(github.event.head_commit.message, '[app-server-4239-v1]'))"

func workflowText(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil || len(data) > 1<<20 {
		t.Fatal("workflow source unavailable", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func workflowCondition(t *testing.T, source, job string) string {
	t.Helper()
	parts := strings.Split(source, "\n  "+job+":\n")
	if len(parts) != 2 {
		t.Fatal("workflow job is missing or ambiguous", job)
	}
	lines := strings.Split(parts[1], "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "    if: ") {
			value := strings.TrimPrefix(line, "    if: ")
			if value != ">-" {
				return value
			}
			var condition []string
			for _, continuation := range lines[i+1:] {
				if !strings.HasPrefix(continuation, "      ") {
					break
				}
				condition = append(condition, strings.TrimSpace(continuation))
			}
			return strings.Join(condition, " ")
		}
		if line != "" && !strings.HasPrefix(line, "    ") {
			break
		}
	}
	t.Fatal("workflow job has no explicit condition", job)
	return ""
}

func TestStandaloneWorkflowExcludesExistingRuntimeJobs(t *testing.T) {
	for name, jobs := range map[string][]string{
		"experimental-desktop-policy-trace.yml": {"observer-unit", "observed-policy-read", "welcome-helper-offline", "native-original-ui", "native-contract-review"},
		"experimental-bridge-windows.yml":       {"bridge-race", "windows-amd64"},
		"experimental-desktop-cold-start.yml":   {"official-baseline", "bridge-alias"},
	} {
		source := workflowText(t, name)
		for _, job := range jobs {
			condition := workflowCondition(t, source, job)
			if !strings.HasSuffix(condition, standalonePushExclusion) || strings.Count(condition, standalonePushExclusion) != 1 {
				t.Fatal("standalone marker can reach an existing runtime job", name, job)
			}
		}
	}
	// The exact exclusion is conditional on push; dispatch retains its old logic.
	if !strings.Contains(standalonePushExclusion, "!(github.event_name == 'push' &&") {
		t.Fatal("standalone exclusion changed workflow_dispatch")
	}
}

func TestStandaloneWorkflowRejectsEveryConflictingMarker(t *testing.T) {
	source := workflowText(t, "experimental-control-plane-native-4239.yml")
	condition := workflowCondition(t, source, "offline")
	for _, required := range []string{
		"github.repository == 'breakingtime126/magpie'", "github.ref == 'refs/heads/experiment/app-server-bridge-v0.1.3035'",
		"github.event_name == 'push'", "&& contains(github.event.head_commit.message, '[app-server-4239-v1]')",
	} {
		if !strings.Contains(condition, required) {
			t.Fatal("standalone workflow lost an explicit trigger guard")
		}
	}
	for _, marker := range []string{"native-entry-4239-v1", "winapp-inspection-v0.7.1", "bridge-ui-4239-v1", "bridge-welcome-4239-v1", "package-identity-only-v1", "native-ui-inspection-v1", "native-contract-review-v1"} {
		if !strings.Contains(condition, "!contains(github.event.head_commit.message, '["+marker+"]')") {
			t.Fatal("standalone workflow permits a conflicting marker", marker)
		}
	}
	for _, required := range []string{"permissions:\n  contents: read\n", "  native:\n    needs: offline\n",
		"      - .github/workflows/experimental-control-plane-native-4239.yml\n      - build/control-plane-native/**\n      - internal/appserverbridge/experimental_feature_run_test.go\n"} {
		if !strings.Contains(source, required) {
			t.Fatal("standalone workflow lost a scope or prerequisite boundary")
		}
	}
}

func TestStandalonePatchPathsCannotStartOtherDesktopWorkflows(t *testing.T) {
	changed := []string{".github/workflows/experimental-control-plane-native-4239.yml", ".github/workflows/experimental-desktop-policy-trace.yml",
		".github/workflows/experimental-bridge-windows.yml", ".github/workflows/experimental-desktop-cold-start.yml",
		"internal/appserverbridge/experimental_feature_run_test.go", "build/control-plane-native/main.go", "docs/subsystems/app-server-bridge.md"}
	for _, name := range []string{"experimental-desktop-native-entry.yml", "experimental-desktop-native-entry-4239.yml"} {
		source := workflowText(t, name)
		parts := strings.Split(source, "    paths:\n")
		if len(parts) != 2 {
			t.Fatal("Desktop workflow path guard is missing or ambiguous", name)
		}
		for _, line := range strings.Split(parts[1], "\n") {
			if !strings.HasPrefix(line, "      - ") {
				break
			}
			pattern := strings.TrimPrefix(line, "      - ")
			if strings.HasPrefix(pattern, "build/control-plane-native/") {
				t.Fatal("another Desktop workflow watches standalone harness files", name, pattern)
			}
			for _, candidate := range changed {
				matched, err := path.Match(pattern, candidate)
				if err != nil || matched || (strings.HasSuffix(pattern, "/**") && strings.HasPrefix(candidate, strings.TrimSuffix(pattern, "**"))) {
					t.Fatal("standalone changed path can start another Desktop workflow", name, pattern)
				}
			}
		}
	}
}

func TestStandaloneWorkflowRequiresFeatureFixtureRegressions(t *testing.T) {
	source := workflowText(t, "experimental-control-plane-native-4239.yml")
	for _, required := range []string{
		"cp internal/appserverbridge/experimental_feature_run_test.go .ci-app-server-4239-base/internal/appserverbridge/",
		"python3 build/control-plane-native/mutate-shared-response.py",
		"--- FAIL: TestRunExperimentalFeature167LateSiblingUsesOriginalGeneration",
		"delayed native child read a different request's fixture response",
		"go test -race -count=5 -timeout=5m -tags nogui",
		"go test -count=20 -timeout=10m -tags nogui",
		"TestRunExperimentalFeature167(FreshPagesAndFailures|LateSiblingUsesOriginalGeneration)|TestExperimentalFeatureFixtureLateReadsKeepTheirGeneration",
		"export GOPATH=", "$saved[$name] = [Environment]::GetEnvironmentVariable($name)",
		"[Environment]::SetEnvironmentVariable($name, $directory, 'Process')",
		"[Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process')",
	} {
		if !strings.Contains(source, required) {
			t.Fatal("standalone workflow lost its isolated fixture regression gate")
		}
	}
}
