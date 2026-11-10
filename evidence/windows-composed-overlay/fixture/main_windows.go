// Authored synthetic fixture only. This is not a production launcher or vendor
// binary. Build under build/native-composed-fixture in the frozen module.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/nativeconnect"
	"github.com/yetone/magpie/internal/nativehost"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/pluginrt"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
	"github.com/yetone/magpie/internal/usage"
)

const callerKey = "MAGPIE_COMPOSED_PUBLIC_CALLER"

type guard struct {
	Root, ID string
	Deadline time.Time
}
type ready struct {
	Profile       string `json:"profile"`
	Generation    string `json:"generation"`
	ProfileSHA256 string `json:"profile_sha256"`
	GatewayURL    string `json:"gateway_url"`
}
type delivery struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
	Body   string `json:"body"`
}

func main() {
	if err := entry(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func entry() error {
	g, err := readGuard()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(context.Background(), g.Deadline)
	defer cancel()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	switch filepath.Base(self) {
	case "Magpie-Native-Codex-Experimental.exe":
		if len(os.Args) != 3 || os.Args[1] != "--composed-fixture" || os.Args[2] != g.ID {
			return errors.New("host fixture guard mismatch")
		}
		return host(ctx, g)
	case "synthetic-backend.exe":
		if len(os.Args) < 2 || os.Args[1] != "app-server" {
			return errors.New("backend fixture requires authored app-server mode")
		}
		return backend(ctx, g)
	default:
		return errors.New("unexpected fixture binary name")
	}
}
func readGuard() (guard, error) {
	var g guard
	home := os.Getenv("HOME")
	if home == "" || !filepath.IsAbs(home) || os.Getenv("USERPROFILE") != home {
		return g, errors.New("fixture home unavailable")
	}
	b, err := os.ReadFile(filepath.Join(home, "composed-guard.json"))
	if err != nil || len(b) > 4096 {
		return g, errors.New("fixture guard missing")
	}
	if json.Unmarshal(b, &g) != nil || g.Root != home || len(g.ID) != 32 || !g.Deadline.After(time.Now()) || time.Until(g.Deadline) > 2*time.Minute {
		return guard{}, errors.New("fixture guard invalid or expired")
	}
	if _, err := hex.DecodeString(g.ID); err != nil {
		return guard{}, err
	}
	return g, nil
}
func inside(root, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	rel, e := filepath.Rel(strings.ToLower(root), strings.ToLower(filepath.Clean(path)))
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func absent(path string) error {
	_, e := os.Lstat(path)
	if !errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("fixture requires absent state: %s", path)
	}
	return nil
}

// Publish completed bytes atomically without replacing an existing report.
// The temporary hard link is on the same filesystem; readers never see a
// final name until Write/Sync/Close succeeds. Failure is not retried silently.
func writeNew(path string, v any) (result error) {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".composed-report-*")
	if e != nil {
		return e
	}
	temporary := f.Name()
	defer func() { result = errors.Join(result, os.Remove(temporary)) }()
	n, e := f.Write(b)
	if e == nil && n != len(b) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	if e = errors.Join(e, f.Close()); e != nil {
		return e
	}
	return os.Link(temporary, path)
}
func preflight(g guard) error {
	if os.Getenv("MAGPIE_NO_ACCOUNTS") != "1" || os.Getenv("MAGPIE_PARTNERS") != "off" || os.Getenv("MAGPIE_ADDR") != "127.0.0.1:0" {
		return errors.New("gateway fixture flags absent")
	}
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CONFIG_DIRS", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "CODEX_HOME", "TEMP", "TMP", "TMPDIR"} {
		if !inside(g.Root, os.Getenv(key)) {
			return fmt.Errorf("unisolated environment path %s", key)
		}
	}
	for _, key := range []string{"MAGPIE_PUBLIC_URL", "MAGPIE_TLS_CERT", "MAGPIE_TLS_KEY", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "CODEX_WINDOWS_REGISTERED_CORE"} {
		if _, ok := os.LookupEnv(key); ok {
			return fmt.Errorf("unexpected environment authority %s", key)
		}
	}
	for _, name := range []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli", "wsl.exe"} {
		p, e := exec.LookPath(name)
		if e != nil || !inside(g.Root, p) {
			return fmt.Errorf("missing inert tool %s", name)
		}
	}
	paths := []string{settings.Dir(), settings.Path(), provider.Path(), plugin.ListPath(), plugin.ToolsPath(), plugin.AuthPath(), pluginrt.Path(), access.Path(), appdir.Cache()}
	for _, p := range paths {
		if !inside(g.Root, p) {
			return fmt.Errorf("gateway state escapes fixture: %s", p)
		}
	}
	// Before invoking any loaders, deny all pre-existing user/account/plugin state.
	for _, p := range []string{settings.Path(), provider.Path(), plugin.ListPath(), plugin.ToolsPath(), plugin.AuthPath(), pluginrt.Path(), access.Path(), filepath.Join(settings.Dir(), "codex-relay.json"), filepath.Join(settings.Dir(), "logins.json"), filepath.Join(settings.Dir(), "claude-proxy-env.json")} {
		if e := absent(p); e != nil {
			return e
		}
	}
	s, e := settings.LoadChecked()
	if e != nil {
		return e
	}
	if !settings.AccountDiscoveryOff() || s.KeepAwake || s.KeepAwakeDisplay || s.CodexWarmup != "" || s.ClaudeWarmup != "" || s.CodexWarmAt != "" || s.ClaudeWarmAt != "" || len(s.CodexWarmAts) != 0 || len(s.ClaudeWarmAts) != 0 || len(s.CodexWarmAtOf) != 0 || s.WorkBuddyCheckin || s.TraeCheckin || s.MiniMaxCheckin || s.QoderCheckin || len(s.PluginCheckins) != 0 {
		return errors.New("gateway maintenance preconditions failed")
	}
	ps, e := provider.Stored()
	if e != nil || len(ps) != 0 {
		return errors.New("configured providers present")
	}
	if len(plugin.Load().Plugins) != 0 || len(plugin.Cached()) != 0 || len(pluginrt.Registered()) != 0 || len(usage.Observers()) != 0 || gateway.MCPHub != nil {
		return errors.New("plugin/observer/hub targets present")
	}
	if gateway.Handover || gateway.Inherited() || len(gateway.WhileServing) != 2 {
		return errors.New("unexpected gateway startup composition")
	}
	if len(gateway.WSLNets()) != 0 {
		return errors.New("WSL adapter present: fixture refuses network binds")
	}
	// Supplemental only. This is not a blanket network-attempt firewall, and
	// cannot retroactively affect transports created during package init.
	testenv.Offline()
	return nil
}

