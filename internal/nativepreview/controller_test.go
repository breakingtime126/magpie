package nativepreview

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/testenv"
)

// Ordinary tests execute only the copied inert fixture. The separately explicit
// official-runtime opt-in retains testenv isolation and restores transport policy.
var officialPreviewTransportReady bool

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		version, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "fixture-version"))
		if len(version) == 0 {
			version = []byte("codex-cli 0.167.0-alpha.3.2")
		}
		fmt.Println(string(version))
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "login" {
		os.Exit(loginFixture())
	}
	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		os.Exit(fixture())
	}
	if os.Getenv("MAGPIE_PREVIEW_OFFICIAL_TEXT_TEST") == "1" {
		type savedValue struct {
			value   string
			present bool
		}
		saved := make(map[string]savedValue)
		for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
			value, present := os.LookupEnv(name)
			saved[name] = savedValue{value: value, present: present}
		}
		setupEntered := false
		code := testenv.RunIn(m, func(string) {
			setupEntered = true
			officialPreviewTransportReady = true
			for name, original := range saved {
				var err error
				if original.present {
					err = os.Setenv(name, original.value)
				} else {
					err = os.Unsetenv(name)
				}
				if err != nil {
					officialPreviewTransportReady = false
				}
			}
		})
		if !setupEntered {
			fmt.Println("native-preview-diagnostic stage=test-isolation class=failed-no-request")
		}
		os.Exit(code)
	}
	testenv.Main(m)
}

func fixture() int {
	home := os.Getenv("HOME")
	mode, _ := os.ReadFile(filepath.Join(home, "fixture-mode"))
	log, err := os.Create(filepath.Join(home, "fixture-requests"))
	if err != nil {
		return 1
	}
	defer log.Close()
	environment, _ := json.Marshal(os.Environ())
	if os.WriteFile(filepath.Join(home, "fixture-environment"), environment, 0600) != nil {
		return 2
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 3
		}
		_, _ = log.Write(append(append([]byte(nil), scanner.Bytes()...), '\n'))
		var method string
		_ = json.Unmarshal(request["method"], &method)
		var result any
		switch method {
		case "initialize":
			result = map[string]string{"userAgent": "synthetic"}
		case "initialized":
			continue
		case "config/read":
			config := map[string]any{"model_provider": "openai"}
			if strings.Contains(strings.Join(os.Args, " "), `model_provider="magpie_preview_aihub"`) {
				config["model_provider"] = "magpie_preview_aihub"
				config["model_providers"] = map[string]any{"magpie_preview_aihub": map[string]any{"name": "AIHub preview", "base_url": "https://example.invalid/v1", "env_key": keyName, "wire_api": "responses", "requires_openai_auth": false}}
			}
			if strings.Contains(strings.Join(os.Args, " "), "features.shell_tool=false") {
				textFixtureConfig(config, string(mode))
			}
			result = map[string]any{"config": config}
		case "thread/start":
			result = textFixtureThread(request, string(mode), home)
		case "turn/start":
			result = textFixtureTurn(request, string(mode), home)
		case "model/list":
			switch string(mode) {
			case "hang":
				if os.WriteFile(filepath.Join(home, "fixture-ready"), nil, 0600) != nil {
					return 4
				}
				for {
					time.Sleep(time.Hour)
				}
			case "error":
				fmt.Println(`{"id":"preview-models-0","error":{"code":-1,"message":"PUBLIC-CANARY"}}`)
				continue
			case "request":
				fmt.Println(`{"id":99,"method":"attestation/generate","params":{}}`)
				continue
			case "duplicate":
				fmt.Println(`{"id":"preview-models-0","id":"preview-models-0","result":{}}`)
				continue
			}
			result = map[string]any{"data": []any{map[string]any{"id": "synthetic", "model": "self-authored-test", "displayName": "Self-authored test", "hidden": false}}, "nextCursor": nil}
		default:
			return 5
		}
		if method == "model/list" && string(mode) == "send-stale" {
			result = map[string]any{"data": []any{}, "nextCursor": nil}
		}
		encoded, _ := json.Marshal(map[string]any{"id": request["id"], "result": result})
		fmt.Println(string(encoded))
	}
	if os.WriteFile(filepath.Join(home, "fixture-eof"), nil, 0600) != nil {
		return 6
	}
	return 0
}

