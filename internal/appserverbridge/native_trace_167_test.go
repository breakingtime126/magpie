package appserverbridge

import (
	"strings"
	"testing"
)

func TestNativeTrace167SchemaAndLocalBounds(t *testing.T) {
	for _, field := range []string{"traceparent", "tracestate"} {
		for _, length := range []int{0, nativeTrace167StringLimit - 1, nativeTrace167StringLimit, nativeTrace167StringLimit + 1} {
			data := `{"` + field + `":"` + strings.Repeat("a", length) + `"}`
			if NativeTrace167Valid([]byte(data)) != (length <= nativeTrace167StringLimit) {
				t.Fatal("decoded trace string boundary changed")
			}
			escaped := `{"` + field + `":"` + strings.Repeat(`\u0061`, length) + `"}`
			if NativeTrace167Valid([]byte(escaped)) != (length <= nativeTrace167StringLimit) {
				t.Fatal("escaped trace string was bounded by encoded rather than decoded bytes")
			}
		}
		// The decoded budget counts UTF-8 bytes, not Go runes.
		for _, count := range []int{nativeTrace167StringLimit / 2, nativeTrace167StringLimit/2 + 1} {
			data := `{"` + field + `":"` + strings.Repeat("é", count) + `"}`
			if NativeTrace167Valid([]byte(data)) != (2*count <= nativeTrace167StringLimit) {
				t.Fatal("multibyte trace exceeded the decoded byte budget")
			}
		}
	}
	for _, length := range []int{nativeTrace167JSONLimit - 1, nativeTrace167JSONLimit, nativeTrace167JSONLimit + 1} {
		data := "{" + strings.Repeat(" ", length-2) + "}"
		if NativeTrace167Valid([]byte(data)) != (length <= nativeTrace167JSONLimit) {
			t.Fatal("encoded trace boundary changed")
		}
	}
	for _, data := range []string{"", "null {}", `{"traceparent":"` + string([]byte{0xff}) + `"}`} {
		if NativeTrace167Valid([]byte(data)) {
			t.Fatal("malformed native trace accepted")
		}
	}
	// A schema-valid string need not be a usable W3C header: native telemetry
	// ignores invalid contexts and this bridge never propagates the carrier.
	if !NativeTrace167Valid([]byte(`{"traceparent":"not-a-w3c-parent","tracestate":""}`)) {
		t.Fatal("trace shape check incorrectly became a telemetry interpreter")
	}
}

func TestNativeStartupMetadataLabelsDoNotEnableProductionRoutes(t *testing.T) {
	r := requirements167Router(t)
	for _, method := range []string{
		"modelProvider/capabilities/read", "experimentalFeature/list", "permissionProfile/list",
		"windowsSandbox/readiness", "mcpServerStatus/list", "skills/list", "hooks/list",
		"plugin/list", "plugin/installed", "app/list", "app/installed",
		"externalAgentConfig/detect", "account/workspaceMessages/read", "account/usage/read",
	} {
		out := client(t, r, `{"id":1,"method":"`+method+`","params":{},"trace":null}`)
		expectError(t, out)
		m, _ := parseObject(out[0].Message)
		e, _ := child(m, "error")
		if string(e["code"]) != "-32601" || len(r.pending) != 0 {
			t.Fatal("diagnostic method label enabled a production RPC")
		}
	}
}
