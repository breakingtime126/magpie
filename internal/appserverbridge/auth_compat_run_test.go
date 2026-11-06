package appserverbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// authCompatFixture is a synthetic process, not an official runtime or proof.
func authCompatFixture() (bool, int) {
	home := os.Getenv("HOME")
	data, err := os.ReadFile(filepath.Join(home, "fixture-auth.json"))
	if os.IsNotExist(err) {
		return false, 0
	}
	var settings struct {
		Provider string
	}
	if err != nil || json.Unmarshal(data, &settings) != nil {
		return true, 31
	}
	log, err := os.OpenFile(filepath.Join(home, "fixture-auth-requests"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return true, 32
	}
	defer log.Close()
	fmt.Fprintln(os.Stderr, "auth-secret-stderr-canary")
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	var pendingModel json.RawMessage
	var pending json.RawMessage
	received := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if _, err := fmt.Fprintln(log, string(line)); err != nil {
			return true, 33
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(line, &msg) != nil {
			return true, 34
		}
		var method string
		json.Unmarshal(msg["method"], &method)
		if method == "" {
			if pending == nil || (string(msg["id"]) != `"shared-attestation"` && string(msg["id"]) != "9007199254740993") {
				return true, 35
			}
			var failure struct {
				Code    int
				Message string
			}
			if json.Unmarshal(msg["error"], &failure) != nil || failure.Code != -32601 || failure.Message != "attestation/generate is unavailable through this bridge" || len(msg["result"]) != 0 {
				return true, 36
			}
			received++
			if received == 2 {
				if encoder.Encode(map[string]any{"id": pending, "result": map[string]any{"thread": map[string]any{"id": settings.Provider + "-thread", "modelProvider": settings.Provider, "model": pendingModel, "turns": []any{}}, "model": pendingModel, "modelProvider": settings.Provider}}) != nil {
					return true, 37
				}
				pending = nil
			}
			continue
		}
		if method == "initialized" {
			continue
		}
		var result any = map[string]any{}
		switch method {
		case "initialize":
			result = map[string]string{"userAgent": "auth-fixture", "platformFamily": "fixture", "platformOs": "fixture"}
		case "thread/start":
			var params map[string]json.RawMessage
			json.Unmarshal(msg["params"], &params)
			pendingModel = params["model"]
			pending = msg["id"]
			received = 0
			for _, id := range []json.RawMessage{json.RawMessage(`"shared-attestation"`), json.RawMessage("9007199254740993")} {
				if encoder.Encode(map[string]any{"id": id, "method": "attestation/generate", "params": map[string]any{}}) != nil {
					return true, 38
				}
			}
			continue
		case "account/gatewayOAuth/read":
			response, err := os.ReadFile(filepath.Join(home, "fixture-auth-response.json"))
			if err != nil {
				return true, 39
			}
			result = json.RawMessage(response)
		case "account/gatewayOAuth/login", "account/gatewayOAuth/cancel":
			fmt.Fprintln(os.Stderr, "auth-secret-forbidden-call")
			return true, 40
		}
		if encoder.Encode(map[string]any{"id": msg["id"], "result": result}) != nil {
			return true, 41
		}
	}
	if scanner.Err() != nil {
		return true, 42
	}
	if os.WriteFile(filepath.Join(home, "fixture-auth-clean-exit"), []byte("EOF"), 0600) != nil {
		return true, 43
	}
	return true, 0
}

func authCompatConfig(t *testing.T) Config {
	t.Helper()
	cfg := requirementsFixtureConfig(t)
	cfg.RequirementsRead = ""
	cfg.Attestation = AttestationUnavailable
	cfg.GatewayOAuth = GatewayOAuthPrimaryRead
	cfg.GatewayOAuthProviderName = "Reviewed Fixture"
	for i := range cfg.Backends {
		b := &cfg.Backends[i]
		// Separate synthetic providers also give their thread IDs distinct
		// namespaces; real cross-backend thread collisions must stay fatal.
		b.Provider = "fixture-" + b.ID
		b.Models = []json.RawMessage{fixtureModel(b.ID+"_model", true)}
		data, _ := json.Marshal(map[string]string{"Provider": b.Provider})
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-auth.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func writeAuthCompatResponse(t *testing.T, cfg Config, required bool, status any) object {
	t.Helper()
	result := object{"providerId": raw(cfg.Backends[0].Provider), "providerName": raw(cfg.GatewayOAuthProviderName), "required": raw(required), "status": raw(status), "error": raw(nil)}
	if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-response.json"), raw(result), 0600); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRunAuthCompatPrewarmAndPrimaryRead(t *testing.T) {
	cfg := authCompatConfig(t)
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	response := stream.ask(t, bridge, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"auth-test","version":"1"},"capabilities":{"requestAttestation":true,"explicitGatewayOauth":true}}}`)
	if !exists(response, "result") {
		t.Fatal("auth capabilities did not initialize")
	}
	fmt.Fprintln(bridge.input, `{"method":"initialized"}`)
	models := stream.ask(t, bridge, `{"id":2,"method":"model/list","params":{}}`)
	catalog, err := child(models, "result")
	var cards []object
	if string(models["id"]) != "2" || exists(models, "error") || err != nil || json.Unmarshal(catalog["data"], &cards) != nil || len(cards) != len(cfg.Backends) {
		t.Fatal("auth fixture model discovery failed")
	}
	for _, backend := range cfg.Backends {
		found := false
		for _, card := range cards {
			if text(card, "model") == alias(backend.ID, backend.ID+"_model") {
				found = true
			}
		}
		if !found {
			t.Fatalf("auth fixture model missing for %s", backend.ID)
		}
	}
	for i, backend := range cfg.Backends {
		request := object{"id": raw(10 + i), "method": raw("thread/start"), "params": raw(object{"model": raw(alias(backend.ID, backend.ID+"_model"))})}
		reply := stream.ask(t, bridge, string(raw(request)))
		if exists(reply, "method") || exists(reply, "error") || !exists(reply, "result") {
			t.Fatalf("attestation prewarm escaped or failed: %s", raw(reply))
		}
	}
	for i, status := range []any{nil, "notReady", "started", "succeeded", "failed"} {
		want := writeAuthCompatResponse(t, cfg, i != 0, status)
		params := []string{"", `,"params":null`, `,"params":{}`}[i%3]
		response := stream.ask(t, bridge, fmt.Sprintf(`{"id":"read-%d","method":"account/gatewayOAuth/read"%s}`, i, params))
		if string(response["id"]) != string(raw(fmt.Sprintf("read-%d", i))) || !sameJSON(response["result"], raw(want)) || exists(response, "error") {
			t.Fatalf("primary truth or ID changed: %s", raw(response))
		}
	}
	for _, method := range []string{"account/gatewayOAuth/login", "account/gatewayOAuth/cancel"} {
		response := stream.ask(t, bridge, string(raw(object{"id": raw(method), "method": raw(method), "params": raw(nil)})))
		if !exists(response, "error") || exists(response, "result") {
			t.Fatal("unsupported auth mutation accepted")
		}
	}
	bridge.input.Close()
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 11 || strings.Contains(string(stream.transcript), "attestation/generate") || strings.Contains(string(stream.transcript), "secret") {
		t.Fatalf("unexpected client auth traffic: %q", stream.transcript)
	}
	for i, b := range cfg.Backends {
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-auth-requests"))
		if err != nil {
			t.Fatal(err)
		}
		reads, failures := 0, 0
		ids := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			msg, err := parseObject([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			switch text(msg, "method") {
			case "initialize":
				p, _ := child(msg, "params")
				caps, _ := child(p, "capabilities")
				if string(caps["requestAttestation"]) != "true" || string(caps["explicitGatewayOauth"]) != "true" {
					t.Fatal("capability dropped before child")
				}
			case "account/gatewayOAuth/read":
				reads++
				if string(msg["params"]) != "null" {
					t.Fatal("gateway params not canonical null")
				}
			case "account/gatewayOAuth/login", "account/gatewayOAuth/cancel":
				t.Fatal("auth mutation reached child")
			case "":
				failures++
				ids[string(msg["id"])] = true
				failure, _ := child(msg, "error")
				if string(failure["code"]) != "-32601" || text(failure, "message") != "attestation/generate is unavailable through this bridge" || exists(msg, "result") {
					t.Fatal("child received fabricated proof rather than error")
				}
			}
		}
		wantReads := 0
		if i == 0 {
			wantReads = 5
		}
		if reads != wantReads || failures != 2 || !ids[`"shared-attestation"`] || !ids["9007199254740993"] {
			t.Fatalf("backend %s reads=%d failures=%d ids=%v", b.ID, reads, failures, ids)
		}
		if _, err := os.Stat(filepath.Join(b.Home, "fixture-auth-clean-exit")); err != nil {
			t.Fatal("child did not exit on EOF", err)
		}
	}
}

func TestRunAuthCompatChecksBothPinsAndModes(t *testing.T) {
	for _, mode := range []string{"attestation", "gateway"} {
		for _, pin := range []string{"", strings.Repeat("0", 64)} {
			t.Run(mode+pin, func(t *testing.T) {
				cfg := authCompatConfig(t)
				if mode == "attestation" {
					cfg.GatewayOAuth = ""
					cfg.GatewayOAuthProviderName = ""
				} else {
					cfg.Attestation = ""
				}
				cfg.Backends[1].StartupProbeSHA256 = pin
				var output discardRequirementsOutput
				if err := Run(context.Background(), cfg, io.NopCloser(strings.NewReader("")), &output); err == nil || len(output.data) != 0 {
					t.Fatal("bad second pin accepted")
				}
				for _, b := range cfg.Backends {
					if _, err := os.Stat(filepath.Join(b.Home, "fixture-report")); !os.IsNotExist(err) {
						t.Fatal("child started before both auth pins passed")
					}
				}
			})
		}
		cfg := authCompatConfig(t)
		if mode == "attestation" {
			cfg.Attestation = "unknown"
		} else {
			cfg.GatewayOAuth = "unknown"
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("unknown auth mode accepted")
		}
	}
}

func TestRunAuthCompatRejectsUnsafePrimaryResponses(t *testing.T) {
	for _, patch := range []object{
		{"error": raw("auth-secret-error")},
		{"authUrl": raw("https://auth-secret.invalid/")},
		{"required": raw(false), "status": raw("succeeded")},
		{"required": raw(true), "status": raw(nil)},
		{"providerId": raw("other")},
		{"providerName": raw("unreviewed")},
	} {
		t.Run(string(raw(patch)), func(t *testing.T) {
			cfg := authCompatConfig(t)
			cfg.Attestation = ""
			result := writeAuthCompatResponse(t, cfg, true, "notReady")
			for k, v := range patch {
				result[k] = v
			}
			if err := os.WriteFile(filepath.Join(cfg.Backends[0].Home, "fixture-auth-response.json"), raw(result), 0600); err != nil {
				t.Fatal(err)
			}
			bridge := startFixtureBridge(t, cfg)
			stream := readRequirementsStream(t, bridge)
			init := stream.ask(t, bridge, `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"auth-test","version":"1"},"capabilities":{"explicitGatewayOauth":true}}}`)
			if !exists(init, "result") {
				t.Fatal("gateway initialization failed")
			}
			fmt.Fprintln(bridge.input, `{"method":"initialized"}`)
			response := stream.ask(t, bridge, `{"id":2,"method":"account/gatewayOAuth/read"}`)
			if !exists(response, "error") || exists(response, "result") {
				t.Fatal("unsafe gateway response accepted")
			}
			bridge.input.Close()
			if err := bridge.wait(t); err != nil {
				t.Fatal(err)
			}
			<-stream.done
			if strings.Contains(string(stream.transcript), "auth-secret") || strings.Contains(string(stream.transcript), "https://") {
				t.Fatal("raw gateway error or URL leaked")
			}
		})
	}
}
