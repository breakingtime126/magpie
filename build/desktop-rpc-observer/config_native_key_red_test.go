package main

import (
	"bytes"
	"strings"
	"testing"
)

// This fixture uses only the observer API already present at 31f269b0.
// Copy this file alone to that commit to demonstrate the false equivalence.
func TestConfigMetadataPreservesNativeLiteralKeys(t *testing.T) {
	for _, argument := range []string{
		"\"model\"=SYNTHETIC_SECRET",
		"'model'=SYNTHETIC_SECRET",
		"features . plugins=true",
		"features. plugins=true",
	} {
		var log bytes.Buffer
		args := []string{"-c", argument}
		newObserver(&log).argv(args)
		es := events(t, log.Bytes())
		if len(es) != 2 || es[1].Kind != "argv_config" || es[1].Class != "unknown" ||
			es[1].ConfigKey != "" || es[1].ConfigValue != "" {
			t.Fatal("literal native key was incorrectly normalized to a known setting")
		}
		if args[0] != "-c" || args[1] != argument {
			t.Fatal("diagnostic observation changed an argument")
		}
		if strings.Contains(log.String(), "SYNTHETIC_SECRET") {
			t.Fatal("diagnostic leaked a value")
		}
	}
}
