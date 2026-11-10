package nativelaunch

import (
	"context"
	"io"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeendpoint"
)

func connectOwner(ctx context.Context, generation string) (lease, error) {
	peer, err := nativebootstrap.ResolvePackagedPeer()
	if err != nil {
		return nil, err
	}
	return nativebootstrap.Connect(ctx, generation, peer)
}

type ownedEndpoint struct{ endpoint *nativeendpoint.Endpoint }

func allocateEndpoint() (endpoint, error) {
	e, err := nativeendpoint.Listen()
	if err != nil {
		return nil, err
	}
	return &ownedEndpoint{e}, nil
}
func (e *ownedEndpoint) shutdown(ctx context.Context) error { return e.endpoint.Shutdown(ctx) }
func (e *ownedEndpoint) run(ctx context.Context, cfg appserverbridge.Config, options appserverbridge.LaunchOptions, reference nativechannel.Reference, in io.ReadCloser, out io.WriteCloser) error {
	return appserverbridge.RunWithNativeEndpoint(ctx, cfg, options, e.endpoint, reference, in, out)
}
