package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogRequiresExactVisibleIdentityPairs(t *testing.T) {
	valid := `{"data":[{"id":"magpie/alpha/gpt-5.5","model":"magpie/alpha/gpt-5.5","displayName":"GPT-5.5 [alpha]","isDefault":true,"hidden":false},{"id":"magpie/beta/gpt-5.5","model":"magpie/beta/gpt-5.5","displayName":"GPT-5.5 [beta]","isDefault":false,"hidden":false}],"nextCursor":null}`
	if def, count, err := checkCatalog([]byte(valid)); err != nil || count != 2 || def != "magpie/alpha/gpt-5.5" {
		t.Fatal("exact catalog identity pairs rejected", err)
	}
	for _, field := range []string{"id", "model", "displayName", "isDefault", "hidden"} {
		for _, value := range []any{nil, "", "gpt-5.5", 0} {
			var obj map[string]any
			if err := json.Unmarshal([]byte(valid), &obj); err != nil {
				t.Fatal(err)
			}
			row := obj["data"].([]any)[1].(map[string]any)
			row[field] = value
			if _, _, err := checkCatalog(jsonBytes(obj)); err != failure("catalog_invalid") {
				t.Fatal("catalog accepted a null, empty or mistyped identity field", field)
			}
			delete(row, field)
			if _, _, err := checkCatalog(jsonBytes(obj)); err != failure("catalog_invalid") {
				t.Fatal("catalog accepted an absent identity field", field)
			}
		}
	}
	for _, bad := range []string{
		strings.Replace(valid, `"id":"magpie/beta/gpt-5.5"`, `"id":"magpie/alpha/gpt-5.5"`, 1),
		strings.Replace(valid, `"displayName":"GPT-5.5 [beta]"`, `"displayName":"GPT-5.5 [alpha]"`, 1),
		strings.Replace(valid, `"hidden":false`, `"hidden":true`, 1),
		strings.Replace(valid, `"hidden":false`, `"hidden":true,"hidden":false`, 1),
		strings.Replace(valid, `,"nextCursor":null`, ``, 1),
		strings.Replace(valid, `"nextCursor":null`, `"nextCursor":"more"`, 1),
		valid + `{}`,
	} {
		if _, _, err := checkCatalog([]byte(bad)); err != failure("catalog_invalid") {
			t.Fatal("collapsed, hidden, duplicate or incomplete catalog accepted")
		}
	}
}
