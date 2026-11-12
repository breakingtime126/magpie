package nativepreview

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/nativeprepdiag"
)

// queryProcess is the existing strict process owner's wait/stop surface. Tests
// supply inert results; production supplies only proc.StartTreeStrict.
type queryProcess interface {
	Wait() error
	Stop(time.Duration) <-chan struct{}
}

type queryRootState uint8

const (
	queryRootUnknown queryRootState = iota
	queryRootRunning
	queryRootExited
)

// The observer owns only a retained wait handle, never process cleanup.
// Its callbacks run serially in runInstalledQuery, including final release.
type queryRootObservation struct {
	state func() queryRootState
	close func()
}

func runInstalledQuery(ctx context.Context, command *exec.Cmd, start func(*exec.Cmd) (queryProcess, error), observe func(*exec.Cmd) queryRootObservation) (string, error) {
	trace := nativeprepdiag.Track(ctx, nativeprepdiag.QueryStart)
	defer trace.End()
	output := &queryOutput{}
	command.Stdout = output
	progress := &queryProgress{}
	command.Stderr = progress
	// Keep the host environment for the read-only Windows package query. No
	// AIHub key has ever been put in it; no network/security setting is changed.
	if ctx.Err() != nil {
		queryContextFailure(ctx)
		return "", ErrUnavailable
	}
	tree, err := start(command)
	if err != nil {
		return "", ErrUnavailable
	}
	var root queryRootObservation
	if observe != nil {
		root = observe(command)
		if root.close != nil {
			defer root.close()
		}
	}
	trace.Next(nativeprepdiag.QueryWait)
	done := make(chan error, 1)
	go func() { done <- tree.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			queryFailure(ctx, queryExitStage(err))
			return "", ErrUnavailable
		}
		trace.Next(nativeprepdiag.QueryOutputBound)
		if output.exceeded {
			return "", ErrUnavailable
		}
	case <-ctx.Done():
		progress.snapshot(ctx)
		queryRootSnapshot(ctx, root)
		queryContextFailure(ctx)
		<-tree.Stop(time.Second)
		<-done
		return "", ErrUnavailable
	}
	trace.Next(nativeprepdiag.QueryJSON)
	if !strictJSON(output.data) {
		return "", ErrUnavailable
	}
	var found struct {
		Path          string `json:"path"`
		Family        string `json:"family"`
		SignatureKind string `json:"signatureKind"`
	}
	if json.Unmarshal(output.data, &found) != nil {
		return "", ErrUnavailable
	}
	trace.Next(nativeprepdiag.QueryIdentity)
	if !filepath.IsAbs(found.Path) || found.Family != "OpenAI.Codex_2p2nqsd0c76g0" || found.SignatureKind != "Store" {
		return "", ErrUnavailable
	}
	trace.Complete()
	return found.Path, nil
}

func queryFailure(ctx context.Context, stage nativeprepdiag.Stage) {
	trace := nativeprepdiag.Track(ctx, stage)
	trace.End()
}
func queryContextFailure(ctx context.Context) {
	stage := nativeprepdiag.QueryCancelled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		stage = nativeprepdiag.QueryDeadline
	}
	queryFailure(ctx, stage)
}
func queryExitStage(err error) nativeprepdiag.Stage {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 2:
			return nativeprepdiag.QueryExitTwo
		case 3:
			return nativeprepdiag.QueryExitThree
		default:
			if exit.ExitCode() > 0 {
				return nativeprepdiag.QueryExitNonzero
			}
		}
	}
	return nativeprepdiag.QueryWaitFailure
}

type queryOutput struct {
	data     []byte
	exceeded bool
}

func (q *queryOutput) Write(data []byte) (int, error) {
	n := len(data)
	if len(q.data)+n > 16384 {
		q.exceeded = true
		return n, nil
	}
	q.data = append(q.data, data...)
	return n, nil
}

// This is a post-cancellation snapshot, not proof of the exact deadline instant
// or of what Cmd.Wait is waiting for. Observation never changes cleanup/result.
func queryRootSnapshot(ctx context.Context, root queryRootObservation) {
	state := queryRootUnknown
	if root.state != nil {
		state = root.state()
	}
	stage := nativeprepdiag.QueryRootUnknown
	switch state {
	case queryRootRunning:
		stage = nativeprepdiag.QueryRootRunning
	case queryRootExited:
		stage = nativeprepdiag.QueryRootExited
	}
	nativeprepdiag.Track(ctx, stage).Complete()
}
