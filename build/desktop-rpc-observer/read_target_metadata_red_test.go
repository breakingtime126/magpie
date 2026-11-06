package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Copy this file alone to 70e603d8481c28b9f84cadf2f7183255044e11b4.
// These tests use only old observer APIs and fail by named assertions, not
// missing symbols. Fixtures are synthetic and perform no target-file I/O.
func targetMetadataRecord(t *testing.T, direction, raw string) map[string]json.RawMessage {
	t.Helper()
	var log bytes.Buffer
	newObserver(&log).line(direction, []byte(raw))
	if bytes.Contains(log.Bytes(), []byte("PRIVATE_TARGET")) {
		t.Fatal("private target input entered diagnostic metadata")
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(log.Bytes()), &record) != nil {
		t.Fatal("expected one target metadata record")
	}
	return record
}

func assertTargetMetadata(t *testing.T, record map[string]json.RawMessage, want map[string]string) {
	t.Helper()
	base := map[string]bool{"seq": true, "pid": true, "kind": true, "direction": true,
		"method": true, "correlation": true, "class": true, "code": true}
	for key, literal := range want {
		if string(record[key]) != literal {
			t.Fatalf("target metadata field %s missing or incorrect", key)
		}
	}
	for key := range record {
		if _, expected := want[key]; !expected && !base[key] {
			t.Fatal("target metadata escaped its exact privacy schema")
		}
	}
}

func TestProjectListMetadataRed(t *testing.T) {
	record := targetMetadataRecord(t, "to_bridge",
		"{\"id\":\"PRIVATE_TARGET_ID\",\"method\":\"project/list\",\"params\":{\"cursor\":\"PRIVATE_TARGET_CURSOR\",\"limit\":0,\"sortKey\":\"recencyAt\",\"sortDirection\":\"desc\"}}")
	assertTargetMetadata(t, record, map[string]string{
		"projectListParamsValid": "true", "projectListCursorProvided": "true",
		"projectListLimitProvided": "true", "projectListSortKey": "\"recencyAt\"",
		"projectListSortDirection": "\"desc\"",
	})
}

func TestFSReadTargetMetadataRed(t *testing.T) {
	record := targetMetadataRecord(t, "to_bridge",
		"{\"id\":\"PRIVATE_TARGET_ID\",\"method\":\"fs/readFile\",\"params\":{\"path\":\"C:/PRIVATE_TARGET/config.toml\"}}")
	assertTargetMetadata(t, record, map[string]string{
		"fsReadParamsShapeValid": "true", "fsReadPathForm": "\"drive_absolute\"",
		"fsReadBasename": "\"config.toml\"", "fsReadLexicalRoot": "\"unconfigured\"",
	})
}

func TestReadTargetEnvironmentMetadataRed(t *testing.T) {
	t.Setenv("MAGPIE_CI_READ_TARGET_ROOTS", "{\"workspace\":\"C:/PRIVATE_TARGET\"}")
	t.Setenv("magpie_ci_read_target_roots", "{\"workspace\":\"C:/PRIVATE_TARGET_ALIAS\"}")
	for _, value := range childEnvironment() {
		key, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, "MAGPIE_CI_READ_TARGET_ROOTS") {
			t.Fatal("observer-only read target roots reached child environment")
		}
	}
	if _, present := os.LookupEnv("MAGPIE_CI_READ_TARGET_ROOTS"); !present {
		t.Fatal("observer input was removed instead of stripping the child copy")
	}
}
