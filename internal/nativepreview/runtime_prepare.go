package nativepreview

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PreparedRuntime contains non-secret launch inputs only. It is an in-process
// result, never an HTTP input, package provenance override, or launch permit.
// The existing bridge still rechecks the pinned executable at native launch.
// Discovery and hashing are path-based snapshots, not a race-free filesystem
// boundary against another process running as the same user.
type PreparedRuntime struct {
	Command   string
	SHA256    string
	Version   string
	Home      string
	CodexHome string
}

// SharedRuntimeVersion is the exact reviewed shared-home bridge runtime.
const SharedRuntimeVersion = "codex-cli 0.167.0-alpha.3.2"

type runtimePreparationOps struct {
	locate  func(context.Context) (string, error)
	digest  func(string) ([32]byte, error)
	version func(context.Context, peerSpec) (string, bool)
	cleanup func(string) error
	mktemp  func(string, string) (string, error)
}

// PrepareInstalledRuntime reuses the supported official package query, bounded
// executable hash and exact --version probe. It never starts app-server or
// reads model records, auth, configuration, history or catalog caches. --version
// runs with newly created disposable homes under the supplied Magpie-owned
// scratch root, not the user's native state. Enterprise transport/trust settings
// keep the existing nativeEnvironment behavior; no zero-egress claim is made.
//
// Call this runtime stage only after supplier metadata and launch-lifetime
// listener ownership are available. A current listener URL is not such an
// ownership contract.
func PrepareInstalledRuntime(ctx context.Context, environment []string, scratchRoot string) (PreparedRuntime, error) {
	return prepareInstalledRuntime(ctx, environment, scratchRoot, runtime.GOOS, runtimePreparationOps{
		locate: installedExecutable, digest: executableDigest, version: nativeVersionReport,
	})
}

func prepareInstalledRuntime(ctx context.Context, environment []string, scratchRoot, goos string, ops runtimePreparationOps) (result PreparedRuntime, resultErr error) {
	if ctx.Err() != nil || ops.locate == nil || ops.digest == nil || ops.version == nil {
		return result, ErrUnavailable
	}
	home, codexHome, err := runtimeHomes(environment, goos)
	if err != nil || !safeStateParent(scratchRoot) || !filepath.IsAbs(scratchRoot) {
		return result, ErrUnavailable
	}
	root, err := filepath.EvalSymlinks(scratchRoot)
	if err != nil || runtimeDirectoriesOverlap(root, codexHome) {
		return result, ErrUnavailable
	}
	homeInfo, err := os.Stat(home)
	if err != nil {
		return result, ErrUnavailable
	}
	codexInfo, err := os.Stat(codexHome)
	if err != nil {
		return result, ErrUnavailable
	}
	command, digest, err := inspectInstalledRuntime(ctx, ops)
	if err != nil {
		return result, err
	}
	// No directory is created for unsupported/ambiguous homes or discovery.
	// Everything removed below was exclusively created by this call.
	mktemp := ops.mktemp
	if mktemp == nil {
		mktemp = os.MkdirTemp
	}
	probe, err := mktemp(root, "runtime-version-")
	if err != nil {
		return result, ErrUnavailable
	}
	cleanup := ops.cleanup
	if cleanup == nil {
		cleanup = os.RemoveAll
	}
	defer func() {
		if err := cleanup(probe); err != nil {
			result, resultErr = PreparedRuntime{}, ErrUnavailable
		}
	}()
	probeHome, probeCodex := filepath.Join(probe, "home"), filepath.Join(probe, "codex")
	if os.Mkdir(probeHome, 0700) != nil || os.Mkdir(probeCodex, 0700) != nil || ctx.Err() != nil {
		return result, ErrUnavailable
	}
	version, ok := ops.version(ctx, peerSpec{command: command, digest: digest, home: probeHome, codexHome: probeCodex, provider: "openai", only167: true})
	if !ok || version != SharedRuntimeVersion || ctx.Err() != nil {
		return result, ErrUnavailable
	}
	// A package update, executable replacement or discovery change during the
	// version probe invalidates the whole result; do not retry another runtime.
	after, afterDigest, err := inspectInstalledRuntime(ctx, ops)
	if err != nil || after != command || afterDigest != digest || ctx.Err() != nil {
		return result, ErrUnavailable
	}
	afterHome, afterCodex, err := runtimeHomes(environment, goos)
	if err != nil || afterHome != home || afterCodex != codexHome {
		return result, ErrUnavailable
	}
	afterHomeInfo, err := os.Stat(afterHome)
	if err != nil || !os.SameFile(homeInfo, afterHomeInfo) {
		return result, ErrUnavailable
	}
	afterCodexInfo, err := os.Stat(afterCodex)
	if err != nil || !os.SameFile(codexInfo, afterCodexInfo) || ctx.Err() != nil {
		return result, ErrUnavailable
	}
	return PreparedRuntime{Command: command, SHA256: hex.EncodeToString(digest[:]), Version: version, Home: home, CodexHome: codexHome}, nil
}

