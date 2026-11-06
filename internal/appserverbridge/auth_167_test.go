package appserverbridge

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func auth167Config(t *testing.T) Config {
	t.Helper()
	cfg := authCompatConfig(t)
	cfg.Attestation = AttestationUnavailable167
	cfg.GatewayOAuth = GatewayOAuthPrimaryRead167
	return cfg
}

func TestAuth167ExactIndependentModes(t *testing.T) {
	base := authCompatConfig(t)
	for _, attestation := range []string{"", AttestationUnavailable, AttestationUnavailable167, "unavailable-v0.167", AttestationUnavailable167 + " "} {
		for _, gateway := range []string{"", GatewayOAuthPrimaryRead, GatewayOAuthPrimaryRead167, "primary-read-only-v0.167", GatewayOAuthPrimaryRead167 + " "} {
			for _, probe := range []string{"", StartupProbeCompareUnrestricted, StartupProbeCompareUnrestricted167} {
				for _, requirements := range []string{"", ConfigRequirementsCompareSafe} {
					cfg := base
					cfg.Attestation, cfg.GatewayOAuth, cfg.StartupProbe, cfg.RequirementsRead = attestation, gateway, probe, requirements
					if gateway == "" {
						cfg.GatewayOAuthProviderName = ""
					}
					old := attestation == AttestationUnavailable || gateway == GatewayOAuthPrimaryRead || requirements != "" || probe == StartupProbeCompareUnrestricted
					newer := attestation == AttestationUnavailable167 || gateway == GatewayOAuthPrimaryRead167 || probe == StartupProbeCompareUnrestricted167
					valid := (attestation == "" || attestation == AttestationUnavailable || attestation == AttestationUnavailable167) &&
						(gateway == "" || gateway == GatewayOAuthPrimaryRead || gateway == GatewayOAuthPrimaryRead167) && !(old && newer)
					if err := cfg.Validate(); (err == nil) != valid {
						t.Fatalf("modes %q %q %q %q: %v", attestation, gateway, probe, requirements, err)
					}
				}
			}
		}
	}
}

func TestAuth167RunPrewarmAndPrimaryRead(t *testing.T) {
	// Same native fixture assertions run under both independently selected modes.
	testRunAuthCompatPrewarmAndPrimaryRead(t, auth167Config(t))
}

func TestAuth167ChecksEachPinBeforeEitherChild(t *testing.T) {
	for _, mode := range []string{"attestation", "gateway"} {
		for index := 0; index < 2; index++ {
			for _, pin := range []string{"", strings.Repeat("0", 64)} {
				cfg := auth167Config(t)
				if mode == "attestation" {
					cfg.GatewayOAuth, cfg.GatewayOAuthProviderName = "", ""
				} else {
					cfg.Attestation = ""
				}
				cfg.Backends[index].StartupProbeSHA256 = pin
				var output discardRequirementsOutput
				if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
					t.Fatal("bad pin accepted")
				}
				for _, b := range cfg.Backends {
					for _, marker := range []string{"fixture-report", "fixture-auth-requests", "fixture-auth-clean-exit"} {
						if _, err := os.Stat(filepath.Join(b.Home, marker)); !os.IsNotExist(err) {
							t.Fatal("child started before both pins passed")
						}
					}
				}
			}
		}
	}
}

func TestAuth167HumanApprovalRejectedWithoutMutation(t *testing.T) {
	for _, value := range []string{"true", "null", "0", `"false"`, "{}", "[]"} {
		cfg := Config{Attestation: AttestationUnavailable167, GatewayOAuth: GatewayOAuthPrimaryRead167}
		r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
		if err != nil {
			t.Fatal(err)
		}
		r.auth = cfg.authCompatibility()
		caps := object{"requestAttestation": raw(true), "explicitGatewayOauth": raw(true), "requiredHumanApproval": json.RawMessage(value)}
		before := string(raw(caps))
		if r.auth.negotiate(caps) == nil || before != string(raw(caps)) || r.auth.attestationNegotiated || r.auth.gatewayNegotiated {
			t.Fatal("unsupported human approval accepted, rewritten or partially negotiated")
		}
		out := client(t, r, string(raw(object{"id": raw(1), "method": raw("initialize"), "params": raw(object{"capabilities": raw(caps)})})))
		expectError(t, out)
		if r.phase != "cold" || len(r.pending) != 0 {
			t.Fatal("rejected capability reached child")
		}
	}
}

