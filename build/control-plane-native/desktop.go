package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Desktop owns the app-server conversation. This mode only serves synthetic
// Responses and prepares isolated native configuration, never authentication.
const desktopSelfAuthoredScope = "desktop-self-authored-fixtures-only"

type desktopAudit struct {
	Schema        int    `json:"schema"`
	Scope         string `json:"scope"`
	Status        string `json:"status"`
	Failure       string `json:"failure"`
	AlphaRequests int    `json:"alpha_requests"`
	BetaRequests  int    `json:"beta_requests"`
	FixtureErrors int    `json:"fixture_errors"`
}

// These pins select a CI fixture runtime, not a Desktop compatibility verdict.
// The independent four-turn acceptance requires its own acceptance-profile.
func desktopFixtureSHA(build string) (string, error) {
	switch build {
	case "":
		return nativeSHA, nil
	case "desktop-4239":
		return native4239SHA, nil
	default:
		return "", failure("input_invalid")
	}
}

func desktopFixturesMain(args []string) {
	report := desktopAudit{Schema: 1, Scope: "desktop-loopback-fixtures-only", Status: "failed"}
	err := runDesktopFixtures(args, os.Stdin, os.Stdout, &report)
	if err != nil {
		report.Failure = "internal_failure"
		if code, ok := err.(failure); ok {
			report.Failure = string(code)
		}
	} else {
		report.Status = "stopped"
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if err != nil {
		os.Exit(1)
	}
}

func runDesktopFixtures(args []string, stop io.ReadCloser, out io.Writer, report *desktopAudit) (runErr error) {
	var codex, catalogPath, scratch, reviewedBuild, profile string
	flags := flag.NewFlagSet("desktop-fixtures", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&codex, "codex", "", "")
	flags.StringVar(&catalogPath, "catalog", "", "")
	flags.StringVar(&scratch, "scratch-parent", "", "")
	flags.StringVar(&reviewedBuild, "reviewed-build", "", "")
	flags.StringVar(&profile, "fixture-profile", "", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return failure("input_invalid")
	}
	catalogSupplied := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "catalog" {
			catalogSupplied = true
		}
	})
	if err := validateDesktopFixtureProfile(profile, reviewedBuild, catalogSupplied); err != nil {
		return err
	}
	if desktopUsesSelfAuthoredCatalog(profile) {
		report.Scope = desktopSelfAuthoredScope
		if profile == desktopOwnedCatalogProfile {
			report.Scope = desktopOwnedCatalogScope
		}
	}
	pinnedSHA, err := desktopFixtureSHA(reviewedBuild)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return failure("platform_rejected")
	}
	if err := checkEnvironment(os.Environ()); err != nil {
		return err
	}
	paths := []string{codex, scratch, os.Getenv("SystemRoot")}
	if profile == "" {
		paths = append(paths, catalogPath)
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return failure("input_invalid")
		}
	}
	if err := emptyDirectory(scratch); err != nil {
		return err
	}
	if err := verifyBinaryHash(codex, pinnedSHA); err != nil {
		return err
	}
	var catalog []byte
	if profile == "" {
		data, err := readBounded(catalogPath, 10<<20)
		if err != nil {
			return err
		}
		catalog, err = selectedCatalog(data)
		if err != nil {
			return err
		}
	}
	// The external owner must stop Desktop and its bridge tree before deleting
	// scratch. We deliberately leave no cleanup claim in this helper's audit.
	root := scratch
	cwd, err := mkdir(root, "workspace")
	if err != nil {
		return err
	}
	if _, err := mkdir(root, "control-home"); err != nil {
		return err
	}
	versionHome, err := mkdir(root, "version-home")
	if err != nil {
		return err
	}
	versionCodex, err := mkdir(root, "version-codex")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = verifyVersion(ctx, codex, childEnvironment(versionHome, versionCodex), cwd)
	cancel()
	if err != nil {
		return err
	}
	selected := filepath.Join(root, "models.json")
	var alpha, beta *fixture
	if desktopUsesSelfAuthoredCatalog(profile) {
		selected = ""
		alpha = newDesktopFixtureForModel("alpha", "test-alpha", syntheticInstructions("alpha"))
		beta = newDesktopFixtureForModel("beta", "test-beta", syntheticInstructions("beta"))
	} else {
		if err := writeNew(selected, catalog); err != nil {
			return err
		}
		alpha, beta = newDesktopFixture("alpha"), newDesktopFixture("beta")
	}
	defer func() {
		alpha.close()
		beta.close()
		a, ae := alpha.counts()
		b, be := beta.counts()
		report.AlphaRequests, report.BetaRequests, report.FixtureErrors = a, b, ae+be
		if ae+be != 0 && runErr == nil {
			runErr = failure("fixture_rejected_request")
		}
	}()
	if err := writeDesktopFixtureConfigForProfile(root, codex, selected, pinnedSHA, profile, alpha, beta); err != nil {
		return err
	}
	// No paths, payloads, URLs, or IDs are emitted into public diagnostics.
	if err := writeDesktopFixtureReadyFromRoot(out, pinnedSHA, profile, root); err != nil {
		return err
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		var b [1]byte
		_, err := stop.Read(b[:])
		done <- err
	}()
	defer func() {
		_ = stop.Close()
		<-finished
	}()
	timer := time.NewTimer(180 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != io.EOF {
			return failure("stop_invalid")
		}
	case <-timer.C:
		return failure("fixture_timeout")
	}
	_, ae := alpha.counts()
	_, be := beta.counts()
	if ae+be != 0 {
		return failure("fixture_rejected_request")
	}
	return nil
}

