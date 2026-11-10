//go:build !windows

package proc

import (
	"errors"
	"os/exec"
)

// StartTreeDrained never substitutes Unix process-group semantics for a
// retained, accounting-verified Windows Job.
func StartTreeDrained(*exec.Cmd) (*DrainedTree, error) { return nil, errors.ErrUnsupported }
