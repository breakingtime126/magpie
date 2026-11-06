package main

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// observedGetAuthStatus is only called for a correlated client request. It
// independently rejects ambiguous envelopes before inspecting the two exact
// optional boolean fields in the pinned official GetAuthStatusParams:
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v1.rs
//
// This is test-only request metadata, not authentication state or permission to
// forward the RPC. Native false defaults do not promise that no refresh occurs.
func observedGetAuthStatus(raw []byte) (valid, include, refresh *bool) {
	envelope, ok := authMetadataObject(raw, 4)
	if !ok {
		return nil, nil, nil
	}
	for key := range envelope {
		switch key {
		case "jsonrpc", "id", "method", "params":
		default:
			return nil, nil, nil
		}
	}
	var method string
	if json.Unmarshal(envelope["method"], &method) != nil || method != "getAuthStatus" {
		return nil, nil, nil
	}
	if _, ok := digestID(envelope["id"]); !ok {
		return nil, nil, nil
	}
	if rawVersion, exists := envelope["jsonrpc"]; exists {
		var version string
		if json.Unmarshal(rawVersion, &version) != nil || version != "2.0" {
			return nil, nil, nil
		}
	}

	// Missing/null/non-object params, extra keys, duplicates and wrong types
	// produce validity=false alone. Never conflate invalid input with false.
	validity := false
	params, ok := authMetadataObject(envelope["params"], 2)
	if !ok {
		return &validity, nil, nil
	}
	for key := range params {
		if key != "includeToken" && key != "refreshToken" {
			return &validity, nil, nil
		}
	}
	includeRequested, includeOK := authRequestedFlag(params["includeToken"])
	refreshRequested, refreshOK := authRequestedFlag(params["refreshToken"])
	if !includeOK || !refreshOK {
		return &validity, nil, nil
	}
	validity = true
	return &validity, &includeRequested, &refreshRequested
}

// This bounded object reader decodes keys before duplicate comparison, so JSON
// escape aliases cannot choose a last-wins flag or disguise envelope ambiguity.
// Nested values stay opaque RawMessages; the only accepted parameter values
// below are null and booleans. No parse error or caller-supplied text escapes.
func authMetadataObject(raw []byte, maxFields int) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > maxLine || !utf8.Valid(raw) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok || len(fields) >= maxFields {
			return nil, false
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		fields[key] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return nil, false
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}

func authRequestedFlag(raw json.RawMessage) (bool, bool) {
	switch string(bytes.TrimSpace(raw)) {
	case "", "null", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}
