package nativeinitialize

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

const clientName = "magpie-synthetic-lifecycle-probe"
const initializeRequest = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"magpie-synthetic-lifecycle-probe","version":"0.0.0"},"capabilities":{"experimentalApi":false,"explicitGatewayOAuth":true}}}` + "\n"
const syntheticCatalog = `{"models":[{"slug":"synthetic-lifecycle-only","display_name":"Synthetic lifecycle fixture","description":"Self-authored fixture, not a supported inference model.","supported_reasoning_levels":[],"shell_type":"disabled","visibility":"list","supported_in_api":false,"priority":0,"support_verbosity":false,"truncation_policy":{"mode":"tokens","limit":1024},"experimental_supported_tools":[],"base_instructions":"Synthetic lifecycle fixture. No inference is requested."}]}`

func uniqueJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate/invalid JSON key")
				}
				seen[name] = true
				if err = value(); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("invalid object")
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("invalid array")
			}
		default:
			return errors.New("invalid delimiter")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func object(data []byte, keys ...string) (map[string]json.RawMessage, error) {
	if uniqueJSON(data) != nil {
		return nil, errors.New("invalid JSON object")
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil || m == nil || len(m) != len(keys) {
		return nil, errors.New("unexpected object shape")
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return nil, errors.New("missing required field")
		}
	}
	return m, nil
}
func textField(data []byte) (string, error) {
	var s string
	if json.Unmarshal(data, &s) != nil || s == "" {
		return "", errors.New("invalid string")
	}
	return s, nil
}
func startupVersion(data []byte) (string, error) {
	m, e := object(data, "appServerVersion", "configRequirements")
	if e != nil {
		return "", e
	}
	version, e := textField(m["appServerVersion"])
	if e != nil || len(version) > 64 {
		return "", errors.New("invalid app-server version")
	}
	for _, r := range version {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", errors.New("invalid version characters")
		}
	}
	requirements, e := object(m["configRequirements"], "supportsIndependentSpeedModes", "requirements")
	if e != nil {
		return "", e
	}
	if string(bytes.TrimSpace(requirements["supportsIndependentSpeedModes"])) != "true" || string(bytes.TrimSpace(requirements["requirements"])) != "null" {
		return "", errors.New("unexpected managed requirements")
	}
	return version, nil
}
func assertInitialize(data []byte, version, home string) error {
	m, e := object(data, "id", "result")
	if e != nil {
		return e
	}
	if string(bytes.TrimSpace(m["id"])) != "1" {
		return errors.New("initialize ID mismatch")
	}
	result, e := object(m["result"], "userAgent", "codexHome", "platformFamily", "platformOs")
	if e != nil {
		return e
	}
	fields := map[string]string{}
	for k, v := range result {
		s, e := textField(v)
		if e != nil {
			return e
		}
		fields[k] = s
	}
	if fields["platformFamily"] != "windows" || fields["platformOs"] != "windows" {
		return errors.New("unexpected platform")
	}
	if !filepath.IsAbs(fields["codexHome"]) || !strings.EqualFold(filepath.Clean(fields["codexHome"]), filepath.Clean(home)) {
		return errors.New("initialize home mismatch")
	}
	if len(fields["userAgent"]) > 2048 || !strings.HasPrefix(fields["userAgent"], clientName+"/"+version+" (") || !strings.HasSuffix(fields["userAgent"], " ("+clientName+"; 0.0.0)") {
		return errors.New("initialize user agent mismatch")
	}
	return nil
}
func TestStrictStartupResponse(t *testing.T) {
	good := `{"appServerVersion":"0.0.0","configRequirements":{"supportsIndependentSpeedModes":true,"requirements":null}}`
	if v, e := startupVersion([]byte(good)); e != nil || v != "0.0.0" {
		t.Fatal("valid authored response rejected")
	}
	for _, bad := range []string{strings.Replace(good, `"requirements":null`, `"requirements":{}`, 1), strings.Replace(good, `"requirements":null`, `"requirements":null,"requirements":null`, 1), strings.Replace(good, `true`, `false`, 1), good + good, `{"error":{}}`} {
		if _, e := startupVersion([]byte(bad)); e == nil {
			t.Fatal("invalid startup accepted")
		}
	}
}
func TestStrictInitializeResponse(t *testing.T) {
	home, _ := filepath.Abs(filepath.Join("synthetic", "home"))
	data, _ := json.Marshal(map[string]any{"id": 1, "result": map[string]string{"userAgent": clientName + "/0.0.0 (Windows x; x64) terminal (" + clientName + "; 0.0.0)", "codexHome": home, "platformFamily": "windows", "platformOs": "windows"}})
	if assertInitialize(data, "0.0.0", home) != nil {
		t.Fatal("valid authored initialization rejected")
	}
	for _, bad := range [][]byte{bytes.Replace(data, []byte(`"id":1`), []byte(`"id":2`), 1), bytes.Replace(data, []byte(`"id":1`), []byte(`"id":1,"id":1`), 1), append(append([]byte{}, data...), data...), []byte(`{"id":1,"error":{}}`)} {
		if assertInitialize(bad, "0.0.0", home) == nil {
			t.Fatal("invalid initialize accepted")
		}
	}
}

// Diagnostics contain only harness-authored finite codes. Never serialize errors.
type failureDiagnostic struct {
	Stage  string
	Reason string
}

var diagnosticStages = strings.Fields("helper_gate helper_policy raw_layout raw_signature_hash fixture native_policy native_inputs startup_command startup_exit_output startup_response initialize_command initialize_assertion initialize_exit_output cleanup report unknown")
var diagnosticReasons = map[string]string{
	"invalid JSON object":                                  "json_object",
	"unexpected object shape":                              "object_shape",
	"missing required field":                               "missing_field",
	"invalid string":                                       "invalid_string",
	"invalid app-server version":                           "version_invalid",
	"invalid version characters":                           "version_characters",
	"unexpected managed requirements":                      "requirements_unexpected",
	"initialize ID mismatch":                               "id_mismatch",
	"unexpected platform":                                  "platform_mismatch",
	"initialize home mismatch":                             "home_mismatch",
	"initialize user agent mismatch":                       "agent_mismatch",
	"unexpected local auth/environment/config input":       "local_input_present",
	"ancestor project configuration present or unreadable": "ancestor_config",

	"native start failed":                        "start_failed",
	"native process reaping unproven":            "reap_unproven",
	"native process did not exit successfully":   "exit_unsuccessful",
	"initialize write failed":                    "write_failed",
	"initialize write deadline":                  "write_deadline",
	"initialize response assertion failed":       "response_assertion",
	"native stderr limit/read failure":           "stderr_read",
	"native stderr closed before initialization": "stderr_closed",
	"initialize response deadline":               "response_deadline",
	"native stdin EOF failed":                    "stdin_close",
	"native stdout bounded read failed":          "stdout_read",
	"native stderr bounded read failed":          "stderr_read",
	"native exit/output deadline":                "exit_output_deadline",
	"unexpected extra native stdout":             "extra_stdout",
	"helper marker deadline":                     "gate_deadline",
}

func knownStage(s string) bool {
	for _, v := range diagnosticStages {
		if v == s {
			return true
		}
	}
	return false
}
func knownReason(s string) bool {
	if s == "operation_failed" {
		return true
	}
	for _, v := range diagnosticReasons {
		if v == s {
			return true
		}
	}
	return false
}
func safeFailure(stage string, err error) failureDiagnostic {
	if !knownStage(stage) {
		stage = "unknown"
	}
	reason := "operation_failed"
	if err != nil {
		// Exact equality only: wrapped/joined errors and arbitrary values are opaque.
		if code, ok := diagnosticReasons[err.Error()]; ok {
			reason = code
		}
	}
	return failureDiagnostic{stage, reason}
}
func decodeFailure(b []byte) (failureDiagnostic, bool) {
	var d failureDiagnostic
	if len(b) > 1024 || uniqueJSON(b) != nil {
		return d, false
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || dec.Decode(new(any)) != io.EOF {
		return failureDiagnostic{}, false
	}
	if !knownStage(d.Stage) || !knownReason(d.Reason) {
		return failureDiagnostic{}, false
	}
	return d, true
}
func TestFailureDiagnosticsSanitized(t *testing.T) {
	for _, stage := range diagnosticStages {
		for text, reason := range diagnosticReasons {
			d := safeFailure(stage, errors.New(text))
			b, _ := json.Marshal(d)
			got, ok := decodeFailure(b)
			if !ok || got.Stage != stage || got.Reason != reason {
				t.Fatal("known code lost")
			}
		}
	}
	for _, err := range []error{nil, errors.New("C:\\private\\secret token=value"), errors.Join(errors.New("native start failed"), errors.New("private")), errors.New("native start failed\nprivate")} {
		d := safeFailure("private stage", err)
		if d.Stage != "unknown" || d.Reason != "operation_failed" {
			t.Fatal("unknown detail disclosed")
		}
	}
	for _, b := range []string{
		`{"Stage":"fixture","Reason":"private"}`,
		`{"Stage":"private","Reason":"operation_failed"}`,
		`{"Stage":"fixture","Reason":"operation_failed","Extra":"private"}`,
		`{"Stage":"fixture","Stage":"cleanup","Reason":"operation_failed"}`,
		`{"Stage":"fixture","Reason":"operation_failed"}{}`,
		strings.Repeat("x", 1025),
	} {
		if _, ok := decodeFailure([]byte(b)); ok {
			t.Fatal("invalid marker accepted")
		}
	}
}
