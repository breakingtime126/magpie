package nativeconnect

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

type journalFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

type journalPublishOps struct {
	create func(string) (journalFile, error)
	link   func(string, string) error
	remove func(string) error
}

// publishJournal never exposes a partial final journal or replaces an existing
// one. Hard-link publication is atomic and non-overwriting; unsupported local
// filesystems fail closed. File Sync does not establish power-loss durability
// of directory metadata or an atomic transaction with Windows registry values.
func publishJournal(path string, data []byte, ops journalPublishOps) (published bool, resultErr error) {
	if ops.create == nil {
		ops.create = func(dir string) (journalFile, error) { return os.CreateTemp(dir, ".selection-*") }
	}
	if ops.link == nil {
		ops.link = os.Link
	}
	if ops.remove == nil {
		ops.remove = os.Remove
	}
	f, err := ops.create(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, ops.remove(f.Name())) }()
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return false, err
	}
	if err := ops.link(f.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}
