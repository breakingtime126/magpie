package appserverbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// Shape and metadata come from the reviewed official 0.166.0-alpha.6
// startup_config.rs and ConfigRequirementsReadResponse, not a JSONL reply.
const probeSuccessFixture = `{"appServerVersion":"0.166.0-alpha.6","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}`

func TestParseStartupClientInfoStrict(t *testing.T) {
	for _, input := range []string{
		`{"name":"codex_desktop","version":"26.1029.7803.0"}`,
		`{"name":"codex_desktop","version":"26.1029.7803.0","title":null}`,
		`{"name":"codex_desktop","version":"26.1029.7803.0","title":""}`,
		`{"version":"26.1029.7803.0","title":"Codex 桌面","name":"codex_desktop"}`,
	} {
		info, err := ParseStartupClientInfo(input)
		if err != nil || info.Name != "codex_desktop" || info.Version != "26.1029.7803.0" {
			t.Fatal("rejected reviewed client identity")
		}
		encoded, err := startupClientInfoJSON(info)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseStartupClientInfo(string(encoded))
		if err != nil || !reflect.DeepEqual(info, again) {
			t.Fatal("client identity changed on round trip")
		}
	}
	for _, input := range []string{
		``, `null`, `[]`, `{}`, `true`, `{"name":"n"}`, `{"version":"v"}`,
		`{"name":"","version":"v"}`, `{"name":null,"version":"v"}`,
		`{"name":"n","version":""}`, `{"name":"n","version":null}`,
		`{"name":42,"version":"v"}`, `{"name":"n","version":true}`,
		`{"name":"n","version":"v","title":[]}`,
		`{"name":"n","version":"v","title":{}}`,
		`{"name":"n","version":"v","unknown":"secret"}`,
		`{"Name":"n","version":"v"}`,
		`{"name":"n","name":"n","version":"v"}`,
		`{"name":"n","\u006eame":"n","version":"v"}`,
		`{"name":"n","version":"v","version":"v"}`,
		`{"name":"n","version":"v","title":null,"title":null}`,
		`{"name":"n","version":"v"} {}`,
		`{"name":"n","version":"v",}`,
		`{"name":"n\u0000","version":"v"}`,
		`{"name":"n","version":"v\n"}`,
		`{"name":"n","version":"v","title":"bad\u007f"}`,
		`{"name":"n","version":"v","title":"bad\u0085"}`,
		`{"name":"n","version":"v","title":"\ud800"}`,
		"{\"name\":\"\xff\",\"version\":\"v\"}",
		strings.Repeat(" ", startupClientInfoLimit+1),
	} {
		if _, err := ParseStartupClientInfo(input); err != errStartupClientInfo {
			t.Fatal("malformed identity did not return the static rejection")
		}
	}
	input := `{"name":"n","version":"v"}`
	input += strings.Repeat(" ", startupClientInfoLimit-len(input))
	if _, err := ParseStartupClientInfo(input); err != nil {
		t.Fatal("identity at byte bound rejected")
	}
	if _, err := ParseStartupClientInfo(input + " "); err == nil {
		t.Fatal("identity above byte bound accepted")
	}
}

