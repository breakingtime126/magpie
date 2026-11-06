package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// This file uses only observer APIs present at bcc80e7c. Copying it alone to
// that base must fail TestGetAuthStatusRequestedFlagsObserved behaviorally,
// not by failing to compile. It never starts a child or contacts a service.
func authRequestRecords(t *testing.T, log []byte) []map[string]json.RawMessage {
	t.Helper()
	if bytes.Contains(log, []byte("PRIVATE_AUTH")) {
		t.Fatal("authentication input entered metadata")
	}
	var records []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("authentication metadata was not JSON")
		}
		records = append(records, record)
	}
	return records
}

func assertAuthRequestFlags(t *testing.T, record map[string]json.RawMessage, valid, include, refresh *bool) {
	t.Helper()
	want := map[string]*bool{
		"authParamsValid": valid, "includeTokenRequested": include, "refreshTokenRequested": refresh,
	}
	for key, expected := range want {
		raw, present := record[key]
		if expected == nil {
			if present {
				t.Fatal("authentication flags escaped their valid request scope")
			}
		} else {
			literal := "false"
			if *expected {
				literal = "true"
			}
			if !present || string(raw) != literal {
				t.Fatal("getAuthStatus requested flag metadata missing or incorrect")
			}
		}
	}
	for key := range record {
		switch key {
		case "seq", "pid", "kind", "direction", "method", "correlation", "class", "code",
			"authParamsValid", "includeTokenRequested", "refreshTokenRequested":
		default:
			t.Fatal("authentication record exposed an unexpected field")
		}
	}
}

func TestGetAuthStatusRequestedFlagsObserved(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name             string
		params           string
		include, refresh bool
	}{
		{"absent fields", "{}", false, false},
		{"include only", `{"includeToken":true}`, true, false},
		{"refresh only", `{"refreshToken":true}`, false, true},
		{"both true", `{"includeToken":true,"refreshToken":true}`, true, true},
		{"both false", `{"includeToken":false,"refreshToken":false}`, false, false},
		{"include null", `{"includeToken":null,"refreshToken":true}`, false, true},
		{"refresh null", `{"includeToken":true,"refreshToken":null}`, true, false},
		{"both null", `{"includeToken":null,"refreshToken":null}`, false, false},
		{"escaped exact keys", `{"\u0069ncludeToken":true,"refresh\u0054oken":false}`, true, false},
		{"whitespace", " { \"includeToken\" : false , \"refreshToken\" : true } ", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log bytes.Buffer
			o := newObserver(&log)
			o.line("to_bridge", []byte(`{"jsonrpc":"2.0","id":"PRIVATE_AUTH_ID","method":"getAuthStatus","params":`+tt.params+"}"))
			records := authRequestRecords(t, log.Bytes())
			if len(records) != 1 || len(records[0]) != 10 {
				t.Fatal("getAuthStatus requested flag metadata missing or incorrect")
			}
			assertAuthRequestFlags(t, records[0], &yes, &tt.include, &tt.refresh)
			if string(records[0]["class"]) != `"request"` ||
				string(records[0]["method"]) != `"getAuthStatus"` {
				t.Fatal("authentication request metadata changed routing labels")
			}
		})
	}

	invalid := []struct{ name, suffix string }{
		{"missing params", ""},
		{"null params", `,"params":null`},
		{"array params", `,"params":[]`},
		{"string params", `,"params":"PRIVATE_AUTH_PARAMS"`},
		{"boolean params", `,"params":false`},
		{"number params", `,"params":0`},
		{"unknown key", `,"params":{"PRIVATE_AUTH_KEY":false}`},
		{"spoof metadata", `,"params":{"authParamsValid":true,"includeTokenRequested":false}`},
		{"wrong case", `,"params":{"IncludeToken":false}`},
		{"nested flags", `,"params":{"PRIVATE_AUTH_KEY":{"includeToken":false,"refreshToken":false}}`},
		{"include string", `,"params":{"includeToken":"false","refreshToken":false}`},
		{"include token", `,"params":{"includeToken":"PRIVATE_AUTH_TOKEN","refreshToken":true}`},
		{"refresh string", `,"params":{"includeToken":false,"refreshToken":"PRIVATE_AUTH_REFRESH"}`},
		{"include number", `,"params":{"includeToken":0,"refreshToken":false}`},
		{"refresh number", `,"params":{"includeToken":true,"refreshToken":1}`},
		{"include array", `,"params":{"includeToken":["PRIVATE_AUTH_ARRAY"]}`},
		{"refresh object", `,"params":{"refreshToken":{"PRIVATE_AUTH_KEY":"PRIVATE_AUTH_PATH"}}`},
		{"include then invalid refresh", `,"params":{"includeToken":true,"refreshToken":{"PRIVATE_AUTH_KEY":false}}`},
		{"invalid refresh then include", `,"params":{"refreshToken":{"PRIVATE_AUTH_KEY":false},"includeToken":true}`},
		{"refresh then invalid include", `,"params":{"refreshToken":true,"includeToken":["PRIVATE_AUTH_VALUE"]}`},
		{"invalid include then refresh", `,"params":{"includeToken":["PRIVATE_AUTH_VALUE"],"refreshToken":true}`},
		{"include then unknown", `,"params":{"includeToken":true,"PRIVATE_AUTH_KEY":false}`},
		{"unknown then include", `,"params":{"PRIVATE_AUTH_KEY":false,"includeToken":true}`},
		{"include then duplicate refresh", `,"params":{"includeToken":true,"refreshToken":true,"refreshToken":false}`},
		{"duplicate refresh then include", `,"params":{"refreshToken":true,"refreshToken":false,"includeToken":true}`},
		{"include duplicate", `,"params":{"includeToken":true,"includeToken":false}`},
		{"refresh duplicate", `,"params":{"refreshToken":true,"refreshToken":false}`},
		{"include escaped duplicate", `,"params":{"includeToken":true,"\u0069ncludeToken":false}`},
		{"refresh escaped duplicate", `,"params":{"refreshToken":false,"refresh\u0054oken":true}`},
		{"unknown nested duplicate", `,"params":{"PRIVATE_AUTH_KEY":{"token":"PRIVATE_AUTH_A","\u0074oken":"PRIVATE_AUTH_B"}}`},
		{"lone surrogate key", `,"params":{"\ud800":false}`},
		{"replacement alias key", `,"params":{"\ud800":true,"\ufffd":false}`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			var log bytes.Buffer
			newObserver(&log).line("to_bridge", []byte(`{"id":"PRIVATE_AUTH_ID","method":"getAuthStatus"`+tt.suffix+"}"))
			records := authRequestRecords(t, log.Bytes())
			if len(records) != 1 || len(records[0]) != 8 {
				t.Fatal("invalid authentication params did not produce validity alone")
			}
			assertAuthRequestFlags(t, records[0], &no, nil, nil)
		})
	}
}

