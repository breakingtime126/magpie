//go:build !windows

package nativebootstrap

import "os"

func rejectReparse(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return ErrUnavailable
	}
	return nil
}
