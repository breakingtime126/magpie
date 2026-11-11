//go:build !windows

package nativelaunch

import (
	"context"
	"errors"
)

func connectProbeOwner(context.Context, string) (probeLease, error) {
	return nil, errors.ErrUnsupported
}
func allocateProbeEndpoint() (probeEndpoint, error) { return nil, errors.ErrUnsupported }
