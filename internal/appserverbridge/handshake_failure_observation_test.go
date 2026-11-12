package appserverbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/nativehandshake"
)

func TestProtocolFailureFixedClasses(t *testing.T) {
	cases := []struct {
		err  error
		want nativehandshake.Stage
	}{
		{errAuthCompatibility, nativehandshake.ProtocolClassEnvelope},
		{errors.New("unknown or wrong-backend response id"), nativehandshake.ProtocolClassCorrelation},
		{errors.New("unscoped backend request is unsupported"), nativehandshake.ProtocolClassUnscopedRequest},
		{errors.New("unsolicited unknown thread"), nativehandshake.ProtocolClassThreadOwnership},
		{errors.New(controlPlaneInitUnavailable167), nativehandshake.ProtocolClassInitialization},
		{errors.New("unexpected client notification"), nativehandshake.ProtocolClassClientNotification},
		{errors.New("PRIVATE_PAYLOAD_TOKEN_PATH"), nativehandshake.ProtocolClassOther},
	}
	for _, c := range cases {
		if protocolFailureClass(c.err) != c.want {
			t.Fatal("wrong fixed failure class")
		}
	}
}

func TestProtocolFailureObservesRealRejectionWithoutPayload(t *testing.T) {
	r := sharedNativeRouterFixture167(t)
	_, err := r.Server(r.backends[1].ID, []byte(`{"id":"PRIVATE_ID","method":"PRIVATE_METHOD","params":{"secret":"PRIVATE_VALUE"}}`))
	if err == nil {
		t.Fatal("unscoped server request unexpectedly admitted")
	}
	var stages []nativehandshake.Stage
	o := newHandshakeObservation(nativehandshake.WithObserver(context.Background(), func(s nativehandshake.Stage) { stages = append(stages, s) }))
	o.routed(r, r.phase, r.backends[1].ID, []byte(`{"id":"PRIVATE_ID","method":"PRIVATE_METHOD","params":{}}`), false, err)
	want := []nativehandshake.Stage{nativehandshake.ProtocolSourceSecondary, nativehandshake.ProtocolShapeRequest, nativehandshake.ProtocolPendingNone, nativehandshake.ProtocolClassUnscopedRequest, nativehandshake.ProtocolFailed}
	if len(stages) != len(want) {
		t.Fatal("missing fixed rejection evidence")
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatal("rejection diagnostic changed")
		}
	}
	if r.phase != "cold" || len(r.pending) != 0 {
		t.Fatal("observation changed routing authority")
	}
}

func TestProtocolPendingFixedAdmissionStages(t *testing.T) {
	cases := []struct {
		g    request
		want nativehandshake.Stage
	}{
		{request{method: "initialize"}, nativehandshake.ProtocolPendingInitialize},
		{request{method: "model/list", sharedNativeAdmission167: true, sharedNativePolicy167: true}, nativehandshake.ProtocolPendingPolicy},
		{request{method: "model/list", sharedNativeAdmission167: true}, nativehandshake.ProtocolPendingConfig},
		{request{method: "model/list"}, nativehandshake.ProtocolPendingCatalog},
		{request{method: "config/read"}, nativehandshake.ProtocolPendingConfig},
		{request{method: "PRIVATE_METHOD"}, nativehandshake.ProtocolPendingOther},
	}
	for _, c := range cases {
		if protocolPendingStage(&c.g) != c.want {
			t.Fatal("wrong fixed pending stage")
		}
	}
	for _, err := range []error{errors.New(ownedCatalogUnavailable), errors.New(ownedSettingsUnavailable167)} {
		if protocolFailureClass(err) != nativehandshake.ProtocolClassCatalog {
			t.Fatal("catalog sentinel lost")
		}
	}
	if protocolFailureClass(errors.New(sharedRouteUnavailable167)) != nativehandshake.ProtocolClassAdmission {
		t.Fatal("admission sentinel lost")
	}
}
