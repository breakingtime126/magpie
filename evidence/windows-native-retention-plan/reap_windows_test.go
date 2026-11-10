//go:build windows

package nativeretention

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestParentReapEvidence(t *testing.T) {
	exit := &exec.ExitError{}
	for _, tc := range []struct {
		name             string
		hasState, exited bool
		err              error
		wantOK           bool
	}{
		{"normal-exit", true, true, nil, true},
		{"bare-exit-error", true, true, exit, true},
		{"missing-state", false, false, nil, false},
		{"missing-state-with-exit-error", false, false, exit, false},
		{"not-exited", true, false, nil, false},
		{"unexpected-error", true, true, errors.New("wait failed"), false},
		{"joined-exit-and-reap-error", true, true, errors.Join(exit, errors.New("reap failed")), false},
		{"joined-exit-alone", true, true, errors.Join(exit), false},
		{"wrapped-exit", true, true, fmt.Errorf("wrapped: %w", exit), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkParentReaped(tc.hasState, tc.exited, tc.err); (got == nil) != tc.wantOK {
				t.Fatalf("got %v, wantOK %v", got, tc.wantOK)
			}
		})
	}
	if parentReapEvidence(nil, nil) == nil || parentReapEvidence(&exec.Cmd{}, nil) == nil {
		t.Fatal("absent process state accepted")
	}
}
