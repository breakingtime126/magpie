// Package nativebootstrap provides a single-generation Windows pipe
// handoff. It authenticates an expected package process, not executable bytes
// against a hostile same-user actor. Activation requires an explicit owner.
package nativebootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/nativechannel"
)

var ErrUnavailable = errors.New("native bootstrap owner unavailable")

const payloadLimit = 16 << 10
const handshakeLimit = 5 * time.Second

// Peer is an explicit owner policy. Path is independently selected by the
// caller, never learned from a profile or peer. Check may enforce the GUI's
// compiled launcher pin. It is not a reverse GUI cryptographic pin.
// Check runs before disclosure and after transfer; it must be bounded and
// must not retain the borrowed Process handle or alter its lifetime.
type Peer struct {
	Path  string
	Check func(context.Context, Process) error
}

// Process describes kernel observations of the retained peer process.
// NativeHandle is borrowed, read-only, and only valid during Check.
type Process struct {
	PID          uint32
	Path         string
	Created      uint64
	NativeHandle uintptr
}

// Record belongs to one live owner generation. Generation is a public opaque
// identifier, not a credential. Reference must originate in that owner, never
// from an unverified self-hashed sibling profile.
type Record struct {
	Version       int                     `json:"version"`
	Generation    string                  `json:"generation"`
	Reference     nativechannel.Reference `json:"reference"`
	ProfileSHA256 string                  `json:"profile_sha256"`
}

func validGeneration(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16 && strings.ToLower(s) == s
}
func (r Record) validate() error {
	b, err := hex.DecodeString(r.Reference.SHA256)
	profile, profileErr := hex.DecodeString(r.ProfileSHA256)
	if profileErr != nil || len(profile) != sha256.Size || strings.ToLower(r.ProfileSHA256) != r.ProfileSHA256 || r.Version != 1 || !validGeneration(r.Generation) || !filepath.IsAbs(r.Reference.Path) || filepath.Clean(r.Reference.Path) != r.Reference.Path || strings.ContainsRune(r.Reference.Path, 0) || err != nil || len(b) != sha256.Size || strings.ToLower(r.Reference.SHA256) != r.Reference.SHA256 {
		return ErrUnavailable
	}
	return nil
}

// VerifyProfile binds the bounded exact raw bytes to the live owner's intent.
// Call only with a Record received over a verified Lease, before LoadConfig or
// any runtime/catalog/routing side effects. A self-asserted Record authenticates
// nothing. The profile contains its locator, never its own expected digest.
func (r Record) VerifyProfile(data []byte) error {
	if r.validate() != nil || len(data) == 0 || len(data) > 1<<20 {
		return ErrUnavailable
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != r.ProfileSHA256 {
		return ErrUnavailable
	}
	return nil
}

func encode(r Record) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(r)
	if len(b) > payloadLimit {
		return nil, ErrUnavailable
	}
	return b, err
}
func decode(b []byte, generation string) (Record, error) {
	var r Record
	if len(b) > payloadLimit {
		return r, ErrUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.validate() != nil || r.Generation != generation {
		return Record{}, ErrUnavailable
	}
	canonical, err := encode(r)
	if err != nil || !bytes.Equal(b, canonical) {
		return Record{}, ErrUnavailable
	}
	return r, nil
}

// ResolvePackagedPeer derives the opposite executable from this process's own
// exact package location. The package directory is a filesystem trust
// assumption, not a signature or independently pinned GUI digest. It rejects
// ambiguous linked paths and does not follow peer/profile-supplied paths.
func ResolvePackagedPeer() (Peer, error) {
	self, err := os.Executable()
	if err != nil {
		return Peer{}, ErrUnavailable
	}
	return resolvePackagedPeer(self)
}
func resolvePackagedPeer(self string) (Peer, error) {
	var other string
	switch filepath.Base(self) {
	case "magpie-codex-bridge.exe":
		other = "Magpie-Native-Codex-Experimental.exe"
	case "Magpie-Native-Codex-Experimental.exe":
		other = "magpie-codex-bridge.exe"
	default:
		return Peer{}, ErrUnavailable
	}
	if err := regularPackagePath(self); err != nil {
		return Peer{}, err
	}
	target := filepath.Join(filepath.Dir(self), other)
	if err := regularPackagePath(target); err != nil {
		return Peer{}, err
	}
	return Peer{Path: target}, nil
}
func regularPackagePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnavailable
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnavailable
		}
		if p == path {
			if !info.Mode().IsRegular() {
				return ErrUnavailable
			}
		} else if !info.IsDir() {
			return ErrUnavailable
		}
		if err := rejectReparse(p); err != nil {
			return err
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

// bootstrapPipeName keeps startup inspection separate from the single-use
// launch channel, while retaining the existing launch name byte for byte.
func bootstrapPipeName(generation string, startupProbe bool) (string, error) {
	if !validGeneration(generation) {
		return "", ErrUnavailable
	}
	name := `\\.\pipe\magpie-native-bootstrap-` + generation
	if startupProbe {
		name += "-startup-probe"
	}
	return name, nil
}