func validateDesktopFixtureProfile(profile, build string, catalogSupplied bool) error {
	switch profile {
	case "":
		return nil
	case acceptanceSynthetic4239, desktopOwnedCatalogProfile:
		if build == "desktop-4239" && !catalogSupplied {
			return nil
		}
	}
	return failure("input_invalid")
}

func writeDesktopFixtureConfig(root, codex, selected, pinnedSHA string, fixtures ...*fixture) error {
	return writeDesktopFixtureConfigForProfile(root, codex, selected, pinnedSHA, "", fixtures...)
}

func writeDesktopFixtureConfigForProfile(root, codex, selected, pinnedSHA, profile string, fixtures ...*fixture) error {
	if profile != "" && !desktopUsesSelfAuthoredCatalog(profile) {
		return failure("input_invalid")
	}
	if desktopUsesSelfAuthoredCatalog(profile) && (selected != "" || pinnedSHA != native4239SHA) {
		return failure("input_invalid")
	}
	if profile == desktopOwnedCatalogProfile && (len(fixtures) != 2 || fixtures[0].name != "alpha" || fixtures[1].name != "beta") {
		return failure("fixture_invalid")
	}
	backends := []map[string]any{}
	for _, f := range fixtures {
		home, err := mkdir(root, f.name+"-home")
		if err != nil {
			return err
		}
		ch, err := mkdir(root, f.name+"-codex")
		if err != nil {
			return err
		}
		catalogPath, model := selected, modelName
		if desktopUsesSelfAuthoredCatalog(profile) {
			if f.model != "test-"+f.name || f.instructions != syntheticInstructions(f.name) {
				return failure("fixture_invalid")
			}
			data, err := selfAuthoredCatalog(f.name)
			if err != nil {
				return err
			}
			catalogPath = filepath.Join(root, f.name+"-self-authored-catalog.json")
			if err := writeNew(catalogPath, data); err != nil {
				return err
			}
			model = f.model
		}
		config, err := nativeConfigForModel(f.name+"_provider", f.server.URL, catalogPath, model)
		if err != nil {
			return err
		}
		if err := writeNew(filepath.Join(ch, "config.toml"), config); err != nil {
			return err
		}
		backends = append(backends, map[string]any{"id": f.name, "provider": f.name + "_provider", "command": codex, "home": home, "codex_home": ch, "startup_probe_sha256": pinnedSHA})
	}
	config := map[string]any{"backends": backends, "working_directory": filepath.Join(root, "workspace"), "control_plane": controlMode, "control_home": filepath.Join(root, "control-home")}
	if profile == desktopOwnedCatalogProfile {
		data, err := bridgeOwnedSelfAuthoredCatalog()
		if err != nil {
			return err
		}
		path := filepath.Join(root, "control-home", "bridge-model-catalog.json")
		if err := writeNew(path, data); err != nil {
			return err
		}
		config["bridge_model_catalog_json"] = path
	}
	return writeNew(filepath.Join(root, "bridge.json"), jsonBytes(config))
}

func writeDesktopFixtureReady(out io.Writer, pinnedSHA string) error {
	if _, err := fmt.Fprintf(out, "{\"schema\":1,\"scope\":\"desktop-loopback-fixtures-only\",\"status\":\"ready\",\"lifetime_seconds\":180,\"native_sha256\":%q,\"catalog_blob\":%q}\n", pinnedSHA, catalogBlob); err != nil {
		return failure("output_failed")
	}
	return nil
}

func writeDesktopFixtureReadyForProfile(out io.Writer, pinnedSHA, profile string) error {
	if profile == "" {
		return writeDesktopFixtureReady(out, pinnedSHA)
	}
	if profile != acceptanceSynthetic4239 || pinnedSHA != native4239SHA {
		return failure("input_invalid")
	}
	if _, err := fmt.Fprintf(out, "{\"schema\":1,\"scope\":%q,\"status\":\"ready\",\"lifetime_seconds\":180,\"native_sha256\":%q,\"catalog_source\":\"self-authored-test-models-v1\"}\n", desktopSelfAuthoredScope, pinnedSHA); err != nil {
		return failure("output_failed")
	}
	return nil
}

func newDesktopFixture(name string) *fixture {
	return newDesktopFixtureForModel(name, modelName, "")
}

