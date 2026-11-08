package appserverbridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

const ownedSettingsLimit = 16 << 10
const ownedSettingsFilename = "config.toml"
const ownedSettingsLockname = ".magpie-settings.lock"

var errOwnedSettingsUnavailable = errors.New("bridge-owned settings unavailable; restart required")
var errOwnedSettingsConflict = errors.New("bridge-owned settings version conflict")

// The caller must validate controlHome's separation from both native homes
// before opening this store. This store never discovers a home or imports a
// native config. The exclusive marker coordinates bridge instances, not an
// adversarial same-user writer ignoring the marker. Stale locks fail closed.
type ownedSettingsStore struct {
	mu       sync.Mutex
	home     string
	path     string
	present  bool
	root     *os.Root
	identity os.FileInfo
	lock     *os.File
	lockInfo os.FileInfo
	version  string
	failed   bool
	closed   bool
	replace  func(string, string, bool) error
}

func openOwnedSettingsStore(controlHome string) (*ownedSettingsStore, error) {
	if !filepath.IsAbs(controlHome) || ownedSettingsCheckHome(controlHome) != nil {
		return nil, errOwnedSettingsUnavailable
	}
	home := filepath.Clean(controlHome)
	identity, err := os.Lstat(home)
	if err != nil || !identity.IsDir() {
		return nil, errOwnedSettingsUnavailable
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, errOwnedSettingsUnavailable
	}
	s := &ownedSettingsStore{home: home, path: filepath.Join(home, ownedSettingsFilename), root: root, identity: identity, replace: ownedSettingsReplace}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close()
		}
	}()
	if s.checkHome() != nil {
		return nil, errOwnedSettingsUnavailable
	}
	// O_EXCL refuses every existing marker, including a stale or linked one.
	s.lock, err = root.OpenFile(ownedSettingsLockname, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, errOwnedSettingsUnavailable
	}
	s.lockInfo, err = s.lock.Stat()
	if err != nil || s.checkLock() != nil {
		return nil, errOwnedSettingsUnavailable
	}
	// Do not create config.toml at open: the caller must first complete its
	// policy and catalog preconditions. Absence is tracked, not presented as
	// a real user layer. An existing empty TOML file remains valid.
	if _, err := root.Lstat(ownedSettingsFilename); err == nil {
		s.present = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errOwnedSettingsUnavailable
	}
	_, version, err := s.load()
	if err != nil {
		return nil, errOwnedSettingsUnavailable
	}
	s.version = version
	ok = true
	return s, nil
}

// Reject symlinks and all Windows reparse points in the selected path. The
// open root additionally confines subsequent relative file operations.
func ownedSettingsCheckHome(home string) error {
	for path := filepath.Clean(home); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || ownedSettingsCheckReparse(path) != nil {
			return errOwnedSettingsUnavailable
		}
		if filepath.Dir(path) == path {
			return nil
		}
	}
}

func (s *ownedSettingsStore) checkHome() error {
	if s.root == nil || ownedSettingsCheckHome(s.home) != nil {
		return errOwnedSettingsUnavailable
	}
	current, err := os.Lstat(s.home)
	opened, rootErr := s.root.Stat(".")
	if err != nil || rootErr != nil || !os.SameFile(s.identity, current) || !os.SameFile(s.identity, opened) {
		return errOwnedSettingsUnavailable
	}
	return nil
}

func (s *ownedSettingsStore) checkLock() error {
	if s.lock == nil || s.lockInfo == nil || s.checkHome() != nil {
		return errOwnedSettingsUnavailable
	}
	info, err := s.root.Lstat(ownedSettingsLockname)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, s.lockInfo) ||
		ownedSettingsCheckReparse(filepath.Join(s.home, ownedSettingsLockname)) != nil {
		return errOwnedSettingsUnavailable
	}
	return nil
}

// load reads only the fixed owned file. Check identities before reading and
// afterwards so a replaced, linked, growing or truncated file fails closed.
func (s *ownedSettingsStore) load() (object, string, error) {
	if s.checkLock() != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	before, err := s.root.Lstat(ownedSettingsFilename)
	if !s.present {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", errOwnedSettingsUnavailable
		}
		return parseOwnedSettings(nil)
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() > ownedSettingsLimit ||
		ownedSettingsCheckReparse(filepath.Join(s.home, ownedSettingsFilename)) != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	file, err := s.root.Open(ownedSettingsFilename)
	if err != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	current, pathErr := s.root.Lstat(ownedSettingsFilename)
	if err != nil || pathErr != nil || !opened.Mode().IsRegular() || !current.Mode().IsRegular() ||
		!os.SameFile(before, opened) || !os.SameFile(opened, current) || opened.Size() > ownedSettingsLimit ||
		ownedSettingsCheckReparse(filepath.Join(s.home, ownedSettingsFilename)) != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	data, readErr := io.ReadAll(io.LimitReader(file, ownedSettingsLimit+1))
	after, statErr := file.Stat()
	current, pathErr = s.root.Lstat(ownedSettingsFilename)
	closeErr := file.Close()
	if closeErr != nil || readErr != nil || statErr != nil || pathErr != nil || len(data) > ownedSettingsLimit ||
		!current.Mode().IsRegular() || !os.SameFile(opened, current) ||
		int64(len(data)) != opened.Size() || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) ||
		s.checkLock() != nil || ownedSettingsCheckReparse(filepath.Join(s.home, ownedSettingsFilename)) != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	return parseOwnedSettings(data)
}

