package nativepreview

import (
	"encoding/json"
	"testing"
)

func TestOnlyDisabledRemoteControlSnapshotIsAccepted(t *testing.T) {
	valid := `{"status":"disabled","serverName":"PUBLIC-SERVER","installationId":"PUBLIC-INSTALLATION","environmentId":null}`
	for _, handler := range []bool{false, true} {
		peer := nativePeer{version: "codex-cli 0.167.0-alpha.3.2"}
		if handler {
			peer.notification = func(string, json.RawMessage) error {
				t.Fatal("remote-control snapshot escaped to operation handler")
				return ErrUnavailable
			}
		}
		check := func(params string) error {
			return peer.acceptNotification(map[string]json.RawMessage{"method": json.RawMessage(`"remoteControl/status/changed"`), "params": json.RawMessage(params), "emittedAtMs": json.RawMessage(`1234`)})
		}
		if check(valid) != nil {
			t.Fatal("disabled snapshot rejected")
		}
		for _, status := range []string{"connecting", "connected", "errored", "unknown"} {
			if check(`{"status":"`+status+`","serverName":"s","installationId":"i","environmentId":null}`) != ErrUnavailable {
				t.Fatal("non-disabled status accepted")
			}
		}
		for _, invalid := range []string{
			`null`, `{}`, `{"status":"disabled"}`,
			`{"status":"disabled","serverName":null,"installationId":"i","environmentId":null}`,
			`{"status":"disabled","serverName":"s","installationId":7,"environmentId":null}`,
			`{"status":"disabled","serverName":"s","installationId":"i","environmentId":"remote"}`,
			`{"status":"disabled","serverName":"s","installationId":"i","environmentId":null,"extra":false}`,
			`{"status":"disabled","status":"connected","serverName":"s","installationId":"i","environmentId":null}`,
		} {
			if check(invalid) != ErrUnavailable {
				t.Fatal("malformed remote-control snapshot accepted")
			}
		}
		peer.version = "codex-cli 0.166.0-alpha.6"
		if check(valid) != ErrUnavailable {
			t.Fatal("unreviewed version accepted")
		}
	}
}