func TestGetAuthStatusFlagsStayWithinExactClientRequest(t *testing.T) {
	valid := `{"id":"PRIVATE_AUTH_ID","method":"getAuthStatus","params":{"includeToken":true,"refreshToken":false}}`
	tests := []struct{ name, direction, raw string }{
		{"backend request", "from_bridge", valid},
		{"other direction", "stderr", valid},
		{"notification", "to_bridge", `{"method":"getAuthStatus","params":{"includeToken":true}}`},
		{"null id", "to_bridge", `{"id":null,"method":"getAuthStatus","params":{"includeToken":true}}`},
		{"invalid id", "to_bridge", `{"id":{},"method":"getAuthStatus","params":{"includeToken":true}}`},
		{"other method", "to_bridge", strings.Replace(valid, "getAuthStatus", "account/read", 1)},
		{"case alias", "to_bridge", strings.Replace(valid, "getAuthStatus", "GetAuthStatus", 1)},
		{"suffix alias", "to_bridge", strings.Replace(valid, "getAuthStatus", "getAuthStatus/PRIVATE_AUTH_METHOD", 1)},
		{"result envelope", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{},"result":{"PRIVATE_AUTH_KEY":true}}`},
		{"error envelope", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{},"error":{"message":"PRIVATE_AUTH_ERROR"}}`},
		{"extra envelope field", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{},"PRIVATE_AUTH_KEY":true}`},
		{"wrong jsonrpc", "to_bridge", `{"jsonrpc":null,"id":1,"method":"getAuthStatus","params":{}}`},
		{"duplicate method first", "to_bridge", `{"id":1,"method":"getAuthStatus","method":"account/read","params":{}}`},
		{"duplicate method last", "to_bridge", `{"id":1,"method":"account/read","method":"getAuthStatus","params":{}}`},
		{"escaped duplicate method", "to_bridge", `{"id":1,"method":"account/read","\u006dethod":"getAuthStatus","params":{}}`},
		{"duplicate params", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{"includeToken":true},"params":{}}`},
		{"escaped duplicate params", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{"refreshToken":true},"p\u0061rams":{}}`},
		{"duplicate id", "to_bridge", `{"id":1,"\u0069d":2,"method":"getAuthStatus","params":{}}`},
		{"duplicate version", "to_bridge", `{"jsonrpc":"2.0","jsonrpc":"2.0","id":1,"method":"getAuthStatus","params":{}}`},
		{"malformed key", "to_bridge", "{\"id\":1,\"method\":\"getAuthStatus\",\"params\":{\"" + string([]byte{0xff}) + "\":false}}"},
		{"malformed JSON", "to_bridge", `{"id":1,"method":"getAuthStatus","params":{"includeToken":false,"PRIVATE_AUTH_KEY":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log bytes.Buffer
			newObserver(&log).line(tt.direction, []byte(tt.raw))
			records := authRequestRecords(t, log.Bytes())
			if len(records) != 1 {
				t.Fatal("authentication observation lost its bounded record")
			}
			assertAuthRequestFlags(t, records[0], nil, nil, nil)
		})
	}
	for _, direction := range []string{"to_bridge", "from_bridge"} {
		for _, reply := range []string{
			`{"id":"PRIVATE_AUTH_ID","result":{"includeToken":true,"refreshToken":"PRIVATE_AUTH_RESPONSE","authParamsValid":true}}`,
			`{"id":"PRIVATE_AUTH_ID","error":{"code":-32601,"message":"PRIVATE_AUTH_ERROR","data":{"includeToken":true,"refreshToken":"PRIVATE_AUTH_TOKEN"}}}`,
		} {
			var log bytes.Buffer
			o := newObserver(&log)
			other := "from_bridge"
			if direction == other {
				other = "to_bridge"
			}
			o.line(direction, []byte(valid))
			o.line(other, []byte(reply))
			records := authRequestRecords(t, log.Bytes())
			if len(records) != 2 || !bytes.Equal(records[0]["correlation"], records[1]["correlation"]) {
				t.Fatal("authentication observation changed correlation")
			}
			assertAuthRequestFlags(t, records[1], nil, nil, nil)
		}
	}
}

