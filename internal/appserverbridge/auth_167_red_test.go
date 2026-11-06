package appserverbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file deliberately uses only APIs and fixture helpers present at
// 31f269b0a6acb9bd5bb7e8d676ca89b797e96e03. Copy this file alone to that base
// for a behavioral red; do not copy auth_167_test.go or replace production code.
func TestExactAuth167LiteralModesNegotiateNativeChildren(t *testing.T) {
	for _, mode := range []string{"attestation", "gateway", "both"} {
		t.Run(mode, func(t *testing.T) {
			cfg := authCompatConfig(t)
			cfg.Attestation = ""
			cfg.GatewayOAuth = ""
			cfg.GatewayOAuthProviderName = ""
			caps := object{"experimentalApi": raw(true), "optOutNotificationMethods": raw([]string{"turn/diff/updated"})}
			if mode == "attestation" || mode == "both" {
				cfg.Attestation = "unavailable-v0.167.0-alpha.3.2"
				caps["requestAttestation"] = raw(true)
			}
			if mode == "gateway" || mode == "both" {
				cfg.GatewayOAuth = "primary-read-only-v0.167.0-alpha.3.2"
				cfg.GatewayOAuthProviderName = "Reviewed Fixture"
				caps["explicitGatewayOauth"] = raw(true)
			}
			// On the old base this is the intended assertion failure, not a
			// compiler error. All binaries and homes belong to synthetic fixtures.
			if err := cfg.Validate(); err != nil {
				t.Fatalf("exact 0.167 %s auth mode must validate: %v", mode, err)
			}
			bridge := startFixtureBridge(t, cfg)
			stream := readRequirementsStream(t, bridge)
			params := object{"clientInfo": raw(object{"name": raw("synthetic-auth-red"), "version": raw("0")}), "capabilities": raw(caps)}
			response := stream.ask(t, bridge, string(raw(object{"id": raw("init"), "method": raw("initialize"), "params": raw(params)})))
			if !exists(response, "result") || exists(response, "error") {
				t.Fatalf("exact 0.167 %s auth mode did not negotiate: %s", mode, raw(response))
			}
			if _, err := fmt.Fprintln(bridge.input, `{"method":"initialized"}`); err != nil {
				t.Fatal(err)
			}
			if err := bridge.input.Close(); err != nil {
				t.Fatal(err)
			}
			if err := bridge.wait(t); err != nil {
				t.Fatal(err)
			}
			<-stream.done
			if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 1 {
				t.Fatal("unexpected synthetic initialization output")
			}
			for _, b := range cfg.Backends {
				data, err := os.ReadFile(filepath.Join(b.Home, "fixture-auth-requests"))
				if err != nil {
					t.Fatal(err)
				}
				initializations := 0
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					message, err := parseObject([]byte(line))
					if err != nil {
						t.Fatal(err)
					}
					if text(message, "method") == "initialize" {
						initializations++
						if !sameJSON(message["params"], raw(params)) {
							t.Fatal("child did not receive unchanged requested capabilities")
						}
					}
				}
				if initializations != 1 {
					t.Fatal("each child must negotiate exactly once")
				}
				exit, err := os.ReadFile(filepath.Join(b.Home, "fixture-auth-clean-exit"))
				if err != nil || string(exit) != "EOF" {
					t.Fatal("synthetic child did not exit on EOF")
				}
			}
		})
	}
}
