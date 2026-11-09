package appserverbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A copied test executable is the only peer. No official runtime, account,
// credential store or network service is involved in these fixtures.
func primaryIdentityFixture() (bool, int) {
	home := os.Getenv("HOME")
	mode, err := os.ReadFile(filepath.Join(home, "fixture-primary-identity"))
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		return true, 71
	}
	fmt.Fprintln(os.Stderr, "PUBLIC-IDENTITY-STDERR-CANARY")
	log, err := os.Create(filepath.Join(home, "identity-requests"))
	if err != nil {
		return true, 72
	}
	defer log.Close()
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request json.RawMessage
		if err := decoder.Decode(&request); err == io.EOF {
			break
		} else if err != nil {
			return true, 73
		}
		fmt.Fprintln(log, string(request))
		var msg object
		if json.Unmarshal(request, &msg) != nil {
			return true, 74
		}
		switch text(msg, "method") {
		case "initialize":
			if string(mode) == "init-error" {
				fmt.Println(`{"id":"identity-init","error":{"code":-1,"message":"PUBLIC-PRIVATE-ERROR"}}`)
				continue
			}
			fmt.Println(string(raw(object{"id": msg["id"], "result": raw(object{
				"userAgent": raw("public-identity-fixture"), "codexHome": raw(os.Getenv("CODEX_HOME")),
				"platformFamily": raw("fixture"), "platformOs": raw("fixture"),
			})})))
		case "initialized":
			continue
		case "getAuthStatus":
			if string(mode) == "hang" {
				if os.WriteFile(filepath.Join(home, "identity-ready"), nil, 0600) != nil {
					return true, 75
				}
				for {
					time.Sleep(time.Hour)
				}
			}
			body, err := os.ReadFile(filepath.Join(home, "identity-response"))
			if err != nil {
				return true, 76
			}
			fmt.Println(string(body))
			if string(mode) == "trailing" {
				fmt.Println(`{"method":"unexpected","params":{}}`)
			}
		default:
			return true, 77
		}
	}
	if os.WriteFile(filepath.Join(home, "identity-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 78
	}
	return true, 0
}

func primaryIdentityConfig(t *testing.T, mode, response string) Config {
	t.Helper()
	cfg := startupProbeFixtureConfig(t)
	cfg.StartupProbe = ""
	for _, b := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-primary-identity"), []byte(mode), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b.Home, "identity-response"), []byte(response), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

const identityConfigured = `{"id":"identity-status","result":{"authMethod":"chatgpt","authToken":null,"requiresOpenaiAuth":true}}`

func primaryIdentityNoSecondary(t *testing.T, cfg Config) {
	t.Helper()
	for _, name := range []string{"fixture-report", "identity-requests", "identity-clean-exit"} {
		if _, err := os.Stat(filepath.Join(cfg.Backends[1].Home, name)); !os.IsNotExist(err) {
			t.Fatal("secondary was started or its state is unknown", name)
		}
	}
}

func TestPrimaryIdentityOnlyQueriesPrimaryAndCleanlyExits(t *testing.T) {
	cfg := primaryIdentityConfig(t, "normal", identityConfigured)
	t.Setenv("OPENAI_API_KEY", "PUBLIC-NOT-A-CREDENTIAL-ENV")
	result, err := InspectPrimaryIdentity167(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got object
	if json.Unmarshal(result, &got) != nil || len(got) != 4 || text(got, "authMethod") != "chatgpt" ||
		text(got, "scope") != "native-reported-identity-only" || string(got["requiresOpenaiAuth"]) != "true" ||
		string(got["schema"]) != "1" || strings.Contains(string(result), "authToken") || strings.Contains(string(result), "PUBLIC-") {
		t.Fatal("identity was not a narrow native metadata projection")
	}
	primaryIdentityNoSecondary(t, cfg)
	data, err := os.ReadFile(filepath.Join(cfg.Backends[0].Home, "identity-requests"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatal("extra native operation")
	}
	methods := []string{"initialize", "initialized", "getAuthStatus"}
	for i, line := range lines {
		var msg object
		if json.Unmarshal([]byte(line), &msg) != nil || text(msg, "method") != methods[i] {
			t.Fatal("handshake order changed")
		}
		if i == 0 {
			params, _ := child(msg, "params")
			if string(params["capabilities"]) != "{}" {
				t.Fatal("unexpected native capability request")
			}
		}
		if i == 2 && string(msg["params"]) != `{"includeToken":false,"refreshToken":false}` {
			t.Fatal("token/refresh flags changed")
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.Backends[0].Home, "identity-clean-exit")); err != nil {
		t.Fatal("no clean EOF")
	}
	reportBytes, err := os.ReadFile(filepath.Join(cfg.Backends[0].Home, "fixture-report"))
	if err != nil {
		t.Fatal(err)
	}
	var report fixtureReport
	if json.Unmarshal(reportBytes, &report) != nil || !reflect.DeepEqual(report.Args, []string{"app-server", "-c", `model_provider="openai"`}) ||
		strings.Contains(strings.Join(report.Environment, "\n"), "PUBLIC-NOT-A-CREDENTIAL-ENV") {
		t.Fatal("launch changed or inherited unrelated credentials")
	}
}

func TestPrimaryIdentityFailsClosedWithoutEchoingNativeMessages(t *testing.T) {
	for name, response := range map[string]string{
		"token": strings.Replace(identityConfigured, `"authToken":null`, `"authToken":"PUBLIC-SECRET-CANARY"`, 1),
		"unknown method": strings.Replace(identityConfigured, `"chatgpt"`, `"new-private-type"`, 1),
		"wrong id": strings.Replace(identityConfigured, `"identity-status"`, `"other"`, 1),
		"duplicate": strings.Replace(identityConfigured, `"authToken":null`, `"authToken":null,"authToken":null`, 1),
		"mixed envelope": strings.Replace(identityConfigured, `"result":`, `"error":{},"result":`, 1),
		"native error": `{"id":"identity-status","error":{"code":-1,"message":"PUBLIC-SECRET-CANARY","data":{"token":"PUBLIC-SECRET-CANARY"}}}`,
		"server request": `{"id":"native","method":"attestation/generate","params":{}}`,
		"notification": `{"method":"account/updated","params":{}}`,
		"oversized": strings.Repeat("x", startupProbeLimit+1),
	} {
		t.Run(name, func(t *testing.T) {
			cfg := primaryIdentityConfig(t, "normal", response)
			result, err := InspectPrimaryIdentity167(context.Background(), cfg)
			if err == nil || result != nil || strings.Contains(err.Error(), "PUBLIC-") {
				t.Fatal("unsupported native result escaped")
			}
			primaryIdentityNoSecondary(t, cfg)
		})
	}
	for _, mode := range []string{"init-error", "trailing"} {
		cfg := primaryIdentityConfig(t, mode, identityConfigured)
		if result, err := InspectPrimaryIdentity167(context.Background(), cfg); err == nil || result != nil {
			t.Fatal("unexpected native message succeeded", mode)
		}
	}
}

func TestPrimaryIdentityAdmissionDoesNotStartEitherChild(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"wrong provider": func(c *Config) { c.Backends[0].Provider = "other" },
		"missing pin": func(c *Config) { c.Backends[0].StartupProbeSHA256 = "" },
		"wrong pin": func(c *Config) { c.Backends[0].StartupProbeSHA256 = strings.Repeat("0", 64) },
		"inherited env": func(c *Config) { c.Backends[0].InheritEnv = []string{"OPENAI_API_KEY"} },
		"token relay mode": func(c *Config) { c.AuthStatus = AuthStatusPrimaryTrustedStdio167 },
		"startup probe": func(c *Config) { c.StartupProbe = StartupProbeCompareUnrestricted167 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := primaryIdentityConfig(t, "normal", identityConfigured)
			mutate(&cfg)
			if result, err := InspectPrimaryIdentity167(context.Background(), cfg); err == nil || result != nil {
				t.Fatal("invalid admission succeeded")
			}
			for _, b := range cfg.Backends {
				if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
					t.Fatal("child started before admission")
				}
			}
		})
	}
}

