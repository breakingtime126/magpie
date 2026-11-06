package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This profile is deliberately literal so its end-to-end regression tests can
// also run against the old implementation without importing a new constant.
const probe167Mode = "compare-unrestricted-v0.167.0-alpha.3.2"
const probe167SuccessFixture = `{"appServerVersion":"0.167.0-alpha.3.2","configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true}}`

func TestStartupProbe167ExactProfile(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = probe167Mode
	cfg.WorkingDirectory = t.TempDir()
	cfg.Backends[1].Provider = "second-provider"
	title := "Codex Desktop"
	info := StartupClientInfo{Name: "codex_desktop", Title: &title, Version: "26.1103.3517.0"}
	identity, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	calls := 0
	got, err := probeStartup(context.Background(), cfg, options, info, time.Minute,
		func(ctx context.Context, b Backend, directory string, args []string) ([]byte, error) {
			if b.ID != cfg.Backends[calls].ID {
				t.Fatal("backend order or authority changed")
			}
			calls++
			wantArgs := append(options.backendArgs(b.Provider), "--print-startup-config", "--startup-client-info", string(identity))
			wantDirectory, err := filepath.EvalSymlinks(cfg.WorkingDirectory)
			if err != nil || directory != wantDirectory || !reflect.DeepEqual(args, wantArgs) {
				t.Fatal("new profile changed client identity, provider, launch controls or cwd")
			}
			if calls == 1 {
				return []byte(probe167SuccessFixture + "\n"), nil
			}
			return []byte(` {"configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null},"appServerVersion":"0.167.0-alpha.3.2"} `), nil
		})
	if err != nil || calls != 2 || string(got) != probe167SuccessFixture {
		t.Fatalf("exact new startup profile failed: calls=%d err=%v", calls, err)
	}
}

func TestStartupProbe167RejectsBeforeEitherAuthority(t *testing.T) {
	base := startupProbeFixtureConfig(t)
	base.StartupProbe = probe167Mode
	for name, mutate := range map[string]func(*Config){
		"default off": func(c *Config) {
			c.StartupProbe = ""
		},
		"unknown mode": func(c *Config) {
			c.StartupProbe = "compare-unrestricted-v0.167"
		},
		"unknown patch": func(c *Config) {
			c.StartupProbe = "compare-unrestricted-v0.167.0-alpha.3.3"
		},
		"missing first pin": func(c *Config) {
			c.Backends[0].StartupProbeSHA256 = ""
		},
		"missing second pin": func(c *Config) {
			c.Backends[1].StartupProbeSHA256 = ""
		},
		"first digest mismatch": func(c *Config) {
			c.Backends[0].StartupProbeSHA256 = strings.Repeat("0", 64)
		},
		"second digest mismatch": func(c *Config) {
			c.Backends[1].StartupProbeSHA256 = strings.Repeat("0", 64)
		},
		"mixed requirements": func(c *Config) {
			c.RequirementsRead = ConfigRequirementsCompareSafe
		},
		"mixed attestation": func(c *Config) {
			c.Attestation = AttestationUnavailable
		},
		"mixed gateway": func(c *Config) {
			c.GatewayOAuth = GatewayOAuthPrimaryRead
			c.GatewayOAuthProviderName = "Fixture provider"
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			mutate(&cfg)
			calls := 0
			got, err := probeStartup(context.Background(), cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"}, time.Minute,
				func(context.Context, Backend, string, []string) ([]byte, error) {
					calls++
					return []byte(probe167SuccessFixture), nil
				})
			if err == nil || got != nil || calls != 0 {
				t.Fatal("rejected profile, mixed version or failed pin reached a child")
			}
		})
	}
}

func TestStartupProbeProfilesDoNotMixResultsOrRetry(t *testing.T) {
	base := startupProbeFixtureConfig(t)
	for _, profile := range []struct {
		mode  string
		good  string
		other string
	}{
		{StartupProbeCompareUnrestricted, probeSuccessFixture, probe167SuccessFixture},
		{probe167Mode, probe167SuccessFixture, probeSuccessFixture},
	} {
		t.Run(profile.mode, func(t *testing.T) {
			cfg := base
			cfg.StartupProbe = profile.mode
			for name, output := range map[string]string{
				"other reviewed version": profile.other,
				"false speed support":    strings.Replace(profile.good, ":true", ":false", 1),
				"non-null policy":        strings.Replace(profile.good, `"requirements":null`, `"requirements":{"network":{"enabled":false}}`, 1),
				"new metadata":           strings.Replace(profile.good, "{", `{"extra":"private-canary",`, 1),
				"official failure":       `{"error":{"code":-32600,"message":"private-canary"}}`,
				"exit failure":           profile.good,
			} {
				for _, index := range []int{0, 1} {
					t.Run(name+"/"+strconv.Itoa(index), func(t *testing.T) {
						calls := 0
						got, err := probeStartup(context.Background(), cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"}, time.Minute,
							func(context.Context, Backend, string, []string) ([]byte, error) {
								current := calls
								calls++
								if current == index {
									if name == "exit failure" {
										return []byte(output), errors.New("private-canary")
									}
									return []byte(output), nil
								}
								return []byte(profile.good), nil
							})
						if err != errStartupProbe || got != nil || calls != index+1 {
							t.Fatal("different results or failed child were accepted, retried or exposed")
						}
					})
				}
			}
		})
	}
}

func TestStartupProbe167NativeChild(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = probe167Mode
	for _, b := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(b.Home, "startup-probe-mode"), []byte("success-167"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	info := StartupClientInfo{Name: "codex_desktop", Version: "26.1103.3517.0"}
	got, err := ProbeStartup(context.Background(), cfg, LaunchOptions{}, info)
	if err != nil || !bytes.Equal(got, []byte(probe167SuccessFixture)) {
		t.Fatalf("native new-profile probe failed: %v", err)
	}
	identity, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "startup-probe-report"))
		if err != nil {
			t.Fatal(err)
		}
		var report fixtureReport
		if json.Unmarshal(data, &report) != nil {
			t.Fatal("missing new-profile native report")
		}
		want := append((LaunchOptions{}).backendArgs(b.Provider), "--print-startup-config", "--startup-client-info", string(identity))
		if !reflect.DeepEqual(report.Args, want) {
			t.Fatal("native new profile changed client identity or provider")
		}
		assertStartupProbeImageReleased(t, b.Command)
		assertStartupProbeProcessExited(t, report.PID)
	}
}
