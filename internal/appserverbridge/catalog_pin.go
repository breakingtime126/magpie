package appserverbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const pinnedCatalogLimit int64 = 16 << 20

// The pin comes from trusted supplier preparation. This validates exact bytes,
// not model semantics or an atomic boundary against hostile same-user writers.
func pinnedCatalogMatches(path, digest string) bool {
	expected, err := hex.DecodeString(digest)
	if err != nil || len(expected) != sha256.Size || digest != strings.ToLower(digest) {
		return false
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return false
		}
		if p == path {
			if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > pinnedCatalogLimit {
				return false
			}
		} else if !info.IsDir() {
			return false
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	before, err := os.Lstat(path)
	if err != nil {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameCatalogFile(before, opened) {
		return false
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, pinnedCatalogLimit+1))
	if err != nil || n != before.Size() || n > pinnedCatalogLimit || hex.EncodeToString(h.Sum(nil)) != digest {
		return false
	}
	after, err := f.Stat()
	if err != nil || !sameCatalogFile(opened, after) {
		return false
	}
	current, err := os.Lstat(path)
	return err == nil && sameCatalogFile(after, current)
}

func sameCatalogFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
