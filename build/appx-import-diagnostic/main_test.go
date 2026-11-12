package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func source(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../../internal/nativepreview/discovery_windows.go")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestBoundScripts(t *testing.T) {
	b := source(t)
	scripts, e := deriveScripts(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, arm := range []string{"A", "C"} {
		s := scripts[arm]
		if strings.Contains(s, "$p=@(") || strings.Contains(s, "& $getPackage") || strings.Contains(s, "OpenAI.Codex") {
			t.Fatal("package query reached probe")
		}
		if !strings.Contains(s, "$assembly=$appxCommand.ImplementingType.Assembly") || !strings.Contains(s, "[System.Management.Automation.PSModuleInfo]") {
			t.Fatal("identity logic missing")
		}
		a := strings.Index(s, "$appxModule=Import-QueryModule 'Appx'")
		m := strings.Index(s, "$managementModule=Import-QueryModule 'Microsoft.PowerShell.Management'")
		u := strings.Index(s, "$utilityModule=Import-QueryModule 'Microsoft.PowerShell.Utility'")
		if arm == "A" && !(a < m && m < u) || arm == "C" && !(m < u && u < a) {
			t.Fatal("wrong import order")
		}
	}
	for _, change := range []string{strings.Replace(string(b), "$loaded.Count -ne 1", "$loaded.Count -ne 2", 1), strings.Replace(string(b), "const installedQuery", "var installedQuery", 1), string(b) + "\nconst installedQuery = `x`\n", strings.Replace(string(b), "exit 2", "exit 0", 1), strings.Replace(string(b), "-LiteralPath $f", "-Path $f", 1), strings.Replace(string(b), "MAGPIE_QUERY_V1:management", "MAGPIE_QUERY_V1:utility", 1)} {
		if _, e := deriveScripts([]byte(change)); e == nil {
			t.Fatal("changed source accepted")
		}
	}
	if !reflect.DeepEqual(arms, []string{"A", "C", "C", "A"}) {
		t.Fatal("order changed")
	}
}

// Exercise derivation independently of the whole-source hash: changing the
// reviewed production order must never silently turn A into C or repeat imports.
func TestDeriveImportScriptsIndependentOfOrder(t *testing.T) {
	appx := "$appxModule=Import-QueryModule 'Appx'\n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:appx')\n"
	management := "$managementModule=Import-QueryModule 'Microsoft.PowerShell.Management'\n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:management')\n"
	utility := "$utilityModule=Import-QueryModule 'Microsoft.PowerShell.Utility'\n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:utility')\n"
	prefix := "reviewed helpers\n[Console]::Error.WriteLine('MAGPIE_QUERY_V1:setup')\n"
	suffix := "$getPackage=Get-QueryCommand $appxModule 'Get-AppxPackage'\nnever execute package suffix"
	end := "[Console]::Error.WriteLine('APPX_IMPORT_DIAG_V1:complete')\n"
	want := map[string]string{"A": prefix + appx + management + utility + end, "C": prefix + management + utility + appx + end}
	for _, imports := range []string{appx + management + utility, appx + utility + management, management + appx + utility, management + utility + appx, utility + appx + management, utility + management + appx} {
		got, err := deriveImportScripts(prefix + imports + suffix)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("production import order altered diagnostic arms: %v", err)
		}
	}
	for _, imports := range []string{management + utility, appx + management + utility + appx, appx + management + utility + "extra\n", strings.Replace(appx, ":appx'", ":wrong'", 1) + management + utility, appx + utility + strings.Replace(management, "Microsoft.PowerShell.Management", "Other", 1)} {
		if _, err := deriveImportScripts(prefix + imports + suffix); err == nil {
			t.Fatal("changed import block accepted")
		}
	}
	for _, query := range []string{appx + management + utility + suffix, prefix + appx + management + utility, prefix + prefix + appx + management + utility + suffix, prefix + appx + management + utility + suffix + suffix} {
		if _, err := deriveImportScripts(query); err == nil {
			t.Fatal("missing or duplicate boundary accepted")
		}
	}
}

