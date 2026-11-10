//go:build !windows

package nativelaunch

import (
	"context"
	"errors"
)

func connectOwner(context.Context, string) (lease, error) { return nil, errors.ErrUnsupported }
func allocateEndpoint() (endpoint, error)                 { return nil, errors.ErrUnsupported }
