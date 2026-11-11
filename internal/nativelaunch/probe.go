package nativelaunch

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeprofile"
)

type probeLease interface {
	lease
	FinishProbe(context.Context) error
}
type probeEndpoint interface {
	probe(context.Context, appserverbridge.Config, appserverbridge.LaunchOptions, appserverbridge.StartupClientInfo, nativechannel.Reference) ([]byte, error)
	shutdown(context.Context) error
}
type probeDependencies struct {
	connect  func(context.Context, string) (probeLease, error)
	allocate func() (probeEndpoint, error)
}

// ProbeStartup uses the distinct one-shot authenticated operation. It never
// consumes the normal JSONL launch lease. A generation permits one inspection;
// repeated/overlapping or post-launch inspection is not a supported fallback.
func ProbeStartup(parent context.Context, profile io.Reader, options appserverbridge.LaunchOptions, info appserverbridge.StartupClientInfo) ([]byte, error) {
	return probeStartup(parent, profile, options, info, probeDependencies{connectProbeOwner, allocateProbeEndpoint})
}
func probeStartup(parent context.Context, profile io.Reader, options appserverbridge.LaunchOptions, info appserverbridge.StartupClientInfo, deps probeDependencies) (result []byte, resultErr error) {
	if parent == nil || parent.Err() != nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	data, err := nativeprofile.Read(profile)
	if err != nil {
		return nil, err
	}
	generation, err := nativeprofile.Locator(data)
	if err != nil {
		return nil, err
	}
	owner, err := deps.connect(ctx, generation)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if owner == nil {
		return nil, ErrUnavailable
	}
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		resultErr = errors.Join(resultErr, owner.Close(closeCtx))
		if resultErr != nil {
			result = nil
		}
	}()
	record := owner.Record()
	if ctx.Err() != nil || owner.Context().Err() != nil || record.Generation != generation || record.VerifyProfile(data) != nil {
		return nil, ErrUnavailable
	}
	cfg, err := nativeprofile.LoadConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.StartupProbe != appserverbridge.StartupProbeCompareUnrestricted167 || ctx.Err() != nil || owner.Context().Err() != nil {
		return nil, ErrUnavailable
	}
	ep, err := deps.allocate()
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, ErrUnavailable
	}
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		resultErr = errors.Join(resultErr, ep.shutdown(closeCtx))
		if resultErr != nil {
			result = nil
		}
	}()
	if ctx.Err() != nil || owner.Context().Err() != nil {
		return nil, ErrUnavailable
	}
	result, err = ep.probe(owner.Context(), cfg, options, info, record.Reference)
	if err != nil {
		return nil, err
	}
	// Endpoint has already drained native processes before source revalidation.
	// The authenticated owner, not profile content, confirms current generation
	// admission. No bytes are released on revoked, failed or uncertain completion.
	if ctx.Err() != nil || owner.Context().Err() != nil || record.VerifyProfile(data) != nil || cfg.Validate() != nil {
		return nil, ErrUnavailable
	}
	if err = owner.FinishProbe(ctx); err != nil {
		return nil, err
	}
	if ctx.Err() != nil || owner.Context().Err() != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}
