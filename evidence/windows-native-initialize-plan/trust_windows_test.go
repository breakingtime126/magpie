//go:build windows

package nativeinitialize

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"unsafe"
)

const nativeSHA256 = "6774a77f5b6fb70df175f6cb3c2861dfd8600a0ac1d1c8f397e6d79d2efe9618"
const nativeSize = 369018160

// Deny concurrent writes/deletes from verification through process lifetime.
// Offline WinVerifyTrust avoids certificate retrieval during the experiment;
// inability to establish trust from this runner's cache fails closed.
func verifyNative(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("native path must be absolute")
	}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), path)
	fail := func(e error) (*os.File, error) { f.Close(); return nil, e }
	st, e := f.Stat()
	if e != nil {
		return fail(e)
	}
	if !st.Mode().IsRegular() || st.Size() != nativeSize {
		return fail(errors.New("native size/type mismatch"))
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, f); e != nil {
		return fail(e)
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != nativeSHA256 {
		return fail(errors.New("native digest mismatch"))
	}
	info := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: p}
	data := windows.WinTrustData{Size: uint32(unsafe.Sizeof(windows.WinTrustData{})), UIChoice: windows.WTD_UI_NONE, RevocationChecks: windows.WTD_REVOKE_NONE, UnionChoice: windows.WTD_CHOICE_FILE, StateAction: windows.WTD_STATEACTION_VERIFY, ProvFlags: windows.WTD_CACHE_ONLY_URL_RETRIEVAL, FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&info)}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	if e = errors.Join(verifyErr, closeErr); e != nil {
		return fail(fmt.Errorf("offline Authenticode trust failed: %w", e))
	}
	return f, nil
}
