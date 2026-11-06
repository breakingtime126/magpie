package appserverbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The field inventory and default auth list come from the pinned Rust
// ConfigRequirements and map_requirements_to_api, not the stable TS projection.
// Oracle: https://github.com/openai/codex/blob/4f093f2748667f3b0e6a07e3d4055121cbd3ce64/codex-rs/app-server-protocol/src/protocol/v2/config.rs
func safeRequirementsFixture() object {
	policy := object{}
	for _, name := range strings.Fields(
		"modelProvider modelProviders allowedLoginMethods cliAuthCredentialsStore chatgptBaseUrl " +
			"additionalDeveloperInstructions allowedApprovalPolicies allowedApprovalsReviewers " +
			"allowedSandboxModes allowedWindowsSandboxImplementations allowedPermissionProfiles " +
			"defaultPermissions allowedWebSearchModes allowManagedHooksOnly allowBrowserAndComputerUse " +
			"allowAppshots allowRemoteControl computerUse browserUse inAppBrowser featureRequirements " +
			"hooks enforceResidency network application autoReview models sqliteHome logDir " +
			"modelCatalogJson checkForUpdateOnStartup allowLoginShell feedback otel") {
		policy[name] = raw(nil)
	}
	policy["allowedLoginMethods"] = raw([]string{"api", "chatgpt"})
	policy["allowRemoteControl"] = raw(false)
	return object{"supportsIndependentSpeedModes": raw(true), "requirements": raw(policy)}
}

// Oracle: config_rpc.rs::config_requirements_read_includes_network_review_domains
// at the same pinned official commit. The legacy arrays are derived views.
func safeRequirementsNetworkFixture() object {
	network := object{}
	for _, name := range strings.Fields(
		"enabled httpPort socksPort allowUpstreamProxy dangerouslyAllowNonLoopbackProxy " +
			"dangerouslyAllowAllUnixSockets domains reviewDomains managedAllowedDomainsOnly " +
			"allowedDomains deniedDomains unixSockets allowUnixSockets allowLocalBinding") {
		network[name] = raw(nil)
	}
	network["enabled"] = raw(true)
	network["domains"] = raw(map[string]string{"**.example.com": "allow"})
	network["reviewDomains"] = raw([]string{"review.example.com"})
	network["allowedDomains"] = raw([]string{"**.example.com"})
	return network
}

func setRequirementsFixtureField(result object, name string, value any) {
	policy, _ := child(result, "requirements")
	policy[name] = raw(value)
	result["requirements"] = raw(policy)
}

func assertRequirementsPreserved(t *testing.T, input object) {
	t.Helper()
	encoded := raw(input)
	result, err := validateConfigRequirementsResponse(encoded)
	if err != nil || result == nil {
		t.Fatal("supported complete requirements were rejected")
	}
	var before, after any
	if json.Unmarshal(encoded, &before) != nil || json.Unmarshal(raw(result), &after) != nil ||
		!reflect.DeepEqual(before, after) {
		t.Fatal("accepted requirements were projected, normalized or replaced")
	}
}

func assertRequirementsRejected(t *testing.T, data []byte) {
	t.Helper()
	result, err := validateConfigRequirementsResponse(data)
	if result != nil || err != errConfigRequirements {
		t.Fatal("unsupported requirements succeeded, returned a partial object or exposed raw input")
	}
}

func TestConfigRequirementsPreservesFullPinnedFixtures(t *testing.T) {
	assertRequirementsPreserved(t, object{"supportsIndependentSpeedModes": raw(true), "requirements": raw(nil)})
	base := safeRequirementsFixture()
	policy, _ := child(base, "requirements")
	if len(policy) != 34 {
		t.Fatal("fixture is not the complete pinned requirements object")
	}
	assertRequirementsPreserved(t, base)
	network := safeRequirementsFixture()
	setRequirementsFixtureField(network, "network", safeRequirementsNetworkFixture())
	setRequirementsFixtureField(network, "featureRequirements", map[string]bool{"fast_mode": false, "ultrafast_mode": true})
	assertRequirementsPreserved(t, network)
	for _, enabled := range []bool{false, true} {
		application := safeRequirementsFixture()
		setRequirementsFixtureField(application, "application", map[string]any{
			"network": map[string]any{"enabled": enabled, "domains": map[string]string{}},
			"storage": nil,
		})
		assertRequirementsPreserved(t, application)
	}
	application := safeRequirementsFixture()
	setRequirementsFixtureField(application, "network", safeRequirementsNetworkFixture())
	setRequirementsFixtureField(application, "application", map[string]any{
		"network": map[string]any{"enabled": true, "domains": map[string]string{"gov.example.com": "allow", "github.com": "deny"}},
		"storage": nil,
	})
	assertRequirementsPreserved(t, application)
}

