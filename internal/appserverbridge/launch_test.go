package appserverbridge

import (
	"reflect"
	"testing"
)

func TestLaunchOptionsPreserveReviewedControlsAndPinnedProvider(t *testing.T) {
	for _, test := range []struct {
		name    string
		options LaunchOptions
		want    []string
	}{
		{
			name: "legacy",
			want: []string{"app-server", "-c", `model_provider="aihub"`},
		},
		{
			name:    "code mode",
			options: LaunchOptions{CodeModeHost: true},
			want:    []string{"-c", "features.code_mode_host.enabled=true", "app-server", "-c", `model_provider="aihub"`},
		},
		{
			name:    "analytics default only",
			options: LaunchOptions{AnalyticsDefaultEnabled: true},
			want:    []string{"app-server", "--analytics-default-enabled", "-c", `model_provider="aihub"`},
		},
		{
			name:    "observed Desktop base",
			options: LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true},
			want:    []string{"-c", "features.code_mode_host.enabled=true", "app-server", "--analytics-default-enabled", "-c", `model_provider="aihub"`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.options.backendArgs("aihub"); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("backend arguments changed: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestLaunchArgumentSlicesAreIndependent(t *testing.T) {
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	first := options.backendArgs("openai")
	second := options.backendArgs("aihub")
	first[0] = "changed"
	first[len(first)-1] = "changed"
	if second[0] != "-c" || second[len(second)-1] != `model_provider="aihub"` {
		t.Fatal("backend launch controls share mutable argument storage")
	}
}
