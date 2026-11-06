// magpie-codex-bridge is an experimental stdio launcher, not the Codex CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// buildVersion may be set by the build to identify this adapter artifact.
var buildVersion = "dev"

const configEnv = "MAGPIE_APP_SERVER_BRIDGE_CONFIG"
const identity = "magpie-codex-bridge (experimental stdio adapter)"
const usage = `magpie-codex-bridge (experimental stdio adapter)
Usage:
  magpie-codex-bridge [-c features.code_mode_host=true] app-server [--analytics-default-enabled] [--listen stdio:// | --stdio]
  magpie-codex-bridge [-c features.code_mode_host=true] app-server [--analytics-default-enabled] --print-startup-config --startup-client-info JSON
  magpie-codex-bridge --check
  magpie-codex-bridge --help
  magpie-codex-bridge --version

Set MAGPIE_APP_SERVER_BRIDGE_CONFIG to an absolute bridge JSON file path.
--listen=stdio:// is also accepted. --stdio selects the same transport.
-c or --config accepts only features.code_mode_host=true, before or after app-server.
It is emitted as features.code_mode_host.enabled=true to preserve existing structured policy.
--analytics-default-enabled is accepted only after app-server. No other overrides are supported.
Startup inspection requires explicit startup_probe configuration and matching supported backend results.
It may refresh authentication, fetch policy and update backend caches. Sign-out is unsupported.
Startup inspection cannot be combined with --stdio or --listen.
JSONL configRequirements/read is independently disabled unless config_requirements_read is enabled.
That mode verifies both backend pins and returns only matching complete reviewed public policy views.
It may refresh policy/authentication state.
Independent pinned auth modes can report attestation unavailable and read only the primary gateway status.
They preserve true capability flags; no proof token, OAuth login or full Desktop compatibility is provided.
Attestation RPC failure is not equivalent to Windows Desktop's opaque error-token response.
--check validates configuration without starting children or changing state.
This is not the official Codex CLI. Desktop launcher compatibility is unverified.
`

type operation int

const (
	serve operation = iota
	help
	version
	check
	startupProbe
)

const maxArgumentCount = 32
const maxArgumentBytes = 4096

var (
	errUnsupportedArguments      = errors.New("unsupported arguments; use --help")
	errUnsupportedStartupSignOut = errors.New("unsupported startup sign-out; no configuration read or children started")
)

const startupProbeError = `{"error":{"code":-32600,"message":"magpie startup probe unavailable"}}`

