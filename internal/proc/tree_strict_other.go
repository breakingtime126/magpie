//go:build !windows

package proc

import "os/exec"

// StartTreeStrict uses StartTree's session ownership on Unix. It is not a
// security sandbox: a descendant can deliberately start another session.
func StartTreeStrict(cmd *exec.Cmd) (*Tree, error) {
	return StartTree(cmd)
}
