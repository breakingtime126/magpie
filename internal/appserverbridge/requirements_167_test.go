package appserverbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const requirements167Fixture = `{"requirements":null,"supportsIndependentSpeedModes":true}`

func requirements167Router(t *testing.T) *Router {
	t.Helper()
	r := requirementsRouter(t)
	r.configRequirementsNull167 = true
	return r
}

func TestRequirements167SeparateValidatorRejectsEveryNonNullPolicy(t *testing.T) {
	if _, err := validateConfigRequirementsResponse(raw(safeRequirementsFixture())); err != nil {
		t.Fatal("legacy reviewed non-null policy stopped being supported")
	}
	if err := validateConfigRequirementsNull167(raw(safeRequirementsFixture())); err == nil {
		t.Fatal("new null-only mode reused the legacy non-null validator")
	}
	for _, valid := range []string{
		requirements167Fixture,
		` { "supportsIndependentSpeedModes" : true, "requirements" : null } `,
	} {
		if err := validateConfigRequirementsNull167([]byte(valid)); err != nil {
			t.Fatal("native complete null view rejected")
		}
	}
	for _, policy := range []string{`{}`, `[]`, `false`, `0`, `"null"`, string(raw(safeRequirementsFixture()))} {
		if err := validateConfigRequirementsNull167([]byte(`{"requirements":` + policy + `,"supportsIndependentSpeedModes":true}`)); err == nil {
			t.Fatalf("non-null policy accepted: %s", policy)
		}
	}
}

func TestRequirements167PreservesFreshSourceBytesAndNativeParams(t *testing.T) {
	for _, params := range []string{"", `,"params":null`, `,"params":{}`} {
		t.Run(params, func(t *testing.T) {
			r := requirements167Router(t)
			var priorID string
			for i := 0; i < 2; i++ {
				requests := requirementsRequest(t, r, `{"id":9007199254740993,"method":"configRequirements/read"`+params+"}")
				first, _ := parseObject(requests[0].Message)
				if string(first["id"]) == priorID {
					t.Fatal("requirements read reused a prior transport request")
				}
				priorID = string(first["id"])
				// Escaping, order and whitespace are real source bytes; they
				// must not be replaced by a fabricated canonical object.
				source := `{ "requirements": null, "supportsIndependentSpeed\u004dodes": true }`
				if out := reply(t, r, requests[0], source); len(out) != 0 {
					t.Fatal("requirements returned before both fresh responses")
				}
				out := reply(t, r, requests[1], `{"supportsIndependentSpeedModes":true,"requirements":null}`)
				expectRequirementsResult(t, out, json.RawMessage("9007199254740993"), json.RawMessage(requirements167Fixture))
				m, _ := parseObject(out[0].Message)
				var compact bytes.Buffer
				if json.Compact(&compact, []byte(source)) != nil || !bytes.Equal(m["result"], compact.Bytes()) {
					t.Fatalf("validated source bytes were reconstructed: %s", m["result"])
				}
				if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
					t.Fatal("completed read retained pending state")
				}
			}
		})
	}
}

func TestRequirements167InvalidClientEnvelopeNeverFansOut(t *testing.T) {
	for _, line := range []string{
		`{"id":1,"id":2,"method":"configRequirements/read"}`,
		`{"id":1,"method":"model/list","method":"configRequirements/read"}`,
		`{"id":1,"method":"configRequirements/read","method":"model/list"}`,
		`{"id":1,"method":"configRequirements/read","params":{},"params":null}`,
		`{"id":1,"method":"configRequirements/read","params":{"private":"requirements-secret-canary"},"params":{}}`,
		`{"id":1,"method":"configRequirements/read","params":{"extra":null}}`,
		`{"id":1,"method":"configRequirements/read","params":[]}`,
		`{"id":1,"method":"configRequirements/read","params":false}`,
		`{"id":1,"method":"configRequirements/read","jsonrpc":"2.0"}`,
		`{"id":1,"method":"configRequirements/read","error":null}`,
		`{"id":1,"method":"configRequirements/read","result":null}`,
		`{"id":1,"method":"configRequirements/read","unknown":null}`,
		`{"id":1.5,"method":"configRequirements/read"}`,
		`{"id":1e2,"method":"configRequirements/read"}`,
		`{"id":9223372036854775808,"method":"configRequirements/read"}`,
		`{"id":null,"method":"configRequirements/read"}`,
		`{"method":"configRequirements/read"}`,
	} {
		t.Run(line, func(t *testing.T) {
			r := requirements167Router(t)
			out, err := r.Client([]byte(line))
			if err == nil {
				expectError(t, out)
			} else if len(out) != 0 {
				t.Fatal("invalid request produced a delivery alongside fatal error")
			}
			if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
				t.Fatal("invalid requirements envelope reached a child")
			}
		})
	}
}

