package nativechannel

import (
	"os"
	"syscall"
)

// Check all currently reported reparse points, not just those represented by
// os.ModeSymlink. This is a path snapshot, not an atomic ownership guarantee.
func publicationPathAllowed(path string, info os.FileInfo) bool {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := syscall.GetFileAttributes(name)
	return err == nil && attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0
}
