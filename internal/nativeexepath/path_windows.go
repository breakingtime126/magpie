package nativeexepath

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

var errPath = errors.New("executable path identity unavailable")

// Resolve reads metadata through an already authorized file handle. Unlike
// filepath.EvalSymlinks on Windows it does not enumerate each parent directory
// to expand its name. A WindowsApps directory can deny that extra enumeration
// while granting access to the installed executable itself.
//
// No ACL changes, privilege changes, ignored errors or unverified-path fallback.
// The caller must still compare this final name with its discovered path and
// retain its digest/version checks. This is not a race-free launch primitive.
func Resolve(path string) (string, error) {
	if !localDOSPath(path) {
		return "", errPath
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return "", errPath
	}
	kind, err := windows.GetFileType(h)
	if err != nil {
		return "", err
	}
	if kind != windows.FILE_TYPE_DISK {
		return "", errPath
	}
	return readFinalName(func(buf []uint16) (uint32, error) {
		// 0 = FILE_NAME_NORMALIZED | VOLUME_NAME_DOS. OPENED names would
		// preserve aliases and must not be substituted on access failure.
		return windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	})
}

func localDOSPath(path string) bool {
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && path[2] == '\\' && filepath.IsAbs(path) && !strings.ContainsRune(path, 0)
}

func finalDOSPath(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\`) {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	if !localDOSPath(path) {
		return "", errPath
	}
	return filepath.Clean(path), nil
}

func readFinalName(read func([]uint16) (uint32, error)) (string, error) {
	const maxUnits = 32768
	buf := make([]uint16, 512)
	for attempt := 0; attempt < 3; attempt++ {
		n, err := read(buf)
		if err != nil {
			return "", err
		}
		if n == 0 || n > maxUnits {
			return "", errPath
		}
		if int(n) >= len(buf) {
			if n == maxUnits {
				return "", errPath
			}
			buf = make([]uint16, int(n)+1)
			continue
		}
		return finalDOSPath(windows.UTF16ToString(buf[:n]))
	}
	return "", errPath
}