func newDesktopFixtureForModel(name, model, instructions string) *fixture {
	f := &fixture{name: name, model: model, instructions: instructions}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveDesktopFixture(f, w, r)
	}))
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.ReadTimeout = 10 * time.Second
	s.Config.WriteTimeout = 10 * time.Second
	s.Config.IdleTimeout = 10 * time.Second
	s.Config.MaxHeaderBytes = 32 << 10
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.Start()
	f.server = s
	return f
}

var syntheticDesktopPrompt = regexp.MustCompile(`^synthetic-(alpha|beta)-[A-Za-z0-9 -]{1,80}$`)

// Pinned protocol/src/models.rs at c40729ea5cf8e947481b130c59807273eda2f8e9
// defines ResponseItem::Message, ResponseInputItem::Message and ContentItem.
// This fixture intentionally supports only their reviewed plaintext subset.
func desktopInputAllowed(raw json.RawMessage, name string) bool {
	if _, err := decodeConfigJSON(raw); err != nil {
		return false
	}
	var input []map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || len(input) == 0 || len(input) > 128 {
		return false
	}
	users := 0
	for _, item := range input {
		if item == nil {
			return false
		}
		for key, value := range item {
			switch key {
			case "type":
				if !desktopString(value, "message") {
					return false
				}
			case "role", "content":
			case "id", "status":
				var text string
				if string(value) != "null" && (json.Unmarshal(value, &text) != nil || len(text) > 256) {
					return false
				}
			case "phase":
				if string(value) != "null" && !desktopString(value, "commentary", "partial_answer", "final_answer") {
					return false
				}
			case "encrypted_content", "internal_chat_message_metadata_passthrough":
				// No opaque/encrypted content or nonempty metadata is needed by
				// the fixture's plaintext response. Review before widening.
				if string(value) != "null" {
					return false
				}
			default:
				return false
			}
		}
		role := stringField(item, "role")
		if role != "user" && role != "assistant" && role != "system" && role != "developer" {
			return false
		}
		var content []map[string]json.RawMessage
		if json.Unmarshal(item["content"], &content) != nil || len(content) == 0 || len(content) > 128 {
			return false
		}
		for _, c := range content {
			kind := stringField(c, "type")
			if (role == "assistant" && kind != "output_text") || (role != "assistant" && kind != "input_text") {
				return false
			}
			for key, value := range c {
				switch key {
				case "type", "text":
				case "annotations", "logprobs":
					if kind != "output_text" {
						return false
					}
					var entries []json.RawMessage
					if json.Unmarshal(value, &entries) != nil || len(entries) != 0 {
						return false
					}
				default:
					return false
				}
			}
			var text string
			if len(c["text"]) == 0 || string(c["text"]) == "null" || json.Unmarshal(c["text"], &text) != nil {
				return false
			}
			if role == "user" {
				if !syntheticDesktopPrompt.MatchString(text) || !strings.HasPrefix(text, "synthetic-"+name+"-") {
					return false
				}
				users++
			}
			if role == "assistant" && text != "fixture-answer-"+name {
				return false
			}
		}
	}
	return users > 0
}

func desktopString(raw json.RawMessage, values ...string) bool {
	var text string
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
		return false
	}
	for _, value := range values {
		if text == value {
			return true
		}
	}
	return false
}

func serveDesktopFixture(f *fixture, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.URL.RawQuery != "" || r.ContentLength > maxMessage || r.Header.Get("Upgrade") != "" || r.Header.Get("Content-Encoding") != "" {
		f.fail(w)
		return
	}
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key", "X-Oai-Attestation"} {
		if len(r.Header.Values(h)) != 0 {
			f.fail(w)
			return
		}
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxMessage+1))
	if err != nil || len(data) > maxMessage {
		f.fail(w)
		return
	}
	// Reject duplicate keys, then inspect the actual typed Responses envelope.
	if _, err := decodeConfigJSON(data); err != nil {
		f.fail(w)
		return
	}
	var req map[string]json.RawMessage
	if json.Unmarshal(data, &req) != nil || stringField(req, "model") != f.model || string(req["stream"]) != "true" || !desktopInputAllowed(req["input"], f.name) {
		f.fail(w)
		return
	}
	if f.instructions != "" && !selfAuthoredRequest(req, f.name) {
		f.fail(w)
		return
	}
	// Native tool declarations and system/developer text are inert here.
	// User URL/nontext payloads and tool call/results are rejected; no input
	// is logged, relayed, fetched or executed. Only fixture text is emitted.
	f.mu.Lock()
	if f.requests >= 16 {
		f.failures++
		f.mu.Unlock()
		http.Error(w, "synthetic fixture rejected request", http.StatusBadRequest)
		return
	}
	f.requests++
	n := f.requests
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	for _, event := range responseEvents(f.name, n) {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonBytes(event)); err != nil {
			f.mu.Lock()
			f.failures++
			f.mu.Unlock()
			return
		}
	}
}