func TestStartupProbeSuccessSchemaIsCompleteAndExact(t *testing.T) {
	want, err := validateStartupProbe([]byte(probeSuccessFixture))
	if err != nil {
		t.Fatal("reviewed startup success rejected")
	}
	for _, input := range []string{
		probeSuccessFixture + "\n",
		` {"configRequirements":{"requirements":null,"supportsIndependentSpeedModes":true},"appServerVersion":"0.166.0-alpha.6"} `,
		strings.Replace(probeSuccessFixture, "0.166", `0.\u003166`, 1),
		probeSuccessFixture + strings.Repeat(" ", startupProbeLimit-len(probeSuccessFixture)),
	} {
		got, err := validateStartupProbe([]byte(input))
		if err != nil || !bytes.Equal(got, want) || bytes.Contains(got, []byte("\n")) {
			t.Fatal("equivalent complete startup success changed")
		}
	}
	bad := []string{
		``, `null`, `[]`, `{}`, `{"error":"secret diagnostic"}`,
		probeSuccessFixture + `{}`,
		probeSuccessFixture + "secret trailing output",
		strings.Replace(probeSuccessFixture, `"0.166.0-alpha.6"`, `null`, 1),
		strings.Replace(probeSuccessFixture, "0.166.0-alpha.6", "0.166.0-alpha.7", 1),
		strings.Replace(probeSuccessFixture, "0.166.0-alpha.6", "0.167.0-alpha.4", 1),
		strings.Replace(probeSuccessFixture, `"appServerVersion"`, `"AppServerVersion"`, 1),
		strings.Replace(probeSuccessFixture, `"appServerVersion":`, `"appServerVersion":"0.166.0-alpha.6","appServerVersion":`, 1),
		strings.Replace(probeSuccessFixture, `"configRequirements":`, `"configRequirements":{},"configRequirements":`, 1),
		strings.Replace(probeSuccessFixture, `"requirements":null`, `"requirements":null,"requirements":null`, 1),
		strings.Replace(probeSuccessFixture, `"requirements":null`, `"requirements":null,"\u0072equirements":null`, 1),
		strings.Replace(probeSuccessFixture, `"requirements":null`, `"requirements":null,"extra":"secret"`, 1),
		strings.Replace(probeSuccessFixture, `"requirements":null`, `"Requirements":null`, 1),
		strings.Replace(probeSuccessFixture, `,"requirements":null`, ``, 1),
		strings.Replace(probeSuccessFixture, `"supportsIndependentSpeedModes":true,`, ``, 1),
		strings.Replace(probeSuccessFixture, `:true`, `:false`, 1),
		strings.Replace(probeSuccessFixture, `:true`, `:null`, 1),
		strings.Replace(probeSuccessFixture, `:true`, `:"true"`, 1),
		strings.Replace(probeSuccessFixture, `"supportsIndependentSpeedModes":true`, `"supportsIndependentSpeedModes":true,"supportsIndependentSpeedModes":true`, 1),
		strings.Replace(probeSuccessFixture, `{`, `{"unknown":"secret",`, 1),
		probeSuccessFixture + strings.Repeat(" ", startupProbeLimit+1-len(probeSuccessFixture)),
		strings.Replace(probeSuccessFixture, "alpha", "\xffalpha", 1),
	}
	for _, value := range []string{`{}`, `[]`, `true`, `false`, `0`, `"null"`, `{"modelProviders":{"secret":"provider-value"}}`} {
		bad = append(bad, strings.Replace(probeSuccessFixture, `"requirements":null`, `"requirements":`+value, 1))
	}
	for _, input := range bad {
		if result, err := validateStartupProbe([]byte(input)); err != errStartupProbe || result != nil {
			t.Fatal("unsupported schema was accepted or exposed raw backend output")
		}
	}
}

func startupProbeFixtureConfig(t *testing.T) Config {
	t.Helper()
	cfg := fixtureConfig(t)
	cfg.StartupProbe = StartupProbeCompareUnrestricted
	for i := range cfg.Backends {
		data, err := os.ReadFile(cfg.Backends[i].Command)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		cfg.Backends[i].StartupProbeSHA256 = hex.EncodeToString(digest[:])
	}
	return cfg
}

func TestStartupProbeRejectsBeforeAnyChild(t *testing.T) {
	base := startupProbeFixtureConfig(t)
	validInfo := StartupClientInfo{Name: "fixture", Version: "1"}
	for name, mutate := range map[string]func(*Config, *StartupClientInfo){
		"disabled": func(c *Config, i *StartupClientInfo) {
			c.StartupProbe = ""
		},
		"unknown mode": func(c *Config, i *StartupClientInfo) {
			c.StartupProbe = "future"
		},
		"invalid config": func(c *Config, i *StartupClientInfo) {
			c.Backends = c.Backends[:1]
		},
		"bad identity": func(c *Config, i *StartupClientInfo) {
			i.Name = "\nsecret"
		},
		"missing identity": func(c *Config, i *StartupClientInfo) {
			i.Version = ""
		},
		"large identity": func(c *Config, i *StartupClientInfo) {
			i.Name = strings.Repeat("n", startupClientInfoLimit)
		},
		"first digest mismatch": func(c *Config, i *StartupClientInfo) {
			c.Backends[0].StartupProbeSHA256 = strings.Repeat("0", 64)
		},
		"second digest mismatch": func(c *Config, i *StartupClientInfo) {
			c.Backends[1].StartupProbeSHA256 = strings.Repeat("0", 64)
		},
		"malformed digest": func(c *Config, i *StartupClientInfo) {
			c.Backends[1].StartupProbeSHA256 = "secret"
		},
		"missing directory": func(c *Config, i *StartupClientInfo) {
			c.WorkingDirectory = filepath.Join(c.Backends[0].Home, "missing")
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			info := validInfo
			mutate(&cfg, &info)
			calls := 0
			got, err := probeStartup(context.Background(), cfg, LaunchOptions{}, info, time.Minute,
				func(context.Context, Backend, string, []string) ([]byte, error) {
					calls++
					return []byte(probeSuccessFixture), nil
				})
			if err == nil || got != nil || calls != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe preflight reached a child or leaked input")
			}
		})
	}
}

