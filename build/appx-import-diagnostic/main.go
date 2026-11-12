// Diagnostic only. Nothing here is imported by production discovery.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

const queryHash = "d90ba13aa248ef5a5d646d59457b64ea63e226141c2242635260f95a992d63e9"
const optIn = "[appx-import-order-v1]"

var arms = []string{"A", "C", "C", "A"}

// Pin the entire literal before deriving prefixes, including the helpers and
// markers. Do not run the suffix, which contains the installed-package query.
func deriveScripts(source []byte) (map[string]string, error) {
	f, e := parser.ParseFile(token.NewFileSet(), "query.go", source, 0)
	if e != nil {
		return nil, errors.New("source unavailable")
	}
	var query string
	count := 0
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			v := spec.(*ast.ValueSpec)
			for i, n := range v.Names {
				if n.Name != "installedQuery" {
					continue
				}
				count++
				if i >= len(v.Values) {
					continue
				}
				literal, ok := v.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				query, e = strconv.Unquote(literal.Value)
				if e != nil {
					return nil, errors.New("source unavailable")
				}
			}
		}
	}
	sum := sha256.Sum256([]byte(query))
	if count != 1 || hex.EncodeToString(sum[:]) != queryHash {
		return nil, errors.New("source binding refused")
	}
	boundary := "$appxModule=Import-QueryModule 'Appx'\n"
	prefix, tail, ok := strings.Cut(query, boundary)
	if !ok {
		return nil, errors.New("prefix unavailable")
	}
	suffix := "$getPackage=Get-QueryCommand $appxModule 'Get-AppxPackage'\n"
	imports, _, ok := strings.Cut(tail, suffix)
	if !ok {
		return nil, errors.New("suffix unavailable")
	}
	appx := boundary + "[Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx')\n"
	if !strings.HasPrefix(imports, "[Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx')\n") {
		return nil, errors.New("import boundary unavailable")
	}
	rest := strings.TrimPrefix(imports, "[Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx')\n")
	end := "[Console]::Error.WriteLine('APPX_IMPORT_DIAG_V1:complete')\n"
	return map[string]string{"A": prefix + appx + rest + end, "C": prefix + rest + appx + end}, nil
}

// This is the acceptance GUI allowlist, not the hosted runner environment.
// Preserve trust/proxy values exactly; every profile/cache/temp location is new.
func fixtureEnvironment(parent map[string]string, home string) []string {
	env := map[string]string{}
	for _, name := range []string{"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "CommonProgramFiles", "ProgramData", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "CODEX_CA_CERTIFICATE", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"} {
		for key, value := range parent {
			if strings.EqualFold(key, name) {
				if previous, present := env[name]; present && previous != value {
					return nil
				}
				env[name] = value
			}
		}
	}
	env["PATH"] = filepath.Join(env["SystemRoot"], "System32")
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		env[name] = home
	}
	env["CODEX_HOME"] = filepath.Join(home, ".codex")
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func markerOrder(arm string) []string {
	names := []string{"entry", "setup"}
	if arm == "A" {
		names = append(names, "appx-import", "appx-return", "appx", "management", "utility")
	} else {
		names = append(names, "management", "utility", "appx-import", "appx-return", "appx")
	}
	for i := range names {
		names[i] = "MAGPIE_QUERY_V1:" + names[i]
	}
	return append(names, "APPX_IMPORT_DIAG_V1:complete")
}

// No arbitrary child bytes are retained. A fixed candidate mask and saturating
// offset match known lines. Protocol contradictions invalidate the entire stream;
// unrelated stderr is discarded. Each critical section handles at most 256 bytes.
type progress struct {
	mu       sync.Mutex
	expected []string
	stage    uint8
	invalid  bool
	mask     uint16
	offset   uint8
	marker   bool
	cr       bool
}

