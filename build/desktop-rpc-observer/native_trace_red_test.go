package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Uses only observer APIs present at 3c3d18fb877e8d376836459c498b1f99f501632d.
// Copy this file alone to that base: the new metadata fails by assertion.
func nativeTraceRecord(t *testing.T, direction, line string) map[string]json.RawMessage {
	t.Helper()
	var log bytes.Buffer
	newObserver(&log).line(direction, []byte(line))
	if bytes.Contains(log.Bytes(), []byte("PRIVATE_TRACE")) {
		t.Fatal("native trace contents escaped into diagnostics")
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(log.Bytes()), &record) != nil {
		t.Fatal("expected one bounded native trace observation")
	}
	return record
}

func assertNativeTraceRecord(t *testing.T, record map[string]json.RawMessage, validity *bool, auth ...bool) {
	t.Helper()
	keys := map[string]bool{
		"seq": true, "pid": true, "kind": true, "direction": true,
		"method": true, "correlation": true, "class": true,
	}
	if validity != nil {
		keys["tracePresent"], keys["traceValid"] = true, true
		want := "false"
		if *validity {
			want = "true"
		}
		if string(record["tracePresent"]) != "true" || string(record["traceValid"]) != want {
			t.Fatal("native trace presence/validity metadata missing or incorrect")
		}
	}
	if len(auth) > 0 {
		keys["authParamsValid"] = true
		want := "false"
		if auth[0] {
			want = "true"
		}
		if string(record["authParamsValid"]) != want {
			t.Fatal("traced authentication params validity missing or incorrect")
		}
		if auth[0] {
			for i, key := range []string{"includeTokenRequested", "refreshTokenRequested"} {
				keys[key] = true
				want := "false"
				if auth[i+1] {
					want = "true"
				}
				if string(record[key]) != want {
					t.Fatal("traced authentication flag missing or incorrect")
				}
			}
		}
	}
	for key := range record {
		if !keys[key] {
			t.Fatal("native trace observation published an unexpected field")
		}
	}
	if len(record) != len(keys) {
		t.Fatal("native trace observation has an unexpected exact shape")
	}
}

func TestNativeTraceMetadataRecognizesSchemaWithoutValues(t *testing.T) {
	yes := true
	for _, trace := range []string{
		"null", "{}", `{"traceparent":null}`, `{"tracestate":null}`,
		`{"traceparent":null,"tracestate":null}`,
		`{"traceparent":""}`, `{"tracestate":""}`,
		`{"traceparent":"PRIVATE_TRACE_PARENT","tracestate":"PRIVATE_TRACE_STATE"}`,
		`{"traceparent":null,"tracestate":"PRIVATE_TRACE_STATE"}`,
		`{"trace\u0070arent":"PRIVATE_TRACE_PARENT","tracestate":null}`,
	} {
		for _, method := range []string{"getAuthStatus", "configRequirements/read"} {
			line := `{"id":"PRIVATE_TRACE_ID","method":"` + method + `","params":{},"trace":` + trace + "}"
			record := nativeTraceRecord(t, "to_bridge", line)
			if method == "getAuthStatus" {
				assertNativeTraceRecord(t, record, &yes, true, false, false)
			} else {
				assertNativeTraceRecord(t, record, &yes)
			}
		}
	}
	for _, include := range []bool{false, true} {
		for _, refresh := range []bool{false, true} {
			params, _ := json.Marshal(map[string]bool{"includeToken": include, "refreshToken": refresh})
			request := `{"id":"PRIVATE_TRACE_ID","method":"getAuthStatus","params":` + string(params) +
				`,"trace":{"traceparent":"PRIVATE_TRACE_PARENT"}}`
			assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", request), &yes, true, include, refresh)
		}
	}
	for _, params := range []string{"", `,"params":null`, `,"params":false`, `,"params":{"includeToken":"PRIVATE_TRACE_TOKEN"}`,
		`,"params":{"includeToken":true,"\u0069ncludeToken":false}`} {
		line := `{"id":1,"method":"getAuthStatus","trace":{}` + params + "}"
		assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), &yes, false)
	}
	for _, params := range []string{`{}`, `{"includeToken":null,"refreshToken":null}`,
		`{"\u0069ncludeToken":false,"refresh\u0054oken":null}`} {
		line := `{"jsonrpc":"2.0","id":1,"method":"getAuthStatus","params":` + params + `,"tr\u0061ce":null}`
		assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), &yes, true, false, false)
	}
}