func TestEnvironment(t *testing.T) {
	parent := map[string]string{"SystemRoot": `C:\Windows`, "WINDIR": `C:\Windows`, "ProgramFiles": `C:\Program Files`, "PSModulePath": "PRIVATE", "OPENAI_API_KEY": "PRIVATE", "GITHUB_TOKEN": "PRIVATE", "USERPROFILE": "PRIVATE", "HTTP_PROXY": "inherited-proxy", "SSL_CERT_FILE": "inherited-cert", "PATH": "PRIVATE"}
	a := fixtureEnvironment(parent, `C:\owned\one`)
	b := fixtureEnvironment(parent, `C:\owned\two`)
	for _, env := range [][]string{a, b} {
		for _, v := range env {
			if strings.Contains(v, "PRIVATE") {
				t.Fatal("ambient environment leaked")
			}
		}
	}
	expected := []string{"APPDATA", "CODEX_HOME", "HOME", "HTTP_PROXY", "LOCALAPPDATA", "PATH", "ProgramFiles", "SSL_CERT_FILE", "SystemRoot", "TEMP", "TMP", "USERPROFILE", "WINDIR", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"}
	keys := []string{}
	for _, v := range a {
		keys = append(keys, strings.SplitN(v, "=", 2)[0])
	}
	if !reflect.DeepEqual(keys, expected) {
		t.Fatalf("keys %v", keys)
	}
	for i, v := range a {
		if strings.ReplaceAll(v, `C:\owned\one`, `HOME`) != strings.ReplaceAll(b[i], `C:\owned\two`, `HOME`) {
			t.Fatal("non-home environment drift")
		}
	}
}
func TestParser(t *testing.T) {
	p := newProgress("A")
	for _, line := range markerOrder("A") {
		for _, c := range []byte(line + "\r\n") {
			p.Write([]byte{c})
		}
	}
	if p.snapshot() != "complete" {
		t.Fatal(p.snapshot())
	}
	for _, input := range []string{"PRIVATE\n", strings.Repeat("X", 100000) + "\n", markerOrder("A")[1] + "\n", markerOrder("A")[0]} {
		p := newProgress("A")
		p.Write([]byte(input))
		if p.snapshot() != "unknown" {
			t.Fatal("invalid input advanced")
		}
	}
	p = newProgress("A")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.Write([]byte("PRIVATE\n"))
		}
	}()
	for i := 0; i < 1000; i++ {
		_ = p.snapshot()
	}
	wg.Wait()
}

type fakeOwner struct {
	waits   int
	stopped int
	first   error
	cleanup error
}

func (f *fakeOwner) Wait(context.Context) error {
	f.waits++
	if f.waits == 1 {
		return f.first
	}
	return f.cleanup
}
func (f *fakeOwner) Stop() error { f.stopped++; return nil }
func TestLifetime(t *testing.T) {
	for _, tc := range []struct {
		name           string
		first, cleanup error
		want           string
		clean          bool
	}{{"complete", nil, nil, "completed", true}, {"deadline", context.DeadlineExceeded, nil, "deadline", true}, {"uncertain", context.DeadlineExceeded, errors.New("private"), "deadline", false}, {"wait-error", errors.New("private"), nil, "wait-failed", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOwner{first: tc.first, cleanup: tc.cleanup}
			r := waitOwned(context.Background(), f, func() bool { return true }, func() string { return "complete" })
			if r.Outcome != tc.want || r.Cleanup != tc.clean {
				t.Fatalf("%+v", r)
			}
			want := 1
			if tc.first != nil {
				want = 2
			}
			if f.waits != want {
				t.Fatal("wait observer count")
			}
		})
	}
	f := &fakeOwner{}
	r := waitOwned(context.Background(), f, func() bool { return false }, func() string { return "complete" })
	if r.Outcome != "exit-failed" {
		t.Fatal("nonzero root claimed success")
	}
	f = &fakeOwner{}
	r = waitOwned(context.Background(), f, func() bool { return true }, func() string { return "appx-import" })
	if r.Outcome != "incomplete" {
		t.Fatal("missing marker claimed success")
	}
}
func TestStopsLaterArms(t *testing.T) {
	calls := 0
	rs := runArms(func(arm string) result { calls++; return result{Outcome: "deadline", Cleanup: calls != 2} })
	if calls != 2 || len(rs) != 2 {
		t.Fatal("ran after uncertain cleanup")
	}
	for _, rs := range [][]result{{{Outcome: "completed", Cleanup: true}}, {{Outcome: "deadline", Cleanup: true}}, {{Outcome: "deadline", Cleanup: true}, {Outcome: "completed", Cleanup: true}, {Outcome: "completed", Cleanup: true}, {Outcome: "completed", Cleanup: true}}} {
		if contrast(rs) == "order-associated" {
			t.Fatal("false causal contrast")
		}
	}
	rs = []result{{Outcome: "deadline", Cleanup: true}, {Outcome: "completed", Cleanup: true}, {Outcome: "completed", Cleanup: true}, {Outcome: "deadline", Cleanup: true}}
	if contrast(rs) != "order-associated" {
		t.Fatal("contrast missing")
	}
}

