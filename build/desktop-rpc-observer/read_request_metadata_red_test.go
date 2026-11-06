package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// Copy this file alone to 4e75e65d04f36618fd05dcd858dd19ceb3bc0a4c.
// Both Red tests use existing observer APIs and fail by named assertions.
// No child, filesystem, credential or service is used.
func readMetadataRecords(t *testing.T, log []byte) []map[string]json.RawMessage {
	t.Helper()
	if bytes.Contains(log, []byte("PRIVATE_READ")) {
		t.Fatal("private read-request input entered metadata")
	}
	var records []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("read-request metadata was not valid JSON")
		}
		records = append(records, record)
	}
	return records
}

func readMetadataRecord(t *testing.T, direction, raw string) map[string]json.RawMessage {
	t.Helper()
	var log bytes.Buffer
	newObserver(&log).line(direction, []byte(raw))
	records := readMetadataRecords(t, log.Bytes())
	if len(records) != 1 {
		t.Fatal("read-request observation did not emit one record")
	}
	return records[0]
}

func assertReadMetadata(t *testing.T, record map[string]json.RawMessage, wanted map[string]bool) {
	t.Helper()
	base := map[string]bool{"seq": true, "pid": true, "kind": true, "direction": true,
		"method": true, "correlation": true, "class": true, "code": true}
	for key, value := range wanted {
		literal := "false"
		if value {
			literal = "true"
		}
		if string(record[key]) != literal {
			t.Fatal("read-request shape boolean missing or incorrect")
		}
	}
	for key := range record {
		if _, expected := wanted[key]; !expected && !base[key] {
			t.Fatal("read-request metadata escaped its exact privacy scope")
		}
	}
}

func TestCompleteRPCVocabularyMetadataRed(t *testing.T) {
	for _, method := range []string{"thread/resume", "gitDiffToRemote", "server/diagnostics"} {
		if allowedMethod(method) != method {
			t.Fatal("source-declared client method missing from complete vocabulary")
		}
	}
	record := readMetadataRecord(t, "to_bridge",
		`{"id":"PRIVATE_READ_ID","method":"\u0074hread/resume","params":{"PRIVATE_READ_KEY":"PRIVATE_READ_VALUE"}}`)
	if string(record["method"]) != `"thread/resume"` {
		t.Fatal("source-declared client method missing from complete vocabulary")
	}
	assertReadMetadata(t, record, nil)
	for _, method := range []string{"PRIVATE_READ_METHOD", "thread/resume/PRIVATE_READ", "Thread/resume", "thread/resume "} {
		raw, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": map[string]any{"PRIVATE_READ": true}})
		record := readMetadataRecord(t, "to_bridge", string(raw))
		if string(record["method"]) != `"unknown"` {
			t.Fatal("unknown method was exposed by complete vocabulary")
		}
		assertReadMetadata(t, record, nil)
	}
}

func TestReadRequestShapesMetadataRed(t *testing.T) {
	for _, method := range []string{"config/read", "experimentalFeature/list"} {
		record := readMetadataRecord(t, "to_bridge", `{"id":1,"method":"`+method+`","params":{},"trace":{}}`)
		wanted := map[string]bool{"tracePresent": true, "traceValid": true}
		if method == "config/read" {
			wanted["configParamsValid"] = true
			wanted["configIncludeLayers"] = false
			wanted["configCwdProvided"] = false
		} else {
			wanted["featureListParamsValid"] = true
			wanted["featureListCursorProvided"] = false
			wanted["featureListLimitProvided"] = false
			wanted["featureListThreadIdProvided"] = false
		}
		assertReadMetadata(t, record, wanted)
		if len(record) != 7+len(wanted) {
			t.Fatal("read-request shape has unexpected fields")
		}
	}
}

