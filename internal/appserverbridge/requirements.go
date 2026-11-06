package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// ConfigRequirementsCompareSafe selects the reviewed 0.166 public-policy subset.
const ConfigRequirementsCompareSafe = "compare-safe-v0.166"

const requirementsJSONLimit = 64 << 10
const requirementsJSONDepth = 12
const requirementsContainerLimit = 256

var errConfigRequirements = errors.New("config requirements read failed or returned unsupported configuration")

// validateRequirementsJSON inspects the entire result or JSON-RPC envelope
// before object decoding can discard duplicates. Bounds apply at every level.
func validateRequirementsJSON(data []byte) error {
	if len(data) > requirementsJSONLimit || !utf8.Valid(data) {
		return errConfigRequirements
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if !requirementsJSONValue(d, 0) {
		return errConfigRequirements
	}
	if _, err := d.Token(); err != io.EOF {
		return errConfigRequirements
	}
	return nil
}

func requirementsJSONValue(d *json.Decoder, depth int) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	if value, ok := token.(string); ok {
		// Also reject encoding/json's replacement of malformed surrogates.
		return startupText(value)
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
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || !startupText(name) || seen[name] {
				return false
			}
			seen[name] = true
			if !requirementsJSONValue(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim('}')
	case '[':
		for count := 0; d.More(); count++ {
			if count >= requirementsContainerLimit || !requirementsJSONValue(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim(']')
	default:
		return false
	}
}

// The source oracle is openai/codex at 4f093f2748667f3b0e6a07e3d4055121cbd3ce64,
// app-server-protocol/src/protocol/v2/config.rs and the config processor's
// map_requirements_to_api. Stable generated schemas omit experimental fields.
// This accepts complete public views, not proof of equal underlying policy.
func validateConfigRequirementsResponse(data []byte) (object, error) {
	if validateRequirementsJSON(data) != nil {
		return nil, errConfigRequirements
	}
	root, ok := requirementsObject(data, "requirements", "supportsIndependentSpeedModes")
	if !ok || !bytes.Equal(bytes.TrimSpace(root["supportsIndependentSpeedModes"]), []byte("true")) {
		return nil, errConfigRequirements
	}
	if !requirementsNull(root["requirements"]) && !requirementsPolicy(root["requirements"]) {
		return nil, errConfigRequirements
	}
	// Keep all accepted values, including nulls, false and empty collections.
	return root, nil
}

func requirementsPolicy(data json.RawMessage) bool {
	fields, err := startupObject(data)
	if err != nil || len(fields) != 34 {
		return false
	}
	for name, value := range fields {
		switch name {
		case "modelProvider", "modelProviders", "chatgptBaseUrl", "additionalDeveloperInstructions",
			"allowedPermissionProfiles", "defaultPermissions", "computerUse", "browserUse",
			"hooks", "autoReview", "models", "sqliteHome", "logDir", "modelCatalogJson", "otel":
			// Never redact or discard an unsupported non-null policy branch.
			if !requirementsNull(value) {
				return false
			}
		case "allowManagedHooksOnly", "allowBrowserAndComputerUse", "allowAppshots",
			"allowRemoteControl", "checkForUpdateOnStartup", "allowLoginShell":
			if !requirementsNullableBool(value) {
				return false
			}
		case "allowedLoginMethods":
			if !requirementsEnumArray(value, "api", "chatgpt") {
				return false
			}
		case "cliAuthCredentialsStore":
			if !requirementsNull(value) && !requirementsEnum(value, "file", "keyring", "auto", "ephemeral") {
				return false
			}
		case "allowedApprovalPolicies":
			if !requirementsApprovalPolicies(value) {
				return false
			}
		case "allowedApprovalsReviewers":
			if !requirementsEnumArray(value, "user", "auto_review", "guardian_subagent") {
				return false
			}
		case "allowedSandboxModes":
			if !requirementsEnumArray(value, "read-only", "workspace-write", "danger-full-access") {
				return false
			}
		case "allowedWindowsSandboxImplementations":
			if !requirementsEnumArray(value, "elevated", "unelevated", "mxc") {
				return false
			}
		case "allowedWebSearchModes":
			if !requirementsEnumArray(value, "disabled", "cached", "indexed", "live") {
				return false
			}
		case "enforceResidency":
			if !requirementsNull(value) && !requirementsEnum(value, "us") {
				return false
			}
		case "featureRequirements":
			if !requirementsFeatures(value) {
				return false
			}
		case "feedback":
			if !requirementsBoolObject(value, "enabled") {
				return false
			}
		case "inAppBrowser":
			if !requirementsBoolObject(value, "allowExternalBrowserSettingsImport") {
				return false
			}
		case "network":
			if !requirementsNetwork(value) {
				return false
			}
		case "application":
			if !requirementsApplication(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func requirementsObject(data []byte, names ...string) (object, bool) {
	fields, err := startupObject(data)
	if err != nil || len(fields) != len(names) {
		return nil, false
	}
	for _, name := range names {
		if _, found := fields[name]; !found {
			return nil, false
		}
	}
	return object(fields), true
}

func requirementsNull(data json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

func requirementsBool(data json.RawMessage) bool {
	value := bytes.TrimSpace(data)
	return bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))
}

func requirementsNullableBool(data json.RawMessage) bool {
	return requirementsNull(data) || requirementsBool(data)
}

func requirementsEnum(data json.RawMessage, choices ...string) bool {
	var value string
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func requirementsEnumArray(data json.RawMessage, choices ...string) bool {
	if requirementsNull(data) {
		return true
	}
	var values []json.RawMessage
	if json.Unmarshal(data, &values) != nil {
		return false
	}
	for _, value := range values {
		if !requirementsEnum(value, choices...) {
			return false
		}
	}
	return true
}

func requirementsApprovalPolicies(data json.RawMessage) bool {
	if requirementsNull(data) {
		return true
	}
	var values []json.RawMessage
	if json.Unmarshal(data, &values) != nil {
		return false
	}
	for _, value := range values {
		if requirementsEnum(value, "untrusted", "on-request", "never") {
			continue
		}
		policy, ok := requirementsObject(value, "granular")
		if !ok {
			return false
		}
		flags, ok := requirementsObject(policy["granular"], "sandbox_approval", "rules",
			"skill_approval", "request_permissions", "mcp_elicitations")
		if !ok {
			return false
		}
		for _, flag := range flags {
			if !requirementsBool(flag) {
				return false
			}
		}
	}
	return true
}

func requirementsFeatures(data json.RawMessage) bool {
	if requirementsNull(data) {
		return true
	}
	fields, err := startupObject(data)
	if err != nil {
		return false
	}
	for name, value := range fields {
		if (name != "fast_mode" && name != "ultrafast_mode") || !requirementsBool(value) {
			return false
		}
	}
	return true
}

func requirementsBoolObject(data json.RawMessage, name string) bool {
	if requirementsNull(data) {
		return true
	}
	fields, ok := requirementsObject(data, name)
	return ok && requirementsNullableBool(fields[name])
}

func requirementsNetwork(data json.RawMessage) bool {
	if requirementsNull(data) {
		return true
	}
	fields, ok := requirementsObject(data, "enabled", "httpPort", "socksPort",
		"allowUpstreamProxy", "dangerouslyAllowNonLoopbackProxy", "dangerouslyAllowAllUnixSockets",
		"domains", "reviewDomains", "managedAllowedDomainsOnly", "allowedDomains", "deniedDomains",
		"unixSockets", "allowUnixSockets", "allowLocalBinding")
	if !ok {
		return false
	}
	for _, name := range []string{"enabled", "allowUpstreamProxy", "dangerouslyAllowNonLoopbackProxy",
		"dangerouslyAllowAllUnixSockets", "managedAllowedDomainsOnly", "allowLocalBinding"} {
		if !requirementsNullableBool(fields[name]) {
			return false
		}
	}
	for _, name := range []string{"httpPort", "socksPort"} {
		var port uint16
		if !requirementsNull(fields[name]) && json.Unmarshal(fields[name], &port) != nil {
			return false
		}
	}
	if !requirementsNull(fields["unixSockets"]) || !requirementsNull(fields["allowUnixSockets"]) ||
		!requirementsHosts(fields["reviewDomains"], true) {
		return false
	}
	domains, ok := requirementsDomains(fields["domains"], true)
	if !ok {
		return false
	}
	// The pinned mapper emits sorted legacy views, using null for no entries.
	return requirementsDomainView(fields["allowedDomains"], domains, "allow") &&
		requirementsDomainView(fields["deniedDomains"], domains, "deny")
}

func requirementsApplication(data json.RawMessage) bool {
	if requirementsNull(data) {
		return true
	}
	fields, ok := requirementsObject(data, "network", "storage")
	if !ok || !requirementsNull(fields["storage"]) {
		return false
	}
	if requirementsNull(fields["network"]) {
		return true
	}
	network, ok := requirementsObject(fields["network"], "enabled", "domains")
	if !ok || !requirementsBool(network["enabled"]) || requirementsNull(network["domains"]) {
		return false
	}
	_, ok = requirementsDomains(network["domains"], false)
	return ok
}

func requirementsDomains(data json.RawMessage, wildcard bool) (object, bool) {
	if requirementsNull(data) {
		return nil, true
	}
	fields, err := startupObject(data)
	if err != nil {
		return nil, false
	}
	for host, permission := range fields {
		if !requirementsHost(host, wildcard) || !requirementsEnum(permission, "allow", "deny") {
			return nil, false
		}
	}
	return object(fields), true
}

func requirementsDomainView(data json.RawMessage, domains object, permission string) bool {
	var expected []string
	for host, value := range domains {
		if requirementsEnum(value, permission) {
			expected = append(expected, host)
		}
	}
	if len(expected) == 0 {
		return requirementsNull(data)
	}
	sort.Strings(expected)
	var actual []string
	if json.Unmarshal(data, &actual) != nil || len(actual) != len(expected) {
		return false
	}
	for i := range expected {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}

func requirementsHosts(data json.RawMessage, wildcard bool) bool {
	if requirementsNull(data) {
		return true
	}
	var hosts []json.RawMessage
	if json.Unmarshal(data, &hosts) != nil {
		return false
	}
	for _, encoded := range hosts {
		var host string
		if json.Unmarshal(encoded, &host) != nil || !requirementsHost(host, wildcard) {
			return false
		}
	}
	return true
}

// Host support is intentionally narrower than the official pattern language.
// No normalization or URL interpretation can make differing policies agree.
func requirementsHost(host string, wildcard bool) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	if wildcard {
		if strings.HasPrefix(host, "**.") {
			host = strings.TrimPrefix(host, "**.")
		} else {
			host = strings.TrimPrefix(host, "*.")
		}
	}
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	// Numeric terminal labels can invoke IPv4 parsing rather than DNS,
	// including shortened, integer, octal and hexadecimal URL forms.
	if strings.Trim(last, "0123456789") == "" ||
		(strings.HasPrefix(last, "0x") && strings.Trim(last[2:], "0123456789abcdef") == "") {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}