func TestNativeTraceMetadataRejectsInvalidCarriersAndAliases(t *testing.T) {
	no := false
	for _, trace := range []string{
		"false", "0", `"PRIVATE_TRACE"`, "[]",
		`{"unknown":"PRIVATE_TRACE"}`, `{"Traceparent":null}`,
		`{"traceparent":false}`, `{"tracestate":1}`,
		`{"traceparent":[]}`, `{"tracestate":{}}`,
		`{"traceparent":null,"traceparent":"PRIVATE_TRACE"}`,
		`{"tracestate":"PRIVATE_TRACE","trace\u0073tate":null}`,
		`{"traceparent":"\ud800"}`, `{"tracestate":"\udfff"}`,
		`{"traceparent":"\ufffd"}`, `{"tracestate":"\u0000"}`,
		`{"traceparent":"PRIVATE_TRACE\n"}`,
		`{"traceparent":"` + strings.Repeat("a", 1025) + `"}`,
		`{"tracestate":"` + strings.Repeat("a", 1025) + `"}`,
		"{" + strings.Repeat(" ", (16<<10)-1) + "}",
	} {
		for _, method := range []string{"getAuthStatus", "configRequirements/read"} {
			line := `{"id":1,"method":"` + method + `","params":{"includeToken":true},"trace":` + trace + "}"
			assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), &no)
		}
	}
	for _, suffix := range []string{
		`,"trace":null,"trace":{}`, `,"trace":null,"tr\u0061ce":{}`,
		`,"trace":null,"Trace":{}`, `,"trace":null,"PRIVATE_TRACE":null`,
		`,"trace":null,"result":null`, `,"trace":null,"error":null`,
		`,"trace":null,"params":null`, `,"trace":null,"method":"getAuthStatus"`,
	} {
		line := `{"id":1,"method":"getAuthStatus","params":{}` + suffix + "}"
		assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), nil)
	}
	// The observer's general correlation stays unchanged; native flag/trace
	// recognition additionally requires a bounded string or native int64 ID.
	for _, id := range []string{"1.5", "1e2", "9223372036854775808", `"\ud800"`, `"` + strings.Repeat("a", 257) + `"`} {
		line := `{"id":` + id + `,"method":"getAuthStatus","params":{},"trace":null}`
		assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), nil)
	}
}

func TestNativeTraceMetadataKeepsDirectionCorrelationAndByteCaps(t *testing.T) {
	valid := `{"id":1,"method":"getAuthStatus","params":{},"trace":{"traceparent":"PRIVATE_TRACE_PARENT"}}`
	for _, direction := range []string{"from_bridge", "stderr"} {
		assertNativeTraceRecord(t, nativeTraceRecord(t, direction, valid), nil)
	}
	for _, method := range []string{"account/read", "GetAuthStatus", "getAuthStatus/PRIVATE_TRACE"} {
		line := strings.Replace(valid, "getAuthStatus", method, 1)
		assertNativeTraceRecord(t, nativeTraceRecord(t, "to_bridge", line), nil)
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(valid))
	o.line("to_bridge", []byte(valid))
	o.line("to_bridge", []byte(valid))
	o.line("from_bridge", []byte(`{"id":1,"result":{"trace":"PRIVATE_TRACE_RESULT"}}`))
	for i, line := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
		if i == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("invalid duplicate trace metadata")
		}
		for _, field := range []string{"tracePresent", "traceValid", "authParamsValid", "includeTokenRequested", "refreshTokenRequested"} {
			if _, exists := record[field]; exists {
				t.Fatal("trace or auth metadata escaped correlation scope")
			}
		}
	}
	payload := []byte(" \t" + valid + "\r\n" +
		`{"id":2,"method":"getAuthStatus","params":{},"trace":{"traceparent":"` +
		strings.Repeat("PRIVATE_TRACE", maxLine/4) + `"}}` + "\nPRIVATE_TRACE_PARTIAL\x00\xff")
	for _, direction := range []string{"to_bridge", "from_bridge"} {
		for _, step := range []int{1, 7, 8192} {
			var forwarded, metadata bytes.Buffer
			observed := newObserver(&metadata)
			p := &lineObserver{o: observed, direction: direction}
			if pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), step}, p) != nil ||
				!bytes.Equal(forwarded.Bytes(), payload) {
				t.Fatal("native trace observation changed forwarding bytes")
			}
			if metadata.Len() > maxTrace || bytes.Contains(metadata.Bytes(), []byte("PRIVATE_TRACE")) ||
				len(p.buf) > maxLine || cap(p.buf) > 2*maxLine {
				t.Fatal("native trace observation leaked data or exceeded old caps")
			}
		}
	}
}
