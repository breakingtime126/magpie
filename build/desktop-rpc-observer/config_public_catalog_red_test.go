package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/proc"
)

// This file uses only APIs present at a54fd10a. Run its executable test on
// Windows against that exact base for a behavioral red, not a compile failure.
func TestPublicCatalogRedChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_CATALOG_RED_CHILD") != "1" {
		return
	}
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "-c" ||
		os.Args[len(os.Args)-1] != " \tplugins.catalog-fixture@fixture-market.enabled=true" {
		os.Exit(41)
	}
	os.Stdout.WriteString("SYNTHETIC_PRIVATE_TRANSPORT\x00\xff\r\n")
	os.Exit(17)
}

func TestPublicCatalogExecutableUsesBoundRefsWithoutMutation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("actual observer entry point is Windows-only")
	}
	work := t.TempDir()
	observerExe := filepath.Join(work, "catalog-observer.exe")
	build := proc.Command("go", "build", "-o", observerExe, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	var buildLog bytes.Buffer
	build.Stdout, build.Stderr = &buildLog, &buildLog
	if err := runOwnedTestCommand(t, build); err != nil {
		t.Fatalf("offline observer build failed: %v\n%s", err, buildLog.String())
	}
	key := "plugins.catalog-fixture@fixture-market.enabled"
	sum := sha256.Sum256([]byte(key))
	digest := hex.EncodeToString(sum[:])
	// Canonical field order is deliberate; these are synthetic public fixtures.
	catalog := "{\"schema\":1,\"packageSha256\":\"" + strings.Repeat("a", 64) +
		"\",\"archiveSha256\":\"" + strings.Repeat("b", 64) + "\",\"keysSha256\":\"" + digest +
		"\",\"keys\":[\"" + key + "\"]}"
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv(activeEnv, "")
	t.Setenv("MAGPIE_OBSERVER_CATALOG_RED_CHILD", "1")
	t.Setenv("MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES", catalog)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", self)
	prefix := filepath.Join(work, "trace")
	t.Setenv("MAGPIE_CI_TRACE_FILE", prefix)
	cmd := proc.Command(observerExe, "-test.run=^TestPublicCatalogRedChildFixture$", "--",
		"-c", " \t"+key+"=true")
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runOwnedTestCommand(t, cmd)
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17 ||
		stdout.String() != "SYNTHETIC_PRIVATE_TRANSPORT\x00\xff\r\n" || stderr.Len() != 0 {
		t.Fatal("catalog observation changed child arguments, bytes or exit")
	}
	files, err := filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 1 {
		t.Fatal("missing public catalog trace")
	}
	log, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(log, []byte("catalog-fixture")) || bytes.Contains(log, []byte("SYNTHETIC_PRIVATE")) {
		t.Fatal("catalog key or transport was logged")
	}
	var bound, matched bool
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil {
			t.Fatal("invalid trace record")
		}
		if record["kind"] == "public_plugin_catalog" {
			if bound || record["publicPluginCatalogSHA256"] != digest {
				t.Fatal("catalog was not bound once to the exact source dictionary")
			}
			bound = true
		}
		if record["kind"] == "argv_config" {
			if !bound || record["publicPluginRef"] != float64(1) ||
				record["configValue"] != "bool_true" || record["configKey"] != nil {
				t.Fatal("catalog argument was not identified by the bound public index")
			}
			matched = true
		}
	}
	if !bound || !matched {
		t.Fatal("public source catalog was not announced and matched")
	}
	// A provided but invalid catalog must not silently fall back or launch a
	// child; the child fixture would otherwise return 17 and write its payload.
	t.Setenv("MAGPIE_CI_PUBLIC_PLUGIN_CANDIDATES", catalog+"\n")
	cmd = proc.Command(observerExe, "-test.run=^TestPublicCatalogRedChildFixture$", "--",
		"-c", " \t"+key+"=true")
	cmd.Stdin = strings.NewReader("")
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runOwnedTestCommand(t, cmd)
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 125 ||
		stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid test-only setup") {
		t.Fatal("malformed catalog did not stop before child launch")
	}
	files, err = filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 1 {
		t.Fatal("invalid catalog created a misleading trace")
	}
}
