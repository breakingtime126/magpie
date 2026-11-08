package appserverbridge

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func ownedSettingsCheckReparse(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return errOwnedSettingsUnavailable
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errOwnedSettingsUnavailable
	}
	return nil
}

func ownedSettingsReplace(home, tempName string, initial bool) error {
	from, err := windows.UTF16PtrFromString(filepath.Join(home, tempName))
	if err != nil {
		return errOwnedSettingsUnavailable
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(home, ownedSettingsFilename))
	if err != nil {
		return errOwnedSettingsUnavailable
	}
	// One same-directory replacement. No COPY_ALLOWED and no remove/rename
	// fallback: sharing violations and other failures close the store. Flush
	// the staged file before this call; no power-loss durability is promised.
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if !initial {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}
