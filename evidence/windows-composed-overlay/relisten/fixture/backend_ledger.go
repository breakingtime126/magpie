package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Every synthetic backend process records entry before any argv/phase checks.
// Unique exclusive empty files make even failed old-phase starts observable.
// The directory belongs to this harness; no arguments or credentials are saved.
func recordBackendStart(root string) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.started", os.Getpid(), hex.EncodeToString(nonce[:]))
	f, err := os.OpenFile(filepath.Join(root, "backend-starts", name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}

// Snapshot only after the relevant retained process Jobs have drained. A failed
// listing or malformed marker is not interpreted as an unchanged/empty ledger.
func backendStartSnapshot(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "backend-starts"))
	if err != nil {
		return nil, err
	}
	if len(entries) > 64 {
		return nil, errors.New("backend start ledger exceeded fixture bound")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		pidText, tail, ok := strings.Cut(name, "-")
		pid, pidErr := strconv.Atoi(pidText)
		nonce, nonceErr := hex.DecodeString(strings.TrimSuffix(tail, ".started"))
		info, infoErr := entry.Info()
		if !ok || pidErr != nil || pid <= 0 || nonceErr != nil || len(nonce) != 16 || !strings.HasSuffix(tail, ".started") || infoErr != nil || !info.Mode().IsRegular() || info.Size() != 0 {
			return nil, errors.New("invalid backend start ledger marker")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