func TestRequirements167RejectsMalformedResponsesAndDisagreement(t *testing.T) {
	results := []string{
		"null", "[]", "{}",
		`{"requirements":null}`,
		`{"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":false}`,
		`{"requirements":null,"supportsIndependentSpeedModes":null}`,
		`{"requirements":null,"supportsIndependentSpeedModes":"true"}`,
		`{"requirements":null,"supportsIndependentSpeedModes":1}`,
		`{"requirements":{},"supportsIndependentSpeedModes":true}`,
		string(raw(safeRequirementsFixture())),
		`{"requirements":null,"supportsIndependentSpeedModes":true,"unknown":"requirements-secret-canary"}`,
		`{"requirements":{},"requirements":null,"supportsIndependentSpeedModes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":true,"supportsIndependentSpeed\u004dodes":true}`,
		`{"requirements":null,"supportsIndependentSpeedModes":true,"\ud800":null}`,
	}
	for i, result := range results {
		for _, order := range []int{0, 1} {
			t.Run(strconv.Itoa(i)+"/"+strconv.Itoa(order), func(t *testing.T) {
				r := requirements167Router(t)
				requests := requirementsRequest(t, r, `{"id":"reject","method":"configRequirements/read"}`)
				if order == 1 {
					if out := reply(t, r, requests[1], requirements167Fixture); len(out) != 0 {
						t.Fatal("returned policy with one authority missing")
					}
				}
				expectRequirementsError(t, reply(t, r, requests[0], result), raw("reject"))
				if order == 0 && len(reply(t, r, requests[1], requirements167Fixture)) != 0 {
					t.Fatal("late sibling became success after rejection")
				}
				if len(r.pending) != 0 || len(r.active) != 0 || r.activeBytes != 0 {
					t.Fatal("rejected requirements retained pending state")
				}
			})
		}
	}
}

func TestRequirements167RejectsMalformedRawResponseEnvelopes(t *testing.T) {
	for _, kind := range []string{"id duplicate", "result duplicate", "error", "null error", "unknown", "jsonrpc", "missing result", "method", "invalid JSON", "trailing JSON", "oversized", "wrong backend"} {
		t.Run(kind, func(t *testing.T) {
			r := requirements167Router(t)
			requests := requirementsRequest(t, r, `{"id":"envelope","method":"configRequirements/read"}`)
			request, _ := parseObject(requests[0].Message)
			id := string(request["id"])
			line := `{"id":` + id + `,"result":` + requirements167Fixture + "}"
			switch kind {
			case "id duplicate":
				line = `{"id":` + id + `,"id":` + id + `,"result":` + requirements167Fixture + "}"
			case "result duplicate":
				line = strings.TrimSuffix(line, "}") + `,"result":` + requirements167Fixture + "}"
			case "error":
				line = `{"id":` + id + `,"error":{"code":-32000,"message":"requirements-secret-canary"}}`
			case "null error":
				line = strings.TrimSuffix(line, "}") + `,"error":null}`
			case "unknown":
				line = strings.TrimSuffix(line, "}") + `,"unknown":"requirements-secret-canary"}`
			case "jsonrpc":
				line = strings.TrimSuffix(line, "}") + `,"jsonrpc":"2.0"}`
			case "missing result":
				line = `{"id":` + id + "}"
			case "method":
				line = strings.TrimSuffix(line, "}") + `,"method":"account/updated","params":{}}`
			case "invalid JSON":
				line = "{"
			case "trailing JSON":
				line += "{}"
			case "oversized":
				line += strings.Repeat(" ", requirementsJSONLimit)
			}
			backend := requests[0].Backend
			if kind == "wrong backend" {
				backend = requests[1].Backend
			}
			out, err := r.Server(backend, []byte(line))
			if err != nil {
				if len(out) != 0 || strings.Contains(err.Error(), "requirements-secret-canary") {
					t.Fatal("fatal response exposed backend payload")
				}
				return
			}
			expectRequirementsError(t, out, raw("envelope"))
			if len(reply(t, r, requests[1], requirements167Fixture)) != 0 {
				t.Fatal("malformed response allowed a late success")
			}
		})
	}
}

