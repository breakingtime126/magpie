package main

import (
	"encoding/json"
	"strings"
)

// The pinned CLI's app-server branch uses SessionSource::VSCode. Its
// app-server/src/filters.rs maps sourceKinds "vscode" to that source;
// "appServer" instead means Mcp. Query after the completed user turns:
// ordinary local listings exclude empty previews and attached threads.
func readThreadLists(p configReader, threads [2]string, report *summary) error {
	return readThreadListsForProfile(p, threads, report, "")
}

func readThreadListsForProfile(p configReader, threads [2]string, report *summary, profile string) error {
	return readThreadListsWithAvailability(p, threads, report, profile, false)
}

func readThreadListsWithAvailability(p configReader, threads [2]string, report *summary, profile string, cold bool) error {
	if threads[0] == "" || threads[1] == "" || threads[0] == threads[1] {
		return failure("thread_list_invalid")
	}
	for _, allLineage := range []bool{false, true} {
		params := map[string]any{
			"limit": 1, "sortKey": "created_at", "sortDirection": "desc",
			"sourceKinds": []string{"vscode"}, "modelProviders": []string{"magpie_bridge"},
			"parentThreadId": nil,
		}
		if allLineage {
			params["ancestorThreadId"] = nil
			params["attachedToThreadId"] = nil
		}
		seen := map[string]bool{}
		for page := 0; page < 2; page++ {
			raw, err := p.rpc("thread/list", params)
			if err != nil {
				return err
			}
			id, next, err := checkThreadListPageAvailability(raw, threads, seen, page == 1, profile, cold)
			if err != nil {
				return err
			}
			seen[id] = true
			report.ThreadListPages++
			report.ThreadListItems++
			// Frontend pagination consumes the bridge's merged snapshot.
			// Never send its cursor to a native backend or repeat filters.
			params = map[string]any{"cursor": next}
		}
		if len(seen) != 2 {
			return failure("thread_list_invalid")
		}
		report.ThreadListQueries++
	}
	return nil
}

// This oracle does not use the production router's list, alias or ownership
// helpers. Each limit-one page must contain one of the actual thread/start IDs,
// exactly once per query, with the model belonging to that ID's original owner.
func checkThreadListPage(raw json.RawMessage, threads [2]string, seen map[string]bool, terminal bool) (string, string, error) {
	return checkThreadListPageForProfile(raw, threads, seen, terminal, "")
}

func checkThreadListPageForProfile(raw json.RawMessage, threads [2]string, seen map[string]bool, terminal bool, profile string) (string, string, error) {
	return checkThreadListPageAvailability(raw, threads, seen, terminal, profile, false)
}

func checkThreadListPageAvailability(raw json.RawMessage, threads [2]string, seen map[string]bool, terminal bool, profile string, cold bool) (string, string, error) {
	bad := failure("thread_list_invalid")
	models, err := acceptanceModels(profile)
	if err != nil {
		return "", "", bad
	}
	if threads[0] == "" || threads[1] == "" || threads[0] == threads[1] {
		return "", "", bad
	}
	// Reuse only the harness's duplicate/trailing-JSON guard, not its config oracle.
	decoded, err := decodeConfigJSON(raw)
	if err != nil {
		return "", "", bad
	}
	obj, ok := decoded.(map[string]any)
	if !ok || len(obj) != 2 {
		return "", "", bad
	}
	data, ok := obj["data"].([]any)
	if !ok || len(data) != 1 {
		return "", "", bad
	}
	cursor, present := obj["nextCursor"]
	next, isString := cursor.(string)
	if !present || (terminal && cursor != nil) || (!terminal && (!isString || strings.TrimSpace(next) == "")) {
		return "", "", bad
	}
	row, ok := data[0].(map[string]any)
	if !ok {
		return "", "", bad
	}
	id, ok := row["id"].(string)
	if !ok || seen[id] {
		return "", "", bad
	}
	model := ""
	switch id {
	case threads[0]:
		model = "magpie/alpha/" + models[0]
	case threads[1]:
		model = "magpie/beta/" + models[1]
	default:
		return "", "", bad
	}
	preview, ok := row["preview"].(string)
	if !ok || strings.TrimSpace(preview) == "" || row["ephemeral"] != false ||
		row["source"] != "vscode" || row["modelProvider"] != "magpie_bridge" {
		return "", "", bad
	}
	// Thread.model is nullable in the pinned protocol. Loaded enrichment
	// normally supplies it; preserve unavailable metadata instead of inventing it.
	gotModel, present := row["model"]
	if !present || (gotModel != nil && gotModel != model) {
		return "", "", bad
	}
	if profile == acceptanceSynthetic4239 && !cold && gotModel != model {
		return "", "", bad
	}
	return id, next, nil
}
