package main

import "testing"

// Uses only the old checkCatalog API, so the baseline fails on behavior.
func TestCatalogRejectsCollapsedIdentity(t *testing.T) {
	raw := `{"data":[{"id":"magpie/alpha/gpt-5.5","model":"magpie/alpha/gpt-5.5","displayName":"GPT-5.5 [alpha]","isDefault":true,"hidden":false},{"id":"magpie/alpha/gpt-5.5","model":"magpie/beta/gpt-5.5","displayName":"GPT-5.5 [alpha]","isDefault":false,"hidden":false}],"nextCursor":null}`
	if _, _, err := checkCatalog([]byte(raw)); err == nil {
		t.Fatal("collapsed catalog IDs and display names were accepted")
	}
}
