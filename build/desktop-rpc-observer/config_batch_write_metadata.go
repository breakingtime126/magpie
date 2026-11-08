package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

const maxObservedConfigEdits = 8

// All strings in these TEST-ONLY observations are fixed enums, not RPC values.
// A shape observation never authorizes a write or says the bridge accepted it.
type configBatchWriteObservation struct {
	Schema int                    `json:"schema"`
	Status string                 `json:"status"`
	Shape  *configBatchWriteShape `json:"shape,omitempty"`
}

type configBatchWriteShape struct {
	Edits            int                     `json:"edits"`
	DuplicateKeyPath bool                    `json:"duplicateKeyPath"`
	UnknownTopLevel  bool                    `json:"unknownTopLevel"`
	FilePath         string                  `json:"filePath"`
	ExpectedVersion  string                  `json:"expectedVersion"`
	ReloadUserConfig string                  `json:"reloadUserConfig"`
	Items            []configEditObservation `json:"items"`
}

type configEditObservation struct {
	Key           string `json:"key"`
	ValueType     string `json:"valueType"`
	ValueMatch    string `json:"valueMatch"`
	MergeStrategy string `json:"mergeStrategy"`
	UnknownFields bool   `json:"unknownFields"`
}

// ConfigBatchWriteParams / ConfigEdit / MergeStrategy, public protocol only:
// https://github.com/openai/codex/blob/c40729ea5cf8e947481b130c59807273eda2f8e9/codex-rs/app-server-protocol/src/protocol/v2/config.rs
// The unambiguous client request, line/trace caps and forwarding are unchanged.
func observedConfigBatchWrite(raw []byte, roots *readTargetRoots) *configBatchWriteObservation {
	params, eligible, valid := observedReadParams(raw, "config/batchWrite", 16)
	if !eligible {
		return nil
	}
	out := &configBatchWriteObservation{Schema: 1, Status: "unavailable"}
	if !valid {
		return out
	}
	rawEdits := bytes.TrimSpace(params["edits"])
	var edits []json.RawMessage
	if len(rawEdits) == 0 || rawEdits[0] != '[' || json.Unmarshal(rawEdits, &edits) != nil {
		return out
	}
	if len(edits) > maxObservedConfigEdits {
		out.Status = "limit"
		return out
	}
	shape := &configBatchWriteShape{
		Edits:            len(edits),
		FilePath:         observedBatchWritePath(params["filePath"], roots),
		ExpectedVersion:  observedBatchWriteVersion(params["expectedVersion"]),
		ReloadUserConfig: observedBatchWriteReload(params["reloadUserConfig"]),
		Items:            make([]configEditObservation, 0, len(edits)),
	}
	for key := range params {
		if key != "edits" && key != "filePath" && key != "expectedVersion" && key != "reloadUserConfig" {
			shape.UnknownTopLevel = true
		}
	}
	seen := make(map[string]bool)
	for _, rawEdit := range edits {
		edit, ok := authMetadataObject(rawEdit, 8)
		if !ok {
			return out
		}
		item := configEditObservation{
			Key:           "other",
			ValueType:     observedBatchWriteValueType(edit["value"]),
			ValueMatch:    "other",
			MergeStrategy: "other",
		}
		if key, ok := observedBatchWriteString(edit["keyPath"]); ok {
			if seen[key] {
				shape.DuplicateKeyPath = true
			}
			seen[key] = true
			if key == "model" || key == "model_reasoning_effort" {
				item.Key = key
			}
		}
		if value, ok := observedBatchWriteString(edit["value"]); ok {
			switch value {
			case "magpie/alpha/test-alpha":
				item.ValueMatch = "alpha"
			case "magpie/beta/test-beta":
				item.ValueMatch = "beta"
			case "none":
				item.ValueMatch = "none"
			}
		}
		if len(edit["mergeStrategy"]) == 0 {
			item.MergeStrategy = "missing"
		} else if strategy, ok := observedBatchWriteString(edit["mergeStrategy"]); ok && (strategy == "replace" || strategy == "upsert") {
			item.MergeStrategy = strategy
		}
		for key := range edit {
			if key != "keyPath" && key != "value" && key != "mergeStrategy" {
				item.UnknownFields = true
			}
		}
		shape.Items = append(shape.Items, item)
	}
	out.Status, out.Shape = "observed", shape
	return out
}

func observedBatchWriteString(raw []byte) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if !nativeJSONString(raw) {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func observedBatchWriteValueType(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "missing"
	}
	switch raw[0] {
	case 'n':
		return "null"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case '[':
		return "array"
	case '{':
		return "object"
	default:
		return "number"
	}
}

func observedBatchWriteVersion(raw []byte) string {
	switch observedBatchWriteValueType(raw) {
	case "missing":
		return "missing"
	case "null":
		return "null"
	case "string":
		return "string"
	default:
		return "other"
	}
}

func observedBatchWriteReload(raw []byte) string {
	switch string(bytes.TrimSpace(raw)) {
	case "":
		return "missing"
	case "true":
		return "true"
	case "false":
		return "false"
	default:
		return "other"
	}
}

func observedBatchWritePath(raw []byte, roots *readTargetRoots) string {
	kind := observedBatchWriteValueType(raw)
	if kind == "missing" || kind == "null" {
		return kind
	}
	path, ok := observedBatchWriteString(raw)
	if !ok || roots == nil {
		return "other"
	}
	canonical, ok := lexicalWindowsAbsolute(path, false)
	if !ok {
		return "other"
	}
	for _, root := range roots.roots {
		if root.label == "control_home" && canonical == strings.TrimSuffix(root.path, "/")+"/config.toml" {
			return "control_config"
		}
	}
	return "other"
}