func TestConfigRequirementsPreservesTypedSubset(t *testing.T) {
	for _, name := range []string{"allowManagedHooksOnly", "allowBrowserAndComputerUse", "allowAppshots",
		"allowRemoteControl", "checkForUpdateOnStartup", "allowLoginShell"} {
		for _, value := range []bool{false, true} {
			result := safeRequirementsFixture()
			setRequirementsFixtureField(result, name, value)
			assertRequirementsPreserved(t, result)
		}
	}
	for name, values := range map[string][]string{
		"allowedLoginMethods":                  {"api", "chatgpt"},
		"allowedApprovalsReviewers":            {"user", "auto_review", "guardian_subagent"},
		"allowedSandboxModes":                  {"read-only", "workspace-write", "danger-full-access"},
		"allowedWindowsSandboxImplementations": {"elevated", "unelevated", "mxc"},
		"allowedWebSearchModes":                {"disabled", "cached", "indexed", "live"},
		"allowedApprovalPolicies":              {"untrusted", "on-request", "never"},
	} {
		for _, list := range [][]string{values, {}} {
			result := safeRequirementsFixture()
			setRequirementsFixtureField(result, name, list)
			assertRequirementsPreserved(t, result)
		}
	}
	for _, value := range []string{"file", "keyring", "auto", "ephemeral"} {
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "cliAuthCredentialsStore", value)
		assertRequirementsPreserved(t, result)
	}
	result := safeRequirementsFixture()
	setRequirementsFixtureField(result, "enforceResidency", "us")
	setRequirementsFixtureField(result, "feedback", map[string]any{"enabled": false})
	setRequirementsFixtureField(result, "inAppBrowser", map[string]any{"allowExternalBrowserSettingsImport": false})
	setRequirementsFixtureField(result, "allowedApprovalPolicies", []any{
		map[string]any{"granular": map[string]bool{
			"sandbox_approval":    true,
			"rules":               false,
			"skill_approval":      true,
			"request_permissions": false,
			"mcp_elicitations":    true,
		}},
		"never",
	})
	assertRequirementsPreserved(t, result)
	for _, field := range []string{"featureRequirements", "application"} {
		result := safeRequirementsFixture()
		if field == "featureRequirements" {
			setRequirementsFixtureField(result, field, map[string]bool{})
		} else {
			setRequirementsFixtureField(result, field, map[string]any{"network": nil, "storage": nil})
		}
		assertRequirementsPreserved(t, result)
	}
}

// This named test must fail if the null-only unsupported-field guard is
// removed. The payloads are synthetic canaries, never real credentials.
func TestConfigRequirementsRejectsUnsupportedNonNullFields(t *testing.T) {
	for _, name := range strings.Fields(
		"modelProvider modelProviders chatgptBaseUrl additionalDeveloperInstructions " +
			"allowedPermissionProfiles defaultPermissions computerUse browserUse hooks autoReview " +
			"models sqliteHome logDir modelCatalogJson otel") {
		t.Run(name, func(t *testing.T) {
			for _, value := range []any{"SYNTHETIC_REQUIREMENTS_CANARY", "", map[string]any{}, []any{}, false, 0} {
				result := safeRequirementsFixture()
				setRequirementsFixtureField(result, name, value)
				assertRequirementsRejected(t, raw(result))
			}
		})
	}
	for name, value := range map[string]any{
		"modelProviders": map[string]any{"fixture": map[string]string{"experimental_bearer_token": "SYNTHETIC_PROVIDER_CANARY"}},
		"browserUse":     map[string]any{"extension": map[string]any{"requestHeaders": []any{map[string]string{"name": "x-fixture", "value": "SYNTHETIC_HEADER_CANARY"}}}},
		"otel":           map[string]any{"exporter": map[string]any{"otlp-http": map[string]any{"endpoint": "https://fixture.example.test", "headers": map[string]string{"x-fixture": "[redacted]"}}}},
	} {
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, name, value)
		assertRequirementsRejected(t, raw(result))
	}
}