type fixtureStore struct{ value nativeconnect.Selection }

func (s *fixtureStore) Read() (nativeconnect.Selection, error) { return s.value, nil }
func (s *fixtureStore) Replace(before, after nativeconnect.Selection) error {
	if s.value != before {
		return nativeconnect.ErrConflict
	}
	s.value = after
	return nil
}
func host(ctx context.Context, g guard) (result error) {
	if err := preflight(g); err != nil {
		return err
	}
	serving, cancel := context.WithCancel(ctx)
	defer cancel()
	server := gateway.New()
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe(serving) }()
	defer func() {
		cancel()
		select {
		case e := <-served:
			if e != nil && !errors.Is(e, context.Canceled) {
				result = errors.Join(result, e)
			}
		case <-time.After(5 * time.Second):
			result = errors.Join(result, errors.New("gateway stop unconfirmed"))
		}
	}()
	for {
		if _, ok := server.OwnedConnection(); ok {
			break
		}
		select {
		case e := <-served:
			served <- e
			return errors.New("gateway serving failed before ownership")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	oldGatewayURL, ok := server.OwnedLoopbackURL()
	if !ok {
		return errors.New("owned gateway URL unavailable for fixture record")
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	command := filepath.Join(filepath.Dir(self), "synthetic-backend.exe")
	data, e := os.ReadFile(command)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(data)
	catalog := filepath.Join(g.Root, "control", "synthetic-catalog.json")
	if e = writeNew(catalog, map[string]any{}); e != nil {
		return e
	}
	cfg := appserverbridge.Config{ControlPlane: appserverbridge.ControlPlaneRouting167, ControlHome: filepath.Join(g.Root, "control"), WorkingDirectory: filepath.Join(g.Root, "work"), SharedNativeRoute: &appserverbridge.SharedNativeRoute167{Mode: appserverbridge.SharedNativeHome167, GatewayURL: oldGatewayURL, GatewayEnvKey: callerKey, GatewayAuth: appserverbridge.SharedNativeLocalGateway167, SecondaryCatalog: catalog}}
	for i, id := range []string{"primary", "secondary"} {
		provider := "openai"
		if i == 1 {
			provider = "magpie_shared_aihub"
		}
		cfg.Backends = append(cfg.Backends, appserverbridge.Backend{ID: id, Provider: provider, Command: command, Home: g.Root, CodexHome: filepath.Join(g.Root, "native"), StartupProbeSHA256: hex.EncodeToString(digest[:])})
	}
	owner, e := nativehost.Start(serving, server, cfg, filepath.Join(g.Root, "publication"))
	if owner != nil {
		defer func() {
			c, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			result = errors.Join(result, owner.Close(c))
		}()
	}
	if e != nil {
		return e
	}
	store := &fixtureStore{}
	controller := nativeconnect.New(filepath.Join(g.Root, "selection"), store, func(context.Context) (nativeconnect.Plan, error) { return owner.Plan() })
	if _, e = controller.Connect(ctx); e != nil {
		return e
	}
	plan, e := owner.Plan()
	if e != nil {
		return e
	}
	if e = writeNew(filepath.Join(g.Root, "host-ready.json"), ready{store.value.Profile.Text, plan.PreparedProfile.Generation(), plan.PreparedProfile.SHA256(), oldGatewayURL}); e != nil {
		return e
	}
	commands := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			commands <- scanner.Text()
		}
		close(commands)
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-owner.Context().Done():
			return errors.New("host lease ended before requested revocation")
		case cmd, ok := <-commands:
			if !ok {
				return errors.New("host control pipe ended early")
			}
			switch cmd {
			case "restore":
				if _, e := controller.Disconnect(); e != nil {
					return e
				}
				if owner.Context().Err() != nil {
					return errors.New("selection restoration revoked owner")
				}
				if e := writeNew(filepath.Join(g.Root, "selection-restored.json"), map[string]bool{"owner_active": true}); e != nil {
					return e
				}
			case "revoke":
				c, stop := context.WithTimeout(context.Background(), 5*time.Second)
				e := owner.Close(c)
				stop()
				if e != nil {
					return e
				}
				return writeNew(filepath.Join(g.Root, "host-revoked.json"), map[string]bool{"cleanup_confirmed": true})
			default:
				return errors.New("unknown fixture control command")
			}
		}
	}
}
func backend(ctx context.Context, g guard) error {
	name := "primary"
	base := ""
	for _, arg := range os.Args[2:] {
		if strings.HasPrefix(arg, "model_providers.magpie_shared_aihub=") {
			name = "secondary"
			_, tail, ok := strings.Cut(arg, "base_url=")
			if !ok {
				return errors.New("secondary URL missing")
			}
			quoted, _, ok := strings.Cut(tail, ",env_key=")
			if !ok {
				return errors.New("secondary URL malformed")
			}
			v, e := strconv.Unquote(quoted)
			if e != nil {
				return e
			}
			base = v
		}
	}
	if e := writeNew(filepath.Join(g.Root, name+"-started.json"), map[string]bool{"authored_backend": true}); e != nil {
		return e
	}
	if name == "secondary" {
		var declared ready
		recorded, e := os.ReadFile(filepath.Join(g.Root, "host-ready.json"))
		if e != nil || len(recorded) > 4096 || json.Unmarshal(recorded, &declared) != nil {
			return errors.New("fixture host record missing")
		}
		if !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.HasSuffix(base, "/v1") || base == declared.GatewayURL {
			return errors.New("runner did not replace persisted placeholder")
		}
		request, e := http.NewRequestWithContext(ctx, "POST", base+"/responses", strings.NewReader(`{}`))
		if e != nil {
			return e
		}
		label := os.Getenv(callerKey)
		if label != "magpie-codex" {
			return errors.New("unexpected synthetic public caller label")
		}
		request.Header.Set("Authorization", "Bearer "+label)
		request.Header.Set("Content-Type", "application/json")
		response, e := (&http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
		if e != nil {
			return e
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, 8193))
		response.Body.Close()
		if e != nil || len(body) > 8192 {
			return errors.New("invalid synthetic HTTP response")
		}
		if response.StatusCode != 400 || !strings.Contains(string(body), "invalid request: model must be a nonempty string") {
			return fmt.Errorf("unexpected gateway response %d %s", response.StatusCode, body)
		}
		if e := writeNew(filepath.Join(g.Root, "http-delivered.json"), delivery{base, response.StatusCode, string(body)}); e != nil {
			return e
		}
	}
	// Keep stdout and inherited handles alive until the actual runner closes stdin.
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, os.Stdin); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
