//go:build !windows

package nativehost

import "github.com/yetone/magpie/internal/nativebootstrap"

func listenOwner(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) {
	return nil, ErrUnavailable
}
