package provider

import (
	"context"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/buildvariant"
)

func TestNativeGPTWatchdogBuildBoundary(t *testing.T) {
	oldRoute, oldList, oldEnd := CodexAppRoute, codexAppServersOn, endCodexAppServer
	oldStart, oldOpen, oldReopen := codexServerStart, CodexAppOpen, CodexAppReopen
	oldTurn, oldQuiet, oldEvery := codexTurnRunning, codexSessionsQuiet, codexAppEvery
	t.Cleanup(func() {
		CodexAppRoute, codexAppServersOn, endCodexAppServer = oldRoute, oldList, oldEnd
		codexServerStart, CodexAppOpen, CodexAppReopen = oldStart, oldOpen, oldReopen
		codexTurnRunning, codexSessionsQuiet, codexAppEvery = oldTurn, oldQuiet, oldEvery
	})
	routeCalls, otherCalls := 0, 0
	CodexAppRoute = func() (string, bool, bool) {
		routeCalls++
		return "", false, false
	}
	codexAppServersOn = func(context.Context, string) ([]int, error) {
		otherCalls++
		return nil, nil
	}
	endCodexAppServer = func(int) error {
		otherCalls++
		return nil
	}
	codexServerStart = func(int) (time.Time, error) {
		otherCalls++
		return time.Time{}, nil
	}
	CodexAppOpen = func(context.Context) (bool, bool) {
		otherCalls++
		return false, false
	}
	CodexAppReopen = func(context.Context) error {
		otherCalls++
		return nil
	}
	codexTurnRunning = func(string, time.Time, time.Time) bool {
		otherCalls++
		return false
	}
	codexSessionsQuiet = func(string, time.Time, time.Duration) bool {
		otherCalls++
		return false
	}
	busy := func() (int, time.Time) {
		otherCalls++
		return 0, time.Time{}
	}
	if ended := codexAppStep(context.Background(), time.Now(), busy); ended != nil {
		t.Fatal("unexpected ended process IDs")
	}
	if buildvariant.NativeCodex {
		// A zero interval would panic if Keep reached ticker construction.
		// A live context also proves it returns without waiting for cancellation.
		codexAppEvery = 0
		KeepCodexAppOnItsProvider(context.Background(), busy)
		if routeCalls != 0 || otherCalls != 0 {
			t.Fatal("native build reached legacy watchdog hooks")
		}
	} else if routeCalls != 1 || otherCalls != 0 {
		t.Fatal("ordinary build changed its unknown-route behavior")
	}
}