func TestRequirements167ExactIndependentConfigAndPins(t *testing.T) {
	base := requirementsFixtureConfig(t)
	base.RequirementsRead = ConfigRequirementsCompareNull167
	if _, err := LoadConfig(strings.NewReader(string(raw(base)))); err != nil {
		t.Fatal("requirements-only opt-in rejected", err)
	}
	for _, mode := range []string{"compare-null-v0.167", ConfigRequirementsCompareNull167 + " ", "compare-safe-v0.167.0-alpha.3.2"} {
		cfg := base
		cfg.RequirementsRead = mode
		if cfg.Validate() == nil {
			t.Fatal("unreviewed requirements mode accepted")
		}
	}
	for _, attestation := range []string{"", AttestationUnavailable, AttestationUnavailable167} {
		for _, gateway := range []string{"", GatewayOAuthPrimaryRead, GatewayOAuthPrimaryRead167} {
			for _, probe := range []string{"", StartupProbeCompareUnrestricted, StartupProbeCompareUnrestricted167} {
				cfg := base
				cfg.Attestation, cfg.GatewayOAuth, cfg.StartupProbe = attestation, gateway, probe
				if gateway != "" {
					cfg.GatewayOAuthProviderName = "Reviewed Fixture"
				}
				want := attestation != AttestationUnavailable && gateway != GatewayOAuthPrimaryRead && probe != StartupProbeCompareUnrestricted
				if err := cfg.Validate(); (err == nil) != want {
					t.Fatalf("version combination %q %q %q: %v", attestation, gateway, probe, err)
				}
			}
		}
	}
	for _, index := range []int{0, 1} {
		for _, pin := range []string{"", "invalid", strings.Repeat("0", 64)} {
			cfg := base
			cfg.Backends = append([]Backend(nil), base.Backends...)
			cfg.Backends[index].StartupProbeSHA256 = pin
			var output discardRequirementsOutput
			if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
				t.Fatal("invalid pin did not stop both children")
			}
			for _, b := range cfg.Backends {
				if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
					t.Fatal("a child started before both pins passed")
				}
			}
		}
	}
}

func TestRequirements167DoesNotEnableAuthOrUnsupportedRoutes(t *testing.T) {
	cfg := Config{RequirementsRead: ConfigRequirementsCompareNull167}
	for _, capability := range []string{"requestAttestation", "explicitGatewayOauth", "requiredHumanApproval"} {
		r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
		if err != nil {
			t.Fatal(err)
		}
		r.configRequirementsRead, r.configRequirementsNull167 = true, true
		r.auth = cfg.authCompatibility()
		expectError(t, client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{"`+capability+`":true}}}`))
		if len(r.pending) != 0 || r.phase != "cold" {
			t.Fatal("requirements-only mode enabled unsupported capability")
		}
	}
	r := requirements167Router(t)
	for _, method := range []string{"getAuthStatus", "config/read", "account/gatewayOAuth/read", "account/gatewayOAuth/login", "account/gatewayOAuth/cancel", "account/chatgptAuthTokens/refresh"} {
		expectError(t, client(t, r, string(raw(object{"id": raw(method), "method": raw(method)}))))
		if len(r.pending) != 0 {
			t.Fatal("requirements mode widened unrelated routing")
		}
	}
	r.configRequirementsRead = false
	expectError(t, client(t, r, `{"id":"disabled","method":"configRequirements/read"}`))
	if len(r.pending) != 0 {
		t.Fatal("requirements opt-out still reached children")
	}
}

func TestRequirements167HumanApprovalGuardPreservesFalseOnly(t *testing.T) {
	for _, fields := range []string{
		`"requiredHumanApproval":null`,
		`"requiredHumanApproval":"false"`,
		`"requiredHumanApproval":0`,
		`"requiredHumanApproval":{}`,
		`"requiredHumanApproval":true,"requiredHumanApproval":false`,
		`"requiredHumanApproval":false,"requiredHumanApproval":true`,
	} {
		r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
		if err != nil {
			t.Fatal(err)
		}
		r.configRequirementsRead, r.configRequirementsNull167 = true, true
		r.auth = (Config{RequirementsRead: ConfigRequirementsCompareNull167}).authCompatibility()
		expectError(t, client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{`+fields+`}}}`))
		if len(r.pending) != 0 || r.phase != "cold" {
			t.Fatal("malformed human-approval capability reached a child")
		}
	}
	for _, capabilities := range []string{`{}`, `{"requiredHumanApproval":false,"requestAttestation":false,"explicitGatewayOauth":false}`} {
		r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
		if err != nil {
			t.Fatal(err)
		}
		r.configRequirementsRead, r.configRequirementsNull167 = true, true
		r.auth = (Config{RequirementsRead: ConfigRequirementsCompareNull167}).authCompatibility()
		out := client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":`+capabilities+`}}`)
		if len(out) != 2 {
			t.Fatal("requirements-only mode rejected absent or false capabilities")
		}
		for _, d := range out {
			m, _ := parseObject(d.Message)
			params, _ := child(m, "params")
			if !sameJSON(params["capabilities"], []byte(capabilities)) {
				t.Fatal("requirements-only mode rewrote capabilities")
			}
		}
	}
}
