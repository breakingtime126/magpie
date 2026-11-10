// magpie-codex-bridge is an experimental stdio launcher, not the Codex CLI.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativelaunch"
	"github.com/yetone/magpie/internal/nativeprofile"
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
  magpie-codex-bridge --inspect-primary-identity-v0.167.0-alpha.3.2
  magpie-codex-bridge --help
  magpie-codex-bridge --version

Set MAGPIE_APP_SERVER_BRIDGE_CONFIG to an absolute bridge JSON file path.
--listen=stdio:// is also accepted. --stdio selects the same transport.
-c or --config accepts features.code_mode_host=true, before or after app-server.
It is emitted as features.code_mode_host.enabled=true to preserve existing structured policy.
--analytics-default-enabled is accepted only after app-server.
Normal JSONL launch also accepts exactly:
plugins.codex-app-tools@openai-bundled.mcp_servers.codex_app.enabled=true
It requires desktop_plugin_override=codex-app-tools-v0.167.0-alpha.3.2 and both SHA-256 pins.
This default-off mode forwards the exact leaf to both children only when supplied.
Native plugin policy may activate an installed plugin and declared MCP subprocess/network effects.
No plugin is installed or copied, no settings are written, and no other plugin override is accepted.
The plugin override is rejected for startup inspection and cannot mix with legacy v0.166 modes.
Startup inspection requires explicit startup_probe configuration and matching supported backend results.
Supported profiles: compare-unrestricted-v0.166 (exact 0.166.0-alpha.6) and
compare-unrestricted-v0.167.0-alpha.3.2 (exact 0.167.0-alpha.3.2).
Both profiles require two SHA-256 pins and reject every non-null requirements result.
The 0.167 profile cannot be combined with the v0.166 JSONL requirements/auth modes.
It may refresh authentication, fetch policy and update backend caches. Sign-out is unsupported.
Startup inspection cannot be combined with --stdio or --listen.
JSONL configRequirements/read is independently disabled unless config_requirements_read is enabled.
compare-safe-v0.166 returns matching complete reviewed public policy views for 0.166.0-alpha.6.
compare-null-v0.167.0-alpha.3.2 accepts only matching native requirements:null and speed-mode support:true.
Both modes verify both backend pins before either child and fan out a fresh native no-argument read.
The exact 0.167 mode may accompany only the same-version startup/auth modes; each stays default off.
It may refresh policy/authentication state. Empty homes and earlier probes do not establish a null policy.
Independent pinned auth modes can report attestation unavailable and read only the primary gateway status.
Exact 0.167 attestation/gateway modes require unavailable-v0.167.0-alpha.3.2 or primary-read-only-v0.167.0-alpha.3.2.
They preserve true capability flags; no proof token, OAuth login or full Desktop compatibility is provided.
Attestation RPC failure is not equivalent to Windows Desktop's opaque error-token response.
getAuthStatus remains disabled by default. auth_status=primary-trusted-stdio-v0.167.0-alpha.3.2
is an independent token-relay opt-in requiring backend[0].provider=openai and both SHA-256 pins.
It may export the primary API key or ChatGPT bearer only to an operator-trusted private stdio frontend.
A clientInfo name does not establish trust. Never expose this mode to an untrusted client or shared pipe.
includeToken/refreshToken are preserved; even false refresh can trigger native authentication/network effects.
This adds no proof, OAuth login, external-token refresh, credential isolation or verified Desktop support.
The separate primary-no-auth-v0.167.0-alpha.3.2 diagnostic keeps rejecting all credentialed results.
--check validates configuration without starting children or changing state.
--inspect-primary-identity-v0.167.0-alpha.3.2 is a separate explicit primary-only operation.
It requires a minimal profile, primary provider=openai and its reviewed SHA-256 pin.
No optional compatibility mode, inherited primary environment, model metadata or working directory is accepted.
It requests includeToken:false and refreshToken:false; any returned token fails closed.
Only native-reported auth type is shown, not account validity or chat readiness.
It starts the primary and may refresh credentials, contact services or update native state.
It does not start the secondary, list models, log in/out or request proof/inference.
The executable pin is not provenance, endpoint binding or OS credential isolation.
This is not the official Codex CLI. Desktop launcher compatibility is unverified.
`

type operation int

const (
	serve operation = iota
	help
	version
	check
	startupProbe
	primaryIdentity
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
		case "--inspect-primary-identity-v0.167.0-alpha.3.2":
			return primaryIdentity, options, clientInfo, nil
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
			if i+1 >= len(args) {
				return serve, options, clientInfo, errUnsupportedArguments
			}
			switch args[i+1] {
			case "features.code_mode_host=true":
				if options.CodeModeHost {
					return serve, options, clientInfo, errUnsupportedArguments
				}
				options.CodeModeHost = true
			case appserverbridge.CodexAppToolsMCPOverride:
				if options.CodexAppToolsMCPEnabled {
					return serve, options, clientInfo, errUnsupportedArguments
				}
				options.CodexAppToolsMCPEnabled = true
			default:
				return serve, options, clientInfo, errUnsupportedArguments
			}
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
	if !serverSeen || probeSeen != clientInfoSeen || probeSeen && (transportSeen || options.CodexAppToolsMCPEnabled) {
		return serve, options, clientInfo, errUnsupportedArguments
	}
	if probeSeen {
		return startupProbe, options, clientInfo, nil
	}
	return serve, options, clientInfo, nil
}

type dependencies struct {
	// read is installed by production. A nil reader retains the original private
	// legacy-config test seam; it is never selected from argv or environment.
	read       func(string) ([]byte, error)
	trustedRun func(context.Context, io.Reader, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error

	getenv   func(string) string
	load     func(string) (appserverbridge.Config, error)
	run      func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, io.ReadCloser, io.WriteCloser) error
	probe    func(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, appserverbridge.StartupClientInfo) ([]byte, error)
	identity func(context.Context, appserverbridge.Config) ([]byte, error)
}

// readProfile snapshots one byte-bounded regular file and checks that its
// identity and metadata remain stable across the read. These checks are not a
// universal filesystem I/O deadline or a boundary against a hostile same-user
// actor. Live-owner authentication still validates the exact returned bytes.
func readProfile(path string) ([]byte, error) {
	return readProfileWith(path, nativeprofile.Read)
}

// readProfileWith keeps the acquisition checks identical in deterministic
// replacement tests. Production always supplies the bounded nativeprofile reader.
func readProfileWith(path string, read func(io.Reader) ([]byte, error)) ([]byte, error) {
	before, err := os.Stat(path)
	if err != nil || !profileFileRegular(before) {
		return nil, nativeprofile.ErrProfile
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nativeprofile.ErrProfile
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameProfileFile(before, opened) {
		return nil, nativeprofile.ErrProfile
	}
	data, err := read(f)
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, nativeprofile.ErrProfile
	}
	after, fileErr := f.Stat()
	current, pathErr := os.Stat(path)
	if fileErr != nil || pathErr != nil || !sameProfileFile(opened, after) || !sameProfileFile(after, current) {
		return nil, nativeprofile.ErrProfile
	}
	return data, nil
}

func profileFileRegular(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= nativeprofile.Limit
}
func sameProfileFile(a, b os.FileInfo) bool {
	return profileFileRegular(a) && profileFileRegular(b) && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// classifyProfile rejects malformed JSON and alternate reserved-marker spelling
// before any legacy Config validation. Exact envelopes use strict canonical parse.
func classifyProfile(data []byte) (bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return false, nativeprofile.ErrProfile
	}
	for key := range fields {
		if strings.EqualFold(key, "native_bootstrap") && key != "native_bootstrap" {
			return true, nativeprofile.ErrProfile
		}
	}
	return nativeprofile.Inspect(data)
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
	var cfg appserverbridge.Config
	if deps.read == nil {
		cfg, err = deps.load(path)
	} else {
		var data []byte
		data, err = deps.read(path)
		if err == nil {
			var trusted bool
			trusted, err = classifyProfile(data)
			if err == nil && trusted {
				// These independent invocations cannot consume a one-use JSONL lease.
				// No successful startup/config/identity result is synthesized.
				if op == startupProbe {
					return failStartupProbe(output, diagnostics)
				}
				if op == primaryIdentity {
					return failPrimaryIdentity(output, diagnostics, nil)
				}
				if op != serve || deps.trustedRun == nil {
					fmt.Fprintln(diagnostics, "magpie-codex-bridge: trusted native operation unavailable")
					return 2
				}
				if err := ctx.Err(); err != nil {
					return 0
				}
				if err := deps.trustedRun(ctx, bytes.NewReader(data), options, input, output); err != nil {
					// Preserve cancellation plus cleanup failures; lease expiry is not a
					// successful conversation or a clean process-tree shutdown.
					fmt.Fprintln(diagnostics, "magpie-codex-bridge: trusted native launch failed")
					return 1
				}
				return 0
			}
			if err == nil {
				cfg, err = appserverbridge.LoadConfig(bytes.NewReader(data))
			}
		}
	}
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
		if cfg.Attestation == appserverbridge.AttestationUnavailable || cfg.Attestation == appserverbridge.AttestationUnavailable167 {
			fmt.Fprintln(diagnostics, "magpie-codex-bridge: attestation mode returns RPC failure, not Windows Desktop error tokens")
		}
		return 0
	}
	if op == primaryIdentity {
		if deps.identity == nil {
			return failPrimaryIdentity(output, diagnostics, nil)
		}
		result, err := deps.identity(ctx, cfg)
		if err != nil {
			return failPrimaryIdentity(output, diagnostics, err)
		}
		line := append(result, '\n')
		if n, err := output.Write(line); err != nil || n != len(line) {
			fmt.Fprintln(diagnostics, "magpie-codex-bridge: cannot write identity inspection result")
			return 1
		}
		return 0
	}
	if op == startupProbe {
		if ctx.Err() != nil || !appserverbridge.StartupProbeSupported(cfg.StartupProbe) {
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

func failPrimaryIdentity(output io.Writer, diagnostics io.Writer, err error) int {
	kind := "unavailable"
	if errors.Is(err, context.DeadlineExceeded) {
		kind = "timeout"
	} else if errors.Is(err, context.Canceled) {
		kind = "cancelled"
	}
	fmt.Fprintf(output, "{\"error\":\"%s\"}\n", kind)
	fmt.Fprintln(diagnostics, "magpie-codex-bridge: primary identity inspection failed; no account validity inferred")
	return 1
}

func main() {
	code := execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, dependencies{
		getenv: os.Getenv,
		read:   readProfile,
		trustedRun: func(parent context.Context, profile io.Reader, options appserverbridge.LaunchOptions, input io.ReadCloser, output io.WriteCloser) error {
			ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer stop()
			return nativelaunch.Run(ctx, profile, options, input, output)
		},
		load: loadConfig,
		identity: func(parent context.Context, cfg appserverbridge.Config) ([]byte, error) {
			ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer stop()
			return appserverbridge.InspectPrimaryIdentity167(ctx, cfg)
		},
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
