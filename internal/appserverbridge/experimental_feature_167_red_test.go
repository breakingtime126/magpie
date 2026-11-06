package appserverbridge

import (
	"bytes"
	"strings"
	"testing"
)

// Uses only APIs at base 2ab7bcd946dba06d9467b9db2dfa5b799a24d87d.
// Copy this file alone to that base: accepting the explicit new mode must fail
// as an assertion, not as an undefined-symbol compilation error.
func TestExperimentalFeature167BehavioralRed(t *testing.T) {
	cfg := fixtureConfig(t)
	for i := range cfg.Backends {
		cfg.Backends[i].StartupProbeSHA256 = strings.Repeat("a", 64)
	}
	settings, err := parseObject(raw(cfg))
	if err != nil {
		t.Fatal(err)
	}
	settings["experimental_feature_list"] = raw("compare-v0.167.0-alpha.3.2")
	if _, err := LoadConfig(bytes.NewReader(raw(settings))); err != nil {
		t.Fatalf("exact default-off experimental feature comparison mode was not accepted: %v", err)
	}
}