func TestConfigRequirementsRejectsMissingUnknownAndWrongTypes(t *testing.T) {
	base := safeRequirementsFixture()
	policy, _ := child(base, "requirements")
	for name := range policy {
		result := safeRequirementsFixture()
		fields, _ := child(result, "requirements")
		delete(fields, name)
		result["requirements"] = raw(fields)
		assertRequirementsRejected(t, raw(result))
	}
	result := safeRequirementsFixture()
	fields, _ := child(result, "requirements")
	delete(fields, "modelProviders")
	fields["futurePolicy"] = raw(nil)
	result["requirements"] = raw(fields)
	assertRequirementsRejected(t, raw(result))
	for _, name := range []string{"requirements", "supportsIndependentSpeedModes"} {
		result := safeRequirementsFixture()
		delete(result, name)
		assertRequirementsRejected(t, raw(result))
	}
	for _, value := range []any{nil, "SYNTHETIC_UNKNOWN_CANARY", true, map[string]any{}} {
		result := safeRequirementsFixture()
		result["unknown"] = raw(value)
		assertRequirementsRejected(t, raw(result))
		result = safeRequirementsFixture()
		setRequirementsFixtureField(result, "unknown", value)
		assertRequirementsRejected(t, raw(result))
	}
	for _, value := range []any{nil, false, 1, "true"} {
		result := safeRequirementsFixture()
		result["supportsIndependentSpeedModes"] = raw(value)
		assertRequirementsRejected(t, raw(result))
	}
	for _, value := range []any{map[string]any{}, []any{}, true, false, 0, "null"} {
		result := safeRequirementsFixture()
		result["requirements"] = raw(value)
		assertRequirementsRejected(t, raw(result))
	}
	for name, values := range map[string][]any{
		"allowRemoteControl":                   {0, "false", []any{}},
		"allowedLoginMethods":                  {"api", []any{"saml"}, []any{nil}, []any{false}},
		"cliAuthCredentialsStore":              {"unknown", []any{}, false},
		"allowedApprovalPolicies":              {[]any{"on-failure"}, []any{nil}, map[string]any{}},
		"allowedApprovalsReviewers":            {[]any{"future"}, []any{0}},
		"allowedSandboxModes":                  {[]any{"writable"}, []any{false}},
		"allowedWindowsSandboxImplementations": {[]any{"future"}, []any{nil}},
		"allowedWebSearchModes":                {[]any{"future"}, []any{0}},
		"enforceResidency":                     {"eu", false, []any{}},
		"featureRequirements":                  {map[string]any{"future": false}, map[string]any{"fast_mode": nil}},
		"feedback":                             {map[string]any{}, map[string]any{"enabled": 0}, map[string]any{"enabled": false, "extra": nil}},
		"inAppBrowser":                         {map[string]any{"allowExternalBrowserSettingsImport": "false"}},
	} {
		for _, value := range values {
			result := safeRequirementsFixture()
			setRequirementsFixtureField(result, name, value)
			assertRequirementsRejected(t, raw(result))
		}
	}
}

func TestConfigRequirementsRejectsGranularPolicyShapeChanges(t *testing.T) {
	valid := map[string]any{
		"sandbox_approval":    true,
		"rules":               false,
		"skill_approval":      true,
		"request_permissions": false,
		"mcp_elicitations":    true,
	}
	for _, name := range []string{"sandbox_approval", "rules", "skill_approval", "request_permissions", "mcp_elicitations", "extra"} {
		flags := map[string]any{}
		for key, value := range valid {
			flags[key] = value
		}
		if name == "extra" {
			flags[name] = false
		} else {
			delete(flags, name)
		}
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "allowedApprovalPolicies", []any{map[string]any{"granular": flags}})
		assertRequirementsRejected(t, raw(result))
	}
	for _, invalid := range []any{nil, "false", 0} {
		flags := map[string]any{}
		for name, value := range valid {
			flags[name] = value
		}
		flags["rules"] = invalid
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "allowedApprovalPolicies", []any{map[string]any{"granular": flags}})
		assertRequirementsRejected(t, raw(result))
	}
}

