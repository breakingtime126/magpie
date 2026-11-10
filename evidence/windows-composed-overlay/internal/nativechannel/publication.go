package nativechannel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const publicationLimit = 16 << 10

var ErrPublication = errors.New("native channel publication unavailable or changed")

// Reference is public, not a secret or opaque capability. BOTH fields must
// originate from an independent owning source, such as an already authenticated
// immutable profile. A digest stored beside its mutable publication (including
// a self-asserted journal) cannot authenticate that publication.
type Reference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type publication struct {
	Version        int    `json:"version"`
	URL            string `json:"url"`
	CertificateDER []byte `json:"certificate_der"`
}

// Publish creates a new public descriptor artifact exclusively and syncs its
// file before returning a reference. The parent directory must already exist
// and be caller-owned. It never overwrites, creates directories, stores private
// keys, discovers sessions or modifies an existing profile. Failure may leave
// an incomplete new artifact; it is not silently removed or retried. File sync
// is not a guarantee of directory durability across machine power loss.
func Publish(path string, descriptor Descriptor) (Reference, error) {
	certificate, _, err := validateDescriptor(descriptor)
	if err != nil {
		return Reference{}, ErrPublication
	}
	data, err := json.Marshal(publication{Version: 1, URL: descriptor.URL, CertificateDER: certificate.Raw})
	if err != nil || len(data) > publicationLimit {
		return Reference{}, ErrPublication
	}
	root, name, err := publicationRoot(path)
	if err != nil {
		return Reference{}, ErrPublication
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Reference{}, ErrPublication
	}
	info, statErr := f.Stat()
	n, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	after, afterErr := root.Lstat(name)
	if statErr != nil || writeErr != nil || syncErr != nil || closeErr != nil || afterErr != nil || n != len(data) || !info.Mode().IsRegular() || !after.Mode().IsRegular() || !publicationPathAllowed(path, after) || !os.SameFile(info, after) {
		return Reference{}, ErrPublication
	}
	sum := sha256.Sum256(data)
	reference := Reference{Path: path, SHA256: hex.EncodeToString(sum[:])}
	if _, err := ReadPublication(reference); err != nil {
		return Reference{}, ErrPublication
	}
	return reference, nil
}

// ReadPublication recovers only the exact authenticated bytes identified by
// reference. It does not establish reference provenance, prove live serving,
// restart a session, accept a new pin, follow a current-session manifest, or dial
// anything. An old publication remains an old session, even at a reused port.
func ReadPublication(reference Reference) (Descriptor, error) {
	digest, err := hex.DecodeString(reference.SHA256)
	if err != nil || len(digest) != sha256.Size || reference.SHA256 != hex.EncodeToString(digest) {
		return Descriptor{}, ErrPublication
	}
	root, name, err := publicationRoot(reference.Path)
	if err != nil {
		return Descriptor{}, ErrPublication
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || !publicationPathAllowed(reference.Path, before) || before.Size() > publicationLimit {
		return Descriptor{}, ErrPublication
	}
	f, err := root.Open(name)
	if err != nil {
		return Descriptor{}, ErrPublication
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return Descriptor{}, ErrPublication
	}
	data, err := io.ReadAll(io.LimitReader(f, publicationLimit+1))
	after, afterErr := root.Lstat(name)
	if err != nil || len(data) > publicationLimit || afterErr != nil || !after.Mode().IsRegular() || !publicationPathAllowed(reference.Path, after) || !os.SameFile(opened, after) {
		return Descriptor{}, ErrPublication
	}
	sum := sha256.Sum256(data)
	if !bytes.Equal(sum[:], digest) {
		return Descriptor{}, ErrPublication
	}
	var value publication
	if json.Unmarshal(data, &value) != nil || value.Version != 1 {
		return Descriptor{}, ErrPublication
	}
	// Require the exact writer format: no unknown/duplicate fields, trailing
	// JSON, alternate encoding or ignored data can acquire meaning later.
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, data) {
		return Descriptor{}, ErrPublication
	}
	descriptor := Descriptor{URL: value.URL, CertificateDER: value.CertificateDER}
	if _, _, err := validateDescriptor(descriptor); err != nil {
		return Descriptor{}, ErrPublication
	}
	return descriptor, nil
}

// Root-relative access pins the opened parent directory. Link/component and
// open-identity checks reject accidental redirection; they are not an OS-level
// promise against a hostile same-user actor or every Windows reparse mechanism.
// The independently trusted digest, not path metadata, authenticates bytes.
func publicationRoot(path string) (*os.Root, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 32767 || strings.ContainsRune(path, 0) {
		return nil, "", ErrPublication
	}
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if !filepath.IsLocal(name) || name == "." || strings.ContainsRune(name, ':') {
		return nil, "", ErrPublication
	}
	var parentInfo os.FileInfo
	for component := parent; ; component = filepath.Dir(component) {
		info, err := os.Lstat(component)
		if err != nil || !info.IsDir() || !publicationPathAllowed(component, info) {
			return nil, "", ErrPublication
		}
		if component == parent {
			parentInfo = info
		}
		if filepath.Dir(component) == component {
			break
		}
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", ErrPublication
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(parentInfo, opened) {
		root.Close()
		return nil, "", ErrPublication
	}
	return root, name, nil
}
