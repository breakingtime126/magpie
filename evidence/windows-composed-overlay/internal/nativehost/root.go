package nativehost

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// Publication paths are checked before any listener or file is created. Ordinary
// Magpie directories below HOME are allowed, but HOME itself and any overlap
// with CODEX_HOME are rejected, including existing filesystem aliases.
func separateRoot(root string, cfg appserverbridge.Config) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return ErrUnavailable
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return ErrUnavailable
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ErrUnavailable
	}
	for _, b := range cfg.Backends {
		home, e := filepath.EvalSymlinks(b.Home)
		if e != nil {
			return ErrUnavailable
		}
		codex, e := filepath.EvalSymlinks(b.CodexHome)
		if e != nil {
			return ErrUnavailable
		}
		hi, e := os.Stat(home)
		if e != nil || os.SameFile(info, hi) || within(real, codex) || within(codex, real) {
			return ErrUnavailable
		}
		// os.SameFile catches aliases (e.g. alternate drive paths) at ancestors.
		if ancestor(root, codex) || ancestor(codex, root) {
			return ErrUnavailable
		}
	}
	return nil
}
func within(parent, child string) bool {
	if runtime.GOOS == "windows" {
		parent, child = strings.ToLower(parent), strings.ToLower(child)
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func ancestor(parent, child string) bool {
	p, err := os.Stat(parent)
	if err != nil || !p.IsDir() {
		return false
	}
	for {
		c, e := os.Stat(child)
		if e == nil && c.IsDir() && os.SameFile(p, c) {
			return true
		}
		next := filepath.Dir(child)
		if next == child {
			return false
		}
		child = next
	}
}
