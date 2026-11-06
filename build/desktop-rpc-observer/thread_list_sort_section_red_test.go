package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Portable onto 02600624: this file uses only preexisting observer APIs and
// decoded JSON maps. The observed-metadata assertion must fail there without
// a compile failure. No child, filesystem, network or authentication access.
func threadListSortSectionRecords(t *testing.T, log []byte) []map[string]json.RawMessage {
	t.Helper()
	if bytes.Contains(log, []byte("PRIVATE_CANARY")) {
		t.Fatal("thread/list sort or section contents entered metadata")
	}
	var records []map[string]json.RawMessage
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("invalid thread/list sort metadata JSON")
		}
		records = append(records, record)
	}
	return records
}

func assertThreadListSortSection(t *testing.T, record map[string]json.RawMessage, sort, section string) {
	t.Helper()
	for field, want := range map[string]string{"threadListSortKey": sort, "threadListSectionIdShape": section} {
		raw, present := record[field]
		if want == "" {
			if present {
				t.Fatal("sort and section metadata escaped its request scope")
			}
			continue
		}
		var actual string
		if !present || json.Unmarshal(raw, &actual) != nil || actual != want {
			t.Fatal("thread/list sort and section metadata missing or incorrect")
		}
	}
}

func TestThreadListSortSectionObserved(t *testing.T) {
	for _, sort := range []struct{ raw, label string }{
		{"", "absent"}, {"null", "null"},
		{`"created_at"`, "created_at"}, {`"updated_at"`, "updated_at"},
		{`"recency_at"`, "recency_at"}, {`"section_position"`, "section_position"},
		{`"section\u005fposition"`, "section_position"},
		{`"PRIVATE_CANARY"`, "other-string"}, {`""`, "other-string"},
		{`"SECTION_POSITION"`, "other-string"}, {`"section_position PRIVATE_CANARY"`, "other-string"},
		{`"PRIVATE_CANARY created_at"`, "other-string"}, {`"created_at\n"`, "other-string"},
		{`"\ud83d\ude00"`, "other-string"}, {`"\ud800"`, "invalid"},
		{"true", "invalid"}, {"1", "invalid"}, {"[]", "invalid"}, {"{}", "invalid"},
	} {
		for _, section := range []struct{ raw, label string }{
			{"", "absent"}, {"null", "null"}, {`"PRIVATE_CANARY"`, "string"},
			{`""`, "string"}, {`"\ud83d\ude00"`, "string"}, {`"\ud800"`, "invalid"},
			{"true", "invalid"}, {"1", "invalid"}, {"[]", "invalid"}, {"{}", "invalid"},
		} {
			var fields []string
			if sort.raw != "" {
				fields = append(fields, `"sortKey":`+sort.raw)
			}
			if section.raw != "" {
				fields = append(fields, `"sectionId":`+section.raw)
			}
			var log bytes.Buffer
			o := newObserver(&log)
			o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{`+strings.Join(fields, ",")+`}}`))
			records := threadListSortSectionRecords(t, log.Bytes())
			if len(records) != 1 {
				t.Fatal("missing thread/list sort request record")
			}
			assertThreadListSortSection(t, records[0], sort.label, section.label)
		}
	}
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{"sort\u004bey":"recency_at","section\u0049d":null}}`))
	assertThreadListSortSection(t, threadListSortSectionRecords(t, log.Bytes())[0], "recency_at", "null")
}

