package nativelaunch

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativehandshake"
)

func TestHandshakeLaunchActualAuthenticationBoundaries(t *testing.T) {
	for _, mode := range []string{"complete", "connect", "profile", "allocate"} {
		t.Run(mode, func(t *testing.T) {
			_, p, record := launchFixture(t)
			events := map[nativehandshake.Stage]int{}
			ctx := nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) { events[s]++ })
			l := &fakeLease{ctx: ctx, record: record}
			ep := &fakeEndpoint{}
			if mode == "profile" {
				l.record.ProfileSHA256 = "invalid"
			}
			deps := dependencies{connect: func(context.Context, string) (lease, error) {
				if mode == "connect" {
					return nil, errors.New("PRIVATE_OWNER_ERROR")
				}
				return l, nil
			}, allocate: func() (endpoint, error) {
				if mode == "allocate" {
					return nil, errors.New("PRIVATE_ENDPOINT_ERROR")
				}
				return ep, nil
			}}
			err := run(ctx, bytes.NewReader(p.Bytes()), appserverbridge.LaunchOptions{}, unopenedStream{t}, unopenedStream{t}, deps)
			if (err == nil) != (mode == "complete") {
				t.Fatal("observation changed launch result", mode)
			}
			if (events[nativehandshake.ProfileAuthenticated] > 0) != (mode == "complete" || mode == "allocate") {
				t.Fatal("unverified profile acknowledged")
			}
			if (events[nativehandshake.EndpointAllocated] > 0) != (mode == "complete") {
				t.Fatal("unallocated endpoint acknowledged")
			}
			if events[nativehandshake.InitializeOutputWritten] != 0 {
				t.Fatal("launch manufactured handshake")
			}
		})
	}
}
