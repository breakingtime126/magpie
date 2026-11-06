package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGetAuthStatusMetadataKeepsAuthenticationOpaque(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(`{"id":"PRIVATE_AUTH_ID","method":"getAuthStatus","params":{"token":"PRIVATE_AUTH_TOKEN","capabilities":{"requestAttestation":true},"account":{"email":"PRIVATE_AUTH_EMAIL"}}}`))
	o.line("from_bridge", []byte(`{"id":"PRIVATE_AUTH_ID","result":{"accessToken":"PRIVATE_AUTH_ACCESS","refreshToken":"PRIVATE_AUTH_REFRESH","authenticated":true,"accountId":"PRIVATE_AUTH_ACCOUNT"}}`))
	es := events(t, log.Bytes())
	if len(es) != 2 || es[0].Method != "getAuthStatus" || es[1].Method != "getAuthStatus" ||
		es[0].Class != "request" || es[1].Class != "response" ||
		es[0].Correlation == 0 || es[0].Correlation != es[1].Correlation {
		t.Fatal("exact authentication method metadata was not correlated")
	}
	for _, line := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			t.Fatal(err)
		}
		for field := range fields {
			switch field {
			case "seq", "pid", "kind", "direction", "method", "correlation", "class":
			default:
				t.Fatal("authentication request or response exposed extra metadata")
			}
		}
	}
	if strings.Contains(log.String(), "PRIVATE_AUTH") {
		t.Fatal("authentication material leaked")
	}
	for _, neighbor := range []string{"getauthstatus", "GetAuthStatus", "getAuthStatus/PRIVATE", "getAuthStatus ", "getAuthStatus\n"} {
		if allowedMethod(neighbor) != "unknown" {
			t.Fatal("authentication method allowlist is not exact")
		}
	}
}