func newProgress(arm string) *progress { return &progress{expected: markerOrder(arm)} }
func (p *progress) Write(data []byte) (int, error) {
	n := len(data)
	for len(data) > 0 {
		size := min(len(data), 256)
		p.mu.Lock()
		for _, b := range data[:size] {
			p.accept(b)
		}
		p.mu.Unlock()
		data = data[size:]
	}
	return n, nil
}
func (p *progress) accept(b byte) {
	if b == '\n' {
		// A recognized namespace, or a truncated matching protocol prefix, cannot
		// be discarded as unrelated text and followed by a successful subsequence.
		if p.marker || (p.offset > 0 && p.mask != 0) {
			matched := false
			for i, token := range p.expected {
				if p.mask&(1<<i) != 0 && int(p.offset) == len(token) && int(p.stage) == i {
					p.stage++
					matched = true
					break
				}
			}
			if !matched {
				p.invalid = true
			}
		}
		p.mask = 0
		p.offset = 0
		p.marker = false
		p.cr = false
		return
	}
	if p.offset == 0 {
		p.mask = (1 << len(p.expected)) - 1
	}
	if b == '\r' && !p.cr {
		p.cr = true
		return
	}
	for i, token := range p.expected {
		if p.cr || int(p.offset) >= len(token) || token[p.offset] != b {
			p.mask &^= 1 << i
		}
	}
	if p.offset < 255 {
		p.offset++
	}
	for _, prefix := range []string{"MAGPIE_QUERY_V1:", "APPX_IMPORT_DIAG_V1:"} {
		if int(p.offset) != len(prefix) {
			continue
		}
		for i, token := range p.expected {
			if p.mask&(1<<i) != 0 && strings.HasPrefix(token, prefix) {
				p.marker = true
			}
		}
	}
}
func (p *progress) snapshot() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.invalid || p.stage == 0 || (p.offset > 0 && (p.mask != 0 || p.marker)) {
		return "unknown"
	}
	_, name, _ := strings.Cut(p.expected[p.stage-1], ":")
	return name
}

type owner interface {
	Wait(context.Context) error
	Stop() error
}
type result struct {
	Arm, Outcome, Progress string
	Cleanup                bool
}

// DrainedTree owns exactly one cmd.Wait internally. Its Wait calls below only
// observe that same root and retained Job. The cleanup window does not extend
// the ten-second operation deadline, and no late completion becomes success.
func waitOwned(ctx context.Context, tree owner, success func() bool, snapshot func() string) result {
	r := result{Outcome: "completed"}
	e := tree.Wait(ctx)
	if e == nil {
		e = ctx.Err()
	}
	r.Progress = snapshot()
	if e != nil {
		r.Outcome = "wait-failed"
		if errors.Is(e, context.DeadlineExceeded) {
			r.Outcome = "deadline"
		} else if errors.Is(e, context.Canceled) {
			r.Outcome = "cancelled"
		}
		stopErr := tree.Stop()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		drainErr := tree.Wait(cleanup)
		r.Cleanup = stopErr == nil && drainErr == nil
		return r
	}
	r.Cleanup = true
	if !success() {
		r.Outcome = "exit-failed"
	} else if r.Progress != "complete" {
		r.Outcome = "incomplete"
	}
	return r
}
func runArms(run func(string) result) []result {
	out := []result{}
	for _, arm := range arms {
		r := run(arm)
		r.Arm = arm
		out = append(out, r)
		if !r.Cleanup {
			break
		}
	}
	return out
}
func contrast(rs []result) string {
	if len(rs) != 4 {
		return "incomplete"
	}
	for _, r := range rs {
		if !r.Cleanup {
			return "incomplete"
		}
	}
	if rs[0].Outcome == "deadline" && rs[1].Outcome == "completed" && rs[2].Outcome == "completed" && rs[3].Outcome == "deadline" {
		return "order-associated"
	}
	if rs[0].Outcome == "completed" && rs[1].Outcome == "completed" && rs[2].Outcome == "completed" && rs[3].Outcome == "completed" {
		return "not-reproduced"
	}
	return "inconclusive"
}

func probe(script, arm, home string, parent map[string]string) result {
	r := result{Outcome: "setup-failed", Progress: "unknown", Cleanup: true}
	if e := os.Mkdir(filepath.Join(home, ".codex"), 0700); e != nil {
		return r
	}
	// Working directory shape, empty native home, script, and env are controlled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := proc.CommandContext(context.WithoutCancel(ctx), filepath.Join(parent["SystemRoot"], "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = fixtureEnvironment(parent, home)
	if cmd.Env == nil {
		return r
	}
	cmd.Dir = home
	cmd.Stdout = io.Discard
	p := newProgress(arm)
	cmd.Stderr = p
	return runCommand(ctx, cmd, p, func(c *exec.Cmd) (owner, error) {
		t, e := proc.StartTreeDrained(c)
		if t == nil {
			return nil, e
		}
		return t, e
	})
}