func TestStartupProbeComparesBothWithoutRetry(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	for _, failureIndex := range []int{-1, 0, 1} {
		for _, childError := range []bool{false, true} {
			calls := 0
			got, err := probeStartup(context.Background(), cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"}, time.Minute,
				func(context.Context, Backend, string, []string) ([]byte, error) {
					index := calls
					calls++
					if index == failureIndex {
						if childError {
							return []byte(probeSuccessFixture), errors.New("secret raw child error")
						}
						return []byte(strings.Replace(probeSuccessFixture, `"requirements":null`, `"requirements":{"secret":true}`, 1)), nil
					}
					return []byte(probeSuccessFixture), nil
				})
			if failureIndex == -1 {
				if err != nil || calls != 2 || len(got) == 0 || bytes.Contains(got, []byte("\n")) {
					t.Fatal("both complete equal results did not succeed once")
				}
			} else if err != errStartupProbe || got != nil || calls != failureIndex+1 {
				t.Fatal("failure continued, retried, or leaked a partial success")
			}
		}
	}
}

func TestStartupProbePreservesLaunchAndOneDeadline(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	cfg.WorkingDirectory = t.TempDir()
	cfg.Backends[1].Provider = "custom_provider"
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	title := "Codex fixture"
	info := StartupClientInfo{Name: "codex_desktop", Version: "26.1029.7803.0", Title: &title}
	identity, _ := json.Marshal(info)
	var firstDeadline time.Time
	calls := 0
	got, err := probeStartup(context.Background(), cfg, options, info, time.Minute,
		func(ctx context.Context, b Backend, directory string, args []string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok || (calls > 0 && deadline != firstDeadline) {
				t.Fatal("backend probes did not share one bounded deadline")
			}
			firstDeadline = deadline
			calls++
			want := append(options.backendArgs(b.Provider), "--print-startup-config", "--startup-client-info", string(identity))
			resolved, _ := filepath.EvalSymlinks(cfg.WorkingDirectory)
			if !reflect.DeepEqual(args, want) || directory != resolved {
				t.Fatal("probe changed reviewed launch arguments or cwd")
			}
			return []byte(probeSuccessFixture), nil
		})
	if err != nil || len(got) == 0 || calls != 2 {
		t.Fatal("valid shared startup failed")
	}
}

func TestStartupProbeTimeoutAndCancellationAreStatic(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	for _, expired := range []bool{false, true} {
		parent, cancel := context.WithCancel(context.Background())
		if expired {
			cancel()
		}
		got, err := probeStartup(parent, cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"}, 20*time.Millisecond,
			func(ctx context.Context, b Backend, directory string, args []string) ([]byte, error) {
				<-ctx.Done()
				return nil, errors.New("secret cancellation detail")
			})
		cancel()
		if err != errStartupProbe || got != nil {
			t.Fatal("timeout or cancellation was exposed or treated as success")
		}
	}
}

// Called by the existing package TestMain before ordinary app-server fixture
// dispatch. It uses only synthetic bytes and isolated temporary fixture homes.
func startupProbeFixture() bool {
	if len(os.Args) > 1 && os.Args[1] == "startup-probe-fixture-leaf" {
		os.Exit(probeFixtureLeaf())
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "--print-startup-config" {
			os.Exit(probeFixtureServer())
			return true
		}
	}
	return false
}

