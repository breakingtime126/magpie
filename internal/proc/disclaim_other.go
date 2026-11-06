//go:build !darwin || !cgo

package proc

import (
	"context"
	"os/exec"
)

func combinedOutputOwn(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	alreadyStarted := cmd.Process != nil
	out, err := cmd.CombinedOutput()
	// Wait has reaped a started command and finished its output before
	// cancellation takes precedence, as on macOS. Keep start and reuse
	// errors intact: only a child started by this call can be canceled here.
	if !alreadyStarted && cmd.Process != nil && cmd.ProcessState != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
	}
	return out, err
}

// Responsible is the process macOS holds responsible for pid, 0 when that
// can't be told (here, always).
func Responsible(pid int) int { return 0 }
