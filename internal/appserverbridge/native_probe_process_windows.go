package appserverbridge

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// This production primary probe uses retained Job accounting, unlike the
// portable comparison harness. Root exit and whole-tree drain are independent.
func runProductionPrimaryProbe(ctx context.Context, b Backend, directory string, args, environment []string) (result []byte, resultErr error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, errStartupProbe
	}
	defer reader.Close()
	command := proc.Command(b.Command, args...)
	command.Env = environment
	command.Dir = directory
	command.Stdout = writer
	tree, err := proc.StartTreeDrained(command)
	writer.Close()
	if tree != nil {
		defer func() {
			closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			stopErr := tree.Stop()
			drainErr := tree.Wait(closeCtx)
			resultErr = errors.Join(resultErr, stopErr, drainErr)
			if drainErr != nil {
				// Retain the SAME Job on an uncertain drain. Background cleanup may only
				// terminate/wait this owned tree; it cannot start a replacement or succeed
				// the failed probe. Keep ownership until accounting confirms zero children.
				go retainProbeDrain(tree)
			}
			if resultErr != nil {
				result = nil
			}
		}()
	}
	if err != nil || tree == nil {
		return nil, errStartupProbe
	}
	waitCtx, stopWait := context.WithCancel(ctx)
	waitDone := make(chan error, 1)
	go func() { waitDone <- tree.Wait(waitCtx) }()
	defer func() { stopWait(); <-waitDone }()
	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(reader, startupProbeLimit+1))
		reads <- readResult{data, err}
	}()
	defer func() { reader.Close(); <-reads }()
	var r readResult
	select {
	case <-ctx.Done():
		return nil, errStartupProbe
	case r = <-reads:
		reads <- r
	}
	if r.err != nil || len(r.data) > startupProbeLimit {
		return nil, errStartupProbe
	}
	select {
	case <-ctx.Done():
		return nil, errStartupProbe
	case waitErr := <-waitDone:
		waitDone <- waitErr
		if waitErr != nil || ctx.Err() != nil {
			return nil, errStartupProbe
		}
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() || !command.ProcessState.Success() {
		return nil, errStartupProbe
	}
	return r.data, nil
}

func retainProbeDrain(tree *proc.DrainedTree) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = tree.Stop()
		err := tree.Wait(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
