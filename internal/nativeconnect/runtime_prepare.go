package nativeconnect

import (
	"context"
	"encoding/hex"
	"path/filepath"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativepreview"
)

// PrepareRuntimeBackends is the runtime-only stage of trusted preparation.
// The caller must first
// establish a verified supplier catalog and a launch-lifetime owned connection;
// neither this stage nor the current listener observation supplies those.
// scratchRoot must be an existing, separate Magpie-owned directory.
func PrepareRuntimeBackends(ctx context.Context, environment []string, scratchRoot string) ([]appserverbridge.Backend, error) {
	runtime, err := nativepreview.PrepareInstalledRuntime(ctx, environment, scratchRoot)
	if err != nil {
		return nil, ErrUnavailable
	}
	return runtimeBackends(runtime)
}

func runtimeBackends(runtime nativepreview.PreparedRuntime) ([]appserverbridge.Backend, error) {
	digest, err := hex.DecodeString(runtime.SHA256)
	if err != nil || len(digest) != 32 || runtime.Version != nativepreview.SharedRuntimeVersion ||
		!filepath.IsAbs(runtime.Command) || !filepath.IsAbs(runtime.Home) || !filepath.IsAbs(runtime.CodexHome) {
		return nil, ErrUnavailable
	}
	// Both entries refer to the one reviewed installed runtime and the same
	// explicitly resolved native home. No model data or credential is copied.
	return []appserverbridge.Backend{
		{ID: "openai", Provider: "openai", Command: runtime.Command, Home: runtime.Home, CodexHome: runtime.CodexHome, StartupProbeSHA256: runtime.SHA256},
		{ID: "aihub", Provider: "magpie_shared_aihub", Command: runtime.Command, Home: runtime.Home, CodexHome: runtime.CodexHome, StartupProbeSHA256: runtime.SHA256},
	}, nil
}
