package nativeconnect

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

func TestManagerDiagnosticsCancellationAndCleanupPreserveResult(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		calls := 0
		g := &fixtureGeneration{closeErr: errors.New("SECRET-CLEANUP")}
		m := NewManager(context.Background(), filepath.Join(t.TempDir(), "selection"), &fakeSelection{}, func(context.Context, context.Context) (Generation, error) {
			calls++
			return g, errors.New("SECRET-PREPARATION")
		})
		defer m.Close(context.Background())
		request, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		defer cancel()
		var lines []string
		ctx, finish := nativeprepdiag.Begin(request, func(s string) { lines = append(lines, s) })
		status, err := m.Connect(ctx)
		finish(err == nil)
		output := strings.Join(lines, "\n")
		if status != (Status{}) || strings.Contains(output, "SECRET") {
			t.Fatal("changed status/leaked error", status, output)
		}
		if cancelled {
			if !errors.Is(err, context.Canceled) || calls != 0 || g.closed != 0 || !strings.Contains(output, "stage=manager-admission outcome=failed") {
				t.Fatal("cancelled attempt changed", err, calls, output)
			}
		} else {
			if !errors.Is(err, ErrUnavailable) || !errors.Is(err, g.closeErr) || calls != 1 || g.closed != 1 || !strings.Contains(output, "stage=preparation-cleanup outcome=failed") {
				t.Fatal("cleanup/original error changed", err, calls, g.closed, output)
			}
		}
	}
}
