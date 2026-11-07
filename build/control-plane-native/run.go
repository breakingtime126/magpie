package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func run(ctx context.Context, o options, report *summary) (runErr error) {
	pinnedSHA, err := acceptanceSHA(o.acceptanceProfile)
	if err != nil {
		return err
	}
	models, err := acceptanceModels(o.acceptanceProfile)
	if err != nil || (o.acceptanceProfile == acceptanceSynthetic4239 && o.catalog != "") {
		return failure("input_invalid")
	}
	if runtime.GOOS != "windows" {
		return failure("platform_rejected")
	}
	if err := checkEnvironment(os.Environ()); err != nil {
		return err
	}
	if !filepath.IsAbs(os.Getenv("SystemRoot")) {
		return failure("environment_rejected")
	}
	if err := emptyDirectory(o.scratch); err != nil {
		return err
	}
	if err := regularFile(o.bridge, 512<<20); err != nil {
		return err
	}
	if err := verifyBinaryHash(o.codex, pinnedSHA); err != nil {
		return err
	}
	report.NativeSHA256, report.BinaryVerified = pinnedSHA, true
	var catalogs [2][]byte
	if o.acceptanceProfile == acceptanceSynthetic4239 {
		for i, name := range []string{"alpha", "beta"} {
			catalogs[i], err = selfAuthoredCatalog(name)
			if err != nil {
				return err
			}
		}
	} else {
		data, err := readBounded(o.catalog, 10<<20)
		if err != nil {
			return err
		}
		catalog, err := selectedCatalog(data)
		if err != nil {
			return err
		}
		catalogs = [2][]byte{catalog, catalog}
	}
	root, err := os.MkdirTemp(o.scratch, "owned-")
	if err != nil {
		return failure("scratch_invalid")
	}
	defer func() {
		if os.RemoveAll(root) != nil {
			runErr = failure("cleanup_failed")
			return
		}
		if emptyDirectory(o.scratch) != nil {
			runErr = failure("cleanup_failed")
			return
		}
		if runErr != failure("cleanup_failed") {
			report.CleanupComplete = true
		}
	}()
	cwd, err := mkdir(root, "workspace")
	if err != nil {
		return err
	}
	frontHome, err := mkdir(root, "frontend-home")
	if err != nil {
		return err
	}
	frontCodex, err := mkdir(root, "frontend-codex")
	if err != nil {
		return err
	}
	controlHome, err := mkdir(root, "control-home")
	if err != nil {
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
	report.Stage = "version"
	if err := verifyVersion(ctx, o.codex, childEnvironment(versionHome, versionCodex), cwd); err != nil {
		return err
	}
	report.VersionVerified = true
	if err := verifyBinaryHash(o.codex, pinnedSHA); err != nil {
		return err
	}
	catalogPath := filepath.Join(root, "models.json")
	if o.acceptanceProfile != acceptanceSynthetic4239 {
		if err := writeNew(catalogPath, catalogs[0]); err != nil {
			return err
		}
	}
	var fixtures []*fixture
	for i, name := range []string{"alpha", "beta"} {
		instructions := ""
		if o.acceptanceProfile == acceptanceSynthetic4239 {
			instructions = syntheticInstructions(name)
		}
		fixtures = append(fixtures, newFixtureWithModel(name, models[i], instructions))
	}
	defer func() {
		for _, f := range fixtures {
			f.close()
		}
		a, ae := fixtures[0].counts()
		b, be := fixtures[1].counts()
		report.AlphaRequests, report.BetaRequests, report.FixtureErrors = a, b, ae+be
		if runErr == nil && (a != 2 || b != 2 || ae+be != 0) {
			runErr = failure("fixture_failed")
		}
	}()
	backends := make([]map[string]any, 0, 2)
	var homes []string
	for i, f := range fixtures {
		home, err := mkdir(root, f.name+"-home")
		if err != nil {
			return err
		}
		ch, err := mkdir(root, f.name+"-codex")
		if err != nil {
			return err
		}
		homes = append(homes, ch)
		selectedCatalogPath := catalogPath
		if o.acceptanceProfile == acceptanceSynthetic4239 {
			selectedCatalogPath = filepath.Join(root, f.name+"-self-authored-catalog.json")
			if err := writeNew(selectedCatalogPath, catalogs[i]); err != nil {
				return err
			}
		}
		config, err := nativeConfigForModel(f.name+"_provider", f.server.URL, selectedCatalogPath, models[i])
		if err != nil {
			return err
		}
		if err := writeNew(filepath.Join(ch, "config.toml"), config); err != nil {
			return err
		}
		entries, err := os.ReadDir(ch)
		if err != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
			return failure("scratch_not_empty")
		}
		backends = append(backends, map[string]any{"id": f.name, "provider": f.name + "_provider",
			"command": o.codex, "home": home, "codex_home": ch, "startup_probe_sha256": pinnedSHA})
	}
	cfg := map[string]any{"backends": backends, "working_directory": cwd, "control_plane": controlMode, "control_home": controlHome}
	cfgPath := filepath.Join(root, "bridge.json")
	if err := writeNew(cfgPath, jsonBytes(cfg)); err != nil {
		return err
	}
	env := append(childEnvironment(frontHome, frontCodex), "MAGPIE_APP_SERVER_BRIDGE_CONFIG="+cfgPath)
	report.Stage = "initialize"
	p, err := startPeer(ctx, o.bridge, env, cwd)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			if err := p.close(false); err == failure("cleanup_failed") {
				runErr = err
			}
		}
		report.UnexpectedServerRequests = int(p.unexpected.Load())
		if report.UnexpectedServerRequests != 0 && runErr == nil {
			runErr = failure("unexpected_server_request")
		}
	}()
	initialized, err := p.rpc("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "magpie_synthetic_ci", "title": "Synthetic isolated CI", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return err
	}
	initResult, err := object(initialized)
	if err != nil || stringField(initResult, "codexHome") != controlHome {
		return failure("control_home_mismatch")
	}
	if err := emptyDirectory(controlHome); err != nil {
		return err
	}
	if err := p.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	report.Stage = "initial_config"
	initialVersion, err := readConfigForProfile(p, "magpie/alpha/"+models[0], report, o.acceptanceProfile)
	if err != nil {
		return err
	}
	report.Stage = "catalog"
	raw, err := p.rpc("model/list", map[string]any{})
	if err != nil {
		return err
	}
	defaultModel, count, err := checkCatalogForProfile(raw, o.acceptanceProfile)
	if err != nil {
		return err
	}
	report.CatalogModels = count
	report.Stage = "resolved_config"
	resolvedVersion, err := readConfigForProfile(p, defaultModel, report, o.acceptanceProfile)
	if err != nil {
		return err
	}
	if initialVersion != resolvedVersion {
		return failure("config_shape_mismatch")
	}
	report.Stage = "threads"
	var threads [2]string
	for i, name := range []string{"alpha", "beta"} {
		selected := "magpie/" + name + "/" + models[i]
		started, err := p.rpc("thread/start", map[string]any{
			"model": selected, "modelProvider": "magpie_bridge", "environments": []any{},
		})
		if err != nil {
			return err
		}
		obj, err := object(started)
		if err != nil || stringField(obj, "modelProvider") != "magpie_bridge" || stringField(obj, "model") != selected {
			return failure("logical_provider_mismatch")
		}
		thread, err := object(obj["thread"])
		if err != nil || stringField(thread, "modelProvider") != "magpie_bridge" || stringField(thread, "id") == "" {
			return failure("logical_provider_mismatch")
		}
		// Project identity must stay absent or null. No project, cwd or native
		// provider identity is supplied by the logical client.
		if v := thread["projectId"]; len(v) != 0 && !bytes.Equal(v, []byte("null")) {
			return failure("project_not_empty")
		}
		threads[i] = stringField(thread, "id")
		if err := p.assistants.owner(threads[i], name); err != nil {
			return err
		}
		report.Threads++
	}
	if threads[0] == threads[1] {
		return failure("ownership_changed")
	}
	report.Stage = "turns"
	var previousTurns [2]string
	for round := 1; round <= 2; round++ {
		for i, name := range []string{"alpha", "beta"} {
			turnID, err := assistantTurn(p, threads[i], name, round)
			if err != nil {
				return err
			}
			if turnID == previousTurns[i] {
				return failure("turn_failed")
			}
			previousTurns[i] = turnID
			report.Turns++
		}
	}
	report.Stage = "thread_list"
	if err := readThreadListsForProfile(p, threads, report, o.acceptanceProfile); err != nil {
		return err
	}
	report.Stage = "ownership"
	for i := range threads {
		other := "alpha"
		if i == 0 {
			other = "beta"
		}
		response, err := p.request("turn/start", map[string]any{
			"threadId": threads[i], "model": "magpie/" + other + "/" + models[1-i],
			"input": []any{map[string]any{"type": "text", "text": "synthetic-owner-denial", "text_elements": []any{}}},
		})
		if err != nil {
			return err
		}
		var denied struct {
			Code int `json:"code"`
		}
		if len(response.Result) != 0 || json.Unmarshal(response.Error, &denied) != nil || denied.Code != -32602 {
			return failure("ownership_changed")
		}
		report.OwnershipRejections++
		raw, err := p.rpc("thread/read", map[string]any{"threadId": threads[i], "includeTurns": false})
		if err != nil {
			return err
		}
		responseObject, err := object(raw)
		if err != nil {
			return err
		}
		thread, err := object(responseObject["thread"])
		if err != nil || stringField(thread, "id") != threads[i] || stringField(thread, "modelProvider") != "magpie_bridge" {
			return failure("logical_provider_mismatch")
		}
		if o.acceptanceProfile == acceptanceSynthetic4239 &&
			stringField(thread, "model") != "magpie/"+[]string{"alpha", "beta"}[i]+"/"+models[i] {
			return failure("ownership_changed")
		}
		report.ThreadReads++
	}
	report.Stage = "final_config"
	finalVersion, err := readConfigForProfile(p, defaultModel, report, o.acceptanceProfile)
	if err != nil {
		return err
	}
	if finalVersion != resolvedVersion {
		return failure("config_shape_mismatch")
	}
	if err := emptyDirectory(controlHome); err != nil {
		return err
	}
	for _, ch := range homes {
		// Inspect only file existence inside homes created by this invocation.
		// Never open an authentication file, even an unexpected one.
		if _, err := os.Lstat(filepath.Join(ch, "auth.json")); !os.IsNotExist(err) {
			return failure("unexpected_auth_data")
		}
	}
	report.Stage = "shutdown"
	err = p.close(true)
	closed = true
	if err != nil {
		return err
	}
	report.AdapterCleanExit = true
	count, err = p.assistants.finish()
	if err != nil {
		return err
	}
	report.AssistantMessages = count
	return nil
}

