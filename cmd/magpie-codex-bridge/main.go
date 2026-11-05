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
	"syscall"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// buildVersion may be set by the build to identify this adapter artifact.
var buildVersion = "dev"

const configEnv = "MAGPIE_APP_SERVER_BRIDGE_CONFIG"
const identity = "magpie-codex-bridge (experimental stdio adapter)"
const usage = `magpie-codex-bridge (experimental stdio adapter)
Usage:
  magpie-codex-bridge app-server [--listen stdio:// | --stdio]
  magpie-codex-bridge --check
  magpie-codex-bridge --help
  magpie-codex-bridge --version

Set MAGPIE_APP_SERVER_BRIDGE_CONFIG to an absolute bridge JSON file path.
--listen=stdio:// is also accepted. --stdio selects the same transport. No other server arguments are supported.
--check validates configuration without starting children or changing state.
This is not the official Codex CLI. Desktop launcher compatibility is unverified.
`

type operation int

const (
	serve operation = iota
	help
	version
	check
)

// parseArgs accepts only documented forms, including help/version without any
// other options. Invalid arguments must never trigger configuration reads.
func parseArgs(args []string) (operation, error) {
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			return help, nil
		case "--version", "-V":
			return version, nil
		case "--check":
			return check, nil
		case "app-server":
			return serve, nil
		}
	}
	if len(args) == 2 && args[0] == "app-server" {
		switch args[1] {
		case "--listen=stdio://", "--stdio":
			return serve, nil
		case "--help", "-h":
			return help, nil
		}
	}
	if len(args) == 3 && args[0] == "app-server" && args[1] == "--listen" && args[2] == "stdio://" {
		return serve, nil
	}
	return serve, errors.New("unsupported arguments; use --help")
}

type dependencies struct {
	getenv func(string) string
	load   func(string) (appserverbridge.Config, error)
	run    func(context.Context, appserverbridge.Config, io.ReadCloser, io.WriteCloser) error
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
	op, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: unsupported arguments; use --help")
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
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: MAGPIE_APP_SERVER_BRIDGE_CONFIG must name an absolute JSON file path")
		return 2
	}
	cfg, err := deps.load(path)
	if err != nil {
		// Do not echo file contents, paths, backend identifiers or raw errors.
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: invalid or unreadable bridge configuration")
		return 2
	}
	if op == check {
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: configuration valid; no children started")
		return 0
	}
	if err := ctx.Err(); err != nil {
		return 0
	}
	if err := deps.run(ctx, cfg, input, output); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return 0
		}
		fmt.Fprintln(diagnostics, "magpie-codex-bridge: app-server bridge failed")
		return 1
	}
	return 0
}

func main() {
	code := execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, dependencies{
		getenv: os.Getenv,
		load:   loadConfig,
		run: func(parent context.Context, cfg appserverbridge.Config, input io.ReadCloser, output io.WriteCloser) error {
			ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
			defer stop()
			err := appserverbridge.Run(ctx, cfg, input, output)
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return err
		},
	})
	os.Exit(code)
}