func TestConfigRequirementsNetworkPreservesBoundsAndDerivedViews(t *testing.T) {
	for _, port := range []uint16{0, 65535} {
		network := safeRequirementsNetworkFixture()
		network["httpPort"] = raw(port)
		network["socksPort"] = raw(port)
		for _, name := range []string{"allowUpstreamProxy", "dangerouslyAllowNonLoopbackProxy",
			"dangerouslyAllowAllUnixSockets", "managedAllowedDomainsOnly", "allowLocalBinding"} {
			network[name] = raw(false)
		}
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "network", network)
		assertRequirementsPreserved(t, result)
	}
	network := safeRequirementsNetworkFixture()
	network["domains"] = raw(map[string]string{"z.example.com": "allow", "a.example.com": "deny", "*.example.net": "allow"})
	network["allowedDomains"] = raw([]string{"*.example.net", "z.example.com"})
	network["deniedDomains"] = raw([]string{"a.example.com"})
	result := safeRequirementsFixture()
	setRequirementsFixtureField(result, "network", network)
	assertRequirementsPreserved(t, result)
	for _, domains := range []any{nil, map[string]string{}} {
		network := safeRequirementsNetworkFixture()
		network["domains"] = raw(domains)
		network["allowedDomains"] = raw(nil)
		network["reviewDomains"] = raw([]string{})
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "network", network)
		assertRequirementsPreserved(t, result)
	}
}

func TestConfigRequirementsRejectsNetworkShapeAndInconsistentViews(t *testing.T) {
	for name := range safeRequirementsNetworkFixture() {
		network := safeRequirementsNetworkFixture()
		delete(network, name)
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "network", network)
		assertRequirementsRejected(t, raw(result))
	}
	for name, values := range map[string][]any{
		"unknown":          {nil, false},
		"enabled":          {0, "true"},
		"httpPort":         {-1, 65536, 1.5, true, "80"},
		"socksPort":        {-1, 65536},
		"unixSockets":      {map[string]string{}, map[string]string{"/synthetic/socket": "allow"}},
		"allowUnixSockets": {[]string{}, []string{"/synthetic/socket"}},
		"domains":          {map[string]string{"**.example.com": "future"}, map[string]any{"**.example.com": nil}, nil},
		"reviewDomains":    {[]any{nil}, []string{"https://review.example.com"}},
		"allowedDomains":   {nil, []string{}, []string{"different.example.com"}, []string{"**.example.com", "**.example.com"}},
		"deniedDomains":    {[]string{}, []string{"**.example.com"}},
	} {
		for _, value := range values {
			network := safeRequirementsNetworkFixture()
			network[name] = raw(value)
			result := safeRequirementsFixture()
			setRequirementsFixtureField(result, "network", network)
			assertRequirementsRejected(t, raw(result))
		}
	}
	network := safeRequirementsNetworkFixture()
	network["httpPort"] = json.RawMessage("1e2")
	result := safeRequirementsFixture()
	setRequirementsFixtureField(result, "network", network)
	assertRequirementsRejected(t, raw(result))
}

func TestConfigRequirementsPreservesDNSNamesWithNumericLabels(t *testing.T) {
	network := safeRequirementsNetworkFixture()
	network["domains"] = raw(map[string]string{"123.example.com": "allow"})
	network["allowedDomains"] = raw([]string{"123.example.com"})
	result := safeRequirementsFixture()
	setRequirementsFixtureField(result, "network", network)
	setRequirementsFixtureField(result, "application", map[string]any{
		"network": map[string]any{"enabled": true, "domains": map[string]string{"123.example.com": "allow"}},
		"storage": nil,
	})
	assertRequirementsPreserved(t, result)
}

func TestConfigRequirementsRejectsUnsupportedHostSyntax(t *testing.T) {
	hosts := []string{"", "*", "*.example..com", "UPPER.example.com", "example.com.", "-bad.example.com",
		"bad-.example.com", "bad_name.example.com", "https://example.com", "name@example.com",
		"example.com:443", "example.com/path", "example.com?x=y", "example.com#fragment",
		" example.com", "例.example.com", "[::1]", "127.0.0.1", "127.1", "2130706433",
		"0177.0.0.1", "0x7f000001", "0x7f.1", "0x", strings.Repeat("x", 64) + ".example.com",
		strings.Repeat("a.", 127) + "a"}
	for _, host := range hosts {
		network := safeRequirementsNetworkFixture()
		network["domains"] = raw(map[string]string{host: "allow"})
		network["allowedDomains"] = raw([]string{host})
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "network", network)
		assertRequirementsRejected(t, raw(result))
	}
	for _, domains := range []any{nil, map[string]string{"*.example.com": "allow"}, map[string]string{"example.com": "future"}} {
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "application", map[string]any{
			"network": map[string]any{"enabled": true, "domains": domains},
			"storage": nil,
		})
		assertRequirementsRejected(t, raw(result))
	}
	for _, application := range []any{
		map[string]any{"network": nil},
		map[string]any{"network": nil, "storage": map[string]any{}},
		map[string]any{"network": map[string]any{"enabled": nil, "domains": map[string]string{}}, "storage": nil},
		map[string]any{"network": nil, "storage": nil, "extra": nil},
	} {
		result := safeRequirementsFixture()
		setRequirementsFixtureField(result, "application", application)
		assertRequirementsRejected(t, raw(result))
	}
}

