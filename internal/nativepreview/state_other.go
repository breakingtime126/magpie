//go:build !windows

package nativepreview

import (
 "os"
 "path/filepath"
)

// Unix synthetic tests require an existing directory. Windows production has
// the stronger local fixed-drive/non-reparse admission; no portable equivalent
// or network-filesystem isolation is claimed here.
func safeStateParent(path string) bool {
 if !filepath.IsAbs(path){return false}
 info,err:=os.Stat(path)
 return err==nil && info.IsDir()
}
