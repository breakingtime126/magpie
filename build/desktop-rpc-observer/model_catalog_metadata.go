package main

import (
	"bytes"
	"encoding/json"

	"github.com/yetone/magpie/internal/appserverbridge"
)

// This is a bounded TEST-ONLY observation, not a protocol validator or routing
// decision. Only the exact legacy and self-authored CI tuples are recognized. No model
// name, ID, description, cursor, parameter, or raw body can enter this struct.
type modelCatalogObservation struct {
	Schema int                 `json:"schema"`
	Status string              `json:"status"`
	Counts *modelCatalogCounts `json:"counts,omitempty"`
}

type modelCatalogCounts struct {
	Items        int  `json:"items"`
	Alpha        int  `json:"alpha"`
	AlphaHidden  int  `json:"alphaHidden"`
	AlphaDefault int  `json:"alphaDefault"`
	Beta         int  `json:"beta"`
	BetaHidden   int  `json:"betaHidden"`
	BetaDefault  int  `json:"betaDefault"`
	Unknown      int  `json:"unknown"`
	HasMore      bool `json:"hasMore"`
}

func modelCatalogRequestEligible(raw []byte) bool {
	envelope, ok := observedRequestEnvelope(raw, "model/list")
	if !ok {
		return false
	}
	trace, present := envelope["trace"]
	return !present || appserverbridge.NativeTrace167Valid(trace)
}

// Called only after the existing direction/ID correlation has matched an
// eligible model/list request. Duplicate/ambiguous response envelopes remain
// unavailable, rather than inheriting encoding/json's last-wins attribution.
func observedModelCatalog(raw []byte) *modelCatalogObservation {
	out := &modelCatalogObservation{Schema: 1, Status: "unavailable"}
	envelope, ok := authMetadataObject(raw, 3)
	if !ok || !appserverbridge.NativeRequestID167Valid(envelope["id"]) {
		return out
	}
	for key := range envelope {
		if key != "id" && key != "result" && key != "trace" {
			return out
		}
	}
	if trace, present := envelope["trace"]; present && !appserverbridge.NativeTrace167Valid(trace) {
		return out
	}
	result, ok := authMetadataObject(envelope["result"], 2)
	if !ok || len(result) != 2 || result["data"] == nil || result["nextCursor"] == nil {
		return out
	}
	data := bytes.TrimSpace(result["data"])
	var models []json.RawMessage
	if len(data) == 0 || data[0] != '[' || json.Unmarshal(data, &models) != nil {
		return out
	}
	if len(models) > 32 {
		out.Status = "limit"
		return out
	}
	cursor := bytes.TrimSpace(result["nextCursor"])
	hasMore := !bytes.Equal(cursor, []byte("null"))
	if hasMore && !nativeJSONString(cursor) {
		return out
	}
	counts := &modelCatalogCounts{Items: len(models), HasMore: hasMore}
	for _, rawModel := range models {
		model, valid := authMetadataObject(rawModel, 32)
		if !valid {
			return out
		}
		values := make([]string, 3)
		for i, key := range []string{"id", "model", "displayName"} {
			value := bytes.TrimSpace(model[key])
			if !nativeJSONString(value) || len(value) > 1024 || json.Unmarshal(value, &values[i]) != nil {
				return out
			}
		}
		hidden, def := boolField(model, "hidden"), boolField(model, "isDefault")
		if hidden == nil || def == nil {
			return out
		}
		switch {
		case values[0] == "magpie/alpha/gpt-5.5" && values[1] == values[0] && values[2] == "GPT-5.5 [alpha]" ||
			values[0] == "magpie/alpha/test-alpha" && values[1] == values[0] && values[2] == "Synthetic Alpha [alpha]":
			counts.Alpha++
			if *hidden {
				counts.AlphaHidden++
			}
			if *def {
				counts.AlphaDefault++
			}
		case values[0] == "magpie/beta/gpt-5.5" && values[1] == values[0] && values[2] == "GPT-5.5 [beta]" ||
			values[0] == "magpie/beta/test-beta" && values[1] == values[0] && values[2] == "Synthetic Beta [beta]":
			counts.Beta++
			if *hidden {
				counts.BetaHidden++
			}
			if *def {
				counts.BetaDefault++
			}
		default:
			counts.Unknown++
		}
	}
	out.Status, out.Counts = "observed", counts
	if counts.Unknown != 0 || hasMore {
		out.Status = "partial"
	}
	return out
}