func TestPartialStart(t *testing.T) {
	for _, cleanup := range []error{nil, errors.New("private")} {
		f := &fakeOwner{first: cleanup}
		r := runCommand(context.Background(), &exec.Cmd{}, newProgress("A"), func(*exec.Cmd) (owner, error) { return f, errors.New("private") })
		if r.Outcome != "start-failed" || r.Cleanup != (cleanup == nil) || f.stopped != 1 || f.waits != 1 {
			t.Fatalf("%+v", r)
		}
	}
	r := runCommand(context.Background(), &exec.Cmd{}, newProgress("A"), func(*exec.Cmd) (owner, error) { return nil, errors.New("private") })
	if r.Cleanup {
		t.Fatal("startup uncertainty became safe")
	}
}
func TestLateCompletionRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeOwner{}
	r := waitOwned(ctx, f, func() bool { return true }, func() string { return "complete" })
	if r.Outcome != "cancelled" {
		t.Fatal("late success accepted")
	}
}
func TestSourceLaunchContract(t *testing.T) {
	b, e := os.ReadFile("main.go")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, literal := range []string{"proc.StartTreeDrained(c)", "context.WithTimeout(context.Background(),10*time.Second)", "cmd.Stdout=io.Discard", "-NoProfile", "-NonInteractive", "-Command"} {
		if !strings.Contains(strings.ReplaceAll(s, " ", ""), strings.ReplaceAll(literal, " ", "")) {
			t.Fatal("launch contract missing", literal)
		}
	}
	for _, bad := range []string{"cmd.Wait()", "WaitDelay", "Set-ExecutionPolicy", "RemoveAll(", "exec.Command("} {
		if strings.Contains(s, bad) {
			t.Fatal("forbidden launch/cleanup change", bad)
		}
	}
}

func TestWorkflowScope(t *testing.T) {
	b, e := os.ReadFile("../../.github/workflows/experimental-appx-import-order.yml")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, required := range []string{"branches: [experiment/app-server-bridge-v0.1.3035]", "build/appx-import-diagnostic/**", "runs-on: windows-2025", "persist-credentials: false", "RUNNER_ENVIRONMENT", "git rev-parse HEAD", "APPX_DIAGNOSTIC_VERIFIED_SHA", "[appx-import-order-v1]", "github.event_name == 'push'", "./build/appx-import-diagnostic"} {
		if !strings.Contains(s, required) {
			t.Fatal("workflow guard missing", required)
		}
	}
	for _, forbidden := range []string{"workflow_dispatch:", "pull_request", "secrets.", "go test ./...", "package-bridge", "original-desktop-acceptance", "Import-Module"} {
		if strings.Contains(s, forbidden) {
			t.Fatal("workflow widened", forbidden)
		}
	}
}

func TestMixedCaseEnvironment(t *testing.T) {
	parent := map[string]string{"sYsTeMrOoT": `C:\Windows`, "Http_Proxy": "proxy-value", "Ssl_Cert_File": "cert-value", "requestS_CA_bundle": "bundle-value", "Github_Token": "PRIVATE"}
	env := fixtureEnvironment(parent, `C:\owned`)
	seen := map[string]bool{}
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToLower(key)
		if seen[key] {
			t.Fatal("duplicate Windows name")
		}
		seen[key] = true
	}
	text := strings.Join(env, "\n")
	for _, value := range []string{"HTTP_PROXY=proxy-value", "SSL_CERT_FILE=cert-value", "REQUESTS_CA_BUNDLE=bundle-value", `SystemRoot=C:\Windows`} {
		if !strings.Contains(text, value) {
			t.Fatal("case-insensitive allowlist dropped value", value)
		}
	}
	if strings.Contains(text, "PRIVATE") {
		t.Fatal("credential inherited")
	}
}