func TestGetAuthStatusDuplicateAndLimitedCorrelationsStayOpaque(t *testing.T) {
	yes, no := true, false
	var log bytes.Buffer
	o := newObserver(&log)
	raw := []byte(`{"id":1,"method":"getAuthStatus","params":{}}`)
	o.line("to_bridge", raw)
	o.line("to_bridge", raw)
	o.line("to_bridge", raw)
	o.line("from_bridge", []byte(`{"id":1,"result":{"PRIVATE_AUTH_KEY":true}}`))
	records := authRequestRecords(t, log.Bytes())
	if len(records) != 4 {
		t.Fatal("duplicate request observations missing")
	}
	assertAuthRequestFlags(t, records[0], &yes, &no, &no)
	for _, record := range records[1:] {
		assertAuthRequestFlags(t, record, nil, nil, nil)
	}
	if string(records[1]["class"]) != `"duplicate_id"` ||
		string(records[2]["class"]) != `"duplicate_id"` ||
		string(records[3]["class"]) != `"unmatched_response"` {
		t.Fatal("authentication observation changed duplicate quarantine")
	}

	log.Reset()
	o = newObserver(&log)
	// Keep the existing capacity mechanism real while discarding its unrelated
	// earlier records so only the attempted post-cap diagnostic is asserted.
	for i := 0; i <= maxPending; i++ {
		request, _ := json.Marshal(map[string]any{"id": i, "method": "account/read"})
		o.line("to_bridge", request)
	}
	log.Reset()
	o.line("to_bridge", raw)
	records = authRequestRecords(t, log.Bytes())
	if len(records) != 1 || string(records[0]["class"]) != `"correlation_limit"` {
		t.Fatal("authentication observation changed correlation capacity")
	}
	assertAuthRequestFlags(t, records[0], nil, nil, nil)
}

