package appserverbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// AuthStatusPrimaryNoAuth167 is a default-off diagnostic for the reviewed
// credential-free CI fixture, not support for real accounts or credential
// isolation. A misconfigured credentialed child can export a token into the
// bridge before response rejection prevents its onward relay.
const AuthStatusPrimaryNoAuth167 = "primary-no-auth-v0.167.0-alpha.3.2"

const authStatusUnavailable = "primary no-auth status unavailable"

// The pinned native GetAuthStatusParams contains two Option<bool> fields.
// Preserve includeToken, including true; do not silently replace a token
// request with false. Explicit refresh is never allowed by this CI mode.
func validateAuthStatusRequest167(line []byte) bool {
	if validateRequirementsJSON(line) != nil {
		return false
	}
	m, err := startupObject(line)
	if err != nil || len(m) < 3 || len(m) > 4 ||
		!requirementsEnum(m["method"], "getAuthStatus") || !NativeRequestID167Valid(m["id"]) {
		return false
	}
	for key := range m {
		if key != "id" && key != "method" && key != "params" && key != "trace" {
			return false
		}
	}
	if trace, present := m["trace"]; present && !NativeTrace167Valid(trace) {
		return false
	}
	params, err := startupObject(m["params"])
	if err != nil || len(params) > 2 {
		return false
	}
	for key, value := range params {
		switch key {
		case "includeToken":
			if !requirementsNullableBool(value) {
				return false
			}
		case "refreshToken":
			if !requirementsNull(value) && !bytes.Equal(bytes.TrimSpace(value), []byte("false")) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Compare native integer identity too: -0 and 0 cannot name two simultaneous
// requests when either one is this strictly typed diagnostic operation.
func (r *Router) authStatusIDInUse(id json.RawMessage, method string) bool {
	key, err := authCompatibilityID(id)
	if err != nil {
		return false
	}
	for _, pending := range r.pending {
		g := pending.group
		if method != "getAuthStatus" && g.method != "getAuthStatus" {
			continue
		}
		other, err := authCompatibilityID(g.id)
		if err == nil && key == other {
			return true
		}
	}
	return false
}

// Retain the actual params object and token flags, compacted only by JSON
// encoding. No trace or extra envelope field enters the primary request.
func (r *Router) sendAuthStatus167(g *request, params json.RawMessage) Delivery {
	id := r.next("request")
	backend := r.backends[0].ID
	r.pending["s:"+id] = pending{g, backend}
	return deliver(backend, object{"id": raw(id), "method": raw(g.method), "params": params})
}

func (r *Router) authStatusFailure(g *request, code int64) []Delivery {
	r.abandon(g)
	return []Delivery{deliver("", object{
		"id": g.id,
		"error": raw(object{
			"code": raw(code), "message": raw(authStatusUnavailable),
		}),
	})}
}

// Only the genuine, complete native no-auth result can succeed. A non-null
// token, auth method, requires-auth flag or unknown field rejects the entire
// response; nothing is sanitized into a successful null result.
func (r *Router) finishAuthStatus167(g *request, line []byte) []Delivery {
	if !authStatusResponseJSON(line) {
		return r.authStatusFailure(g, -32000)
	}
	m, err := startupObject(line)
	if err != nil || len(m) != 2 || !NativeRequestID167Valid(m["id"]) {
		return r.authStatusFailure(g, -32000)
	}
	if encoded, present := m["error"]; present {
		failure, err := startupObject(encoded)
		if err != nil || len(failure) < 2 || len(failure) > 3 {
			return r.authStatusFailure(g, -32000)
		}
		for key := range failure {
			if key != "code" && key != "message" && key != "data" {
				return r.authStatusFailure(g, -32000)
			}
		}
		var code int64
		var message string
		if requirementsNull(failure["code"]) || json.Unmarshal(failure["code"], &code) != nil ||
			requirementsNull(failure["message"]) || json.Unmarshal(failure["message"], &message) != nil {
			return r.authStatusFailure(g, -32000)
		}
		// Preserve the native code without forwarding arbitrary text or data.
		return r.authStatusFailure(g, code)
	}
	result, ok := requirementsObject(m["result"], "authMethod", "authToken", "requiresOpenaiAuth")
	if !ok || !requirementsNull(result["authMethod"]) || !requirementsNull(result["authToken"]) ||
		!bytes.Equal(bytes.TrimSpace(result["requiresOpenaiAuth"]), []byte("false")) {
		return r.authStatusFailure(g, -32000)
	}
	r.end(g)
	return []Delivery{deliver("", object{"id": g.id, "result": m["result"]})}
}

// Native error strings/data are opaque and may legitimately contain control
// escapes, unlike policy text. Check bounded structure and decoded duplicate
// keys without copying those values into diagnostics. This validator never
// relaxes the independent requirements, capability or trace validators.
func authStatusResponseJSON(data []byte) bool {
	if len(data) > requirementsJSONLimit || !utf8.Valid(data) || !authStatusSurrogatesValid(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if !authStatusResponseValue(d, 0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func authStatusResponseValue(d *json.Decoder, depth int) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	if depth >= requirementsJSONDepth {
		return false
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for count := 0; d.More(); count++ {
			if count >= requirementsContainerLimit {
				return false
			}
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
			if !authStatusResponseValue(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim('}')
	case '[':
		for count := 0; d.More(); count++ {
			if count >= requirementsContainerLimit || !authStatusResponseValue(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim(']')
	default:
		return false
	}
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject those
// malformed escapes before decoding, but retain genuinely valid U+FFFD text
// in discarded native errors. Escaped backslashes never begin Unicode escapes.
func authStatusSurrogatesValid(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xDC00 && value <= 0xDFFF {
			return false
		}
		if value < 0xD800 || value > 0xDBFF {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return true
}
