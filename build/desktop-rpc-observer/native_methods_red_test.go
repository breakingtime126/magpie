package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Exact method enums from the pinned native ClientRequest.json at
// c40729ea5cf8e947481b130c59807273eda2f8e9. This changes metadata labels only;
// production routes, parameters and authentication remain unsupported.
// Copy this file alone to 3c3d18fb877e8d376836459c498b1f99f501632d for a
// behavior-level red without requiring new production APIs.
func TestNativeStartupMethodsAreExactMetadataOnly(t *testing.T) {
	methods := []string{
		"modelProvider/capabilities/read", "experimentalFeature/list", "permissionProfile/list",
		"windowsSandbox/readiness", "mcpServerStatus/list", "skills/list", "hooks/list",
		"plugin/list", "plugin/installed", "app/list", "app/installed",
		"externalAgentConfig/detect", "account/workspaceMessages/read", "account/usage/read",
	}
	for _, method := range methods {
		if allowedMethod(method) != method {
			t.Fatal("reviewed native startup method lacks its exact metadata label")
		}
		for _, poison := range []string{strings.ToUpper(method), method + " ", method + "/PRIVATE_METHOD", "PRIVATE_METHOD/" + method, method + "\n"} {
			if allowedMethod(poison) != "unknown" {
				t.Fatal("native startup label used non-exact matching")
			}
		}
		for _, direction := range []string{"to_bridge", "from_bridge"} {
			var log bytes.Buffer
			o := newObserver(&log)
			line := `{"id":"PRIVATE_METHOD_ID","method":"` + method +
				`","params":{"PRIVATE_METHOD_PARAM":"PRIVATE_METHOD_VALUE"},"trace":{"traceparent":"PRIVATE_METHOD_TRACE"}}`
			o.line(direction, []byte(line))
			opposite := "to_bridge"
			if direction == opposite {
				opposite = "from_bridge"
			}
			o.line(opposite, []byte(`{"id":"PRIVATE_METHOD_ID","result":{"PRIVATE_METHOD_RESULT":true}}`))
			es := events(t, log.Bytes())
			if len(es) != 2 || es[0].Method != method || es[1].Method != method ||
				es[0].Correlation == 0 || es[0].Correlation != es[1].Correlation {
				t.Fatal("startup method observation changed directional correlation")
			}
			for _, encoded := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
				var record map[string]json.RawMessage
				if json.Unmarshal(encoded, &record) != nil || len(record) != 7 {
					t.Fatal("startup method metadata gained payload fields")
				}
				for field := range record {
					switch field {
					case "seq", "pid", "kind", "direction", "method", "correlation", "class":
					default:
						t.Fatal("startup method metadata exposed values or auth/trace flags")
					}
				}
			}
			if bytes.Contains(log.Bytes(), []byte("PRIVATE_METHOD")) {
				t.Fatal("startup method contents leaked")
			}
		}
	}
}
