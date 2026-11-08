//go:build !windows

package appserverbridge

import (
	"os"
	"path/filepath"
)

func ownedSettingsCheckReparse(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return errOwnedSettingsUnavailable
	}
	return nil
}

func ownedSettingsReplace(home, tempName string, initial bool) error {
	// Both files are in the same validated directory. Never remove the
	// destination first or copy over its contents as a fallback.
	var err error
	if initial {
		// Link publishes complete staged bytes only if the destination is
		// absent. The caller removes the temporary name after this call.
		err = os.Link(filepath.Join(home, tempName), filepath.Join(home, ownedSettingsFilename))
	} else {
		err = os.Rename(filepath.Join(home, tempName), filepath.Join(home, ownedSettingsFilename))
	}
	if err != nil {
		return err
	}
	// A sync failure after publication has an uncertain durability outcome;
	// the caller poisons the connection instead of claiming rollback.
	directory, err := os.Open(home)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
