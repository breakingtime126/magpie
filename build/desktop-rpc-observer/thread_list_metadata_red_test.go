package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Portable onto 09b526eb: only existing observer/pump APIs and decoded JSON
// are used. TestThreadListFilterShapesObserved must fail behaviorally there,
// never by requiring new production fields or helpers. No processes or network.
var threadListShapeTestFields = []string{
	"threadListProjectIdShape", "threadListParentIdShape",
	"threadListAncestorIdShape", "threadListAttachedIdShape",
}

func threadListTestRecords(t *testing.T, log []byte) []map[string]json.RawMessage {
	t.Helper()
	if bytes.Contains(log, []byte("PRIVATE_CANARY")) {
		t.Fatal("private thread-list input entered metadata")
	}
	var records []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("invalid thread-list metadata JSON")
		}
		records = append(records, record)
	}
	return records
}

func assertThreadListTestShapes(t *testing.T, record map[string]json.RawMessage, want [4]string) {
	t.Helper()
	for i, field := range threadListShapeTestFields {
		raw, present := record[field]
		if want[i] == "" {
			if present {
				t.Fatal("thread/list shapes escaped eligible request scope")
			}
			continue
		}
		var value string
		if !present || json.Unmarshal(raw, &value) != nil || value != want[i] {
			t.Fatal("thread/list filter shape metadata missing or incorrect")
		}
	}
}

func TestThreadListFilterShapesObserved(t *testing.T) {
	for index, field := range []string{"projectId", "parentThreadId", "ancestorThreadId", "attachedToThreadId"} {
		for _, fixture := range []struct{ value, shape string }{
			{"", "absent"}, {"null", "null"}, {`"PRIVATE_CANARY"`, "string"},
			{`""`, "string"}, {`"\ud83d\ude00"`, "string"}, {`"\ud800"`, "invalid"},
			{"true", "invalid"}, {"1", "invalid"}, {"[]", "invalid"}, {"{}", "invalid"},
		} {
			params := "{}"
			if fixture.value != "" {
				params = fmt.Sprintf(`{%q:%s}`, field, fixture.value)
			}
			var log bytes.Buffer
			o := newObserver(&log)
			o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":`+params+`}`))
			want := [4]string{"absent", "absent", "absent", "absent"}
			want[index] = fixture.shape
			records := threadListTestRecords(t, log.Bytes())
			if len(records) != 1 {
				t.Fatal("missing single thread/list request record")
			}
			assertThreadListTestShapes(t, records[0], want)
		}
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{"project\u0049d":null,"parentThreadId":"PRIVATE_CANARY","ancestorThreadId":false,"attachedToThreadId":null}}`))
	assertThreadListTestShapes(t, threadListTestRecords(t, log.Bytes())[0], [4]string{"null", "string", "invalid", "null"})
}

func TestThreadListMalformedParamsAndBounds(t *testing.T) {
	for _, params := range []string{"", "null", "true", "[]", `{"projectId":null,"projectId":"PRIVATE_CANARY"}`,
		`{"projectId":null,"project\u0049d":"PRIVATE_CANARY"}`, `{"PRIVATE_CANARY":1,"PRIVATE_CANARY":2}`} {
		var log bytes.Buffer
		o := newObserver(&log)
		suffix := ""
		if params != "" {
			suffix = `,"params":` + params
		}
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list"`+suffix+`}`))
		assertThreadListTestShapes(t, threadListTestRecords(t, log.Bytes())[0], [4]string{"invalid", "invalid", "invalid", "invalid"})
	}
	for _, count := range []int{18, 19} {
		params := []string{`"projectId":null`}
		for i := 1; i < count; i++ {
			params = append(params, fmt.Sprintf(`"PRIVATE_CANARY_%d":{"opaque":"PRIVATE_CANARY"}`, i))
		}
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{`+strings.Join(params, ",")+`}}`))
		want := [4]string{"null", "absent", "absent", "absent"}
		if count == 19 {
			want = [4]string{"invalid", "invalid", "invalid", "invalid"}
		}
		assertThreadListTestShapes(t, threadListTestRecords(t, log.Bytes())[0], want)
	}
}

func TestThreadListShapesStayWithinRequestScope(t *testing.T) {
	for _, fixture := range []struct{ direction, raw string }{
		{"to_bridge", `{"id":1,"id":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"\u0069d":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"PRIVATE_CANARY":null}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"trace":false}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{"projectId":"` + string([]byte{0xff}) + `"}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":`},
		{"to_bridge", `{"method":"thread/list","params":{}}`},
		{"from_bridge", `{"id":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"project/list","params":{}}`},
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.line(fixture.direction, []byte(fixture.raw))
		assertThreadListTestShapes(t, threadListTestRecords(t, log.Bytes())[0], [4]string{})
	}
}