func checkCatalog(raw json.RawMessage) (string, int, error) {
	return checkCatalogForProfile(raw, "")
}

func checkCatalogForProfile(raw json.RawMessage, profile string) (string, int, error) {
	if _, err := acceptanceModels(profile); err != nil {
		return "", 0, err
	}
	if _, err := decodeConfigJSON(raw); err != nil {
		return "", 0, failure("catalog_invalid")
	}
	var list struct {
		Data []struct {
			ID          string `json:"id"`
			Model       string `json:"model"`
			DisplayName string `json:"displayName"`
			Default     *bool  `json:"isDefault"`
			Hidden      *bool  `json:"hidden"`

			Reasoning json.RawMessage `json:"defaultReasoningEffort"`
			Efforts   json.RawMessage `json:"supportedReasoningEfforts"`
		} `json:"data"`
		Next json.RawMessage `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &list) != nil || len(list.Data) != 2 || !bytes.Equal(list.Next, []byte("null")) {
		return "", 0, failure("catalog_invalid")
	}
	// Each profile checks its own exact identities, never labels borrowed from
	// the other model set. The existing bridge appends the owning backend.
	var want map[string]string
	if profile == acceptanceSynthetic4239 {
		want = map[string]string{"magpie/alpha/test-alpha": "Synthetic Alpha [alpha]", "magpie/beta/test-beta": "Synthetic Beta [beta]"}
	} else {
		want = map[string]string{"magpie/alpha/" + modelName: "GPT-5.5 [alpha]", "magpie/beta/" + modelName: "GPT-5.5 [beta]"}
	}
	seen := map[string]bool{}
	def := ""
	for _, m := range list.Data {
		label, ok := want[m.Model]
		if !ok || seen[m.Model] || m.ID != m.Model || m.DisplayName != label ||
			m.Default == nil || m.Hidden == nil || *m.Hidden {
			return "", 0, failure("catalog_invalid")
		}
		seen[m.Model] = true
		if profile == acceptanceSynthetic4239 && !selfAuthoredReasoning(m.Reasoning, m.Efforts) {
			return "", 0, failure("catalog_invalid")
		}
		if *m.Default {
			if def != "" {
				return "", 0, failure("catalog_invalid")
			}
			def = m.Model
		}
	}
	if def == "" {
		return "", 0, failure("catalog_invalid")
	}
	if profile == acceptanceSynthetic4239 && def != "magpie/alpha/test-alpha" {
		return "", 0, failure("catalog_invalid")
	}
	return def, len(list.Data), nil
}

func assistantTurn(p *peer, thread, name string, number int) (string, error) {
	if err := p.assistants.begin(thread, name); err != nil {
		return "", err
	}
	raw, err := p.rpc("turn/start", map[string]any{"threadId": thread,
		"input": []any{map[string]any{"type": "text", "text": fmt.Sprintf("synthetic-%s-turn-%d", name, number), "text_elements": []any{}}}})
	if err != nil {
		return "", err
	}
	result, err := object(raw)
	if err != nil {
		return "", err
	}
	started, err := object(result["turn"])
	if err != nil {
		return "", err
	}
	id := stringField(started, "id")
	if id == "" {
		return "", failure("protocol_invalid")
	}
	if err := p.assistants.bind(thread, id); err != nil {
		return "", err
	}
	matches := func(m message, method string) bool {
		if m.Method != method {
			return false
		}
		params, err := object(m.Params)
		if err != nil || stringField(params, "threadId") != thread {
			return false
		}
		if method == "turn/completed" {
			t, err := object(params["turn"])
			return err == nil && stringField(t, "id") == id
		}
		item, err := object(params["item"])
		return err == nil && stringField(params, "turnId") == id && stringField(item, "type") == "agentMessage"
	}
	done, err := p.wait(func(m message) bool { return matches(m, "turn/completed") })
	if err != nil {
		return "", err
	}
	params, _ := object(done.Params)
	turn, err := object(params["turn"])
	if err != nil || stringField(turn, "status") != "completed" || (len(turn["error"]) != 0 && !bytes.Equal(turn["error"], []byte("null"))) {
		return "", failure("turn_failed")
	}
	answer, err := p.wait(func(m message) bool { return matches(m, "item/completed") })
	if err != nil {
		return "", err
	}
	params, _ = object(answer.Params)
	item, _ := object(params["item"])
	if stringField(item, "text") != "fixture-answer-"+name {
		return "", failure("answer_crossed_route")
	}
	if err := p.assistants.claim(thread, id, stringField(item, "id")); err != nil {
		return "", err
	}
	return id, nil
}