func fixtureController(t *testing.T) *Controller {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); _ = os.RemoveAll(c.root) })
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	name := "native-fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("copy failed")
	}
	if c.SelectExecutable(path) != nil {
		t.Fatal("selection failed")
	}
	return c
}

func TestNativeDiscoveryUsesSeparateHomesAndNarrowProjection(t *testing.T) {
	c := fixtureController(t)
	t.Setenv("OPENAI_API_KEY", "PUBLIC-AMBIENT-CANARY")
	if c.ConfigureAIHub("https://example.invalid/v1", "PUBLIC-AIHUB-CANARY") != nil {
		t.Fatal("configure failed")
	}
	for _, provider := range []string{"openai", "aihub"} {
		models, err := c.Discover(context.Background(), provider)
		if err != nil || len(models) != 1 || models[0].ID != "self-authored-test" {
			t.Fatal("synthetic native discovery failed")
		}
		home := filepath.Join(c.root, provider, "home")
		data, err := os.ReadFile(filepath.Join(home, "fixture-requests"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), "\n") != 4 || strings.Contains(string(data), "thread/start") || strings.Contains(string(data), "PUBLIC-") {
			t.Fatal("unexpected native request")
		}
		environment, err := os.ReadFile(filepath.Join(home, "fixture-environment"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(environment), "PUBLIC-AMBIENT-CANARY") || (strings.Contains(string(environment), "PUBLIC-AIHUB-CANARY") != (provider == "aihub")) {
			t.Fatal("credential environment crossed provider")
		}
		if _, err := os.Stat(filepath.Join(home, "fixture-eof")); err != nil {
			t.Fatal("missing clean EOF")
		}
	}
	state := c.State()
	if !state.CanSend || state.CanLogin != nativeLoginAllowed() || !state.CanDiscover {
		t.Fatal("unsupported operations advertised")
	}
	if _, err := c.Send(context.Background(), "openai", "unknown-model", "hello"); err != ErrUnavailable {
		t.Fatal("unbound model activated")
	}
}

func TestNativeDiscoveryFailsClosedWithoutSourceError(t *testing.T) {
	for _, mode := range []string{"error", "request", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			c := fixtureController(t)
			if os.WriteFile(filepath.Join(c.root, "openai", "home", "fixture-mode"), []byte(mode), 0600) != nil {
				t.Fatal("fixture setup")
			}
			models, err := c.Discover(context.Background(), "openai")
			if models != nil || err != ErrUnavailable || strings.Contains(err.Error(), "PUBLIC-") {
				t.Fatal("source data escaped failure")
			}
			if _, err := os.Stat(filepath.Join(c.root, "aihub", "home", "fixture-requests")); !os.IsNotExist(err) {
				t.Fatal("secondary was launched")
			}
		})
	}
}

func TestNativeDiscoveryCancellationClosesOwnedPeer(t *testing.T) {
	c := fixtureController(t)
	home := filepath.Join(c.root, "openai", "home")
	if os.WriteFile(filepath.Join(home, "fixture-mode"), []byte("hang"), 0600) != nil {
		t.Fatal("fixture setup")
	}
	done := make(chan error, 1)
	go func() { _, err := c.Discover(context.Background(), "openai"); done <- err }()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-ticker.C:
			_, err := os.Stat(filepath.Join(home, "fixture-ready"))
			ready = err == nil
		case <-deadline.C:
			c.Cancel()
			<-done
			t.Fatal("fixture did not become ready")
		}
	}
	c.Cancel()
	select {
	case err := <-done:
		if err != ErrUnavailable {
			t.Fatal("cancel classification")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("owned peer cleanup stalled")
	}
	if c.State().Busy {
		t.Fatal("busy did not clear")
	}
}

