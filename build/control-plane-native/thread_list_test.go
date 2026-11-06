package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var listTestThreads = [2]string{"01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002"}

func listTestPage(owner string, cursor any) json.RawMessage {
	index := 0
	if owner == "beta" {
		index = 1
	}
	return jsonBytes(map[string]any{
		"data": []any{map[string]any{
			"id": listTestThreads[index], "model": "magpie/" + owner + "/gpt-5.5",
			"modelProvider": "magpie_bridge", "source": "vscode", "ephemeral": false,
			"preview": "synthetic-" + owner + "-turn-1",
		}},
		"nextCursor": cursor,
	})
}

func TestThreadListRequiresKnownNonemptyOwnedRowsAndTerminalPagination(t *testing.T) {
	first := listTestPage("alpha", "opaque-bridge-cursor")
	last := listTestPage("beta", nil)
	seen := map[string]bool{}
	id, cursor, err := checkThreadListPage(first, listTestThreads, seen, false)
	if err != nil || id != listTestThreads[0] || cursor != "opaque-bridge-cursor" {
		t.Fatal("valid first page rejected")
	}
	seen[id] = true
	id, cursor, err = checkThreadListPage(last, listTestThreads, seen, true)
	if err != nil || id != listTestThreads[1] || cursor != "" {
		t.Fatal("valid terminal page rejected")
	}
	nullModel := strings.Replace(string(last), `"model":"magpie/beta/gpt-5.5"`, `"model":null`, 1)
	if _, _, err := checkThreadListPage([]byte(nullModel), listTestThreads, seen, true); err != nil {
		t.Fatal("native nullable model rejected")
	}
	// Order is not tied to backend iteration: native timestamps determine it.
	if _, _, err := checkThreadListPage(listTestPage("beta", "next"), listTestThreads, nil, false); err != nil {
		t.Fatal("valid reverse owner order rejected")
	}

	for _, test := range []struct {
		name string
		raw  string
	}{
		{"empty", `{"data":[],"nextCursor":null}`},
		{"missing-data", `{"nextCursor":null}`},
		{"null-data", `{"data":null,"nextCursor":null}`},
		{"wrong-data-type", `{"data":{},"nextCursor":null}`},
		{"null-row", `{"data":[null],"nextCursor":null}`},
		{"missing-id", strings.Replace(string(last), `"id":"`+listTestThreads[1]+`",`, "", 1)},
		{"unknown-id", strings.Replace(string(last), listTestThreads[1], "unknown-id", 1)},
		{"duplicate-id", string(listTestPage("alpha", nil))},
		{"wrong-provider", strings.Replace(string(last), "magpie_bridge", "beta_provider", 1)},
		{"wrong-owner-model", strings.Replace(string(last), "magpie/beta/gpt-5.5", "magpie/alpha/gpt-5.5", 1)},
		{"wrong-model-type", strings.Replace(string(last), `"model":"magpie/beta/gpt-5.5"`, `"model":false`, 1)},
		{"native-model", strings.Replace(string(last), "magpie/beta/gpt-5.5", "gpt-5.5", 1)},
		{"missing-model", strings.Replace(string(last), `"model":"magpie/beta/gpt-5.5",`, "", 1)},
		{"empty-preview", strings.Replace(string(last), "synthetic-beta-turn-1", "", 1)},
		{"ephemeral", strings.Replace(string(last), `"ephemeral":false`, `"ephemeral":true`, 1)},
		{"missing-ephemeral", strings.Replace(string(last), `"ephemeral":false,`, "", 1)},
		{"wrong-source", strings.Replace(string(last), "vscode", "appServer", 1)},
		{"missing-cursor", strings.Replace(string(last), `,"nextCursor":null`, "", 1)},
		{"extra-page", string(listTestPage("beta", "another-page"))},
		{"wrong-cursor-type", string(listTestPage("beta", 17))},
		{"duplicate-field", strings.Replace(string(last), `"nextCursor":null`, `"nextCursor":"hidden","nextCursor":null`, 1)},
		{"error-envelope", `{"error":{"code":-32602},"data":[],"nextCursor":null}`},
		{"trailing-json", string(last) + "{}"},
		{"over-limit", string(jsonBytes(map[string]any{"data": []any{
			map[string]any{"id": listTestThreads[0]}, map[string]any{"id": listTestThreads[1]},
		}, "nextCursor": nil}))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := checkThreadListPage([]byte(test.raw), listTestThreads, seen, true); err != failure("thread_list_invalid") {
				t.Fatal("invalid thread list page accepted")
			}
		})
	}
	for _, cursor := range []any{nil, "", " ", 1, false, []any{}, map[string]any{}} {
		if _, _, err := checkThreadListPage(listTestPage("alpha", cursor), listTestThreads, nil, false); err == nil {
			t.Fatal("missing continuation accepted")
		}
	}
	for _, ids := range [][2]string{{"", ""}, {listTestThreads[0], ""}, {listTestThreads[0], listTestThreads[0]}} {
		if _, _, err := checkThreadListPage(first, ids, nil, false); err == nil {
			t.Fatal("invalid expected identities accepted")
		}
	}
}

