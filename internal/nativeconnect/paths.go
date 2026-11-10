package nativeconnect

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// separateConnectionRoot permits ordinary Magpie folders beneath shared HOME,
// but never HOME itself, nor any overlap with native CODEX_HOME. Identity checks
// cover aliases of existing ancestors; lexical checks cover an absent root.
func separateConnectionRoot(root, home, codexHome string) bool {
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	codexHome, err = filepath.EvalSymlinks(codexHome)
	if err != nil {
		return false
	}
	if sameDirectory(root, home) || pathWithin(root, home) && pathWithin(home, root) {
		return false
	}
	return !pathWithin(root, codexHome) && !pathWithin(codexHome, root) &&
		!directoryAncestor(root, codexHome) && !directoryAncestor(codexHome, root)
}

func sameDirectory(a, b string) bool {
	x, err := os.Stat(a)
	if err != nil || !x.IsDir() {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && y.IsDir() && os.SameFile(x, y)
}

func directoryAncestor(parent, child string) bool {
	for {
		if sameDirectory(parent, child) {
			return true
		}
		next := filepath.Dir(child)
		if next == child {
			return false
		}
		child = next
	}
}

func pathWithin(parent, child string) bool {
	if runtime.GOOS == "windows" {
		parent, child = strings.ToLower(parent), strings.ToLower(child)
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