// parseArgs accepts only reviewed literal forms. It does not interpret TOML or
// forward arbitrary configuration. Invalid arguments never read configuration.
func parseArgs(args []string) (operation, appserverbridge.LaunchOptions, appserverbridge.StartupClientInfo, error) {
	var options appserverbridge.LaunchOptions
	var clientInfo appserverbridge.StartupClientInfo
	// Sign-out never reaches environment/configuration access or client-info
	// parsing, even when it follows malformed arguments or another operation.
	for _, arg := range args {
		if arg == "--startup-sign-out" || strings.HasPrefix(arg, "--startup-sign-out=") {
			return serve, options, clientInfo, errUnsupportedStartupSignOut
		}
	}
	if len(args) > maxArgumentCount {
		return serve, options, clientInfo, errUnsupportedArguments
	}
	total := 0
	for _, arg := range args {
		if len(arg) > maxArgumentBytes-total {
			return serve, options, clientInfo, errUnsupportedArguments
		}
		total += len(arg)
	}
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			return help, options, clientInfo, nil
		case "--version", "-V":
			return version, options, clientInfo, nil
		case "--check":
			return check, options, clientInfo, nil
		}
	}
	if len(args) == 2 && args[0] == "app-server" && (args[1] == "--help" || args[1] == "-h") {
		return help, options, clientInfo, nil
	}
	serverSeen := false
	transportSeen := false
	probeSeen := false
	clientInfoSeen := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "app-server":
			if serverSeen {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			serverSeen = true
		case "-c", "--config":
			if i+1 >= len(args) || args[i+1] != "features.code_mode_host=true" || options.CodeModeHost {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			options.CodeModeHost = true
			i++
		case "--analytics-default-enabled":
			if !serverSeen || options.AnalyticsDefaultEnabled {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			options.AnalyticsDefaultEnabled = true
		case "--listen=stdio://", "--stdio":
			if !serverSeen || transportSeen {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			transportSeen = true
		case "--listen":
			if !serverSeen || transportSeen || i+1 >= len(args) || args[i+1] != "stdio://" {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			transportSeen = true
			i++
		case "--print-startup-config":
			if !serverSeen || probeSeen {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			probeSeen = true
		case "--startup-client-info":
			if !serverSeen || clientInfoSeen || i+1 >= len(args) {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			var err error
			clientInfo, err = appserverbridge.ParseStartupClientInfo(args[i+1])
			if err != nil {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			clientInfoSeen = true
			i++
		default:
			return serve, options, clientInfo, errUnsupportedArguments
		}
	}
	if !serverSeen || probeSeen != clientInfoSeen || probeSeen && transportSeen {
		return serve, options, clientInfo, errUnsupportedArguments
	}
	if probeSeen {
		return startupProbe, options, clientInfo, nil
	}
	return serve, options, clientInfo, nil
}

type dependencies struct {
	getenv func(string) string
	load   func(string) (appserverbridge.Config, error)
	run    func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error
	probe  func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, appserverbridge.StartupClientInfo) ([]byte, error)
}

func loadConfig(path string) (appserverbridge.Config, error) {
	// Reject directories and special files before opening (a FIFO could block).
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return appserverbridge.Config{}, errors.New("cannot read bridge configuration file")
	}
	f, err := os.Open(path)
	if err != nil {
		return appserverbridge.Config{}, errors.New("cannot read bridge configuration file")
	}
	defer f.Close()
	return appserverbridge.LoadConfig(f)
}

func execute(ctx context.Context, args []string, input io.ReadCloser, output io.WriteCloser, diagnostics io.Writer, deps dependencies) int {
	op, options, clientInfo, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(diagnostics, "magpie-codex-bridge:", err)
		return 2
	}
	switch op {
	case help:
		fmt.Fprint(output, usage)
		return 0
	case version:
		fmt.Fprintln(output, identity, buildVersion)
		return 0
	}
	path := deps.getenv(configEnv)
	if !filepath.IsAbs(path) {
		if op == startupProbe {
			return failStartupProbe(output, diagnostics)
		}
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: MAGPIE_APP_SERVER_BRIDGE_CONFIG must name an absolute JSON file path")
		return 2
	}
	cfg, err := deps.load(path)
	if err != nil {
		if op == startupProbe {
			return failStartupProbe(output, diagnostics)
		}
		// Do not echo file contents, paths, backend identifiers or raw errors.
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: invalid or unreadable bridge configuration")
		return 2
	}
	if op == check {
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: configuration valid; no children started")
		if cfg.Attestation == appserverbridge.AttestationUnavailable {
			fmt.Fprintln(diagnostics, "magpie-codex-bridge: attestation mode returns RPC failure, not Windows Desktop error tokens")
		}
		return 0
	}
	if op == startupProbe {
		if ctx.Err() != nil || cfg.StartupProbe != appserverbridge.StartupProbeCompareUnrestricted {
			return failStartupProbe(output, diagnostics)
		}
		result, err := deps.probe(ctx, cfg, options, clientInfo)
		if err != nil {
			return failStartupProbe(output, diagnostics)
		}
		// ProbeStartup returns only validated, compact real backend bytes.
		// Never synthesize a success response or enter the JSONL bridge here.
		line := append(result, '\n')
		if n, err := output.Write(line); err != nil || n != len(line) {
			fmt.Fprintln(diagnostics, "magpie-codex-bridge: cannot write startup configuration")
			return 1
		}
		return 0
	}
	if err := ctx.Err(); err != nil {
		return 0
	}
	if err := deps.run(ctx, cfg, options, input, output); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return 0
		}
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: app-server bridge failed")
		return 1
	}
	return 0
}

func failStartupProbe(output io.Writer, diagnostics io.Writer) int {
	fmt.Fprintln(output, startupProbeError)
	fmt.Fprintln(diagnostics, "magpie-codex-bridge: magpie startup probe unavailable")
	return 1
}

func main() {
	code := execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, dependencies{
		getenv: os.Getenv,
		load:   loadConfig,
		probe: func(parent context.Context, cfg appserverbridge.Config, options appserverbridge.LaunchOptions, clientInfo appserverbridge.StartupClientInfo) ([]byte, error) {
			ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer stop()
			return appserverbridge.ProbeStartup(ctx, cfg, options, clientInfo)
		},
		run: func(parent context.Context, cfg appserverbridge.Config, options appserverbridge.LaunchOptions, input io.ReadCloser, output io.WriteCloser) error {
			ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer stop()
			err := appserverbridge.RunWithOptions(ctx, cfg, options, input, output)
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return err
		},
	})
	os.Exit(code)
}