func parseOwnedSettings(data []byte) (object, string, error) {
	if len(data) > ownedSettingsLimit || !utf8.Valid(data) {
		return nil, "", errOwnedSettingsUnavailable
	}
	values := map[string]any{}
	if toml.Unmarshal(data, &values) != nil {
		return nil, "", errOwnedSettingsUnavailable
	}
	result := object{}
	for key, value := range values {
		str, ok := value.(string)
		if !ok {
			return nil, "", errOwnedSettingsUnavailable
		}
		result[key] = raw(str)
	}
	if len(result) != 0 && !validOwnedSettings(result) {
		return nil, "", errOwnedSettingsUnavailable
	}
	version, err := ownedSettingsVersion(result)
	return result, version, err
}

func validOwnedSettings(value object) bool {
	if len(value) != 2 || !requirementsEnum(value["model_reasoning_effort"], "none") {
		return false
	}
	var model string
	return json.Unmarshal(value["model"], &model) == nil && model != "" && startupText(model)
}

func ownedSettingsVersion(value object) (string, error) {
	if len(value) != 0 && !validOwnedSettings(value) {
		return "", errOwnedSettingsUnavailable
	}
	values := map[string]any{}
	for key, encoded := range value {
		var str string
		if json.Unmarshal(encoded, &str) != nil || !utf8.ValidString(str) {
			return "", errOwnedSettingsUnavailable
		}
		values[key] = str
	}
	canonical, err := appendControlPlaneJSON167(nil, values)
	if err != nil {
		return "", errOwnedSettingsUnavailable
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (s *ownedSettingsStore) snapshot() (object, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *ownedSettingsStore) snapshotLocked() (object, string, error) {
	if s.closed || s.failed {
		return nil, "", errOwnedSettingsUnavailable
	}
	value, version, err := s.load()
	if err != nil || version != s.version {
		s.failed = true
		return nil, "", errOwnedSettingsUnavailable
	}
	return value, version, nil
}

func (s *ownedSettingsStore) read() (object, error) {
	value, _, err := s.snapshot()
	return value, err
}

func (s *ownedSettingsStore) commit(expectedVersion *string, next object) (version string, resultErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.snapshotLocked(); err != nil {
		return "", err
	}
	if expectedVersion != nil && *expectedVersion != s.version {
		return "", errOwnedSettingsConflict
	}
	if !validOwnedSettings(next) {
		return "", errOwnedSettingsUnavailable
	}
	values := map[string]string{}
	for key, encoded := range next {
		var str string
		if json.Unmarshal(encoded, &str) != nil {
			return "", errOwnedSettingsUnavailable
		}
		values[key] = str
	}
	data, err := toml.Marshal(values)
	if err != nil || len(data) > ownedSettingsLimit {
		return "", errOwnedSettingsUnavailable
	}
	_, nextVersion, err := parseOwnedSettings(data)
	if err != nil {
		return "", errOwnedSettingsUnavailable
	}
	// A no-op still verifies actual on-disk semantic content and CAS above.
	if nextVersion == s.version {
		return s.version, nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		s.failed = true
		return "", errOwnedSettingsUnavailable
	}
	tempName := ".magpie-settings-" + hex.EncodeToString(nonce[:]) + ".tmp"
	file, err := s.root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		s.failed = true
		return "", errOwnedSettingsUnavailable
	}
	tempInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		s.failed = true
		return "", errOwnedSettingsUnavailable
	}
	defer func() {
		info, err := s.root.Lstat(tempName)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(tempInfo, info) || s.root.Remove(tempName) != nil {
			s.failed = true
			version, resultErr = "", errOwnedSettingsUnavailable
		}
	}()
	n, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || n != len(data) || syncErr != nil || closeErr != nil {
		s.failed = true
		return "", errOwnedSettingsUnavailable
	}
	// Check again after staging. The lifetime lock excludes other bridge
	// writers; editors must also respect it. No filesystem API offers CAS
	// against a hostile writer changing the file after this final check.
	if _, _, err := s.snapshotLocked(); err != nil {
		return "", err
	}
	if s.replace(s.home, tempName, !s.present) != nil {
		s.failed = true // A replacement error may have an uncertain outcome.
		return "", errOwnedSettingsUnavailable
	}
	s.present = true
	_, actualVersion, err := s.load()
	if err != nil || actualVersion != nextVersion {
		s.failed = true
		return "", errOwnedSettingsUnavailable
	}
	s.version = nextVersion
	return nextVersion, nil
}

func (s *ownedSettingsStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	if s.lock != nil {
		owned := s.checkLock() == nil
		closeErr := s.lock.Close()
		if closeErr != nil || !owned {
			result = errOwnedSettingsUnavailable
		}
		if owned && closeErr == nil && s.root.Remove(ownedSettingsLockname) != nil {
			result = errOwnedSettingsUnavailable
		}
	}
	if s.root != nil && s.root.Close() != nil {
		result = errOwnedSettingsUnavailable
	}
	return result
}