func TestAuth167PreservesCapabilitiesAndRejectsProhibitedMethods(t *testing.T) {
	cfg := Config{Attestation: AttestationUnavailable167, GatewayOAuth: GatewayOAuthPrimaryRead167, GatewayOAuthProviderName: "OpenAI"}
	r, err := NewRouter([]Backend{{ID: "openai", Provider: "openai"}, {ID: "aihub", Provider: "aihub"}})
	if err != nil {
		t.Fatal(err)
	}
	r.auth = cfg.authCompatibility()
	params := json.RawMessage(`{"clientInfo":{"name":"synthetic-desktop","version":"0"},"capabilities":{"requestAttestation":true,"explicitGatewayOauth":true,"requiredHumanApproval":false,"experimentalApi":true,"optOutNotificationMethods":["turn/diff/updated"],"extensions":{"fixture":{"preserved":true}}}}`)
	out := client(t, r, string(raw(object{"id": raw("init"), "method": raw("initialize"), "params": params})))
	if len(out) != 2 {
		t.Fatal("new auth mode did not initialize")
	}
	for _, d := range out {
		m, _ := parseObject(d.Message)
		if !sameJSON(m["params"], params) {
			t.Fatal("capabilities changed")
		}
	}
	reply(t, r, out[0], `{"userAgent":"first"}`)
	reply(t, r, out[1], `{"userAgent":"second"}`)
	client(t, r, `{"method":"initialized"}`)
	for _, backend := range []string{"openai", "aihub"} {
		got := server(t, r, backend, `{"id":8,"method":"attestation/generate","params":{}}`)
		if len(got) != 1 || got[0].Backend != backend || len(r.threads) != 0 {
			t.Fatal("prewarm failure escaped origin")
		}
		m, _ := parseObject(got[0].Message)
		failure, _ := child(m, "error")
		if string(failure["code"]) != "-32601" || exists(m, "result") {
			t.Fatal("attestation produced a success")
		}
	}
	for _, method := range []string{"configRequirements/read", "config/read", "account/gatewayOAuth/login", "account/gatewayOAuth/cancel", "getAuthStatus", "account/chatgptAuthTokens/refresh"} {
		expectError(t, client(t, r, string(raw(object{"id": raw(method), "method": raw(method)}))))
		if len(r.pending) != 0 {
			t.Fatal("prohibited method reached a child")
		}
	}
	for _, backend := range []string{"openai", "aihub"} {
		line := raw(object{"method": raw("account/gatewayOAuth/changed"), "params": raw(object{"providerId": raw(backend), "status": raw("notReady"), "authUrl": raw(nil), "error": raw(nil)})})
		got := server(t, r, backend, string(line))
		if (backend == "openai" && (len(got) != 1 || got[0].Backend != "")) || (backend == "aihub" && len(got) != 0) {
			t.Fatal("gateway notification authority changed")
		}
		for _, field := range []string{"authUrl", "error"} {
			p := object{"providerId": raw(backend), "status": raw("notReady"), "authUrl": raw(nil), "error": raw(nil)}
			p[field] = raw("synthetic-private-value")
			got, err := r.Server(backend, raw(object{"method": raw("account/gatewayOAuth/changed"), "params": raw(p)}))
			if err == nil || len(got) != 0 {
				t.Fatal("unsafe gateway notification escaped")
			}
		}
	}
}

func TestAuth167RunRejectsUnsafePrimaryResponses(t *testing.T) {
	testRunAuthCompatRejectsUnsafePrimaryResponses(t, auth167Config)
}

func TestAuth167IndependentCapabilitiesAndRawHumanGuard(t *testing.T) {
	for _, cfg := range []Config{
		{Attestation: AttestationUnavailable167},
		{GatewayOAuth: GatewayOAuthPrimaryRead167},
		{StartupProbe: StartupProbeCompareUnrestricted167},
	} {
		for _, capability := range []string{"requestAttestation", "explicitGatewayOauth"} {
			r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
			if err != nil {
				t.Fatal(err)
			}
			r.auth = cfg.authCompatibility()
			out := client(t, r, string(raw(object{"id": raw(1), "method": raw("initialize"), "params": raw(object{"capabilities": raw(object{capability: raw(true)})})})))
			allowed := (capability == "requestAttestation" && cfg.Attestation != "") || (capability == "explicitGatewayOauth" && cfg.GatewayOAuth != "")
			if allowed {
				if len(out) != 2 || len(r.pending) != 2 {
					t.Fatal("independent capability rejected")
				}
			} else {
				expectError(t, out)
				if len(r.pending) != 0 || r.phase != "cold" {
					t.Fatal("one mode enabled another capability")
				}
			}
		}
		for _, fields := range []string{
			`"requiredHumanApproval":true`,
			`"requiredHumanApproval":null`,
			`"requiredHumanApproval":true,"requiredHumanApproval":false`,
			`"requiredHumanApproval":false,"requiredHumanApproval":true`,
		} {
			r, err := NewRouter([]Backend{{ID: "a", Provider: "a"}, {ID: "b", Provider: "b"}})
			if err != nil {
				t.Fatal(err)
			}
			r.auth = cfg.authCompatibility()
			out := client(t, r, `{"id":1,"method":"initialize","params":{"capabilities":{`+fields+`}}}`)
			expectError(t, out)
			if len(r.pending) != 0 || r.phase != "cold" {
				t.Fatal("human approval guard reached a child")
			}
		}
	}
}
