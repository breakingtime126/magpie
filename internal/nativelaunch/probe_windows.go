package nativelaunch

import (
	"context"
	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeendpoint"
)

func connectProbeOwner(ctx context.Context, generation string) (probeLease, error) {
	peer, err := nativebootstrap.ResolvePackagedPeer()
	if err != nil {
		return nil, err
	}
	return nativebootstrap.ConnectStartupProbe(ctx, generation, peer)
}
func allocateProbeEndpoint() (probeEndpoint, error) {
	ep, err := nativeendpoint.Listen()
	if err != nil {
		return nil, err
	}
	return &ownedEndpoint{ep}, nil
}
func (e *ownedEndpoint) probe(ctx context.Context, cfg appserverbridge.Config, options appserverbridge.LaunchOptions, info appserverbridge.StartupClientInfo, reference nativechannel.Reference) ([]byte, error) {
	return appserverbridge.ProbeStartupWithNativeEndpoint(ctx, cfg, options, info, e.endpoint, reference)
}
