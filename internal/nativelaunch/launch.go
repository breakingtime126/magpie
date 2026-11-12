// Package nativelaunch composes the explicit staged bootstrap/profile/runner
// path. No CLI switch, environment variable or GUI Prepare enables it.
package nativelaunch

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativehandshake"
	"github.com/yetone/magpie/internal/nativeprofile"
)

var ErrUnavailable = errors.New("trusted native launch unavailable")

// Run consumes a bounded profile snapshot and authenticates its exact bytes
// before Config validation, endpoint allocation or process startup. This is an
// explicit staged API, never an auto-detected replacement for ordinary Run.
// Missing/unknown/malformed envelopes and failed ownership have no fallback.
// Stream ownership remains with the caller until entry into the native runner;
// pre-entry failure never reads or closes caller streams. The runner owns them
// after entry and reports incomplete cleanup rather than claiming full drain.
func Run(parent context.Context, profile io.Reader, options appserverbridge.LaunchOptions, input io.ReadCloser, output io.WriteCloser) error {
	return run(parent, profile, options, input, output, dependencies{connect: connectOwner, allocate: allocateEndpoint})
}

type lease interface {
	Context() context.Context
	Record() nativebootstrap.Record
	Close(context.Context) error
}
type endpoint interface {
	// run transfers ownership immediately, including on failure.
	run(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, nativechannel.Reference, io.ReadCloser, io.WriteCloser) error
	shutdown(context.Context) error
}
type dependencies struct {
	connect  func(context.Context, string) (lease, error)
	allocate func() (endpoint, error)
}

func run(parent context.Context, profile io.Reader, options appserverbridge.LaunchOptions, input io.ReadCloser, output io.WriteCloser, deps dependencies) (result error) {
	if input == nil || output == nil || parent.Err() != nil {
		return ErrUnavailable
	}
	data, err := nativeprofile.Read(profile)
	if err != nil {
		return err
	}
	generation, err := nativeprofile.Locator(data)
	if err != nil {
		return err
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	owner, err := deps.connect(parent, generation)
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if owner == nil {
		return ErrUnavailable
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result = errors.Join(result, owner.Close(ctx))
	}()
	if owner.Context().Err() != nil || parent.Err() != nil {
		return ErrUnavailable
	}
	record := owner.Record()
	if record.Generation != generation || record.VerifyProfile(data) != nil {
		return ErrUnavailable
	}
	nativehandshake.Emit(parent, nativehandshake.ProfileAuthenticated)
	// This is the first filesystem-aware Config operation. It uses the exact
	// previously hashed bytes, not a reopened profile path.
	cfg, err := nativeprofile.LoadConfig(data)
	if err != nil {
		return err
	}
	if owner.Context().Err() != nil || parent.Err() != nil {
		return ErrUnavailable
	}
	ep, err := deps.allocate()
	if err != nil {
		return err
	}
	if ep == nil {
		return ErrUnavailable
	}
	nativehandshake.Emit(parent, nativehandshake.EndpointAllocated)
	if owner.Context().Err() != nil || parent.Err() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return errors.Join(ErrUnavailable, ep.shutdown(ctx))
	}
	// Production Connect roots this context in parent and retains the peer handle.
	// The runner independently watches cancellation while starting/draining children.
	return ep.run(owner.Context(), cfg, options, record.Reference, input, output)
}
