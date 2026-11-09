package nativepreview

import (
	"bytes"
	"encoding/json"
)

// c407 reports current remote-control status after initialize, even when disabled.
// Accept only the known disabled snapshot, not permission to enable/control a host.
// Identity strings are validated and discarded, never exposed or persisted here.
func (p *nativePeer) acceptDisabledRemoteControl(params json.RawMessage) error {
	if p.version != "codex-cli 0.167.0-alpha.3.2" || !strictJSON(params) {
		return ErrUnavailable
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil || len(fields) != 4 {
		return ErrUnavailable
	}
	for _, key := range []string{"status", "serverName", "installationId", "environmentId"} {
		if fields[key] == nil {
			return ErrUnavailable
		}
	}
	var status, serverName, installationID *string
	if json.Unmarshal(fields["status"], &status) != nil || status == nil || *status != "disabled" ||
		json.Unmarshal(fields["serverName"], &serverName) != nil || serverName == nil ||
		json.Unmarshal(fields["installationId"], &installationID) != nil || installationID == nil ||
		!bytes.Equal(bytes.TrimSpace(fields["environmentId"]), []byte("null")) {
		return ErrUnavailable
	}
	return nil
}
