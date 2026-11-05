package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/yetone/magpie/internal/appserverbridge"
)

const appServerBridgeUsage = `magpie app-server-bridge --config <absolute-file.json>

Experimental stdio bridge to two independently configured official Codex
app-server processes. Does not enable Magpie's HTTP gateway, modify an agent,
copy sign-ins, or install desktop integration. See docs/subsystems/app-server-bridge.md.
`

func appServerBridgeCmd(args []string) error {
	if helpAsked(append([]string{"app-server-bridge"}, args...)) {
		fmt.Print(appServerBridgeUsage)
		return nil
	}
	if len(args) != 2 || args[0] != "--config" || !filepath.IsAbs(args[1]) {
		return errors.New("app-server-bridge requires --config with one absolute JSON file path")
	}
	file, err := os.Open(args[1])
	if err != nil {
		return errors.New("cannot open app-server bridge configuration")
	}
	cfg, err := appserverbridge.LoadConfig(file)
	file.Close()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return appserverbridge.Run(ctx, cfg, os.Stdin, os.Stdout)
}
