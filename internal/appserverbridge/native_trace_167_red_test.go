package appserverbridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Only APIs already present at 3c3d18fb877e8d376836459c498b1f99f501632d
// are used here. Copy this file alone to that base for an assertion-level red.
func TestNativeTrace167RequirementsAcceptedWithoutPropagation(t *testing.T) {
	traces := []string{
		"null", "{}", `{"traceparent":null}`, `{"tracestate":null}`,
		`{"traceparent":null,"tracestate":null}`,
		`{"traceparent":""}`, `{"tracestate":""}`,
		`{"traceparent":"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"}`,
		`{"traceparent":"PRIVATE_TRACE_PARENT","tracestate":"PRIVATE_TRACE_STATE"}`,
		`{"traceparent":null,"tracestate":"PRIVATE_TRACE_STATE"}`,
		`{"trace\u0070arent":"PRIVATE_TRACE_PARENT","tracestate":null}`,
	}
	for _, trace := range traces {
		for _, params := range []string{"", `,"params":null`, `,"params":{}`} {
			r := requirements167Router(t)
			request := `{"id":"trace","method":"configRequirements/read"` + params
			out := client(t, r, request+`,"trace":`+trace+"}")
			if len(out) != 2 || out[0].Backend == "" || out[1].Backend == "" {
				t.Fatal("bounded native trace must permit a fresh dual requirements read")
			}
			plain := client(t, requirements167Router(t), request+"}")
			for i, delivery := range out {
				if delivery.Backend != plain[i].Backend || !bytes.Equal(delivery.Message, plain[i].Message) {
					t.Fatal("trace changed child bytes or propagated between providers")
				}
				m, err := parseObject(delivery.Message)
				if err != nil || len(m) != 3 || string(m["params"]) != "null" ||
					bytes.Contains(delivery.Message, []byte("PRIVATE_TRACE")) {
					t.Fatal("native no-argument child request changed")
				}
			}
			source := `{ "requirements": null, "supportsIndependentSpeedModes": true }`
			if len(reply(t, r, out[1], requirements167Fixture)) != 0 {
				t.Fatal("trace allowed success before both policy authorities replied")
			}
			done := reply(t, r, out[0], source)
			expectRequirementsResult(t, done, raw("trace"), raw(json.RawMessage(requirements167Fixture)))
			m, _ := parseObject(done[0].Message)
			var compact bytes.Buffer
			if json.Compact(&compact, []byte(source)) != nil || !bytes.Equal(m["result"], compact.Bytes()) {
				t.Fatal("trace caused synthetic policy reconstruction")
			}
		}
	}
}

func TestNativeTrace167RequirementsRejectsUnsafeEnvelope(t *testing.T) {
	invalidTraces := []string{
		"false", "0", `"PRIVATE_TRACE"`, "[]", `[{}]`,
		`{"traceparent":false}`, `{"traceparent":1}`,
		`{"traceparent":[]}`, `{"tracestate":{}}`,
		`{"unknown":null}`, `{"Traceparent":null}`,
		`{"traceparent":null,"traceparent":"PRIVATE_TRACE"}`,
		`{"traceparent":"PRIVATE_TRACE","trace\u0070arent":null}`,
		`{"tracestate":null,"tracestate":null}`,
		`{"tracestate":"PRIVATE_TRACE","trace\u0073tate":null}`,
		`{"traceparent":"\ud800"}`, `{"tracestate":"\udfff"}`,
		`{"traceparent":"\ufffd"}`, `{"traceparent":"\u0000"}`,
		`{"tracestate":"PRIVATE_TRACE\n"}`,
		`{"traceparent":"` + strings.Repeat("a", 1025) + `"}`,
		`{"tracestate":"` + strings.Repeat("a", 1025) + `"}`,
		"{" + strings.Repeat(" ", (16<<10)-1) + "}",
	}
	prefix := `{"id":1,"method":"configRequirements/read","params":{}`
	var requests []string
	for _, trace := range invalidTraces {
		requests = append(requests, prefix+`,"trace":`+trace+"}")
	}
	requests = append(requests,
		prefix+`,"trace":null,"trace":{} }`,
		prefix+`,"trace":{},"tr\u0061ce":null}`,
		prefix+`,"trace":{},"private":"PRIVATE_TRACE"}`,
		prefix+`,"trace":{},"jsonrpc":"2.0"}`,
		prefix+`,"trace":{},"result":null}`,
		prefix+`,"trace":{},"error":null}`,
		`{"id":1,"method":"configRequirements/read","params":{"trace":null},"trace":{}}`,
		`{"id":1,"method":"configRequirements/read","params":false,"trace":{}}`,
		`{"id":1.5,"method":"configRequirements/read","trace":null}`,
		`{"id":9223372036854775808,"method":"configRequirements/read","trace":null}`,
	)
	for _, line := range requests {
		r := requirements167Router(t)
		out, err := r.Client([]byte(line))
		if err == nil {
			expectError(t, out)
		} else if len(out) != 0 {
			t.Fatal("invalid traced envelope produced a delivery with a fatal error")
		}
		if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
			t.Fatal("invalid traced envelope reached a child")
		}
		for _, delivery := range out {
			if delivery.Backend != "" || bytes.Contains(delivery.Message, []byte("PRIVATE_TRACE")) {
				t.Fatal("rejected trace escaped the local error")
			}
		}
	}
}

func TestNativeTrace167RequirementsKeepsPolicyAndMethodGates(t *testing.T) {
	for _, policy := range []string{
		`{"requirements":{},"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":false}`,
		string(raw(safeRequirementsFixture())),
	} {
		for _, rejectedBackend := range []int{0, 1} {
			r := requirements167Router(t)
			out := client(t, r, `{"id":1,"method":"configRequirements/read","trace":{}}`)
			if len(out) != 2 || out[0].Backend == "" {
				t.Fatal("traced requirements request never reached both authorities")
			}
			if len(reply(t, r, out[1-rejectedBackend], requirements167Fixture)) != 0 {
				t.Fatal("trace bypassed dual-result gate")
			}
			expectRequirementsError(t, reply(t, r, out[rejectedBackend], policy), raw(1))
		}
	}
	r := requirements167Router(t)
	out := client(t, r, `{"id":1,"method":"getAuthStatus","params":{"includeToken":false,"refreshToken":false},"trace":null}`)
	expectError(t, out)
	m, _ := parseObject(out[0].Message)
	e, _ := child(m, "error")
	if string(e["code"]) != "-32601" || len(r.pending) != 0 {
		t.Fatal("trace enabled production authentication")
	}
	r.configRequirementsRead = false
	expectError(t, client(t, r, `{"id":2,"method":"configRequirements/read","trace":null}`))
	if len(r.pending) != 0 {
		t.Fatal("trace enabled disabled requirements mode")
	}
}
