package appserverbridge

import (
	"bytes"
	"encoding/json"
)

// AuthStatusPrimaryNoAuth167 is a default-off diagnostic for the reviewed
// credential-free CI fixture, not support for real accounts or credential
// isolation. A misconfigured credentialed child can export a token into the
// bridge before response rejection prevents its onward relay.
const AuthStatusPrimaryNoAuth167 = "primary-no-auth-v0.167.0-alpha.3.2"

// AuthStatusPrimaryTrustedStdio167 deliberately permits native credential relay
// to an operator-trusted private stdio frontend. It is not frontend authentication
// or OS credential isolation, and never grants another backend access.
const AuthStatusPrimaryTrustedStdio167 = "primary-trusted-stdio-v0.167.0-alpha.3.2"

const authStatusUnavailable = "primary no-auth status unavailable"

// The pinned native GetAuthStatusParams contains two Option<bool> fields.
// Preserve includeToken, including true; do not silently replace a token
// request with false. Explicit refresh is never allowed by this CI mode.
func validateAuthStatusRequest167(line []byte) bool {
	return validateAuthStatusRequestMode167(line, false)
}

func validateAuthStatusRequestMode167(line []byte, trusted bool) bool {
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
			if !requirementsNullableBool(value) || (!trusted && !requirementsNull(value) && !bytes.Equal(bytes.TrimSpace(value), []byte("false"))) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Retain the actual params object and token flags, compacted only by JSON
// encoding. No trace or extra envelope field enters the primary request.
func (r *Router) sendAuthStatus167(g *request, params json.RawMessage) Delivery {
	return r.sendParams(g, r.backends[0].ID, params)
}

func (r *Router) authStatusFailure(g *request, code int64) []Delivery {
	r.abandon(g)
	message := authStatusUnavailable
	if r.auth.authStatusTrusted {
		message = "primary trusted stdio auth status unavailable"
	}
	return []Delivery{deliver("", object{
		"id": g.id,
		"error": raw(object{
			"code": raw(code), "message": raw(message),
		}),
	})}
}

// Validate the native envelope before either explicitly selected result policy.
// The original diagnostic still permits only the genuine complete no-auth
// triple; the separate trusted mode permits its narrow credential relay subset.
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
	if !ok {
		return r.authStatusFailure(g, -32000)
	}
	if r.auth.authStatusTrusted {
		if !trustedAuthStatusResult167(result, g.params) {
			return r.authStatusFailure(g, -32000)
		}
	} else if !requirementsNull(result["authMethod"]) || !requirementsNull(result["authToken"]) ||
		!bytes.Equal(bytes.TrimSpace(result["requiresOpenaiAuth"]), []byte("false")) {
		return r.authStatusFailure(g, -32000)
	}
	r.end(g)
	return []Delivery{deliver("", object{"id": g.id, "result": m["result"]})}
}

// Keep the auth response limits unchanged while sharing strict native JSON
// decoding with the separately bounded experimental feature catalog.
func authStatusResponseJSON(data []byte) bool {
	return nativeReadJSON(data, requirementsJSONLimit, requirementsContainerLimit)
}

// The public schema permits requiresOpenaiAuth:null, but the pinned native
// handler always emits a bool. Reject unobserved variants rather than claiming
// broad schema compatibility. Nonexportable methods may report metadata only.
func trustedAuthStatusResult167(result object, params object) bool {
	if !requirementsBool(result["requiresOpenaiAuth"]) {
		return false
	}
	methodNull, tokenNull := requirementsNull(result["authMethod"]), requirementsNull(result["authToken"])
	if bytes.Equal(bytes.TrimSpace(result["requiresOpenaiAuth"]), []byte("false")) {
		return methodNull && tokenNull
	}
	if !methodNull && !requirementsEnum(result["authMethod"], "apikey", "chatgpt", "chatgptAuthTokens", "headers", "agentIdentity", "personalAccessToken", "bedrockApiKey", "bedrockAccessKeys") {
		return false
	}
	if tokenNull {
		return true
	}
	// This explicit relay supports only ordinary OpenAI API-key and managed
	// ChatGPT tokens. External-host/internal and other credential modes do not
	// gain export support merely because their metadata enum is recognized.
	if !bytes.Equal(bytes.TrimSpace(params["includeToken"]), []byte("true")) ||
		!requirementsEnum(result["authMethod"], "apikey", "chatgpt") {
		return false
	}
	var token string
	return json.Unmarshal(result["authToken"], &token) == nil && token != ""
}