func inspectInstalledRuntime(ctx context.Context, ops runtimePreparationOps) (string, [32]byte, error) {
	var empty [32]byte
	path, err := ops.locate(ctx)
	if err != nil || !filepath.IsAbs(path) || ctx.Err() != nil {
		return "", empty, ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(path)
	// Do not turn a package-location result into authority for a linked
	// executable outside that location. Windows comparison is case-insensitive.
	if err != nil || !sameRuntimePath(path, resolved) {
		return "", empty, ErrUnavailable
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", empty, ErrUnavailable
	}
	digest, err := ops.digest(resolved)
	if err != nil || ctx.Err() != nil {
		return "", empty, ErrUnavailable
	}
	return resolved, digest, nil
}

func sameRuntimePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// runtimeHomes resolves only the explicitly supplied environment and the
// default .codex directory. It never searches for alternate accounts or reads
// native files. Empty CODEX_HOME uses the existing Magpie default contract;
// relative values and unsupported alternate-state overrides are not ignored.
func runtimeHomes(environment []string, goos string) (string, string, error) {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if goos == "windows" {
			key = strings.ToUpper(key)
		}
		switch key {
		case "HOME", "USERPROFILE", "CODEX_HOME", "CODEX_WINDOWS_REGISTERED_CORE", "CODEX_SQLITE_HOME":
		default:
			continue
		}
		if strings.ContainsRune(value, 0) {
			return "", "", ErrUnavailable
		}
		if old, present := values[key]; present && old != value {
			return "", "", ErrUnavailable
		}
		values[key] = value
	}
	if _, present := values["CODEX_WINDOWS_REGISTERED_CORE"]; present {
		return "", "", ErrUnavailable
	}
	// The reviewed shared bridge environment does not forward this override.
	// Selecting a different database namespace silently would be incorrect.
	if _, present := values["CODEX_SQLITE_HOME"]; present {
		return "", "", ErrUnavailable
	}
	home := values["HOME"]
	if goos == "windows" {
		home = values["USERPROFILE"]
	}
	if !filepath.IsAbs(home) || !safeStateParent(home) {
		return "", "", ErrUnavailable
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", "", ErrUnavailable
	}
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if value, present := values[key]; present {
			if !filepath.IsAbs(value) || !safeStateParent(value) {
				return "", "", ErrUnavailable
			}
			other, err := filepath.EvalSymlinks(value)
			if err != nil || !sameRuntimePath(home, other) {
				return "", "", ErrUnavailable
			}
		}
	}
	codexHome := values["CODEX_HOME"]
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if !filepath.IsAbs(codexHome) || !safeStateParent(codexHome) {
		return "", "", ErrUnavailable
	}
	codexHome, err = filepath.EvalSymlinks(codexHome)
	if err != nil || sameRuntimePath(home, codexHome) {
		return "", "", ErrUnavailable
	}
	return home, codexHome, nil
}

func runtimeDirectoriesOverlap(a, b string) bool {
	within := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && (rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
	}
	return within(a, b) || within(b, a)
}