func TestThreadListSortSectionMalformedAndBounded(t *testing.T) {
	for _, params := range []string{"", "null", "[]", "true",
		`{"sortKey":"created_at","sortKey":"updated_at"}`,
		`{"sortKey":"created_at","sort\u004bey":"PRIVATE_CANARY"}`,
		`{"sectionId":null,"sectionId":"PRIVATE_CANARY"}`,
		`{"sectionId":null,"section\u0049d":"PRIVATE_CANARY"}`} {
		var log bytes.Buffer
		o := newObserver(&log)
		suffix := ""
		if params != "" {
			suffix = `,"params":` + params
		}
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list"`+suffix+`}`))
		record := threadListSortSectionRecords(t, log.Bytes())[0]
		assertThreadListSortSection(t, record, "invalid", "invalid")
		for _, field := range []string{"threadListProjectIdShape", "threadListParentIdShape", "threadListAncestorIdShape", "threadListAttachedIdShape"} {
			if string(record[field]) != `"invalid"` {
				t.Fatal("malformed params produced inconsistent field observations")
			}
		}
	}
	for _, count := range []int{18, 19} {
		fields := []string{`"sortKey":"section_position"`, `"sectionId":null`}
		for i := 2; i < count; i++ {
			fields = append(fields, fmt.Sprintf(`"PRIVATE_CANARY_%d":{"value":"PRIVATE_CANARY"}`, i))
		}
		var log bytes.Buffer
		o := newObserver(&log)
		o.line("to_bridge", []byte(`{"id":1,"method":"thread/list","params":{`+strings.Join(fields, ",")+`}}`))
		sort, section := "section_position", "null"
		if count == 19 {
			sort, section = "invalid", "invalid"
		}
		assertThreadListSortSection(t, threadListSortSectionRecords(t, log.Bytes())[0], sort, section)
	}
}

func TestThreadListSortSectionScope(t *testing.T) {
	for _, fixture := range []struct{ direction, raw string }{
		{"to_bridge", `{"id":1,"id":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"\u0069d":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"trace":false}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{"sortKey":"` + string([]byte{0xff}) + `"}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{"sectionId":"` + string([]byte{0xff}) + `"}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":`},
		{"to_bridge", `{"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"thread/list","params":{},"PRIVATE_CANARY":true}`},
		{"from_bridge", `{"id":1,"method":"thread/list","params":{}}`},
		{"to_bridge", `{"id":1,"method":"project/list","params":{}}`},
	} {
		var log bytes.Buffer
		o := newObserver(&log)
		o.line(fixture.direction, []byte(fixture.raw))
		assertThreadListSortSection(t, threadListSortSectionRecords(t, log.Bytes())[0], "", "")
	}
	var log bytes.Buffer
	o := newObserver(&log)
	request := []byte(`{"id":1,"method":"thread/list","params":{}}`)
	o.line("to_bridge", request)
	o.line("to_bridge", request)
	o.line("from_bridge", []byte(`{"id":1,"error":{"code":-32602,"message":"unsupported thread sort key"}}`))
	records := threadListSortSectionRecords(t, log.Bytes())
	assertThreadListSortSection(t, records[1], "", "")
	assertThreadListSortSection(t, records[2], "", "")
}

func TestThreadListSortSectionWireUnchanged(t *testing.T) {
	request := []byte("{\"id\":1,\"method\":\"thread/list\",\"params\":{\"sortKey\":\"section_position\",\"sectionId\":\"PRIVATE_CANARY\"}}\r\n")
	response := []byte("{\"id\":1,\"error\":{\"code\":-32602,\"message\":\"unsupported thread sort key\",\"data\":\"PRIVATE_CANARY\"}}\r\n")
	oversized := []byte(`{"id":2,"method":"thread/list","params":{"sortKey":"` + strings.Repeat("PRIVATE_CANARY", maxLine) + `"}}` + "\n")
	for _, step := range []int{1, 7, 8192} {
		var log bytes.Buffer
		o := newObserver(&log)
		for _, stream := range []struct {
			direction string
			payload   []byte
		}{
			{"to_bridge", append(append([]byte(nil), request...), oversized...)}, {"from_bridge", response},
		} {
			var forwarded bytes.Buffer
			p := &lineObserver{o: o, direction: stream.direction}
			if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), stream.payload...), step}, p); err != nil {
				t.Fatal("sort metadata forwarding failed")
			}
			if !bytes.Equal(stream.payload, forwarded.Bytes()) || len(p.buf) > maxLine || log.Len() > maxTrace {
				t.Fatal("sort metadata changed wire bytes or observation bounds")
			}
		}
		records := threadListSortSectionRecords(t, log.Bytes())
		assertThreadListSortSection(t, records[0], "section_position", "string")
		if string(records[1]["class"]) != `"oversized"` {
			t.Fatal("oversized sort input lost its fixed label")
		}
		for _, record := range records[1:] {
			assertThreadListSortSection(t, record, "", "")
		}
	}
}
