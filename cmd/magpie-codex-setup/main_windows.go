//go:build windows && !nogui

// The independent preview intentionally imports neither ordinary cli nor GUI.
package main

import (
	"os"
	"unsafe"

	"github.com/yetone/magpie/internal/codexsetup"
	"golang.org/x/sys/windows"
)

func main() {
	if codexsetup.Run() != nil {
		// A fixed notice, not a source error that might contain private data.
		text, _ := windows.UTF16PtrFromString("预览窗口无法启动。需要系统已提供 WebView2；请检查实验包说明。未声明供应商连接成功。")
		title, _ := windows.UTF16PtrFromString("Magpie 供应商预览")
		windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0,
			uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
		os.Exit(1)
	}
}
