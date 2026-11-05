package appserverbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Keep a handle open before shutdown so a recycled PID cannot make a surviving
// fixture look dead (or make cleanup terminate an unrelated process).
func fixtureProcessHandle(t *testing.T, pid int) windows.Handle {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open fixture process %d: %v", pid, err)
	}
	t.Cleanup(func() {
		defer windows.CloseHandle(h)
		state, err := windows.WaitForSingleObject(h, 0)
		if err != nil {
			t.Errorf("query fixture process: %v", err)
			return
		}
		if state == uint32(windows.WAIT_TIMEOUT) {
			if err := windows.TerminateProcess(h, 1); err != nil {
				t.Errorf("clean up fixture process: %v", err)
			}
			if state, err := windows.WaitForSingleObject(h, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
				t.Errorf("fixture cleanup did not finish: state=%d error=%v", state, err)
			}
		}
	})
	if state, err := windows.WaitForSingleObject(h, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("fixture was not alive before shutdown: state=%d error=%v", state, err)
	}
	return h
}

func waitFixtureFamily(t *testing.T, b Backend) []windows.Handle {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var parent windows.Handle
	for {
		if parent == 0 {
			data, err := os.ReadFile(filepath.Join(b.Home, "fixture-report"))
			var report fixtureReport
			if err == nil && json.Unmarshal(data, &report) == nil && report.PID > 0 {
				parent = fixtureProcessHandle(t, report.PID)
			}
		}
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-descendant"))
		pid, parseErr := strconv.Atoi(string(data))
		if parent != 0 && err == nil && parseErr == nil && pid > 0 {
			return []windows.Handle{parent, fixtureProcessHandle(t, pid)}
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("backend family did not become ready")
		}
	}
}

func TestRunWindowsReapsStartedBackendFamilies(t *testing.T) {
	for _, scenario := range []struct {
		name, mode, stop string
	}{
		{"client-eof", "descendant", "eof"},
		{"context-cancel", "descendant", "cancel"},
		{"forced-eof", "descendant-stubborn", "eof"},
		{"forced-cancel", "descendant-stubborn", "cancel"},
		{"backend-malformed", "descendant-malformed", "request"},
		{"backend-exit", "descendant-exit", "request"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := fixtureConfig(t)
			for i, b := range cfg.Backends {
				mode := scenario.mode
				if scenario.stop == "request" && i == 0 {
					mode = "descendant-stubborn"
				}
				if err := os.WriteFile(filepath.Join(b.Home, "fixture-mode"), []byte(mode), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := startFixtureBridge(t, cfg)
			var processes []windows.Handle
			for _, b := range cfg.Backends {
				processes = append(processes, waitFixtureFamily(t, b)...)
			}
			switch scenario.stop {
			case "eof":
				r.input.Close()
			case "cancel":
				r.cancel()
			case "request":
				if _, err := fmt.Fprintln(r.input, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"windows-family-fixture","version":"1"}}}`); err != nil {
					t.Fatal(err)
				}
			}
			err := r.wait(t)
			if scenario.stop == "eof" && err != nil {
				t.Fatalf("client EOF: %v", err)
			}
			if scenario.stop == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("context cancellation: %v", err)
			}
			if scenario.stop == "request" && err == nil {
				t.Fatal("backend failure did not fail the bridge")
			}
			for _, process := range processes {
				if state, err := windows.WaitForSingleObject(process, 10000); err != nil || state != windows.WAIT_OBJECT_0 {
					t.Errorf("fixture process survived bridge shutdown: state=%d error=%v", state, err)
				}
			}
		})
	}
}