func TestThreadListRejectionClassesObserved(t *testing.T) {
	for _, fixture := range []struct{ message, class string }{
		{"unsupported thread/list field: projectId", "thread_list_project_id"},
		{"unsupported thread/list field: parentThreadId", "thread_list_parent_id"},
		{"unsupported thread/list field: ancestorThreadId", "thread_list_ancestor_id"},
		{"unsupported thread/list field: attachedToThreadId", "thread_list_attached_id"},
		{"unsupported thread sort key", "thread_list_sort_key"},
		{"unsupported thread sort direction", "thread_list_sort_direction"},
		{"unsupported logical provider filter", "thread_list_provider_filter"},
		{"invalid bridge cursor", "thread_list_cursor"},
	} {
		for _, variant := range []string{fixture.message, "PRIVATE_CANARY " + fixture.message,
			fixture.message + ": PRIVATE_CANARY", fixture.message + "\n", strings.ToUpper(fixture.message)} {
			var log bytes.Buffer
			o := newObserver(&log)
			o.line("to_bridge", []byte(`{"id":"PRIVATE_CANARY","method":"thread/list","params":{"projectId":null}}`))
			message, _ := json.Marshal(variant)
			o.line("from_bridge", []byte(`{"id":"PRIVATE_CANARY","error":{"code":-32602,"message":`+string(message)+`,"data":{"PRIVATE_CANARY":"PRIVATE_CANARY"}}}`))
			records := threadListTestRecords(t, log.Bytes())
			want := "unknown"
			if variant == fixture.message {
				want = fixture.class
			}
			if len(records) != 2 || string(records[1]["class"]) != `"`+want+`"` ||
				string(records[0]["correlation"]) != string(records[1]["correlation"]) {
				t.Fatal("thread/list exact rejection class missing or unsafe")
			}
			assertThreadListTestShapes(t, records[1], [4]string{})
		}
	}
}

func TestThreadListRejectionAmbiguityStaysUnknown(t *testing.T) {
	message := `"unsupported thread/list field: projectId"`
	for _, failure := range []string{
		`{"code":-32000,"message":` + message + `}`,
		`{"code":-32602.0,"message":` + message + `}`,
		`{"code":-32602,"code":-32602,"message":` + message + `}`,
		`{"code":-32602,"co\u0064e":-32602,"message":` + message + `}`,
		`{"code":-32602,"message":"PRIVATE_CANARY","message":` + message + `}`,
		`{"code":-32602,"message":` + message + `,"PRIVATE_CANARY":true}`,
		`{"code":-32602,"message":"unsupported thread/list field: PRIVATE_CANARY"}`,
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{}}`))
		o.line("from_bridge", []byte(`{"id":1,"error":`+failure+`}`))
		if string(threadListTestRecords(t, log.Bytes())[1]["class"]) != `"unknown"` {
			t.Fatal("ambiguous thread/list failure was classified")
		}
	}
	for _, suffix := range []string{`,"id":1`, `,"\u0069d":1`, `,"trace":false`, `,"PRIVATE_CANARY":null`,
		`,"error":{"code":-32602,"message":` + message + `}`} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{}}`))
		o.line("from_bridge", []byte(`{"id":1,"error":{"code":-32602,"message":`+message+`}`+suffix+`}`))
		if string(threadListTestRecords(t, log.Bytes())[1]["class"]) != `"unknown"` {
			t.Fatal("ambiguous error envelope was classified")
		}
	}
	for _, fixture := range []struct{ method, direction, id string }{
		{"project/list", "from_bridge", "1"}, {"thread/list", "to_bridge", "1"}, {"thread/list", "from_bridge", "2"},
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(`{"id":1,"method":"`+fixture.method+`","params":{}}`))
		o.line(fixture.direction, []byte(`{"id":`+fixture.id+`,"error":{"code":-32602,"message":`+message+`}}`))
		if string(threadListTestRecords(t, log.Bytes())[1]["class"]) != `"unknown"` {
			t.Fatal("thread/list rejection escaped method/direction/correlation scope")
		}
	}
}

func TestThreadListMetadataNeverChangesWire(t *testing.T) {
	payload := []byte("{\"id\":1,\"method\":\"thread/list\",\"params\":{\"projectId\":null,\"parentThreadId\":\"PRIVATE_CANARY\"}}\r\n")
	payload = append(payload, []byte(`{"id":2,"method":"thread/list","params":{"projectId":"`+strings.Repeat("PRIVATE_CANARY", maxLine)+`"}}`+"\n")...)
	for _, step := range []int{1, 7, 8192} {
		var log, forwarded bytes.Buffer
		o := newObserver(&log)
		p := &lineObserver{o: o, direction: "to_bridge"}
		if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), step}, p); err != nil {
			t.Fatal("forwarding failed")
		}
		if !bytes.Equal(payload, forwarded.Bytes()) || log.Len() > maxTrace || len(p.buf) > maxLine {
			t.Fatal("thread/list wire or observation bounds changed")
		}
		records := threadListTestRecords(t, log.Bytes())
		assertThreadListTestShapes(t, records[0], [4]string{"null", "string", "absent", "absent"})
		if string(records[1]["class"]) != `"oversized"` {
			t.Fatal("oversized request did not retain its fixed label")
		}
		assertThreadListTestShapes(t, records[1], [4]string{})
	}
}
