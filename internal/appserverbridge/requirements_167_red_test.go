package appserverbridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file uses only APIs and fixture helpers present at
// e1c8fb52c61406baba1a6d712c0fcea83aacaf0d. Copy it alone to that commit for a
// behavioral red, not a compilation failure or production-code replacement.
func TestExactRequirements167LiteralModeFreshNativeReads(t *testing.T) {
	cfg := requirementsFixtureConfig(t)
	cfg.RequirementsRead = "compare-null-v0.167.0-alpha.3.2"
	cfg.StartupProbe = "compare-unrestricted-v0.167.0-alpha.3.2"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("exact 0.167 null-only requirements mode must validate: %v", err)
	}
	for _, b := range cfg.Backends {
		if err := os.WriteFile(filepath.Join(b.Home, "startup-probe-mode"), []byte("success-167"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The earlier native one-shot succeeds. Its snapshot must not substitute
	// for a later JSONL read after requirements become non-null.
	probe, err := ProbeStartup(context.Background(), cfg, LaunchOptions{}, StartupClientInfo{Name: "synthetic-requirements", Version: "1"})
	if err != nil || string(probe) != probe167SuccessFixture {
		t.Fatalf("synthetic earlier probe failed: %v", err)
	}
	bridge := startFixtureBridge(t, cfg)
	stream := readRequirementsStream(t, bridge)
	if init := stream.ask(t, bridge, `{"id":1,"method":"initialize"}`); !exists(init, "result") {
		t.Fatal("synthetic initialization failed")
	}
	if _, err := fmt.Fprintln(bridge.input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	reject := func(id, barrier int) {
		t.Helper()
		response := stream.ask(t, bridge, fmt.Sprintf(`{"id":%d,"method":"configRequirements/read","params":null}`, id))
		if string(response["id"]) != fmt.Sprint(id) || !exists(response, "error") || exists(response, "result") {
			t.Fatal("fresh non-null requirements became a success")
		}
		// A fanout barrier drains an abandoned sibling before changing its
		// fixture file. This is synchronization, not a timing assumption.
		response = stream.ask(t, bridge, fmt.Sprintf(`{"id":%d,"method":"environment/status","params":{"environmentId":"local"}}`, barrier))
		if string(response["id"]) != fmt.Sprint(barrier) || !exists(response, "result") {
			t.Fatal("late requirements sibling poisoned the next request")
		}
	}
	// requirementsFixtureConfig supplied matching complete legacy non-null
	// policies. The newer mode must reject rather than reuse the probe null.
	reject(2, 3)
	want := object{"requirements": raw(nil), "supportsIndependentSpeedModes": raw(true)}
	for _, b := range cfg.Backends {
		writeRequirementsFixture(t, b, want)
	}
	check := func(request, id string) {
		t.Helper()
		response := stream.ask(t, bridge, request)
		if string(response["id"]) != id || exists(response, "error") || !sameJSON(response["result"], raw(want)) {
			t.Fatalf("fresh native null requirements were not preserved: %s", raw(response))
		}
	}
	check(`{"id":4,"method":"configRequirements/read","params":{}}`, "4")
	writeRequirementsFixture(t, cfg.Backends[1], safeRequirementsFixture())
	reject(5, 6)
	writeRequirementsFixture(t, cfg.Backends[0], safeRequirementsFixture())
	reject(7, 8)
	for _, b := range cfg.Backends {
		writeRequirementsFixture(t, b, want)
	}
	check(`{"id":9,"method":"configRequirements/read"}`, "9")
	if err := bridge.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bridge.wait(t); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	if stream.err != nil || strings.Count(string(stream.transcript), "\n") != 9 {
		t.Fatal("unexpected synthetic native stream or extra late response")
	}
	assertRequirementsFixtureRequests(t, cfg, 5)
}