func runCommand(ctx context.Context, cmd *exec.Cmd, p *progress, start func(*exec.Cmd) (owner, error)) result {
	tree, e := start(cmd)
	if e != nil {
		r := result{Outcome: "start-failed", Progress: p.snapshot(), Cleanup: false}
		if tree != nil {
			cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			stopErr := tree.Stop()
			r.Cleanup = tree.Wait(cleanup) == nil && stopErr == nil
		}
		return r
	}
	if tree == nil {
		return result{Outcome: "start-failed", Progress: p.snapshot(), Cleanup: false}
	}
	return waitOwned(ctx, tree, func() bool { return cmd.ProcessState != nil && cmd.ProcessState.Success() }, p.snapshot)
}

func admitted(get func(string) string) bool {
	sha := get("GITHUB_SHA")
	if len(sha) != 40 {
		return false
	}
	if _, e := hex.DecodeString(sha); e != nil {
		return false
	}
	return runtime.GOOS == "windows" && get("GITHUB_ACTIONS") == "true" && get("RUNNER_ENVIRONMENT") == "github-hosted" && get("RUNNER_OS") == "Windows" && get("GITHUB_REPOSITORY") == "breakingtime126/magpie" && get("GITHUB_REF") == "refs/heads/experiment/app-server-bridge-v0.1.3035" && get("GITHUB_EVENT_NAME") == "push" && get("APPX_DIAGNOSTIC_VERIFIED_SHA") == sha && get("APPX_DIAGNOSTIC_OPT_IN") == optIn
}
func main() { os.Exit(run()) }
func run() int {
	if !admitted(os.Getenv) {
		fmt.Println("appx-import-diagnostic-v1 status=admission-refused")
		return 1
	}
	source, e := os.ReadFile("internal/nativepreview/discovery_windows.go")
	if e != nil {
		fmt.Println("appx-import-diagnostic-v1 status=source-unavailable")
		return 1
	}
	scripts, e := deriveScripts(source)
	if e != nil {
		fmt.Println("appx-import-diagnostic-v1 status=source-refused")
		return 1
	}
	parent := map[string]string{}
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			parent[k] = v
		}
	}
	// Windows names are case-insensitive. Normalize only fixed allowed names.
	for _, name := range []string{"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "CommonProgramFiles", "ProgramData"} {
		if value, present := os.LookupEnv(name); present {
			parent[name] = value
		}
	}
	if !filepath.IsAbs(parent["SystemRoot"]) || !filepath.IsAbs(os.Getenv("RUNNER_TEMP")) {
		fmt.Println("appx-import-diagnostic-v1 status=paths-refused")
		return 1
	}
	root, e := os.MkdirTemp(os.Getenv("RUNNER_TEMP"), "appx-import-order-")
	if e != nil {
		fmt.Println("appx-import-diagnostic-v1 status=setup-refused")
		return 1
	}
	rs := runArms(func(arm string) result {
		home, e := os.MkdirTemp(root, "home-")
		if e != nil {
			return result{Outcome: "setup-failed", Progress: "unknown", Cleanup: false}
		}
		r := probe(scripts[arm], arm, home, parent)
		clean := "unknown"
		if r.Cleanup {
			clean = "confirmed"
		}
		fmt.Printf("appx-import-diagnostic-v1 arm=%s outcome=%s progress=%s cleanup=%s\n", arm, r.Outcome, r.Progress, clean)
		return r
	})
	fmt.Printf("appx-import-diagnostic-v1 contrast=%s causal-proof=none acceptance=not-tested\n", contrast(rs))
	// Never remove evidence/owned paths while drainage is uncertain. The runner
	// disposes all fresh directories; this harness clears no cache or live state.
	if len(rs) != 4 {
		return 1
	}
	for _, r := range rs {
		if !r.Cleanup || r.Outcome == "setup-failed" || r.Outcome == "start-failed" || r.Outcome == "wait-failed" {
			return 1
		}
	}
	return 0 // Diagnostic execution completed, NOT original GUI acceptance.
}
