package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func batchWriteTestRequest(params string) []byte {
	return []byte(`{"id":"PRIVATE_CANARY_ID","method":"config/batchWrite","params":` + params + `}`)
}

func TestConfigBatchWriteFixedMetadata(t *testing.T) {
	roots, err := parseReadTargetRoots(`{"controlHome":"C:\\PRIVATE_CANARY_CONTROL"}`)
	if err != nil {
		t.Fatal("test roots")
	}
	raw := batchWriteTestRequest(`{"edits":[
		{"keyPath":"model","value":"magpie/beta/test-beta","mergeStrategy":"replace"},
		{"keyPath":"model_reasoning_effort","value":"none","mergeStrategy":"upsert"},
		{"keyPath":"PRIVATE_CANARY_KEY","value":{"PRIVATE_CANARY_BODY":true},"mergeStrategy":"PRIVATE_CANARY_STRATEGY","PRIVATE_CANARY_EXTRA":true},
		{"keyPath":"PRIVATE_CANARY_KEY","value":null}],
		"filePath":"c:/private_canary_control/config.toml","expectedVersion":"PRIVATE_CANARY_VERSION","reloadUserConfig":false,"PRIVATE_CANARY_PARAM":"PRIVATE_CANARY_VALUE"}`)
	got := observedConfigBatchWrite(raw, roots)
	if got == nil || got.Status != "observed" || got.Shape == nil {
		t.Fatal("missing fixed write shape")
	}
	s := got.Shape
	if s.Edits != 4 || !s.DuplicateKeyPath || !s.UnknownTopLevel || s.FilePath != "control_config" || s.ExpectedVersion != "string" || s.ReloadUserConfig != "false" {
		t.Fatal("incorrect fixed write summary")
	}
	want := []configEditObservation{
		{Key: "model", ValueType: "string", ValueMatch: "beta", MergeStrategy: "replace"},
		{Key: "model_reasoning_effort", ValueType: "string", ValueMatch: "none", MergeStrategy: "upsert"},
		{Key: "other", ValueType: "object", ValueMatch: "other", MergeStrategy: "other", UnknownFields: true},
		{Key: "other", ValueType: "null", ValueMatch: "other", MergeStrategy: "missing"},
	}
	for i := range want {
		if s.Items[i] != want[i] {
			t.Fatal("incorrect edit enum")
		}
	}
	b, err := json.Marshal(got)
	if err != nil || bytes.Contains(bytes.ToLower(b), []byte("private_canary")) || bytes.Contains(b, []byte("magpie/")) {
		t.Fatal("raw write metadata escaped")
	}
}

