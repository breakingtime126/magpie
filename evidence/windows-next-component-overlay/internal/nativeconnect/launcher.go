package nativeconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrLauncherIdentity = errors.New("packaged launcher identity unavailable")

// Set only by the native GUI packager after building and checking the adapter
// from the same clean source commit. Ordinary builds have no launcher trust.
var packagedLauncherSHA256 string
var packagedLauncherSourceRevision string

// ResolvePackagedLauncher uses this process's executable location and the
// existing native GUI package layout. No manifest or environment supplies trust.
// A valid result still needs revalidation at launch; see VerifyPackagedLauncher.
func ResolvePackagedLauncher() (PackagedLauncher, error) {
	if !canonicalHex(packagedLauncherSHA256, sha256.Size) || !canonicalHex(packagedLauncherSourceRevision, 20) {
		return PackagedLauncher{}, ErrLauncherIdentity
	}
	executable, err := os.Executable()
	if err != nil {
		return PackagedLauncher{}, ErrLauncherIdentity
	}
	return resolvePackagedLauncher(executable, packagedLauncherSHA256, packagedLauncherSourceRevision)
}

func resolvePackagedLauncher(executable, digest, revision string) (PackagedLauncher, error) {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || filepath.Base(executable) != "Magpie-Native-Codex-Experimental.exe" {
		return PackagedLauncher{}, ErrLauncherIdentity
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return PackagedLauncher{}, ErrLauncherIdentity
	}
	return VerifyPackagedLauncher(filepath.Dir(executable), TrustedLauncherIdentity{
		Filename: "magpie-codex-bridge.exe", SHA256: digest, SourceRevision: revision,
	})
}

// TrustedLauncherIdentity must come from trusted build provenance, not a mutable
// sibling BUILD.json or a digest calculated from the candidate being checked.
// SourceRevision attributes the expected digest; this verifier does not prove
// that attribution or authenticate whoever supplied it.
type TrustedLauncherIdentity struct {
	Filename       string
	SHA256         string
	SourceRevision string
}

type PackagedLauncher struct {
	Path           string
	SHA256         string
	SourceRevision string
}

const maxLauncherBytes int64 = 512 << 20

// VerifyPackagedLauncher checks an explicitly selected package artifact without
// executing it, searching PATH, or reading any manifest or native state. The two
// filenames come from package-native-magpie-windows.ps1 and
// package-bridge-windows.ps1. There is no default package layout or trust input.
// The result is a point-in-time check, not a launch-lifetime file lock or an OS
// same-user security boundary. Revalidate at use; no production Prepare calls it.
func VerifyPackagedLauncher(packageDir string, expected TrustedLauncherIdentity) (PackagedLauncher, error) {
	fail := func() (PackagedLauncher, error) { return PackagedLauncher{}, ErrLauncherIdentity }
	if expected.Filename != "magpie-codex-bridge.exe" && expected.Filename != "magpie-codex-bridge-windows-amd64.exe" {
		return fail()
	}
	if !canonicalHex(expected.SHA256, sha256.Size) || !canonicalHex(expected.SourceRevision, 20) ||
		!filepath.IsAbs(packageDir) || filepath.Clean(packageDir) != packageDir {
		return fail()
	}
	if !launcherDirectory(packageDir) {
		return fail()
	}
	path := filepath.Join(packageDir, expected.Filename)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxLauncherBytes {
		return fail()
	}
	f, err := os.Open(path)
	if err != nil {
		return fail()
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameLauncher(before, opened) {
		return fail()
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxLauncherBytes+1))
	if err != nil || n != before.Size() || hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
		return fail()
	}
	after, err := f.Stat()
	if err != nil || !sameLauncher(before, after) {
		return fail()
	}
	current, err := os.Lstat(path)
	if err != nil || !sameLauncher(before, current) || !launcherDirectory(packageDir) {
		return fail()
	}
	return PackagedLauncher{Path: path, SHA256: expected.SHA256, SourceRevision: expected.SourceRevision}, nil
}

func canonicalHex(s string, size int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == size && s == strings.ToLower(s)
}

func launcherDirectory(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return true
		}
		path = parent
	}
}

func sameLauncher(a, b os.FileInfo) bool {
	return b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}