func TestConfigReadShapesAreBooleanOnly(t *testing.T) {
	for _, include := range []string{"", `"includeLayers":false`, `"includeLayers":true`} {
		for _, cwd := range []string{"", `"cwd":null`, `"cwd":""`,
			`"cwd":"C:\\PRIVATE_READ_PATH"`, `"cwd":"\ud83d\ude00"`,
			`"cwd":"\\ud800"`, `"cwd":"\ufffd"`} {
			var fields []string
			if include != "" {
				fields = append(fields, include)
			}
			if cwd != "" {
				fields = append(fields, cwd)
			}
			record := readMetadataRecord(t, "to_bridge",
				`{"id":1,"method":"config/read","params":{`+strings.Join(fields, ",")+"}}")
			assertReadMetadata(t, record, map[string]bool{
				"configParamsValid":   true,
				"configIncludeLayers": include == `"includeLayers":true`,
				"configCwdProvided":   cwd != "" && cwd != `"cwd":null`,
			})
		}
	}
	record := readMetadataRecord(t, "to_bridge",
		`{"id":1,"method":"config\u002fread","params":{"include\u004cayers":true,"\u0063wd":"PRIVATE_READ"},"tr\u0061ce":null}`)
	assertReadMetadata(t, record, map[string]bool{"configParamsValid": true, "configIncludeLayers": true,
		"configCwdProvided": true, "tracePresent": true, "traceValid": true})
}

func TestFeatureListShapesAreBooleanOnly(t *testing.T) {
	for _, cursor := range []string{"", `"cursor":null`, `"cursor":""`, `"cursor":"PRIVATE_READ_CURSOR"`} {
		for _, limit := range []string{"", `"limit":null`, `"limit":0`, `"limit":4294967295`} {
			for _, thread := range []string{"", `"threadId":null`, `"threadId":""`, `"threadId":"PRIVATE_READ_THREAD"`} {
				var fields []string
				for _, field := range []string{cursor, limit, thread} {
					if field != "" {
						fields = append(fields, field)
					}
				}
				record := readMetadataRecord(t, "to_bridge",
					`{"id":1,"method":"experimentalFeature/list","params":{`+strings.Join(fields, ",")+"}}")
				assertReadMetadata(t, record, map[string]bool{
					"featureListParamsValid":      true,
					"featureListCursorProvided":   cursor != "" && cursor != `"cursor":null`,
					"featureListLimitProvided":    limit != "" && limit != `"limit":null`,
					"featureListThreadIdProvided": thread != "" && thread != `"threadId":null`,
				})
			}
		}
	}
}

func TestReadRequestInvalidShapesNeverEmitPartialFlags(t *testing.T) {
	config := []string{
		"", `,"params":null`, `,"params":false`, `,"params":[]`,
		`,"params":{"includeLayers":null}`, `,"params":{"includeLayers":"false"}`,
		`,"params":{"includeLayers":0}`, `,"params":{"includeLayers":{}}`,
		`,"params":{"cwd":false}`, `,"params":{"cwd":["PRIVATE_READ"]}`,
		`,"params":{"cwd":{"PRIVATE_READ_KEY":"PRIVATE_READ_PATH"}}`,
		`,"params":{"cwd":"\ud800"}`, `,"params":{"cwd":"\udfff"}`,
		`,"params":{"cwd":"\ud800\u0041"}`, `,"params":{"cwd":"\ud800\ud800"}`,
		`,"params":{"IncludeLayers":false}`, `,"params":{"PRIVATE_READ_KEY":true}`,
		`,"params":{"includeLayers":true,"includeLayers":false}`,
		`,"params":{"includeLayers":true,"include\u004cayers":false}`,
		`,"params":{"cwd":null,"\u0063wd":"PRIVATE_READ"}`,
		`,"params":{"includeLayers":true,"cwd":0}`,
		`,"params":{"cwd":0,"includeLayers":true}`,
		`,"params":{"includeLayers":true,"PRIVATE_READ_KEY":false}`,
		`,"params":{"PRIVATE_READ_KEY":false,"includeLayers":true}`,
	}
	feature := []string{"", `,"params":null`, `,"params":[]`, `,"params":1`,
		`,"params":{"PRIVATE_READ_KEY":"PRIVATE_READ_VALUE"}`,
		`,"params":{"threadID":null}`,
		`,"params":{"cursor":"PRIVATE_READ","cursor":null}`,
		`,"params":{"cursor":null,"\u0063ursor":"PRIVATE_READ"}`,
		`,"params":{"limit":0,"li\u006dit":1}`,
		`,"params":{"threadId":null,"thread\u0049d":"PRIVATE_READ"}`,
	}
	for _, field := range []string{"cursor", "threadId"} {
		for _, value := range []string{"false", "0", "[]", `{"PRIVATE_READ_KEY":true}`,
			`"\ud800"`, `"\udfff"`} {
			feature = append(feature, `,"params":{"`+field+`":`+value+"}")
		}
	}
	for _, value := range []string{"false", `"0"`, "-1", "-0", "4294967296", "1.0", "1e0", "0.5", "[]", "{}"} {
		feature = append(feature, `,"params":{"cursor":"PRIVATE_READ","limit":`+value+"}",
			`,"params":{"limit":`+value+`,"cursor":"PRIVATE_READ"}`)
	}
	for method, suffixes := range map[string][]string{"config/read": config, "experimentalFeature/list": feature} {
		for _, suffix := range suffixes {
			for _, trace := range []string{"", `,"trace":{"traceparent":"PRIVATE_READ_PARENT"}`} {
				record := readMetadataRecord(t, "to_bridge", `{"id":1,"method":"`+method+`"`+suffix+trace+"}")
				field := "configParamsValid"
				if method == "experimentalFeature/list" {
					field = "featureListParamsValid"
				}
				wanted := map[string]bool{field: false}
				if trace != "" {
					wanted["tracePresent"], wanted["traceValid"] = true, true
				}
				assertReadMetadata(t, record, wanted)
			}
		}
	}
}