func TestConfigBatchWriteBoundsAndAmbiguity(t *testing.T) {
	for _, params := range []string{
		`null`, `{}`, `{"edits":null}`, `{"edits":{}}`, `{"edits":[null]}`,
		`{"edits":[],"edits":[]}`, `{"edits":[],"ed\u0069ts":[]}`,
		`{"edits":[{"keyPath":"model","keyPath":"other"}]}`,
		`{"edits":[{"value":1,"v\u0061lue":2}]}`,
	} {
		got := observedConfigBatchWrite(batchWriteTestRequest(params), nil)
		if got == nil || got.Status != "unavailable" || got.Shape != nil {
			t.Fatal("ambiguous write shape was not withheld")
		}
	}
	for _, raw := range []string{
		`{"id":1,"id":2,"method":"config/batchWrite","params":{"edits":[]}}`,
		`{"id":1,"method":"config/batchWrite","method":"other","params":{"edits":[]}}`,
		`{"id":1,"method":"config/batchWrite","params":{"edits":[]},"unknown":"PRIVATE_CANARY"}`,
		`{"method":"config/batchWrite","params":{"edits":[]}}`,
		`{"id":1,"method":"config/batchWrite","params":{"edits":[]},"trace":false}`,
	} {
		if observedConfigBatchWrite([]byte(raw), nil) != nil {
			t.Fatal("ineligible envelope gained write metadata")
		}
	}
	got := observedConfigBatchWrite(batchWriteTestRequest(`{"edits":[],"profile":null,"filePath":null,"expectedVersion":null,"reloadUserConfig":true}`), nil)
	if got.Status != "observed" || !got.Shape.UnknownTopLevel || got.Shape.FilePath != "null" || got.Shape.ExpectedVersion != "null" || got.Shape.ReloadUserConfig != "true" {
		t.Fatal("unknown profile or explicit null/true facts lost")
	}
	for _, n := range []int{0, 8, 9} {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"keyPath":"model","value":"magpie/alpha/test-alpha","mergeStrategy":"replace"}`
		}
		got := observedConfigBatchWrite(batchWriteTestRequest(`{"edits":[`+strings.Join(parts, ",")+`]}`), nil)
		if n > maxObservedConfigEdits {
			if got.Status != "limit" || got.Shape != nil {
				t.Fatal("edit cap bypassed")
			}
		} else if got.Status != "observed" || got.Shape.Edits != n || len(got.Shape.Items) != n || got.Shape.Items == nil {
			t.Fatal("bounded array changed")
		}
	}
}

func TestConfigBatchWriteOptionalShapesAndValueTypes(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, version, reload string
	}{
		{"", "missing", "missing", "missing"}, {"null", "null", "null", "other"},
		{`"PRIVATE_CANARY"`, "string", "string", "other"}, {"true", "boolean", "other", "true"},
		{"false", "boolean", "other", "false"}, {"1.25", "number", "other", "other"},
		{"[]", "array", "other", "other"}, {"{}", "object", "other", "other"},
	} {
		if observedBatchWriteValueType([]byte(tc.raw)) != tc.kind || observedBatchWriteVersion([]byte(tc.raw)) != tc.version || observedBatchWriteReload([]byte(tc.raw)) != tc.reload {
			t.Fatal("optional field shapes collapsed")
		}
	}
	roots, err := parseReadTargetRoots(`{"controlHome":"C:/control","workspace":"C:/workspace"}`)
	if err != nil {
		t.Fatal("roots")
	}
	for _, path := range []string{`"C:/control/config.toml"`, `"C:\\CONTROL\\CONFIG.TOML"`} {
		if observedBatchWritePath([]byte(path), roots) != "control_config" {
			t.Fatal("exact lexical control config missed")
		}
		if observedBatchWritePath([]byte(path), nil) != "other" {
			t.Fatal("undeclared control config claimed")
		}
	}
	for _, path := range []string{`"C:/control/other.toml"`, `"C:/workspace/config.toml"`, `"C:/control/../control/config.toml"`, `"config.toml"`, `true`, `"PRIVATE_CANARY"`} {
		if observedBatchWritePath([]byte(path), roots) != "other" {
			t.Fatal("non-exact control config claimed")
		}
	}
	if observedBatchWritePath(nil, roots) != "missing" || observedBatchWritePath([]byte("null"), roots) != "null" {
		t.Fatal("missing and null file paths collapsed")
	}
	for _, tc := range []struct {
		value, match string
	}{
		{`"magpie/alpha/test-alpha"`, "alpha"}, {`"magpie/beta/test-beta"`, "beta"}, {`"none"`, "none"},
		{`"magpie/alpha/test-alphaX"`, "other"}, {`"NONE"`, "other"}, {`null`, "other"},
	} {
		got := observedConfigBatchWrite(batchWriteTestRequest(`{"edits":[{"value":`+tc.value+`}]}`), nil)
		if got.Shape.Items[0].ValueMatch != tc.match {
			t.Fatal("value match is not exact")
		}
	}
}

func TestConfigBatchWriteObservationDoesNotChangeForwarding(t *testing.T) {
	var log, forwarded bytes.Buffer
	o := newObserver(&log)
	raw := append(batchWriteTestRequest(`{"edits":[{"keyPath":"model","value":"magpie/alpha/test-alpha","mergeStrategy":"replace"}]}`), '\n')
	p := &lineObserver{o: o, direction: "to_bridge"}
	if err := pump(&forwarded, bytes.NewReader(raw), p); err != nil || !bytes.Equal(raw, forwarded.Bytes()) {
		t.Fatal("write request forwarding changed")
	}
	es := events(t, log.Bytes())
	if len(es) != 2 || es[0].ConfigBatchWrite == nil || es[0].Class != "request" || es[0].Correlation == 0 {
		t.Fatal("request metadata not attached")
	}
	o.line("to_bridge", bytes.TrimSpace(raw)) // Same outstanding ID is quarantined.
	o.line("from_bridge", bytes.TrimSpace(raw))
	o.line("to_bridge", []byte(`{"method":"config/batchWrite","params":{"edits":[]}}`))
	o.line("from_bridge", []byte(`{"id":"PRIVATE_CANARY_ID","error":{"code":-32601,"message":"PRIVATE_CANARY_ERROR"}}`))
	for _, e := range events(t, log.Bytes())[2:] {
		if e.ConfigBatchWrite != nil {
			t.Fatal("duplicate/reverse/notification/error gained write metadata")
		}
	}
	if strings.Contains(log.String(), "PRIVATE_CANARY") {
		t.Fatal("request/error payload leaked")
	}
	var invalidLog, invalidForward bytes.Buffer
	invalid := append(batchWriteTestRequest(`{"edits":[{"value":1,"value":2}]}`), '\n')
	if err := pump(&invalidForward, bytes.NewReader(invalid), &lineObserver{o: newObserver(&invalidLog), direction: "to_bridge"}); err != nil || !bytes.Equal(invalid, invalidForward.Bytes()) {
		t.Fatal("invalid write forwarding changed")
	}
}

func TestConfigBatchWriteMaximumPublicRecordFitsExistingCap(t *testing.T) {
	item := configEditObservation{Key: "model_reasoning_effort", ValueType: "boolean", ValueMatch: "other", MergeStrategy: "missing", UnknownFields: false}
	shape := &configBatchWriteShape{Edits: 8, FilePath: "control_config", ExpectedVersion: "missing", ReloadUserConfig: "missing", Items: make([]configEditObservation, 8)}
	for i := range shape.Items {
		shape.Items[i] = item
	}
	b, err := json.Marshal(event{Sequence: 4294967296, PID: 2147483647, Kind: "rpc", Direction: "to_bridge", Method: "config/batchWrite", Correlation: 4294967296, Class: "request", ConfigBatchWrite: &configBatchWriteObservation{Schema: 1, Status: "observed", Shape: shape}})
	if err != nil || len(b) > 4096 {
		t.Fatal("write shape exceeds existing publication record limit")
	}
}
