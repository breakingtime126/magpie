package appserverbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitializeReadyWithoutNotification(t *testing.T) {
	for _, mode := range []string{ConfigRequirementsCompareSafe, ConfigRequirementsCompareNull167} {
		t.Run(mode, func(t *testing.T) {
			cfg := requirementsFixtureConfig(t)
			cfg.RequirementsRead = mode
			policy, _ := parseObject([]byte(requirements167Fixture))
			for _, backend := range cfg.Backends {
				writeRequirementsFixture(t, backend, policy)
			}
			bridge := startFixtureBridge(t, cfg)
			stream := readRequirementsStream(t, bridge)
			init := stream.ask(t, bridge, `{"id":9007199254740993,"method":"initialize","params":{"clientInfo":{"name":"no-notification-fixture","version":"1"},"capabilities":{"experimentalApi":true}}}`)
			if string(init["id"]) != "9007199254740993" || !exists(init, "result") || exists(init, "error") {
				t.Fatal("compiled fixture initialization failed")
			}
			check := func(id int) {
				t.Helper()
				response := stream.ask(t, bridge, fmt.Sprintf(`{"id":%d,"method":"configRequirements/read"}`, id))
				if string(response["id"]) != fmt.Sprint(id) || exists(response, "error") || !sameJSON(response["result"], []byte(requirements167Fixture)) {
					t.Fatalf("requirements blocked or changed after native initialize success: %s", raw(response))
				}
			}
			// Do not send initialized: the very next client operation must work.
			check(2)
			for id := 3; id <= 4; id++ {
				if _, err := fmt.Fprintln(bridge.input, `{"method":"initialized","params":{}}`); err != nil {
					t.Fatal(err)
				}
				check(id)
			}
			if err := bridge.input.Close(); err != nil {
				t.Fatal(err)
			}
			if err := bridge.wait(t); err != nil {
				t.Fatal(err)
			}
			<-stream.done
			if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 4 {
				t.Fatalf("unexpected compiled fixture stdout: %q, %v", stream.transcript, stream.err)
			}
			assertRequirementsFixtureRequests(t, cfg, 3)
			for _, backend := range cfg.Backends {
				data, err := os.ReadFile(filepath.Join(backend.Home, "fixture-requests"))
				if err != nil {
					t.Fatal(err)
				}
				var methods []string
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					message, err := parseObject([]byte(line))
					if err != nil {
						t.Fatal(err)
					}
					methods = append(methods, text(message, "method"))
				}
				want := "initialize,configRequirements/read,initialized,configRequirements/read,initialized,configRequirements/read"
				if strings.Join(methods, ",") != want {
					t.Fatalf("backend %s received fabricated or reordered traffic: %v", backend.ID, methods)
				}
			}
		})
	}
}
