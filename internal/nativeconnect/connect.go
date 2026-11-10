// Package nativeconnect configures a future Codex launch. It never claims that
// a persisted launch choice is a successful backend handshake or conversation.
package nativeconnect

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
	"sync"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/nativeprofile"
)

var (
	ErrMissingCatalog = errors.New("native Codex connection requires a verified AIHub catalog")
	ErrUnavailable    = errors.New("native Codex launch selection unavailable")
	ErrConflict       = errors.New("native Codex launch selection changed externally; nothing restored")
	ErrUncertain      = errors.New("native Codex launch selection partially changed; use Disconnect to recover")
)

// Value preserves absence, empty values and the Windows string registry type.
// Only REG_SZ (1) and REG_EXPAND_SZ (2) are supported.
type Value struct {
	Present bool   `json:"present"`
	Kind    uint32 `json:"kind"`
	Text    string `json:"text"`
}

// Selection contains only non-secret launch paths, never a gateway API key.
type Selection struct {
	CLI     Value `json:"cli"`
	Profile Value `json:"profile"`
}

// Store compares both values before changing either. A partial or uncertain
// change returns an error and leaves the journal available for explicit recovery.
// External writers do not participate in Magpie's lock; this is not OS-wide CAS.
type Store interface {
	Read() (Selection, error)
	Replace(before, after Selection) error
}

// Plan is returned by trusted in-process preparation, not accepted from HTTP.
// Prepare owns native discovery/version/provenance and supplier catalog checks;
// it must not return until the launcher and secondary gateway handoff are ready.
// Plans transfer no ownership. The generation owner retains resources even when
// selection fails, and supplies a trusted launcher identity and frozen profile.
type Plan struct {
	Launcher         string
	LauncherIdentity TrustedLauncherIdentity
	Config           appserverbridge.Config
	PreparedProfile  *nativeprofile.Prepared
}

type Prepare func(context.Context) (Plan, error)

type Status struct {
	JournalPresent           bool `json:"journalPresent"`
	RestoreAvailable         bool `json:"restoreAvailable"`
	SelectionConfigured      bool `json:"selectionConfigured"`
	BackendHandshakeVerified bool `json:"backendHandshakeVerified"`
	ChatVerified             bool `json:"chatVerified"`
	RecoveryRequired         bool `json:"recoveryRequired"`
}

type Controller struct {
	mu         sync.Mutex
	root       string
	store      Store
	prepare    Prepare
	journalOps journalPublishOps
}

type journal struct {
	Version       int       `json:"version"`
	Before        Selection `json:"before"`
	Installed     Selection `json:"installed"`
	ProfileSHA256 string    `json:"profileSHA256"`
}

// New is side-effect free. root must be the separate Magpie-owned connection
// directory, not a native HOME or CODEX_HOME. No native process is started.
func New(root string, store Store, prepare Prepare) *Controller {
	return &Controller{root: root, store: store, prepare: prepare}
}

func validValue(v Value) bool {
	if !v.Present {
		return v.Kind == 0 && v.Text == ""
	}
	return (v.Kind == 1 || v.Kind == 2) && len(v.Text) <= 32767 && !bytes.ContainsRune([]byte(v.Text), 0)
}

func validSelection(s Selection) bool {
	return validValue(s.CLI) && validValue(s.Profile)
}

func (c *Controller) lock() (func(), error) {
	if c.store == nil || !filepath.IsAbs(c.root) {
		return nil, ErrUnavailable
	}
	if err := c.validateRoot(); err != nil {
		return nil, err
	}
	unlock, held := filelock.Lock(filepath.Join(c.root, "selection.lock"), 0)
	if !held {
		return nil, ErrUnavailable
	}
	return unlock, nil
}

