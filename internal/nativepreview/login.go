package nativepreview

import (
	"context"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

func login(parent context.Context, spec peerSpec) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	if !safeStateParent(spec.home) || !safeStateParent(spec.codexHome) {
		return ErrUnavailable
	}
	digest, err := executableDigest(spec.command)
	if err != nil || digest != spec.digest || ctx.Err() != nil {
		return ErrUnavailable
	}
	if !nativeVersion(ctx, spec) || !nativeLoginAllowed() {
		return ErrUnavailable
	}
	return runLoginRoot(ctx, spec)
}

// Production calls this only after version and inherited-job admission. Tests
// may use an inert copied test executable to exercise root-only lifecycle.
func runLoginRoot(ctx context.Context, spec peerSpec) error {
	// The official CLI owns browser opening and its OAuth callback. Never read,
	// parse, display or log its stdout/stderr; never add account/token arguments.
	command := proc.Command(spec.command, "login")
	command.Env, command.Dir = nativeEnvironment(spec), spec.home
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	if err := command.Start(); err != nil {
		return ErrUnavailable
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return ErrUnavailable
		}
		return nil
	case <-ctx.Done():
		// Login may launch a user browser. Only cancel our exact root process,
		// never its process tree or a browser selected by name/PID.
		_ = command.Process.Kill()
		<-done
		return ErrUnavailable
	}
}
