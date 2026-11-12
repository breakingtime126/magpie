//go:build !nativehandshakediag

package nativehandshake

func processEnabled() bool { return false }
func processEmit(Stage)    {}
