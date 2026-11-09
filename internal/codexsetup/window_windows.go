//go:build windows && !nogui

package codexsetup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/yetone/magpie/internal/nativepreview"
)

// Run creates only this independent WebView2 window and the explicitly scoped
// preview controller. It never calls ordinary cli.Main, gui.Run or gui.Handler.
func Run() error {
	controller, err := nativepreview.New()
	if err != nil {
		return errors.New("preview unavailable")
	}
	defer controller.Close()
	cache, err := os.UserCacheDir()
	if err != nil {
		return errors.New("preview cache unavailable")
	}
	cacheRoot := filepath.Join(cache, "MagpieCodexPreview", "webview")
	if err = os.MkdirAll(cacheRoot, 0700); err != nil {
		return errors.New("preview cache unavailable")
	}
	// WebView state never shares ordinary Magpie or Codex browser stores.
	viewData, err := os.MkdirTemp(cacheRoot, "window-")
	if err != nil {
		return errors.New("preview cache unavailable")
	}
	var app *application.App
	handler, err := NewHandler(controller, func() (string, error) {
		return app.Dialog.OpenFile().CanChooseFiles(true).CanChooseDirectories(false).
			SetTitle("选择你信任的官方 Codex 程序（仅一次）").PromptForSingleSelection()
	})
	if err != nil {
		return errors.New("preview unavailable")
	}
	app = application.New(application.Options{
		Name:         "Magpie Codex Provider Preview",
		Description:  "Independent provider connectivity preview",
		Assets:       application.AssetOptions{Handler: handler, DisableLogging: true},
		Windows:      application.WindowsOptions{WebviewUserDataPath: viewData},
		OnShutdown:   func() { controller.Cancel() },
		ErrorHandler: func(error) {}, // Do not log native/provider errors or secrets.
	})
	if version, present := app.Env.Info().PlatformInfo["WebView2"]; !present || fmt.Sprint(version) == "" {
		return errors.New("WebView2 unavailable")
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "provider-preview", Title: "Magpie · Codex 供应商预览", URL: "/",
		Width: 900, Height: 700, MinWidth: 480, MinHeight: 420,
	})
	return app.Run()
}