func TestNativePreviewRejectsUnsafeConfigurationAndClosedUse(t *testing.T) {
	c := fixtureController(t)
	for _, endpoint := range []string{"http://example.invalid", "https://user:pass@example.invalid", "https://example.invalid/?secret=x", "https://example.invalid/#x"} {
		if c.ConfigureAIHub(endpoint, "PUBLIC-KEY") != ErrUnavailable {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if c.ConfigureAIHub("https://example.invalid", "bad\nkey") != ErrUnavailable {
		t.Fatal("unsafe key accepted")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.State().CanDiscover {
		t.Fatal("closed controller active")
	}
	if _, err := c.Discover(context.Background(), "openai"); err != ErrBusy {
		t.Fatal("closed controller launched")
	}
}

func TestStrictJSONRejectsAmbiguousMetadata(t *testing.T) {
	for _, data := range []string{`{"a":1,"a":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `null`} {
		if strictJSON([]byte(data)) {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	if !strictJSON([]byte(`{"data":[],"nextCursor":null}`)) {
		t.Fatal("valid JSON rejected")
	}
}

func loginFixture() int {
	home := os.Getenv("HOME")
	root := filepath.Dir(filepath.Dir(home))
	data, _ := json.Marshal(map[string]any{"home": home, "codexHome": os.Getenv("CODEX_HOME"), "keyPresent": os.Getenv(keyName) != "", "args": os.Args[1:]})
	if os.WriteFile(filepath.Join(root, "fixture-login-report"), data, 0600) != nil {
		return 1
	}
	fmt.Println("PUBLIC-OAUTH-OUTPUT-CANARY")
	fmt.Fprintln(os.Stderr, "PUBLIC-OAUTH-ERROR-CANARY")
	mode, _ := os.ReadFile(filepath.Join(root, "fixture-login-mode"))
	if string(mode) == "hang" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if string(mode) == "fail" {
		return 2
	}
	return 0
}

func TestNativeLoginUsesFreshContextAndNoSecondaryCredential(t *testing.T) {
	if !nativeLoginAllowed() {
		t.Skip("inherited Windows job: native browser handoff deliberately unsupported")
	}
	c := fixtureController(t)
	if c.ConfigureAIHub("https://example.invalid/v1", "PUBLIC-KEY") != nil {
		t.Fatal("configuration")
	}
	first := c.primaryHome
	for i := 0; i < 2; i++ {
		if c.Login(context.Background()) != nil {
			t.Fatal("synthetic login failed")
		}
		if c.primaryHome == first || !strings.HasPrefix(c.primaryHome, c.root+string(filepath.Separator)) {
			t.Fatal("not a fresh owned home")
		}
		first = c.primaryHome
		data, err := os.ReadFile(filepath.Join(c.root, "fixture-login-report"))
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			Home       string   `json:"home"`
			CodexHome  string   `json:"codexHome"`
			KeyPresent bool     `json:"keyPresent"`
			Args       []string `json:"args"`
		}
		if json.Unmarshal(data, &report) != nil || report.KeyPresent || report.Home != c.primaryHome || report.CodexHome != c.primaryCodexHome || len(report.Args) != 1 || report.Args[0] != "login" {
			t.Fatal("login crossed context")
		}
	}
	if _, err := c.Discover(context.Background(), "openai"); err != nil {
		t.Fatal("discovery did not use login context")
	}
	if _, err := os.Stat(filepath.Join(c.primaryHome, "fixture-requests")); err != nil {
		t.Fatal("wrong post-login context")
	}
	if os.WriteFile(filepath.Join(c.root, "fixture-login-mode"), []byte("fail"), 0600) != nil {
		t.Fatal("fixture setup")
	}
	if c.Login(context.Background()) != ErrUnavailable {
		t.Fatal("source failure escaped")
	}
	if _, err := os.Stat(c.primaryHome); err != nil {
		t.Fatal("failed login context was removed")
	}
}

func TestTransportPolicyIsPreservedAndSecurityFlagBlocks(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid:1234")
	t.Setenv("CODEX_CA_CERTIFICATE", "PUBLIC-CA-PATH")
	env := strings.Join(nativeEnvironment(peerSpec{home: "home", codexHome: "codex", provider: "openai"}), "\n")
	if !strings.Contains(env, "HTTPS_PROXY=http://proxy.example.invalid:1234") || !strings.Contains(env, "CODEX_CA_CERTIFICATE=PUBLIC-CA-PATH") {
		t.Fatal("transport policy lost")
	}
	c := fixtureController(t)
	t.Setenv("CODEX_WINDOWS_REGISTERED_CORE", "")
	if _, err := c.Discover(context.Background(), "openai"); err != ErrUnavailable {
		t.Fatal("security flag bypassed")
	}
	if c.Login(context.Background()) != ErrUnavailable {
		t.Fatal("login security admission bypassed")
	}
	if _, err := os.Stat(filepath.Join(c.root, "openai", "home", "fixture-requests")); !os.IsNotExist(err) {
		t.Fatal("blocked child started")
	}
}

func TestUnsupportedVersionNeverStartsAppServer(t *testing.T) {
	c := fixtureController(t)
	if os.WriteFile(filepath.Join(c.primaryHome, "fixture-version"), []byte("codex-cli unknown"), 0600) != nil {
		t.Fatal("fixture setup")
	}
	if _, err := c.Discover(context.Background(), "openai"); err != ErrUnavailable {
		t.Fatal("unsupported version admitted")
	}
	if _, err := os.Stat(filepath.Join(c.primaryHome, "fixture-requests")); !os.IsNotExist(err) {
		t.Fatal("app-server started")
	}
}

func TestNativeLoginCancellationRetainsState(t *testing.T) {
	if !nativeLoginAllowed() {
		t.Skip("inherited Windows job: native browser handoff deliberately unsupported")
	}
	c := fixtureController(t)
	if os.WriteFile(filepath.Join(c.root, "fixture-login-mode"), []byte("hang"), 0600) != nil {
		t.Fatal("fixture setup")
	}
	done := make(chan error, 1)
	go func() { done <- c.Login(context.Background()) }()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-ticker.C:
			_, err := os.Stat(filepath.Join(c.root, "fixture-login-report"))
			ready = err == nil
		case <-deadline.C:
			c.Cancel()
			<-done
			t.Fatal("login fixture not ready")
		}
	}
	c.Cancel()
	select {
	case err := <-done:
		if err != ErrUnavailable {
			t.Fatal("cancel result")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("login root cleanup stalled")
	}
	if _, err := os.Stat(c.primaryHome); err != nil {
		t.Fatal("native state deleted on cancel")
	}
	if c.State().Busy {
		t.Fatal("busy stuck")
	}
}

func TestEffectiveProviderCannotSilentlyChangeAuthOrEndpoint(t *testing.T) {
	spec := peerSpec{provider: "aihub", endpoint: "https://example.invalid/v1"}
	good := map[string]any{"name": "AIHub preview", "base_url": spec.endpoint, "env_key": keyName, "wire_api": "responses", "requires_openai_auth": false, "auth": nil, "supports_websockets": false}
	encode := func(provider map[string]any) json.RawMessage {
		data, _ := json.Marshal(map[string]any{"config": map[string]any{"model_provider": "magpie_preview_aihub", "model_providers": map[string]any{"magpie_preview_aihub": provider}}})
		return data
	}
	if !matchesProvider(encode(good), spec) {
		t.Fatal("native inert defaults rejected")
	}
	for key, value := range map[string]any{"base_url": "https://other.invalid", "env_key": "OPENAI_API_KEY", "requires_openai_auth": true, "auth": map[string]string{"command": "not-run"}, "unknown_field": nil} {
		changed := map[string]any{}
		for k, v := range good {
			changed[k] = v
		}
		changed[key] = value
		if matchesProvider(encode(changed), spec) {
			t.Fatal("provider mismatch admitted")
		}
	}
}

func TestSyntheticLoginRootLifecycleIndependentOfJobAdmission(t *testing.T) {
	c := fixtureController(t)
	spec := peerSpec{command: c.executable, digest: c.digest, home: c.primaryHome, codexHome: c.primaryCodexHome, provider: "openai"}
	// The inert fixture never launches a browser. Calling the internal lifecycle
	// here does not permit the production Login admission to bypass a Windows job.
	if runLoginRoot(context.Background(), spec) != nil {
		t.Fatal("synthetic root exit")
	}
	if os.WriteFile(filepath.Join(c.root, "fixture-login-mode"), []byte("hang"), 0600) != nil {
		t.Fatal("fixture setup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if runLoginRoot(ctx, spec) != ErrUnavailable {
		t.Fatal("synthetic root cancel")
	}
	if !nativeLoginAllowed() && c.State().CanLogin {
		t.Fatal("job admission changed")
	}
}