func probeFixtureServer() int {
	home := os.Getenv("HOME")
	mode, _ := os.ReadFile(filepath.Join(home, "startup-probe-mode"))
	directory, _ := os.Getwd()
	report, _ := json.Marshal(fixtureReport{Args: os.Args[1:], Environment: os.Environ(), Directory: directory, PID: os.Getpid()})
	if os.WriteFile(filepath.Join(home, "startup-probe-report"), report, 0600) != nil {
		return 2
	}
	// Null stdin must be EOF. Receiving any JSONL initialization is a failure.
	var input [1]byte
	if n, err := os.Stdin.Read(input[:]); n != 0 || err != io.EOF {
		return 3
	}
	fmt.Fprintln(os.Stderr, "secret fixture stderr must never escape")
	if strings.HasPrefix(string(mode), "descendant") {
		self, _ := os.Executable()
		leaf := proc.CommandContext(context.Background(), self, "startup-probe-fixture-leaf")
		leaf.Env = os.Environ()
		leaf.Stdout, leaf.Stderr = os.Stdout, os.Stderr
		if leaf.Start() != nil {
			return 4
		}
		// Wait for a real readiness marker, not an assumed scheduling delay.
		deadline := time.After(30 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(home, "startup-probe-leaf")); err == nil {
				break
			}
			select {
			case <-deadline:
				_ = leaf.Process.Kill()
				_ = leaf.Wait()
				return 5
			case <-time.After(time.Millisecond):
			}
		}
	}
	switch string(mode) {
	case "malformed":
		fmt.Print("secret malformed stdout")
	case "oversized":
		fmt.Print(strings.Repeat("s", startupProbeLimit+1))
	case "nonzero":
		fmt.Print(probeSuccessFixture)
		return 7
	case "hang", "descendant-hang":
		time.Sleep(time.Minute)
		return 8
	case "closed-output-hang":
		os.Stdout.Close()
		time.Sleep(time.Minute)
		return 9
	default:
		fmt.Println(probeSuccessFixture)
	}
	return 0
}

func probeFixtureLeaf() int {
	path := filepath.Join(os.Getenv("HOME"), "startup-probe-leaf")
	if os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
		return 10
	}
	// Hold inherited stdout/stderr after the leader exits; Tree.Wait must
	// sweep this child before the output reader can observe EOF.
	time.Sleep(time.Minute)
	return 0
}

func TestStartupProbeNativeChildStdioAndEnvironment(t *testing.T) {
	cfg := startupProbeFixtureConfig(t)
	t.Setenv("STARTUP_PROBE_AMBIENT_SECRET", "must-not-inherit")
	cfg.WorkingDirectory = t.TempDir()
	options := LaunchOptions{CodeModeHost: true, AnalyticsDefaultEnabled: true}
	info := StartupClientInfo{Name: "fixture", Version: "1"}
	got, err := ProbeStartup(context.Background(), cfg, options, info)
	if err != nil || bytes.Contains(got, []byte("secret")) || bytes.Contains(got, []byte("\n")) {
		t.Fatal("native one-shot output failed or exposed diagnostics")
	}
	identity, _ := json.Marshal(info)
	for _, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "startup-probe-report"))
		if err != nil {
			t.Fatal(err)
		}
		var report fixtureReport
		if json.Unmarshal(data, &report) != nil {
			t.Fatal("invalid native fixture report")
		}
		want := append(options.backendArgs(b.Provider), "--print-startup-config", "--startup-client-info", string(identity))
		directory, _ := filepath.EvalSymlinks(report.Directory)
		resolved, _ := filepath.EvalSymlinks(cfg.WorkingDirectory)
		environment := strings.Join(report.Environment, "\n")
		if !reflect.DeepEqual(report.Args, want) || directory != resolved || strings.Contains(environment, "must-not-inherit") ||
			!strings.Contains(environment, "HOME="+b.Home) || !strings.Contains(environment, "CODEX_HOME="+b.CodexHome) {
			t.Fatal("one-shot child lost process-scoped isolation or launch controls")
		}
	}
}

func TestStartupProbeNativeFailureStopsBeforeSecondChild(t *testing.T) {
	for _, mode := range []string{"malformed", "oversized", "nonzero"} {
		t.Run(mode, func(t *testing.T) {
			cfg := startupProbeFixtureConfig(t)
			if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "startup-probe-mode"), []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ProbeStartup(context.Background(), cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"})
			if err != errStartupProbe || got != nil {
				t.Fatal("native child failure exposed output or succeeded")
			}
			if _, err := os.Stat(filepath.Join(cfg.Backends[1].Home, "startup-probe-report")); !os.IsNotExist(err) {
				t.Fatal("first failed child still allowed second authority to execute")
			}
		})
	}
}