func TestReadRequestEnvelopeAndDirectionStayFailClosed(t *testing.T) {
	for _, method := range []string{"config/read", "experimentalFeature/list"} {
		valid := `{"id":1,"method":"` + method + `","params":{},"trace":null}`
		for _, direction := range []string{"from_bridge", "stderr"} {
			assertReadMetadata(t, readMetadataRecord(t, direction, valid), nil)
		}
		for _, raw := range []string{
			strings.Replace(valid, `"id":1,`, "", 1),
			strings.Replace(valid, `"id":1`, `"id":null`, 1),
			strings.Replace(valid, `"id":1`, `"id":1.5`, 1),
			strings.Replace(valid, `"id":1`, `"id":9223372036854775808`, 1),
			strings.Replace(valid, "{", `{"jsonrpc":"2.0",`, 1),
			strings.Replace(valid, `"params":{}`, `"params":{},"p\u0061rams":{}`, 1),
			strings.Replace(valid, `"trace":null`, `"trace":null,"tr\u0061ce":{}`, 1),
			strings.Replace(valid, `"trace":null`, `"trace":null,"result":"PRIVATE_READ"`, 1),
			strings.Replace(valid, `"trace":null`, `"trace":null,"error":{"message":"PRIVATE_READ"}`, 1),
			strings.Replace(valid, `"trace":null`, `"trace":null,"PRIVATE_READ":true`, 1),
			strings.Replace(valid, method, method+"/PRIVATE_READ", 1),
			strings.Replace(valid, `"id":1`, `"id":1,"\u0069d":2`, 1),
		} {
			assertReadMetadata(t, readMetadataRecord(t, "to_bridge", raw), nil)
		}
		for _, trace := range []string{"false", `{"traceparent":false}`,
			`{"traceparent":null,"trace\u0070arent":"PRIVATE_READ"}`,
			`{"PRIVATE_READ":true}`} {
			raw := strings.Replace(valid, `"trace":null`, `"trace":`+trace, 1)
			assertReadMetadata(t, readMetadataRecord(t, "to_bridge", raw),
				map[string]bool{"tracePresent": true, "traceValid": false})
		}
	}
}

func TestReadRequestCorrelationAndRepliesRemainOpaque(t *testing.T) {
	for _, method := range []string{"config/read", "experimentalFeature/list"} {
		for _, reply := range []string{
			`{"id":1,"result":{"PRIVATE_READ_KEY":"PRIVATE_READ_RESULT","includeLayers":true}}`,
			`{"id":1,"error":{"code":-32601,"message":"PRIVATE_READ_ERROR","data":{"cursor":"PRIVATE_READ"}}}`,
		} {
			var log bytes.Buffer
			o := newObserver(&log)
			raw := []byte(`{"id":1,"method":"`+method+`","params":{}}`)
			o.line("to_bridge", raw)
			o.line("from_bridge", []byte(reply))
			records := readMetadataRecords(t, log.Bytes())
			if len(records) != 2 || !bytes.Equal(records[0]["correlation"], records[1]["correlation"]) {
				t.Fatal("read-request observation changed response correlation")
			}
			assertReadMetadata(t, records[1], nil)
			o.line("to_bridge", raw)
			o.line("to_bridge", raw)
			o.line("to_bridge", raw)
			o.line("from_bridge", []byte(reply))
			records = readMetadataRecords(t, log.Bytes())
			for _, record := range records[3:] {
				assertReadMetadata(t, record, nil)
			}
			if string(records[3]["class"]) != `"duplicate_id"` ||
				string(records[4]["class"]) != `"duplicate_id"` || len(records[5]["correlation"]) != 0 {
				t.Fatal("read-request observation changed duplicate quarantine")
			}
		}
	}
	var log bytes.Buffer
	o := newObserver(&log)
	for i := 0; i <= maxPending; i++ {
		raw, _ := json.Marshal(map[string]any{"id": i, "method": "account/read"})
		o.line("to_bridge", raw)
	}
	log.Reset()
	o.line("to_bridge", []byte(`{"id":1,"method":"config/read","params":{}}`))
	o.line("to_bridge", []byte(`{"id":1,"method":"experimentalFeature/list","params":{}}`))
	for _, record := range readMetadataRecords(t, log.Bytes()) {
		assertReadMetadata(t, record, nil)
		if string(record["class"]) != `"correlation_limit"` {
			t.Fatal("read-request observation resumed lost correlation state")
		}
	}
}