func TestRequirementsJSONRejectsNestedDuplicatesAndMalformedInput(t *testing.T) {
	bad := []string{
		"",
		"null null",
		"{}{}",
		"{",
		"[] trailing",
		"{\"id\":1,\"id\":2,\"result\":null}",
		"{\"result\":{\"requirements\":null,\"requirements\":{}}}",
		"{\"result\":{\"network\":{\"enabled\":true,\"enabled\":false}}}",
		"{\"array\":[{\"x\":true,\"x\":false}]}",
		"{\"x\":0,\"\\u0078\":1}",
		"{\"x\":\"\\ud800\"}",
		"{\"\\udfff\":null}",
		"{\"x\":\"\\u0000\"}",
		"{\"x\":\"\\ufffd\"}",
		string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
	}
	for _, input := range bad {
		if validateRequirementsJSON([]byte(input)) != errConfigRequirements {
			t.Fatal("nested duplicate or ambiguous malformed input was accepted")
		}
		assertRequirementsRejected(t, []byte(input))
	}
	base := string(raw(safeRequirementsFixture()))
	for _, input := range []string{
		strings.Replace(base, "\"requirements\":", "\"requirements\":null,\"requirements\":", 1),
		strings.Replace(base, "\"allowRemoteControl\":false", "\"allowRemoteControl\":false,\"allowRemoteControl\":false", 1),
	} {
		assertRequirementsRejected(t, []byte(input))
	}
}

func TestRequirementsJSONBoundsBeforeMaterializingValues(t *testing.T) {
	array := func(count int) string {
		return "[" + strings.TrimSuffix(strings.Repeat("null,", count), ",") + "]"
	}
	objectJSON := func(count int) string {
		parts := make([]string, count)
		for i := range parts {
			parts[i] = fmt.Sprintf("\"k%d\":null", i)
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	atLimit := "{}" + strings.Repeat(" ", requirementsJSONLimit-2)
	for _, input := range []string{
		atLimit,
		array(requirementsContainerLimit),
		objectJSON(requirementsContainerLimit),
		strings.Repeat("[", requirementsJSONDepth) + "null" + strings.Repeat("]", requirementsJSONDepth),
		"{\"result\":{\"values\":" + array(requirementsContainerLimit) + "}}",
		"{\"unicode\":\"\\ud83d\\ude00\"}",
	} {
		if validateRequirementsJSON([]byte(input)) != nil {
			t.Fatal("documented structural boundary was rejected")
		}
	}
	for _, input := range []string{
		atLimit + " ",
		array(requirementsContainerLimit + 1),
		objectJSON(requirementsContainerLimit + 1),
		strings.Repeat("[", requirementsJSONDepth+1) + "null" + strings.Repeat("]", requirementsJSONDepth+1),
		"{\"result\":{\"values\":" + array(requirementsContainerLimit+1) + "}}",
		"{\"result\":{\"values\":" + objectJSON(requirementsContainerLimit+1) + "}}",
	} {
		if validateRequirementsJSON([]byte(input)) != errConfigRequirements {
			t.Fatal("oversized nested JSON was accepted")
		}
	}
	valid := raw(safeRequirementsFixture())
	padded := append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), requirementsJSONLimit-len(valid))...)
	if _, err := validateConfigRequirementsResponse(padded); err != nil {
		t.Fatal("valid result at the exact byte limit was rejected")
	}
	assertRequirementsRejected(t, append(padded, ' '))
}

func TestConfigRequirementsRejectsDeepAndWideUnsupportedBranches(t *testing.T) {
	for _, branch := range []string{
		strings.Repeat("[", requirementsJSONDepth+1) + "null" + strings.Repeat("]", requirementsJSONDepth+1),
		"[" + strings.TrimSuffix(strings.Repeat("null,", requirementsContainerLimit+1), ",") + "]",
	} {
		result := safeRequirementsFixture()
		policy, _ := child(result, "requirements")
		policy["modelProviders"] = json.RawMessage(branch)
		result["requirements"] = raw(policy)
		assertRequirementsRejected(t, raw(result))
	}
}
