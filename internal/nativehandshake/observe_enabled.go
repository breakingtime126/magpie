//go:build nativehandshakediag

package nativehandshake

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const maxEvents = 64
const maxFiles = 16

var ownedParent = regexp.MustCompile(`^original-desktop-acceptance-[0-9a-f]{32}$`)

type recorder struct {
	mu       sync.Mutex
	file     *os.File
	sequence int
	seen     [len(names)]bool
}

func diagnosticLocation(executable string) (string, string, bool) {
	if !filepath.IsAbs(executable) {
		return "", "", false
	}
	dir := filepath.Dir(executable)
	if filepath.Base(dir) != "app" || !ownedParent.MatchString(filepath.Base(filepath.Dir(dir))) {
		return "", "", false
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil || !strings.EqualFold(resolved, executable) {
		return "", "", false
	}
	role := ""
	switch filepath.Base(executable) {
	case "Magpie-Native-Codex-Experimental.exe":
		role = "host"
	case "magpie-codex-bridge.exe":
		role = "bridge"
	default:
		return "", "", false
	}
	return dir, role, true
}

func openRecorder(dir, role string) *recorder {
	if role != "host" && role != "bridge" {
		return nil
	}
	for i := 1; i <= maxFiles; i++ {
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("native-handshake-%s.%02d.log", role, i)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil
		}
		return &recorder{file: f}
	}
	return nil
}

func (r *recorder) emit(stage Stage) {
	if r == nil || stage.String() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil || r.sequence >= maxEvents || r.seen[stage] {
		return
	}
	r.seen[stage] = true
	r.sequence++
	line := fmt.Sprintf("native-backend-handshake-v1 sequence=%d stage=%s\n", r.sequence, stage)
	n, err := r.file.WriteString(line)
	if err != nil || n != len(line) {
		_ = r.file.Close()
		r.file = nil
	}
}

var process struct {
	once     sync.Once
	recorder *recorder
	role     string
}

func processEnabled() bool {
	process.once.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		dir, role, ok := diagnosticLocation(exe)
		if !ok {
			return
		}
		process.role = role
		process.recorder = openRecorder(dir, role)
	})
	return process.recorder != nil
}

func processEmit(stage Stage) {
	if !processEnabled() {
		return
	}
	if (stage <= HostJSONLAdmitted) != (process.role == "host") {
		return
	}
	process.recorder.emit(stage)
}