func TestPrimaryIdentityCancellationAndDeadlineAreFailures(t *testing.T) {
	cfg := primaryIdentityConfig(t, "normal", identityConfigured)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := InspectPrimaryIdentity167(ctx, cfg); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request started or succeeded")
	}
	for _, b := range cfg.Backends {
		if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
			t.Fatal("pre-cancelled child started")
		}
	}
	cfg = primaryIdentityConfig(t, "hang", identityConfigured)
	if result, err := inspectPrimaryIdentity167(context.Background(), cfg, 2*time.Second); result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stalled native response did not time out safely")
	}
	primaryIdentityNoSecondary(t, cfg)
}

func TestPrimaryIdentityCancellationStopsStartedPeer(t *testing.T) {
	cfg := primaryIdentityConfig(t, "hang", identityConfigured)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		_, err := InspectPrimaryIdentity167(ctx, cfg)
		done <- err
	}()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ready := filepath.Join(cfg.Backends[0].Home, "identity-ready")
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-ticker.C:
		case err := <-done:
			t.Fatal("peer did not reach the controlled stall", err)
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("peer did not become ready")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("started query did not report cancellation")
		}
	case <-deadline.C:
		t.Fatal("owned peer cleanup did not finish")
	}
	primaryIdentityNoSecondary(t, cfg)
}

func TestPrimaryIdentityPreservesNativeNullAndMetadata(t *testing.T) {
	for _, native := range []string{
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":true}`,
		`{"authMethod":null,"authToken":null,"requiresOpenaiAuth":false}`,
		`{"authMethod":"apikey","authToken":null,"requiresOpenaiAuth":true}`,
		`{"authMethod":"headers","authToken":null,"requiresOpenaiAuth":true}`,
	} {
		cfg := primaryIdentityConfig(t, "normal", `{"id":"identity-status","result":`+native+`}`)
		result, err := InspectPrimaryIdentity167(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		var expected, got object
		if json.Unmarshal([]byte(native), &expected) != nil || json.Unmarshal(result, &got) != nil ||
			string(expected["authMethod"]) != string(got["authMethod"]) ||
			string(expected["requiresOpenaiAuth"]) != string(got["requiresOpenaiAuth"]) {
			t.Fatal("native null or metadata was replaced by a login assertion")
		}
		primaryIdentityNoSecondary(t, cfg)
	}
}

func TestPrimaryIdentityInitializeShapeIsConservative(t *testing.T) {
	valid := `{"userAgent":"public","codexHome":"/public/test","platformFamily":"unix","platformOs":"linux"}`
	if !primaryIdentityInitialize167([]byte(valid)) {
		t.Fatal("reviewed public shape rejected")
	}
	for _, value := range []string{
		`null`,
		strings.Replace(valid, `"codexHome":"/public/test",`, "", 1),
		strings.Replace(valid, `"userAgent":"public"`, `"userAgent":null`, 1),
		strings.Replace(valid, `"userAgent":"public"`, `"userAgent":"public","authToken":"PUBLIC-CANARY"`, 1),
		strings.Replace(valid, `"platformOs":"linux"`, `"platformOs":"linux","platformOs":"linux"`, 1),
	} {
		if primaryIdentityInitialize167([]byte(value)) {
			t.Fatal("unreviewed initialize shape accepted")
		}
	}
}