func TestConflictingWindowsEnvironmentRefused(t *testing.T) {
	if fixtureEnvironment(map[string]string{"HTTP_PROXY": "one", "http_proxy": "two"}, "home") != nil {
		t.Fatal("conflicting Windows names accepted")
	}
	env := fixtureEnvironment(map[string]string{"HTTP_PROXY": "same", "http_proxy": "same"}, "home")
	count := 0
	for _, s := range env {
		if strings.HasPrefix(s, "HTTP_PROXY=") {
			count++
		}
	}
	if count != 1 {
		t.Fatal("same Windows name duplicated")
	}
}

func TestFullEnvironmentShape(t *testing.T) {
	keys := []string{"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "CommonProgramFiles", "ProgramData", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "CODEX_CA_CERTIFICATE", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"}
	parent := map[string]string{}
	for _, k := range keys {
		parent[k] = "value-" + k
	}
	home := t.TempDir()
	env := fixtureEnvironment(parent, home)
	got := map[string]string{}
	for _, v := range env {
		k, value, _ := strings.Cut(v, "=")
		got[k] = value
	}
	for _, k := range keys {
		if got[k] != parent[k] {
			t.Fatal("preserved setting changed", k)
		}
	}
	for _, k := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		if got[k] != home {
			t.Fatal("home not isolated", k)
		}
	}
	if got["PATH"] != filepath.Join(parent["SystemRoot"], "System32") || got["CODEX_HOME"] != filepath.Join(home, ".codex") || len(got) != len(keys)+10 {
		t.Fatal("unexpected environment shape")
	}
}

func TestProtocolContradictions(t *testing.T) {
	for _, arm := range []string{"A", "C"} {
		t.Run(arm, func(t *testing.T) {
			markers := markerOrder(arm)
			valid := strings.Join(markers, "\n") + "\n"
			cases := map[string]string{
				"duplicate-before-valid":      markers[0] + "\n" + valid,
				"out-of-order-before-valid":   markers[1] + "\n" + valid,
				"malformed-before-valid":      "MAGPIE_QUERY_V1:corrupt\n" + valid,
				"short-protocol-before-valid": "MAGPIE_QUERY_V1\n" + valid,
				"complete-then-duplicate":     valid + markers[0] + "\n",
				"complete-then-corrupt":       valid + "APPX_IMPORT_DIAG_V1:wrong\n",
				"complete-then-partial":       valid + "APPX_IMPORT_DIAG_V1:com",
				"complete-then-short-partial": valid + "MAGPIE_QUERY_V",
				"complete-then-cr-partial":    valid + "APPX_IMPORT_DIAG_V1:complete\r",
				"malformed-cr-before-valid":   "MAGPIE_QUERY_V1:entry\rX\n" + valid,
			}
			for name, input := range cases {
				t.Run(name, func(t *testing.T) {
					p := newProgress(arm)
					for _, b := range []byte(input) {
						p.Write([]byte{b})
					}
					if p.snapshot() != "unknown" {
						t.Fatal("contradictory/partial protocol admitted", p.snapshot())
					}
					r := waitOwned(context.Background(), &fakeOwner{}, func() bool { return true }, p.snapshot)
					if r.Outcome == "completed" {
						t.Fatal("invalid evidence completed")
					}
				})
			}
			for _, tail := range []string{"PRIVATE\n", "PRIVATE", "APPX_IMPORT_DIARY unrelated\n", strings.Repeat("X", 100000) + "\n"} {
				p := newProgress(arm)
				p.Write([]byte("unrelated stderr\n" + valid + tail))
				if p.snapshot() != "complete" {
					t.Fatal("unrelated stderr changed valid result")
				}
			}
		})
	}
}
func TestCorruptCDoesNotImplyOrderAssociation(t *testing.T) {
	p := newProgress("C")
	p.Write([]byte(markerOrder("C")[0] + "\n" + strings.Join(markerOrder("C"), "\n") + "\n"))
	c := waitOwned(context.Background(), &fakeOwner{}, func() bool { return true }, p.snapshot)
	rs := []result{{Outcome: "deadline", Cleanup: true}, c, c, {Outcome: "deadline", Cleanup: true}}
	if contrast(rs) == "order-associated" {
		t.Fatal("corrupt C evidence became an import-order contrast")
	}
}

func TestParserSaturatesAndChunks(t *testing.T) {
	p := newProgress("A")
	p.Write([]byte(strings.Repeat("X", 1000000)))
	if p.offset != 255 || p.mask != 0 || p.stage != 0 {
		t.Fatal("unbounded parser state")
	}
	b, e := os.ReadFile("main.go")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "size := min(len(data), 256)") {
		t.Fatal("parser write lock not chunked")
	}
}
