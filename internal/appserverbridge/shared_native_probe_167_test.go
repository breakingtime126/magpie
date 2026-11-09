package appserverbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestSharedNativeProbe167UsesPinnedRoutesAndGenuinePair(t *testing.T) {
	for _, badSecond := range []bool{false, true} {
		cfg := sharedNativeConfigFixture167(t)
		cfg.StartupProbe = StartupProbeCompareUnrestricted167
		cfg.AuthStatus = AuthStatusPrimaryTrustedStdio167
		cfg.RequirementsRead = ConfigRequirementsCompareNull167
		data, err := os.ReadFile(cfg.Backends[0].Command)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		for i := range cfg.Backends {
			cfg.Backends[i].StartupProbeSHA256 = hex.EncodeToString(digest[:])
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal("exact startup/auth/requirements combination was rejected")
		}
		calls := 0
		info := StartupClientInfo{Name: "synthetic", Version: "1"}
		infoJSON, _ := startupClientInfoJSON(info)
		good := []byte(`{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true}}`)
		out, err := probeStartup(context.Background(), cfg, LaunchOptions{}, info, time.Second, func(_ context.Context, b Backend, cwd string, args []string) ([]byte, error) {
			calls++
			for _, arg := range args {
				if arg == "--listen" || arg == "--stdio" {
					t.Fatal("one-shot probe included a conflicting serve transport")
				}
			}
			want := append(cfg.nativeProbeArgs167(LaunchOptions{}, b), "--print-startup-config", "--startup-client-info", string(infoJSON))
			if !reflect.DeepEqual(args, want) || cwd != cfg.WorkingDirectory {
				t.Fatal("one-shot probe lost shared route overrides or working context")
			}
			if badSecond && calls == 2 {
				return []byte(`{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":{"requirements":{},"supportsIndependentSpeedModes":true}}`), nil
			}
			return good, nil
		})
		if calls != 2 || (err != nil) != badSecond || (badSecond && len(out) != 0) {
			t.Fatal("one-shot comparison manufactured shared success or skipped an authority")
		}
		if !badSecond {
			want, _ := validateStartupProbe(good, cfg.StartupProbe)
			if string(out) != string(want) {
				t.Fatal("probe changed genuine validated response")
			}
		}
	}
}
