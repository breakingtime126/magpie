package main

import (
	"bytes"
	"encoding/json"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// These are only four JSON field shapes, not whole-request validity, identity
// ownership or permission to forward a filter. In particular projectId:null
// means unassigned threads, while omission means every project. Pinned source:
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/thread.rs#L1428-L1511
// The 18-field bound covers its 17 declared params and the bridge's legacy
// currentAccountOnly field. Unknown names and all unrelated values stay opaque.
func observedThreadList(raw []byte, e *event) {
	params, eligible, validObject := observedReadParams(raw, "thread/list", 18)
	if !eligible {
		return
	}
	e.ThreadListProjectIDShape = "invalid"
	e.ThreadListParentIDShape = "invalid"
	e.ThreadListAncestorIDShape = "invalid"
	e.ThreadListAttachedIDShape = "invalid"
	if !validObject {
		return
	}
	e.ThreadListProjectIDShape = observedThreadFilterShape(params["projectId"])
	e.ThreadListParentIDShape = observedThreadFilterShape(params["parentThreadId"])
	e.ThreadListAncestorIDShape = observedThreadFilterShape(params["ancestorThreadId"])
	e.ThreadListAttachedIDShape = observedThreadFilterShape(params["attachedToThreadId"])
}

func observedThreadFilterShape(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "absent"
	}
	if bytes.Equal(raw, []byte("null")) {
		return "null"
	}
	if nativeJSONString(raw) {
		return "string"
	}
	return "invalid"
}

// Only correlated bridge-to-client thread/list failures call this helper.
// Duplicate/ambiguous envelopes or errors cannot turn last-wins parsing into
// evidence. No message fragment, arbitrary field name or request value escapes.
func observedThreadListRejection(raw []byte) string {
	envelope, ok := authMetadataObject(raw, 3)
	if !ok || !appserverbridge.NativeRequestID167Valid(envelope["id"]) {
		return "unknown"
	}
	for key := range envelope {
		if key != "id" && key != "error" && key != "trace" {
			return "unknown"
		}
	}
	if trace, present := envelope["trace"]; present && !appserverbridge.NativeTrace167Valid(trace) {
		return "unknown"
	}
	failure, ok := authMetadataObject(envelope["error"], 3)
	if !ok {
		return "unknown"
	}
	for key := range failure {
		if key != "code" && key != "message" && key != "data" {
			return "unknown"
		}
	}
	if !bytes.Equal(bytes.TrimSpace(failure["code"]), []byte("-32602")) || !nativeJSONString(failure["message"]) {
		return "unknown"
	}
	var message string
	if json.Unmarshal(failure["message"], &message) != nil {
		return "unknown"
	}
	// These whole strings are emitted by the existing bridge protocol/router.
	// A prefix, suffix, casing change or unknown name must remain unknown.
	switch message {
	case "unsupported thread/list field: projectId":
		return "thread_list_project_id"
	case "unsupported thread/list field: parentThreadId":
		return "thread_list_parent_id"
	case "unsupported thread/list field: ancestorThreadId":
		return "thread_list_ancestor_id"
	case "unsupported thread/list field: attachedToThreadId":
		return "thread_list_attached_id"
	case "unsupported thread sort key":
		return "thread_list_sort_key"
	case "unsupported thread sort direction":
		return "thread_list_sort_direction"
	case "unsupported logical provider filter":
		return "thread_list_provider_filter"
	case "invalid bridge cursor":
		return "thread_list_cursor"
	}
	return "unknown"
}
