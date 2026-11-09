package nativepreview

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicNotificationClassificationDoesNotGrantSupport(t *testing.T) {
	seen := make(map[string]bool)
	stage := ""
	peer := nativePeer{stage: func(label string) { stage = label }}
	for _, method := range publicNotificationMethods {
		label := notificationDiagnosticLabel(method)
		if label != "notification-"+strings.ReplaceAll(method, "/", "-") || seen[label] || !previewDiagnosticStageAllowed(label) {
			t.Fatal("public notification diagnostic table mismatch")
		}
		seen[label] = true
		encoded, _ := json.Marshal(method)
		if peer.acceptNotification(map[string]json.RawMessage{"method": encoded, "params": json.RawMessage(`{"unused":"PUBLIC-BODY-CANARY"}`), "emittedAtMs": json.RawMessage(`1234`)}) != ErrUnavailable || stage != label {
			t.Fatal("diagnostic classification changed nil-handler rejection")
		}
	}
	if len(seen) != 88 {
		t.Fatal("fixed public declaration set changed")
	}
	for _, method := range []string{"PUBLIC-UNKNOWN-METHOD", "thread/started/PUBLIC-UNKNOWN", "account/login/completed-extra"} {
		if notificationDiagnosticLabel(method) != "notification-unsupported" {
			t.Fatal("unknown method escaped fixed diagnostic label")
		}
	}
}