type readMetadataChunkReader struct {
	raw  []byte
	step int
}

func (r *readMetadataChunkReader) Read(out []byte) (int, error) {
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

func TestReadRequestForwardingAndBoundsNeverChange(t *testing.T) {
	for _, method := range []string{"config/read", "experimentalFeature/list"} {
		field := "cwd"
		if method == "experimentalFeature/list" {
			field = "cursor"
		}
		prefix := `{"id":1,"method":"` + method + `","params":{"` + field + `":`
		valid := prefix + `"PRIVATE_READ"}}`
		deep := strings.Replace(prefix, `"id":1`, `"id":2`, 1) + strings.Repeat("[", 256) + `"PRIVATE_READ"` + strings.Repeat("]", 256) + "}}"
		tooDeep := strings.Replace(prefix, `"id":1`, `"id":3`, 1) + strings.Repeat("[", 10001) + `"PRIVATE_READ"` + strings.Repeat("]", 10001) + "}}"
		oversized := strings.Replace(prefix, `"id":1`, `"id":4`, 1) + `"` + strings.Repeat("PRIVATE_READ", maxLine) + `"}}`
		malformedUTF8 := strings.Replace(prefix, `"id":1`, `"id":5`, 1) + "\"" + string([]byte{0xff}) + "\"}}"
		payload := []byte(valid + "\r\n" + deep + "\n" + tooDeep + "\n" + oversized + "\n" + malformedUTF8 + "\nPRIVATE_READ_PARTIAL\x00\xff")
		for _, direction := range []string{"to_bridge", "from_bridge"} {
			for _, step := range []int{1, 7, 8192} {
				var forwarded, log bytes.Buffer
				o := newObserver(&log)
				p := &lineObserver{o: o, direction: direction}
				if pump(&forwarded, &readMetadataChunkReader{append([]byte(nil), payload...), step}, p) != nil ||
					!bytes.Equal(forwarded.Bytes(), payload) || len(p.buf) > maxLine || cap(p.buf) > 2*maxLine ||
					log.Len() > maxTrace {
					t.Fatal("read-request observation changed bytes or existing bounds")
				}
				records := readMetadataRecords(t, log.Bytes())
				if len(records) != 7 || string(records[2]["class"]) != `"unrecognized"` ||
					string(records[3]["class"]) != `"oversized"` ||
					string(records[5]["class"]) != `"unterminated"` {
					t.Fatal("read-request malformed stream classifications changed")
				}
				if direction == "to_bridge" {
					if method == "config/read" {
						assertReadMetadata(t, records[0], map[string]bool{"configParamsValid": true,
							"configIncludeLayers": false, "configCwdProvided": true})
						assertReadMetadata(t, records[1], map[string]bool{"configParamsValid": false})
					} else {
						assertReadMetadata(t, records[0], map[string]bool{"featureListParamsValid": true,
							"featureListCursorProvided": true, "featureListLimitProvided": false, "featureListThreadIdProvided": false})
						assertReadMetadata(t, records[1], map[string]bool{"featureListParamsValid": false})
					}
				} else {
					assertReadMetadata(t, records[0], nil)
					assertReadMetadata(t, records[1], nil)
				}
				for _, record := range records[2:] {
					assertReadMetadata(t, record, nil)
				}
			}
		}
	}
}
