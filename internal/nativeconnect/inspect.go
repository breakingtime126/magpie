package nativeconnect

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Inspect reads selection evidence without creating a lock or any other file.
// It is a snapshot, not a transaction or proof of a live backend.
func (c *Controller) Inspect() (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inspectLocked()
}

func (c *Controller) inspectLocked() (Status, error) {
	if !filepath.IsAbs(c.root) {
		return Status{}, ErrUnavailable
	}
	if err := c.validateRoot(); err != nil {
		return Status{RecoveryRequired: true}, err
	}
	info, err := os.Lstat(filepath.Join(c.root, "selection.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	status := Status{JournalPresent: true, RecoveryRequired: true}
	if err != nil || !info.Mode().IsRegular() {
		return status, ErrUnavailable
	}
	j, err := c.readJournal()
	if err != nil || c.store == nil {
		return status, ErrUnavailable
	}
	actual, err := c.store.Read()
	if err != nil {
		return status, ErrUnavailable
	}
	status.RestoreAvailable = restorable(actual, j)
	if actual == j.Installed {
		status.SelectionConfigured, status.RecoveryRequired = true, false
		return status, nil
	}
	if (actual.CLI != j.Installed.CLI && actual.CLI != j.Before.CLI) || (actual.Profile != j.Installed.Profile && actual.Profile != j.Before.Profile) {
		return status, ErrConflict
	}
	return status, nil
}

// validateRoot rejects linked/non-directory components without creating state.
// It is shared by read-only inspection and the writing transaction's lock.
// These checks do not make filesystem operations atomic against same-user writers.
func (c *Controller) validateRoot() error {
	if !filepath.IsAbs(c.root) {
		return ErrUnavailable
	}
	for p := filepath.Clean(c.root); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return ErrUnavailable
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrUnavailable
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

// selectionReady is stricter than recovery inspection. A repeated Connect may
// reuse only the unchanged regular profile that was originally journaled.
// Disconnect deliberately does not call this: damaged profiles must be recoverable.
func (c *Controller) selectionReady() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateRoot(); err != nil {
		return err
	}
	j, err := c.readJournal()
	if err != nil || c.store == nil {
		return ErrUnavailable
	}
	actual, err := c.store.Read()
	if err != nil || actual != j.Installed {
		return ErrConflict
	}
	profile := j.Installed.Profile.Text
	directory, err := os.Lstat(filepath.Dir(profile))
	if err != nil || !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 {
		return ErrUnavailable
	}
	info, err := os.Lstat(profile)
	if err != nil || !info.Mode().IsRegular() {
		return ErrUnavailable
	}
	f, err := os.Open(profile)
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return ErrUnavailable
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != j.ProfileSHA256 {
		return ErrConflict
	}
	return nil
}