type authRequestChunkReader struct {
	raw  []byte
	step int
}

func (r *authRequestChunkReader) Read(out []byte) (int, error) {
	if len(r.raw) == 0 {
		return 0, io.EOF
	}
	n := r.step
	if n > len(out) {
		n = len(out)
	}
	if n > len(r.raw) {
		n = len(r.raw)
	}
	copy(out, r.raw[:n])
	r.raw = r.raw[n:]
	return n, nil
}

func TestGetAuthStatusObservationPreservesEveryWireByteAndCap(t *testing.T) {
	valid := []byte(" \t" + `{"id":"PRIVATE_AUTH_ID","method":"getAuthStatus","params":{"includeToken":true,"refreshToken":null}}` + "\r\n")
	invalid := []byte(`{"id":2,"method":"getAuthStatus","params":{"refreshToken":{"PRIVATE_AUTH_PATH":"C:\\private\\auth"}}}` + "\n")
	oversized := []byte(`{"id":3,"method":"getAuthStatus","params":{"includeToken":false,"PRIVATE_AUTH_KEY":"` + strings.Repeat("PRIVATE_AUTH", maxLine/4) + `"}}` + "\n")
	payload := append(append(append(append([]byte(nil), valid...), invalid...), oversized...), []byte("PRIVATE_AUTH_PARTIAL\x00\xff")...)
	for _, direction := range []string{"to_bridge", "from_bridge"} {
		for _, step := range []int{1, 7, 8192} {
			var forwarded, log bytes.Buffer
			o := newObserver(&log)
			p := &lineObserver{o: o, direction: direction}
			reader := &authRequestChunkReader{raw: append([]byte(nil), payload...), step: step}
			if pump(&forwarded, reader, p) != nil || !bytes.Equal(forwarded.Bytes(), payload) {
				t.Fatal("authentication observation changed wire bytes")
			}
			if len(p.buf) > maxLine || cap(p.buf) > maxLine*2 || log.Len() > maxTrace {
				t.Fatal("authentication observation exceeded existing bounds")
			}
			records := authRequestRecords(t, log.Bytes())
			if len(records) != 5 || string(records[2]["class"]) != `"oversized"` ||
				string(records[3]["class"]) != `"unterminated"` {
				t.Fatal("authentication stream classifications missing")
			}
			yes, no := true, false
			if direction == "to_bridge" {
				assertAuthRequestFlags(t, records[0], &yes, &yes, &no)
				assertAuthRequestFlags(t, records[1], &no, nil, nil)
			} else {
				assertAuthRequestFlags(t, records[0], nil, nil, nil)
				assertAuthRequestFlags(t, records[1], nil, nil, nil)
			}
			for _, record := range records[2:] {
				assertAuthRequestFlags(t, record, nil, nil, nil)
			}
		}
	}
}

func TestGetAuthStatusDeepParamsFailClosedWithoutChangingWire(t *testing.T) {
	for _, depth := range []int{256, 10001} {
		// The second fixture exceeds encoding/json's 10,000-level guard while
		// remaining below maxLine. Neither parsing failure nor nested data may
		// become a false requested flag or alter the forwarded bytes.
		raw := []byte(`{"id":1,"method":"getAuthStatus","params":{"includeToken":true,"refreshToken":` +
			strings.Repeat("[", depth) + `"PRIVATE_AUTH_NESTED"` + strings.Repeat("]", depth) + "}}\n")
		if len(raw) >= maxLine {
			t.Fatal("deep authentication fixture exceeded the observation bound")
		}
		var forwarded, log bytes.Buffer
		o := newObserver(&log)
		if pump(&forwarded, bytes.NewReader(raw), &lineObserver{o: o, direction: "to_bridge"}) != nil ||
			!bytes.Equal(forwarded.Bytes(), raw) {
			t.Fatal("nested authentication input changed on the wire")
		}
		records := authRequestRecords(t, log.Bytes())
		if len(records) != 2 {
			t.Fatal("nested authentication observation lost stream metadata")
		}
		if depth == 256 {
			no := false
			assertAuthRequestFlags(t, records[0], &no, nil, nil)
		} else {
			assertAuthRequestFlags(t, records[0], nil, nil, nil)
			if string(records[0]["class"]) != `"unrecognized"` {
				t.Fatal("excessively nested authentication input was not rejected")
			}
		}
	}
}