type threadListReader struct {
	calls  []map[string]any
	failAt int
	badAt  int
}

func (p *threadListReader) rpc(method string, params any) (json.RawMessage, error) {
	if method != "thread/list" {
		return nil, failure("protocol_invalid")
	}
	m, ok := params.(map[string]any)
	if !ok {
		return nil, failure("protocol_invalid")
	}
	p.calls = append(p.calls, m)
	call := len(p.calls)
	if call == p.failAt {
		return nil, failure("native_error")
	}
	if call == p.badAt {
		return []byte(`{"data":[],"nextCursor":null}`), nil
	}
	switch call {
	case 1:
		return listTestPage("alpha", "cursor-one"), nil
	case 2:
		return listTestPage("beta", nil), nil
	case 3:
		return listTestPage("beta", "cursor-two"), nil
	case 4:
		return listTestPage("alpha", nil), nil
	default:
		return nil, failure("protocol_limit")
	}
}

func TestThreadListQueriesForwardReviewedNullsAndConsumeOnlyBridgeCursors(t *testing.T) {
	p := &threadListReader{}
	var report summary
	if err := readThreadLists(p, listTestThreads, &report); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"limit": 1, "sortKey": "created_at", "sortDirection": "desc",
			"sourceKinds": []string{"vscode"}, "modelProviders": []string{"magpie_bridge"}, "parentThreadId": nil},
		{"cursor": "cursor-one"},
		{"limit": 1, "sortKey": "created_at", "sortDirection": "desc",
			"sourceKinds": []string{"vscode"}, "modelProviders": []string{"magpie_bridge"},
			"parentThreadId": nil, "ancestorThreadId": nil, "attachedToThreadId": nil},
		{"cursor": "cursor-two"},
	}
	if !reflect.DeepEqual(p.calls, want) || report.ThreadListQueries != 2 ||
		report.ThreadListPages != 4 || report.ThreadListItems != 4 {
		t.Fatal("list query, pagination or independently validated counts changed")
	}
}

func TestThreadListsFailImmediatelyOnNativeErrorOrMissingKnownThread(t *testing.T) {
	for call := 1; call <= 4; call++ {
		for _, nativeError := range []bool{false, true} {
			p := &threadListReader{}
			want := failure("thread_list_invalid")
			if nativeError {
				p.failAt = call
				want = failure("native_error")
			} else {
				p.badAt = call
			}
			var report summary
			if err := readThreadLists(p, listTestThreads, &report); err != want ||
				len(p.calls) != call || report.ThreadListPages != call-1 ||
				report.ThreadListItems != call-1 || report.ThreadListQueries != (call-1)/2 {
				t.Fatal("error or missing thread retried, concealed or counted as success")
			}
		}
	}
	p := &threadListReader{}
	if readThreadLists(p, [2]string{}, &summary{}) == nil || len(p.calls) != 0 {
		t.Fatal("invalid expected identities sent to native")
	}
}
