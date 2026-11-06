// control-plane-native is a synthetic-only acceptance harness for the exact
// official Windows runtime. It never launches Desktop or reads user state.
package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	nativeSHA     = "40ecc3d8b6899afd8f935a8cccb7d53ba41ffd7c199f679e186d30ddd0d0bf39"
	nativeVersion = "codex-cli 0.167.0-alpha.3.2"
	catalogBlob   = "489ebe3f2483348641a16160ce8491dd077eabca"
	controlMode   = "routing-v0.167.0-alpha.3.2"
	modelName     = "gpt-5.5"
	maxMessage    = 4 << 20
)

type summary struct {
	Schema                   int    `json:"schema"`
	Scope                    string `json:"scope"`
	Status                   string `json:"status"`
	Stage                    string `json:"stage"`
	Failure                  string `json:"failure"`
	NativeSHA256             string `json:"native_sha256"`
	BinaryVerified           bool   `json:"binary_verified"`
	VersionVerified          bool   `json:"version_verified"`
	ConfigReads              int    `json:"config_reads"`
	LayerReads               int    `json:"layer_reads"`
	StableLayerRereads       int    `json:"stable_layer_rereads"`
	LayerVersionChanges      int    `json:"layer_version_changes"`
	CatalogModels            int    `json:"catalog_models"`
	Threads                  int    `json:"threads"`
	Turns                    int    `json:"turns"`
	AssistantMessages        int    `json:"assistant_messages"`
	ThreadReads              int    `json:"thread_reads"`
	ThreadListQueries        int    `json:"thread_list_queries"`
	ThreadListPages          int    `json:"thread_list_pages"`
	ThreadListItems          int    `json:"thread_list_items"`
	OwnershipRejections      int    `json:"ownership_rejections"`
	AlphaRequests            int    `json:"alpha_requests"`
	BetaRequests             int    `json:"beta_requests"`
	FixtureErrors            int    `json:"fixture_errors"`
	UnexpectedServerRequests int    `json:"unexpected_server_requests"`
	AdapterCleanExit         bool   `json:"adapter_clean_exit"`
	CleanupComplete          bool   `json:"cleanup_complete"`
}

type options struct{ bridge, codex, catalog, scratch string }
type failure string

func (e failure) Error() string { return string(e) }

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-desktop-fixtures" {
		desktopFixturesMain(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-supervise" {
		supervise(os.Args[2:])
		return
	}
	report := summary{Schema: 1, Scope: "synthetic-loopback-only", Status: "failed", Stage: "inputs", Failure: "internal_failure"}
	func() {
		defer func() {
			if recover() != nil {
				report.Failure = "internal_failure"
			}
		}()
		opts, err := arguments(os.Args[1:])
		if err != nil {
			report.Failure = string(err.(failure))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
		defer cancel()
		err = run(ctx, opts, &report)
		if err != nil {
			report.Failure = "internal_failure"
			if code, ok := err.(failure); ok {
				report.Failure = string(code)
			}
			return
		}
		report.Status, report.Stage, report.Failure = "passed", "complete", ""
	}()
	// No native data, error strings, prompts, IDs, paths or diagnostic tails.
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if report.Status != "passed" {
		os.Exit(1)
	}
}

func arguments(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("control-plane-native", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.bridge, "bridge", "", "")
	f.StringVar(&o.codex, "codex", "", "")
	f.StringVar(&o.catalog, "catalog", "", "")
	f.StringVar(&o.scratch, "scratch-parent", "", "")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return o, failure("input_invalid")
	}
	for _, p := range []string{o.bridge, o.codex, o.catalog, o.scratch} {
		if !filepath.IsAbs(p) {
			return o, failure("input_invalid")
		}
	}
	return o, nil
}

func regularFile(path string, limit int64) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return failure("input_invalid")
	}
	return nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	if err := regularFile(path, limit); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, failure("input_invalid")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, failure("input_invalid")
	}
	return b, nil
}

func verifyBinary(path string) error {
	if err := regularFile(path, 512<<20); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return failure("input_invalid")
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (512<<20)+1))
	if err != nil || n > 512<<20 || hex.EncodeToString(h.Sum(nil)) != nativeSHA {
		return failure("native_hash_mismatch")
	}
	return nil
}

func selectedCatalog(data []byte) ([]byte, error) {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(data))
	_, _ = h.Write(data)
	if hex.EncodeToString(h.Sum(nil)) != catalogBlob {
		return nil, failure("catalog_hash_mismatch")
	}
	var catalog struct {
		Models []json.RawMessage `json:"models"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		return nil, failure("catalog_invalid")
	}
	var selected []json.RawMessage
	for _, raw := range catalog.Models {
		var m struct {
			Slug          string          `json:"slug"`
			Visibility    string          `json:"visibility"`
			ToolMode      json.RawMessage `json:"tool_mode"`
			ResponsesLite bool            `json:"use_responses_lite"`
		}
		if json.Unmarshal(raw, &m) != nil {
			return nil, failure("catalog_invalid")
		}
		if m.Slug == modelName {
			if m.Visibility != "list" || !bytes.Equal(m.ToolMode, []byte("null")) || m.ResponsesLite {
				return nil, failure("catalog_invalid")
			}
			selected = append(selected, raw)
		}
	}
	if len(selected) != 1 {
		return nil, failure("catalog_invalid")
	}
	return json.Marshal(struct {
		Models []json.RawMessage `json:"models"`
	}{selected})
}

// The parent starts this helper with a deliberately minimal environment.
// Reject unknown entries instead of discovering or reading any credential store.
func checkEnvironment(env []string) error {
	allowed := map[string]bool{}
	for _, k := range strings.Fields("SYSTEMROOT WINDIR PATH PATHEXT HOME USERPROFILE CODEX_HOME APPDATA LOCALAPPDATA TMP TEMP TMPDIR LANG") {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for _, entry := range env {
		k, _, ok := strings.Cut(entry, "=")
		k = strings.ToUpper(k)
		if !ok || !allowed[k] || seen[k] {
			return failure("environment_rejected")
		}
		seen[k] = true
	}
	return nil
}

func emptyDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return failure("scratch_invalid")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return failure("scratch_not_empty")
	}
	return nil
}

func mkdir(root, name string) (string, error) {
	path := filepath.Join(root, name)
	if os.Mkdir(path, 0700) != nil {
		return "", failure("scratch_invalid")
	}
	if err := emptyDirectory(path); err != nil {
		return "", err
	}
	return path, nil
}

func childEnvironment(home, codex string) []string {
	env := []string{
		"HOME=" + home, "USERPROFILE=" + home, "CODEX_HOME=" + codex,
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"TMP=" + home, "TEMP=" + home, "TMPDIR=" + home, "LANG=C.UTF-8",
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		env = append(env, "SystemRoot="+root, "WINDIR="+root, "PATH="+filepath.Join(root, "System32"))
	} else {
		env = append(env, "PATH=/usr/bin:/bin")
	}
	return env
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return failure("scratch_invalid")
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return failure("scratch_invalid")
	}
	return nil
}

func jsonBytes(value any) []byte { data, _ := json.Marshal(value); return data }