func TestStartupProbeNativeCancellationAndDescendantCleanup(t *testing.T) {
	for _, mode := range []string{"hang", "closed-output-hang", "descendant-hang", "descendant-success", "injected-timeout"} {
		t.Run(mode, func(t *testing.T) {
			cfg := startupProbeFixtureConfig(t)
			b := cfg.Backends[0]
			fixtureMode := mode
			if fixtureMode == "injected-timeout" {
				fixtureMode = "hang"
			}
			if err := os.WriteFile(filepath.Join(b.Home, "startup-probe-mode"), []byte(fixtureMode), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// Expire a parent deadline only after native readiness. This tests
			// timeout cleanup without assuming a child starts within N ms.
			deadline := &startupProbeTestDeadline{Context: context.Background(), expired: make(chan struct{})}
			probeContext := context.Context(ctx)
			if mode == "injected-timeout" {
				probeContext = deadline
			}
			done := make(chan error, 1)
			go func() {
				_, err := ProbeStartup(probeContext, cfg, LaunchOptions{}, StartupClientInfo{Name: "fixture", Version: "1"})
				done <- err
			}()
			joined := false
			defer func() {
				cancel()
				if mode == "injected-timeout" {
					select {
					case <-deadline.expired:
					default:
						close(deadline.expired)
					}
				}
				if !joined {
					<-done
				}
			}()
			marker := "startup-probe-report"
			if strings.HasPrefix(mode, "descendant") {
				marker = "startup-probe-leaf"
			}
			for {
				if data, _ := os.ReadFile(filepath.Join(b.Home, marker)); len(data) != 0 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("native child did not reach readiness")
				case <-time.After(time.Millisecond):
				}
			}
			if mode != "descendant-success" && (runtime.GOOS == "windows" || runtime.GOOS == "linux") {
				file, err := os.OpenFile(b.Command, os.O_WRONLY, 0)
				if err == nil {
					file.Close()
					t.Fatal("fixture executable did not establish loaded-image exclusion")
				}
			}
			if mode == "injected-timeout" {
				close(deadline.expired)
			} else if mode != "descendant-success" {
				cancel()
			}
			err := <-done
			joined = true
			if (err == nil) != (mode == "descendant-success") {
				t.Fatal("native cancellation or inherited-pipe cleanup failed")
			}
			assertStartupProbeImageReleased(t, b.Command)
			data, _ := os.ReadFile(filepath.Join(b.Home, "startup-probe-report"))
			var report fixtureReport
			if json.Unmarshal(data, &report) != nil {
				t.Fatal("missing owned process identity")
			}
			assertStartupProbeProcessExited(t, report.PID)
			if strings.HasPrefix(mode, "descendant") {
				data, _ := os.ReadFile(filepath.Join(b.Home, "startup-probe-leaf"))
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					t.Fatal("missing descendant identity")
				}
				assertStartupProbeProcessExited(t, pid)
			}
		})
	}
}

// The expiration event is injected, but the child process, pipes and owned
// cleanup are real. The outer context remains a last-resort test deadline.
type startupProbeTestDeadline struct {
	context.Context
	expired chan struct{}
}

func (ctx *startupProbeTestDeadline) Done() <-chan struct{} { return ctx.expired }

func (ctx *startupProbeTestDeadline) Err() error {
	select {
	case <-ctx.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func assertStartupProbeImageReleased(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return
	}
	// Windows image sections and Linux ETXTBSY prevent opening the fixture
	// writable while any copied-native descendant still executes it. Open
	// without writing: no process-name scan, signal or global kill is used.
	deadline := time.After(30 * time.Second)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			file.Close()
			return
		}
		select {
		case <-deadline:
			t.Fatal("owned fixture still holds its executable image after cleanup")
		case <-time.After(time.Millisecond):
		}
	}
}

func assertStartupProbeProcessExited(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	if pid <= 0 {
		t.Fatal("invalid fixture process identity")
	}
	deadline := time.After(30 * time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		_, rest, _ := strings.Cut(string(data), ") ")
		if os.IsNotExist(err) || strings.HasPrefix(rest, "Z ") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("owned native process survived startup probe cleanup")
		case <-time.After(time.Millisecond):
		}
	}
}
