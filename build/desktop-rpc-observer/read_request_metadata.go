package main

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// These TEST-ONLY classifiers describe the pinned request shapes, not valid
// paths, cursors, thread identities or permission to route either method.
// Source identities and their complete method vocabulary are in rpc_method_facts.json.
func observedReadParams(raw []byte, method string, maxFields int) (map[string]json.RawMessage, bool, bool) {
	envelope, ok := observedRequestEnvelope(raw, method)
	if !ok {
		return nil, false, false
	}
	if trace, present := envelope["trace"]; present && !appserverbridge.NativeTrace167Valid(trace) {
		return nil, false, false
	}
	params, valid := authMetadataObject(envelope["params"], maxFields)
	return params, true, valid
}

func observedConfigRead(raw []byte) (valid, include, cwd *bool) {
	params, eligible, ok := observedReadParams(raw, "config/read", 2)
	if !eligible {
		return nil, nil, nil
	}
	validity := false
	if !ok {
		return &validity, nil, nil
	}
	for key := range params {
		if key != "includeLayers" && key != "cwd" {
			return &validity, nil, nil
		}
	}
	// ConfigReadParams defaults an omitted bool, but does not accept null.
	includeLayers := false
	if value, present := params["includeLayers"]; present {
		switch string(bytes.TrimSpace(value)) {
		case "true":
			includeLayers = true
		case "false":
		default:
			return &validity, nil, nil
		}
	}
	cwdProvided, cwdOK := observedOptionalString(params["cwd"])
	if !cwdOK {
		return &validity, nil, nil
	}
	validity = true
	return &validity, &includeLayers, &cwdProvided
}

func observedExperimentalFeatureList(raw []byte) (valid, cursor, limit, thread *bool) {
	params, eligible, ok := observedReadParams(raw, "experimentalFeature/list", 3)
	if !eligible {
		return nil, nil, nil, nil
	}
	validity := false
	if !ok {
		return &validity, nil, nil, nil
	}
	for key := range params {
		if key != "cursor" && key != "limit" && key != "threadId" {
			return &validity, nil, nil, nil
		}
	}
	cursorProvided, cursorOK := observedOptionalString(params["cursor"])
	limitProvided, limitOK := observedOptionalUint32(params["limit"])
	threadProvided, threadOK := observedOptionalString(params["threadId"])
	if !cursorOK || !limitOK || !threadOK {
		return &validity, nil, nil, nil
	}
	validity = true
	return &validity, &cursorProvided, &limitProvided, &threadProvided
}

func observedOptionalString(raw json.RawMessage) (bool, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, true
	}
	return true, nativeJSONString(raw)
}

func observedOptionalUint32(raw json.RawMessage) (bool, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, true
	}
	var value uint32
	// Native Option<u32> accepts integer tokens, not floating/exponent tokens,
	// booleans, strings, negative values or overflow. No numeric value is kept.
	return true, json.Unmarshal(raw, &value) == nil
}

// encoding/json replaces invalid UTF-8 and unpaired escaped surrogates. Native
// serde_json rejects them. Preserve normal JSON escapes and valid pairs without
// treating replacement decoding as evidence that a native string was valid.
func nativeJSONString(raw []byte) bool {
	if len(raw) < 2 || len(raw) > maxLine || raw[0] != '"' || !utf8.Valid(raw) || !json.Valid(raw) {
		return false
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code := nativeHex4(raw[i+1 : i+5])
		i += 4
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw)-1 || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low := nativeHex4(raw[i+3 : i+7])
			if low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		} else if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
	}
	return true
}

// Called only for four valid JSON hexadecimal digits after json.Valid.
func nativeHex4(raw []byte) uint16 {
	var value uint16
	for _, c := range raw {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			value |= uint16(c-'a') + 10
		default:
			value |= uint16(c-'A') + 10
		}
	}
	return value
}
