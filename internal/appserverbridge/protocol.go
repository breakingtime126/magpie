// Package appserverbridge joins independent Codex app-server processes at
// their local JSONL protocol. It never serves or forwards model HTTP traffic.
package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxMessageBytes bounds a single line from either side of the bridge.
const MaxMessageBytes = 16 << 20

type object map[string]json.RawMessage

// Delivery names a backend, or the client when Backend is empty.
type Delivery struct {
	Backend string
	Message []byte
}

func parseObject(b []byte) (object, error) {
	if len(b) > MaxMessageBytes {
		return nil, errors.New("app-server message exceeds the size limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var o object
	if err := d.Decode(&o); err != nil || o == nil {
		return nil, errors.New("expected an app-server JSON object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one app-server JSON object")
	}
	return o, nil
}
func raw(v any) json.RawMessage        { b, _ := json.Marshal(v); return b }
func text(o object, key string) string { var s string; _ = json.Unmarshal(o[key], &s); return s }
func child(o object, key string) (object, error) {
	b := o[key]
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return object{}, nil
	}
	return parseObject(b)
}
func exists(o object, key string) bool {
	b := o[key]
	return len(b) != 0 && !bytes.Equal(b, []byte("null"))
}
func clone(o object) object {
	n := object{}
	for k, v := range o {
		n[k] = v
	}
	return n
}
func deliver(backend string, o object) Delivery { return Delivery{backend, raw(o)} }
func fail(id json.RawMessage, code int, message string) Delivery {
	if len(id) == 0 {
		id = raw(nil)
	}
	return deliver("", object{"id": id, "error": raw(map[string]any{"code": code, "message": message})})
}
func idKey(id json.RawMessage) (string, error) {
	if len(id) == 0 {
		return "", errors.New("missing request id")
	}
	var s string
	if json.Unmarshal(id, &s) == nil {
		return "s:" + s, nil
	}
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return "", errors.New("invalid request id")
	}
	if n, ok := v.(json.Number); ok {
		return "n:" + n.String(), nil
	}
	return "", errors.New("request id must be a string or number")
}
func stringList(b json.RawMessage) ([]string, error) {
	var v []string
	if json.Unmarshal(b, &v) != nil {
		return nil, errors.New("expected an array of identifiers")
	}
	return v, nil
}
func fieldString(o object, key string) (string, error) {
	if !exists(o, key) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(o[key], &s); err != nil || s == "" {
		return "", fmt.Errorf("invalid %s", key)
	}
	return s, nil
}

// Only fields whose routing meaning has been reviewed enter a backend.
// In particular, experimental nested models and provider/config fallbacks
// must not silently bypass the top-level model check after an upgrade.
func validateParams(method string, p object) error {
	fields := map[string]string{
		"initialize":              "clientInfo capabilities",
		"model/list":              "cursor limit includeHidden",
		"thread/list":             "currentAccountOnly cursor limit excludedThreadIds sortKey sortDirection modelProviders sourceKinds originators archived sectionId cwd useStateDbOnly searchTerm",
		"thread/loaded/list":      "cursor limit",
		"thread/start":            "model modelProvider serviceTier cwd approvalPolicy approvalsReviewer sandbox config serviceName baseInstructions developerInstructions personality ephemeral sessionStartSource threadSource",
		"thread/resume":           "threadId model modelProvider serviceTier cwd approvalPolicy approvalsReviewer sandbox config baseInstructions developerInstructions personality excludeTurns",
		"thread/fork":             "threadId lastTurnId model modelProvider serviceTier cwd approvalPolicy approvalsReviewer sandbox config baseInstructions developerInstructions ephemeral threadSource excludeTurns",
		"thread/read":             "threadId includeTurns",
		"thread/turns/list":       "threadId cursor limit sortDirection itemsView",
		"thread/items/list":       "threadId turnId cursor limit sortDirection",
		"thread/archive":          "threadId",
		"thread/unarchive":        "threadId",
		"thread/name/set":         "threadId name",
		"thread/compact/start":    "threadId",
		"thread/revert":           "threadId turnId",
		"thread/rollback":         "threadId numTurns",
		"thread/unsubscribe":      "threadId",
		"turn/start":              "threadId disabledPluginIds clientUserMessageId input turnTrigger parentTurnId rootTurnId toolOutput cwd approvalPolicy approvalsReviewer sandboxPolicy model serviceTier serviceTierForTurn effort summary personality outputSchema",
		"turn/interrupt":          "threadId turnId",
		"turn/steer":              "threadId clientUserMessageId input expectedTurnId",
		"account/read":            "refreshToken",
		"account/rateLimits/read": "",
	}
	list, ok := fields[method]
	if !ok {
		return nil
	}
	allowed := map[string]bool{}
	for _, name := range strings.Fields(list) {
		allowed[name] = true
	}
	for name := range p {
		if !allowed[name] {
			return fmt.Errorf("unsupported %s field: %s", method, name)
		}
	}
	return nil
}

func validateModel(m object) error {
	for _, k := range []string{"id", "model", "displayName", "description", "defaultReasoningEffort"} {
		var v string
		if json.Unmarshal(m[k], &v) != nil || (k != "description" && v == "") {
			return fmt.Errorf("model descriptor requires string %s", k)
		}
	}
	for _, k := range []string{"hidden", "supportsPersonality", "isDefault"} {
		var v bool
		if json.Unmarshal(m[k], &v) != nil || !exists(m, k) {
			return fmt.Errorf("model descriptor requires boolean %s", k)
		}
	}
	var modalities []string
	if json.Unmarshal(m["inputModalities"], &modalities) != nil || !exists(m, "inputModalities") {
		return errors.New("model descriptor requires inputModalities")
	}
	var efforts []object
	if json.Unmarshal(m["supportedReasoningEfforts"], &efforts) != nil || !exists(m, "supportedReasoningEfforts") {
		return errors.New("model descriptor requires supportedReasoningEfforts")
	}
	for _, effort := range efforts {
		if text(effort, "reasoningEffort") == "" {
			return errors.New("invalid model reasoning effort")
		}
		var description string
		if json.Unmarshal(effort["description"], &description) != nil {
			return errors.New("invalid model reasoning description")
		}
	}
	return nil
}
