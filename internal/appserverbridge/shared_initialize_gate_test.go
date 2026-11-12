package appserverbridge

import (
	"testing"
)

func TestSharedSerialGateRequiresRouterValidatedPrimary(t *testing.T) {
	for _, kind := range []string{"good", "error", "bad-home", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			r := sharedNativeRouterFixture167(t)
			cfg := Config{Backends: r.backends, SharedNativeRoute: r.sharedNativeRoute167}
			gate := newSharedInitializeGate(cfg)
			out := client(t, r, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"},"capabilities":{"experimentalApi":true}}}`)
			if len(out) != 2 {
				t.Fatal("missing router fanout")
			}
			if held, err := gate.hold(out[1]); err != nil || !held {
				t.Fatal("secondary delivery not retained")
			}
			request, _ := parseObject(out[0].Message)
			response := raw(object{"id": request["id"], "result": raw(object{"userAgent": raw("synthetic"), "codexHome": raw(r.controlNativeHomes167[r.backends[0].ID]), "platformFamily": raw("test"), "platformOs": raw("test")})})
			if kind == "error" {
				response = raw(object{"id": request["id"], "error": raw(object{"code": raw(-32000), "message": raw("synthetic")})})
			}
			if kind == "bad-home" {
				response = raw(object{"id": request["id"], "result": raw(object{"userAgent": raw("synthetic"), "codexHome": raw("wrong"), "platformFamily": raw("test"), "platformOs": raw("test")})})
			}
			if kind == "malformed" {
				response = []byte("not-json")
			}
			group := gate.pendingPrimary(r, r.backends[0].ID, response)
			_, routeErr := r.Server(r.backends[0].ID, response)
			var released bool
			var held []Delivery
			if routeErr == nil {
				held, released = gate.validated(group)
			}
			if released != (kind == "good") {
				t.Fatal("gate did not follow actual router validation")
			}
			if released && (len(held) != 1 || string(held[0].Message) != string(out[1].Message)) {
				t.Fatal("buffered initialize changed")
			}
		})
	}
}

func TestSharedSerialGateBoundsNotificationsAndDefaultMode(t *testing.T) {
	if newSharedInitializeGate(Config{}) != nil {
		t.Fatal("ordinary mode gained serialized startup")
	}
	gate := &sharedInitializeGate{secondary: "s"}
	notification := Delivery{Backend: "s", Message: []byte(`{"method":"initialized"}`)}
	for i := 0; i < 128; i++ {
		if held, err := gate.hold(notification); !held || err != nil {
			t.Fatal("bounded notification rejected")
		}
	}
	if _, err := gate.hold(notification); err == nil {
		t.Fatal("unbounded notification queue")
	}
	gate = &sharedInitializeGate{secondary: "s", bytes: 32 << 20}
	if _, err := gate.hold(notification); err == nil {
		t.Fatal("byte limit ignored")
	}
}