func (c *Controller) Connect(ctx context.Context) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Recover an existing transaction even when preparation is now unavailable.
	// This read is side-effect free, preserving missing-input zero-write behavior.
	if current, err := c.inspectLocked(); err != nil || current.JournalPresent {
		if err != nil {
			return current, err
		}
		return current, ErrConflict
	}
	// Missing metadata is checked before even creating the lock/profile folder.
	if c.prepare == nil {
		return Status{}, ErrMissingCatalog
	}
	plan, err := c.prepare(ctx)
	if err != nil {
		if errors.Is(err, ErrMissingCatalog) {
			return Status{}, safeMissingCatalog(err)
		}
		return Status{}, ErrUnavailable
	}
	if ctx.Err() != nil || plan.Config.SharedNativeRoute == nil || plan.Config.Validate() != nil || !filepath.IsAbs(plan.Launcher) {
		return Status{}, ErrUnavailable
	}
	for _, backend := range plan.Config.Backends {
		if !separateConnectionRoot(c.root, backend.Home, backend.CodexHome) {
			return Status{}, ErrUnavailable
		}
	}
	if !planLauncherMatches(plan) {
		return Status{}, ErrUnavailable
	}
	var data []byte
	if plan.PreparedProfile != nil {
		if plan.PreparedProfile.ValidateFor(plan.Config) != nil {
			return Status{}, ErrUnavailable
		}
		data = plan.PreparedProfile.Bytes()
	} else {
		data, err = json.MarshalIndent(plan.Config, "", "  ")
	}
	if err != nil || len(data) == 0 || len(data) > 1<<20 {
		return Status{}, ErrUnavailable
	}
	unlock, err := c.lock()
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(c.root, "selection.json")); !errors.Is(err, os.ErrNotExist) {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrConflict
	}
	before, err := c.store.Read()
	if err != nil || !validSelection(before) {
		return Status{}, ErrUnavailable
	}
	dir, err := os.MkdirTemp(c.root, "profile-")
	if err != nil {
		return Status{}, ErrUnavailable
	}
	profile := filepath.Join(dir, "bridge.json")
	if err := writeNew(profile, data); err != nil {
		return Status{}, ErrUnavailable
	}
	installed := Selection{CLI: Value{true, 1, plan.Launcher}, Profile: Value{true, 1, profile}}
	sum := sha256.Sum256(data)
	j := journal{1, before, installed, hex.EncodeToString(sum[:])}
	encoded, _ := json.Marshal(j)
	if len(encoded) > 128<<10 {
		return Status{}, ErrUnavailable
	}
	published, err := publishJournal(filepath.Join(c.root, "selection.json"), encoded, c.journalOps)
	if err != nil {
		if published {
			return Status{JournalPresent: true, RestoreAvailable: true, RecoveryRequired: true}, ErrUnavailable
		}
		return Status{}, ErrUnavailable
	}
	// A complete non-overwriting journal is published before any registry
	// mutation. This is crash recovery, not power-loss transaction durability.
	// Recheck the caller's build pin immediately before selecting its path.
	// This still cannot authenticate a later Desktop launch after replacement.
	if !planLauncherMatches(plan) {
		return Status{JournalPresent: true, RestoreAvailable: true, RecoveryRequired: true}, ErrUnavailable
	}
	// Never roll back automatically after an uncertain result or retry a launch.
	if ctx.Err() != nil || c.store.Replace(before, installed) != nil {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUncertain
	}
	actual, err := c.store.Read()
	if err != nil || actual != installed {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUncertain
	}
	return Status{JournalPresent: true, RestoreAvailable: true, SelectionConfigured: true}, nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (c *Controller) readJournal() (journal, error) {
	var j journal
	path := filepath.Join(c.root, "selection.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return j, ErrUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return j, ErrUnavailable
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if err != nil || len(b) > 128<<10 {
		return j, ErrUnavailable
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&j) != nil || dec.Decode(new(any)) != io.EOF || j.Version != 1 || !validSelection(j.Before) || !validSelection(j.Installed) {
		return j, ErrUnavailable
	}
	profile := j.Installed.Profile.Text
	rel, err := filepath.Rel(c.root, profile)
	if err != nil || filepath.IsAbs(rel) || filepath.Dir(filepath.Dir(profile)) != filepath.Clean(c.root) || filepath.Base(profile) != "bridge.json" {
		return j, ErrUnavailable
	}
	// Restoration does not execute or consume the profile. Its loss or damage
	// must not trap a user on this selection when the journal and pair match.
	digest, err := hex.DecodeString(j.ProfileSHA256)
	if err != nil || len(digest) != sha256.Size || !j.Installed.CLI.Present || j.Installed.CLI.Kind != 1 || !filepath.IsAbs(j.Installed.CLI.Text) || !j.Installed.Profile.Present || j.Installed.Profile.Kind != 1 {
		return j, ErrUnavailable
	}
	return j, nil
}

func (c *Controller) Disconnect() (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	unlock, err := c.lock()
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	j, err := c.readJournal()
	if err != nil {
		return Status{RecoveryRequired: true}, err
	}
	actual, err := c.store.Read()
	if err != nil {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUnavailable
	}
	// Each value must still be ours or its exact prior value (crash recovery).
	// Check the entire pair before restoring anything.
	if !restorable(actual, j) {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrConflict
	}
	if c.store.Replace(actual, j.Before) != nil {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUncertain
	}
	restored, err := c.store.Read()
	if err != nil || restored != j.Before {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUncertain
	}
	if os.Remove(filepath.Join(c.root, "selection.json")) != nil {
		return Status{JournalPresent: true, RecoveryRequired: true}, ErrUncertain
	}
	// Keep the immutable Magpie profile for diagnostics; never delete native
	// state or assume an already running Desktop changed its environment.
	return Status{}, nil
}

// The expected identity belongs to trusted preparation, never a mutable sibling
// manifest or a hash calculated from this candidate at verification time.
func planLauncherMatches(plan Plan) bool {
	verified, err := VerifyPackagedLauncher(filepath.Dir(plan.Launcher), plan.LauncherIdentity)
	return err == nil && verified.Path == plan.Launcher
}

func restorable(actual Selection, j journal) bool {
	return (actual.CLI == j.Installed.CLI || actual.CLI == j.Before.CLI) && (actual.Profile == j.Installed.Profile || actual.Profile == j.Before.Profile)
}
