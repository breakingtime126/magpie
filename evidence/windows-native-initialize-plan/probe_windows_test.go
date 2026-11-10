//go:build windows

// Local reviewed-scope candidate only. No installed-runtime or chat acceptance.
package nativeinitialize

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const prefix = "MAGPIE_NATIVE_INITIALIZE_"
const outputLimit = 64 << 10

var trustedEnv = []string{"SystemRoot", "WINDIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"}

// Source-reviewed runner CLI directory selectors, not a credential-free claim.
// Native's audited OpenAI-only startup never invokes their CLI/auth chains.
// Fixed/common KnownFolder expansion context, preserved only from originals.
var machineDirectoryEnv = []string{"ProgramData", "SystemDrive", "ALLUSERSPROFILE"}

var reviewedRunnerDirectories = []string{"AZURE_CONFIG_DIR", "AZURE_EXTENSION_DIR", "AZURE_DEVOPS_CACHE_DIR", "AZ_DEVOPS_GLOBAL_CONFIG_DIR"}

// Conservative admission check, not a claim of exhaustive native environment schema.
// Unknown values are never forwarded. Proxy/CA names outside the reviewed inventory
// abort rather than being silently dropped; recognized values are copied unchanged.
func ambientAdmission() error { return admitEntries(os.Environ()) }
func admitEntries(entries []string) error {
	reviewed := map[string]bool{}
	for _, name := range reviewedRunnerDirectories {
		reviewed[name] = true
	}
	known := map[string]bool{}
	for _, k := range trustedEnv {
		known[strings.ToUpper(k)] = true
	}
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		u := strings.ToUpper(name)
		if reviewed[u] {
			continue
		}
		// Harness-owned Go offline mode is not a native proxy setting and is not forwarded.
		if u == "GOPROXY" && value == "off" {
			continue
		}
		for _, p := range []string{"CODEX_", "OPENAI_", "AWS_", "AZURE_", "AZ_", "GOOGLE_", "GCP_", "OTEL_"} {
			if strings.HasPrefix(u, p) {
				return errors.New("inherited application/provider environment rejected")
			}
		}
		if strings.HasSuffix(u, "_API_KEY") || strings.HasSuffix(u, "_ACCESS_TOKEN") {
			return errors.New("inherited auth environment rejected")
		}
		for _, part := range []string{"PROXY", "CERT", "SSL", "TLS", "TRUST", "CA_BUNDLE"} {
			if strings.Contains(u, part) && !known[u] {
				return errors.New("unreviewed proxy/trust environment rejected")
			}
		}
	}
	return nil
}
func baseEnv() []string { return preservedEnv(os.LookupEnv) }
func preservedEnv(lookup func(string) (string, bool)) []string {
	out := []string{}
	names := append(append(append([]string{}, trustedEnv...), reviewedRunnerDirectories...), machineDirectoryEnv...)
	for _, name := range names {
		if v, ok := lookup(name); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}
func policyAbsent() error {
	programData, e := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if e = assertNormalProgramData(programData, e, os.Getenv(prefix+"PROGRAMDATA_SHA256")); e != nil {
		return e
	}
	// A native env-based ProgramData route must agree with the independent API.
	if e = assertProgramDataVariable(programData, os.Getenv("ProgramData")); e != nil {
		return e
	}
	// Stronger than the two config/requirements files: refuse the entire vendor
	// policy directory if present, without reading any policy/config contents.
	_, e = os.Lstat(filepath.Join(programData, "OpenAI", "Codex"))
	if !errors.Is(e, os.ErrNotExist) {
		return errors.New("normal machine policy directory present or unreadable")
	}
	key, e := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\OpenAI\Codex`, registry.READ|registry.WOW64_64KEY)
	if e == nil {
		key.Close()
		return errors.New("normal 64-bit machine policy key present")
	}
	if !errors.Is(e, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(e, windows.ERROR_PATH_NOT_FOUND) {
		return errors.New("normal machine policy absence unproven")
	}
	return nil
}
func marker(dir, name string, data any) error {
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	tmp := filepath.Join(dir, name+".tmp")
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}
func waitMarker(dir, name string, until time.Time) error {
	for time.Now().Before(until) {
		if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("helper marker deadline")
}

type report struct {
	ConfigSHA256, CatalogSHA256                                                              string
	Version                                                                                  string
	StartupExitZero, InitializeExitZero, PolicyAbsent                                        bool
	InitializeRequests                                                                       int
	StartupStdout, StartupStderr, InitializeStdout, InitializeStderr                         int
	StartupStdoutSHA256, StartupStderrSHA256, InitializeStdoutSHA256, InitializeStderrSHA256 string
}
type streamResult struct {
	data []byte
	err  error
}

func readBounded(r *os.File, done chan<- streamResult) {
	b, e := io.ReadAll(io.LimitReader(r, outputLimit+1))
	if len(b) > outputLimit {
		e = errors.New("native output limit exceeded")
	}
	done <- streamResult{b, e}
}
func exited(c *exec.Cmd, e error) error {
	if c.ProcessState == nil || !c.ProcessState.Exited() {
		return errors.New("native process reaping unproven")
	}
	if e != nil || !c.ProcessState.Success() {
		return errors.New("native process did not exit successfully")
	}
	return nil
}

// Each native process inherits root's non-breakaway Job through this helper.
// File stdio stays owned here; no exec.Cmd I/O copying goroutines are relied on.
func runNative(exe, home, cwd string, initialize bool, version string, stage *string) (out, errout []byte, result error) {
	*stage = "native_policy"
	if e := policyAbsent(); e != nil {
		return nil, nil, e
	}
	*stage = "native_inputs"
	if e := localInputsAbsent(home, cwd); e != nil {
		return nil, nil, e
	}
	*stage = "startup_command"
	if initialize {
		*stage = "initialize_command"
	}
	args := []string{"app-server", "--strict-config", "--print-startup-config"}
	if initialize {
		args = []string{"app-server", "--strict-config", "--stdio"}
	}
	c := exec.Command(exe, args...)
	c.Dir = cwd
	c.Env = baseEnv()
	c.Env = append(c.Env, "HOME="+home, "USERPROFILE="+home, "CODEX_HOME="+home, "APPDATA="+home, "LOCALAPPDATA="+home, "TEMP="+filepath.Join(home, "tmp"), "TMP="+filepath.Join(home, "tmp"), "PATH="+filepath.Join(os.Getenv("SystemRoot"), "System32"))
	stdoutR, stdoutW, e := os.Pipe()
	if e != nil {
		return nil, nil, e
	}
	defer stdoutR.Close()
	defer stdoutW.Close()
	stderrR, stderrW, e := os.Pipe()
	if e != nil {
		return nil, nil, e
	}
	defer stderrR.Close()
	defer stderrW.Close()
	c.Stdout = stdoutW
	c.Stderr = stderrW
	var stdinR, stdinW *os.File
	if initialize {
		stdinR, stdinW, e = os.Pipe()
		if e != nil {
			return nil, nil, e
		}
		defer stdinR.Close()
		defer stdinW.Close()
		c.Stdin = stdinR
	}
	if e = c.Start(); e != nil {
		return nil, nil, errors.New("native start failed")
	}
	stdoutW.Close()
	stderrW.Close()
	if stdinR != nil {
		stdinR.Close()
	}
	waited := false
	wait := make(chan error, 1)
	go func() { wait <- c.Wait() }()
	defer func() {
		if !waited {
			_ = c.Process.Kill()
			select {
			case <-wait:
				waited = true
				if c.ProcessState == nil || !c.ProcessState.Exited() {
					*stage = "cleanup"
					result = errors.Join(result, errors.New("failed native cleanup reaping"))
				}
			case <-time.After(3 * time.Second):
				*stage = "cleanup"
				result = errors.Join(result, errors.New("native cleanup deadline"))
			}
		}
	}()
	stderrDone := make(chan streamResult, 1)
	go readBounded(stderrR, stderrDone)
	stdoutDone := make(chan streamResult, 1)
	if !initialize {
		go readBounded(stdoutR, stdoutDone)
	} else {
		first := make(chan streamResult, 1)
		go func() {
			r := bufio.NewReader(io.LimitReader(stdoutR, outputLimit+1))
			line, e := r.ReadBytes('\n')
			if len(line) > outputLimit {
				e = errors.New("native response limit")
			}
			first <- streamResult{line, e}
			rest, re := io.ReadAll(r)
			all := append(line, rest...)
			if len(all) > outputLimit {
				re = errors.New("native output limit")
			}
			stdoutDone <- streamResult{all, re}
		}()
		wrote := make(chan error, 1)
		go func() { _, e := io.WriteString(stdinW, initializeRequest); wrote <- e }()
		select {
		case e := <-wrote:
			if e != nil {
				return nil, nil, errors.New("initialize write failed")
			}
		case <-time.After(3 * time.Second):
			return nil, nil, errors.New("initialize write deadline")
		}
		*stage = "initialize_assertion"
		select {
		case frame := <-first:
			if frame.err != nil {
				return nil, nil, errors.New("initialize response assertion failed")
			}
			if assertionErr := assertInitialize(frame.data, version, home); assertionErr != nil {
				return nil, nil, assertionErr
			}
		case s := <-stderrDone:
			if s.err != nil {
				return nil, nil, errors.New("native stderr limit/read failure")
			}
			return nil, nil, errors.New("native stderr closed before initialization")
		case <-time.After(15 * time.Second):
			return nil, nil, errors.New("initialize response deadline")
		}
		// EOF immediately after the matching response; never send initialized,
		// model/list, account, thread, turn, tool, or any other request.
		if e = stdinW.Close(); e != nil {
			return nil, nil, errors.New("native stdin EOF failed")
		}
	}
	*stage = "startup_exit_output"
	if initialize {
		*stage = "initialize_exit_output"
	}
	until := time.NewTimer(20 * time.Second)
	defer until.Stop()
	var so, se *streamResult
	var waitErr error
	for !waited || so == nil || se == nil {
		select {
		case v := <-stdoutDone:
			so = &v
			stdoutDone = nil
			if v.err != nil {
				return nil, nil, errors.New("native stdout bounded read failed")
			}
		case v := <-stderrDone:
			se = &v
			stderrDone = nil
			if v.err != nil {
				return nil, nil, errors.New("native stderr bounded read failed")
			}
		case waitErr = <-wait:
			waited = true
			wait = nil
			if e = exited(c, waitErr); e != nil {
				return nil, nil, e
			}
		case <-until.C:
			return nil, nil, errors.New("native exit/output deadline")
		}
	}
	if initialize {
		// Exactly one response and terminal newline; no additional JSON/requests,
		// notifications, diagnostics, or non-whitespace protocol output accepted.
		*stage = "initialize_assertion"
		line := bytes.TrimSuffix(so.data, []byte("\n"))
		if bytes.Contains(line, []byte("\n")) || assertInitialize(line, version, home) != nil {
			return nil, nil, errors.New("unexpected extra native stdout")
		}
	}
	return so.data, se.data, nil
}
func lockFile(path string) (*os.File, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
func fixture(dir string) (home, cwd string, locks []*os.File, hashes map[string]string, e error) {
	hashes = map[string]string{}
	home = filepath.Join(dir, "codex-home")
	cwd = filepath.Join(dir, "cwd")
	for _, p := range []string{home, cwd, filepath.Join(home, "tmp")} {
		if e = os.Mkdir(p, 0700); e != nil {
			return
		}
	}
	catalog := filepath.Join(home, "synthetic-catalog.json")
	config, configErr := authoredConfig(home)
	if configErr != nil {
		e = configErr
		return
	}
	for path, data := range map[string]string{catalog: syntheticCatalog, filepath.Join(home, "config.toml"): config} {
		// Hash only authored bytes before writing/locking, never native catalog data.
		hashes[filepath.Base(path)] = digest([]byte(data))
		if e = os.WriteFile(path, []byte(data), 0600); e != nil {
			return
		}
		var f *os.File
		f, e = lockFile(path)
		if e != nil {
			return
		}
		locks = append(locks, f)
	}
	return
}
func TestInitializeHelper(t *testing.T) {
	if os.Getenv(prefix+"ROLE") == "" {
		t.Skip("private helper")
	}
	dir := os.Getenv(prefix + "DIR")
	if !filepath.IsAbs(dir) || os.Getenv(prefix+"ROLE") != "parent" {
		t.Fatal("invalid helper role/path")
	}
	token, e := os.ReadFile(filepath.Join(dir, "token"))
	if e != nil || len(token) != 32 || string(token) != os.Getenv(prefix+"TOKEN") {
		t.Fatal("invalid helper token")
	}
	if e = ambientAdmission(); e != nil {
		t.Fatal(e)
	}
	go func() { time.Sleep(55 * time.Second); os.Exit(90) }()
	stage := "helper_gate"
	fail := func(e error) {
		d := safeFailure(stage, e)
		_ = marker(dir, "failure", d)
		t.Fatalf("helper failure stage=%s reason=%s", d.Stage, d.Reason)
	}
	if e = waitMarker(dir, "go", time.Now().Add(10*time.Second)); e != nil {
		fail(e)
	}
	stage = "helper_policy"
	if e = policyAbsent(); e != nil {
		fail(e)
	}
	exe := os.Getenv(prefix + "EXE")
	stage = "raw_layout"
	if e = rawLayout(exe); e != nil {
		fail(e)
	}
	stage = "raw_signature_hash"
	locked, e := verifyNative(exe)
	if e != nil {
		fail(e)
	}
	defer locked.Close()
	stage = "fixture"
	home, cwd, locks, fixtureHashes, e := fixture(dir)
	defer func() {
		for _, f := range locks {
			f.Close()
		}
	}()
	if e != nil {
		fail(e)
	}
	out, stderr, e := runNative(exe, home, cwd, false, "", &stage)
	if e != nil {
		fail(e)
	}
	stage = "startup_response"
	version, e := startupVersion(out)
	if e != nil {
		fail(e)
	}
	result := report{ConfigSHA256: fixtureHashes["config.toml"], CatalogSHA256: fixtureHashes["synthetic-catalog.json"], Version: version, StartupExitZero: true, PolicyAbsent: true, StartupStdout: len(out), StartupStderr: len(stderr), StartupStdoutSHA256: digest(out), StartupStderrSHA256: digest(stderr)}
	out, stderr, e = runNative(exe, home, cwd, true, version, &stage)
	if e != nil {
		fail(e)
	}
	result.InitializeExitZero = true
	result.InitializeRequests = 1
	result.InitializeStdout = len(out)
	result.InitializeStderr = len(stderr)
	result.InitializeStdoutSHA256 = digest(out)
	result.InitializeStderrSHA256 = digest(stderr)
	stage = "report"
	if e = marker(dir, "result", result); e != nil {
		fail(e)
	}
}

type jobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}

func drainJob(job windows.Handle) error {
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var a jobAccounting
		e := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&a)), uint32(unsafe.Sizeof(a)), nil)
		if e != nil {
			return e
		}
		if a.Active == 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("owned Job drain deadline")
}
func TestRawStrictStartupInitialize(t *testing.T) {
	if os.Getenv(prefix+"EXECUTE") != "strict-initialize-only-v1" {
		t.Skip("requires reviewed exact-native initialize opt-in")
	}
	if os.Getenv(prefix+"ROLE") != "" {
		t.Fatal("root cannot be helper")
	}
	if e := ambientAdmission(); e != nil {
		t.Fatal(e)
	}
	if e := policyAbsent(); e != nil {
		t.Fatal(e)
	}
	t.Log("Normal policy location matches original KnownFolder; normal machine policy absent (paths withheld)")
	exe := os.Getenv(prefix + "EXE")
	if e := rawLayout(exe); e != nil {
		t.Fatal(e)
	}
	locked, e := verifyNative(exe)
	if e != nil {
		t.Fatal(e)
	}
	defer locked.Close()
	dir := t.TempDir()
	var random [16]byte
	if _, e = rand.Read(random[:]); e != nil {
		t.Fatal(e)
	}
	token := hex.EncodeToString(random[:])
	if e = os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600); e != nil {
		t.Fatal(e)
	}
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
		t.Fatal(e)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestInitializeHelper$", "-test.timeout=60s")
	c.Dir = dir
	c.Env = baseEnv()
	c.Env = append(c.Env, prefix+"ROLE=parent", prefix+"DIR="+dir, prefix+"TOKEN="+token, prefix+"EXE="+exe, prefix+"PROGRAMDATA_SHA256="+os.Getenv(prefix+"PROGRAMDATA_SHA256"))
	if e = c.Start(); e != nil {
		t.Fatal(e)
	}
	waited := false
	wait := make(chan error, 1)
	go func() { wait <- c.Wait() }()
	defer func() {
		_ = windows.TerminateJobObject(job, 99)
		_ = c.Process.Kill()
		if !waited {
			select {
			case <-wait:
				if c.ProcessState == nil || !c.ProcessState.Exited() {
					t.Error("helper cleanup reaping unproven")
				}
			case <-time.After(3 * time.Second):
				t.Error("helper cleanup deadline")
			}
		}
		if e = drainJob(job); e != nil {
			t.Error(e)
		}
	}()
	var assignErr error
	e = c.Process.WithHandle(func(h uintptr) { assignErr = windows.AssignProcessToJobObject(job, windows.Handle(h)) })
	if e = errors.Join(e, assignErr); e != nil {
		t.Fatal("owned Job assignment failed")
	}
	if e = marker(dir, "go", true); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-wait:
		waited = true
		if e = exited(c, e); e != nil {
			f, openErr := os.Open(filepath.Join(dir, "failure"))
			if openErr == nil {
				b, readErr := io.ReadAll(io.LimitReader(f, 1025))
				f.Close()
				if readErr == nil {
					if d, ok := decodeFailure(b); ok {
						t.Fatalf("helper native lifecycle failed: stage=%s reason=%s; raw output withheld", d.Stage, d.Reason)
					}
				}
			}
			t.Fatal("helper native lifecycle failed: bounded diagnostic unavailable; raw output withheld")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("helper deadline")
	}
	if e = drainJob(job); e != nil {
		t.Fatal(e)
	}
	var r report
	b, e := os.ReadFile(filepath.Join(dir, "result"))
	if e != nil || json.Unmarshal(b, &r) != nil || !r.PolicyAbsent || !r.StartupExitZero || !r.InitializeExitZero || r.InitializeRequests != 1 {
		t.Fatal("missing successful lifecycle report")
	}
	expectedConfig, configErr := authoredConfig(filepath.Join(dir, "codex-home"))
	if configErr != nil || r.ConfigSHA256 != digest([]byte(expectedConfig)) || r.CatalogSHA256 != digest([]byte(syntheticCatalog)) {
		t.Fatal("authored fixture report hash mismatch")
	}
	for name, want := range map[string]string{"config.toml": r.ConfigSHA256, "synthetic-catalog.json": r.CatalogSHA256} {
		data, err := os.ReadFile(filepath.Join(dir, "codex-home", name))
		if err != nil || digest(data) != want {
			t.Fatal("authored fixture bytes changed")
		}
	}
	t.Logf("Authored fixture SHA256 config/catalog: %s %s", r.ConfigSHA256, r.CatalogSHA256)
	t.Logf("Output SHA256 (startup stdout/stderr; initialize stdout/stderr): %s %s %s %s", r.StartupStdoutSHA256, r.StartupStderrSHA256, r.InitializeStdoutSHA256, r.InitializeStderrSHA256)
	t.Logf("RAW STRICT INITIALIZE PASS ONLY: reported compile-time appServerVersion=%q; one initialize request; policy absent; successful EOF exit and owned Job drain; stdout sizes=%d/%d stderr sizes=%d/%d; no installed-runtime/model/chat claim", r.Version, r.StartupStdout, r.InitializeStdout, r.StartupStderr, r.InitializeStderr)
}
func TestAmbientAdmissionRejectsApplicationSettings(t *testing.T) {
	for _, name := range []string{"CODEX_ACCESS_TOKEN", "OPENAI_WORKLOAD_IDENTITY_CONTEXT", "AWS_PROFILE", "OTEL_EXPORTER_OTLP_ENDPOINT", "UNREVIEWED_TLS_SETTING"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "synthetic")
			if ambientAdmission() == nil {
				t.Fatal("application/unknown trust environment accepted")
			}
		})
	}
}

func rawLayout(exe string) error {
	if !filepath.IsAbs(exe) {
		return errors.New("raw EXE path must be absolute")
	}
	entries, e := os.ReadDir(filepath.Dir(exe))
	if e != nil || len(entries) != 1 || !strings.EqualFold(entries[0].Name(), filepath.Base(exe)) {
		return errors.New("raw EXE must be alone in a fresh directory")
	}
	for _, p := range []string{exe, filepath.Dir(exe)} {
		name, e := windows.UTF16PtrFromString(p)
		if e != nil {
			return e
		}
		a, e := windows.GetFileAttributes(name)
		if e != nil || a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("raw EXE/layout reparse point rejected")
		}
	}
	return nil
}
func localInputsAbsent(home, cwd string) error {
	for _, name := range []string{"auth.json", ".env", "environments.toml", "requirements.toml", "codex-package.json"} {
		_, e := os.Lstat(filepath.Join(home, name))
		if !errors.Is(e, os.ErrNotExist) {
			return errors.New("unexpected local auth/environment/config input")
		}
	}
	for d := cwd; ; d = filepath.Dir(d) {
		_, e := os.Lstat(filepath.Join(d, ".codex"))
		if !errors.Is(e, os.ErrNotExist) {
			return errors.New("ancestor project configuration present or unreadable")
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	return nil
}

func digest(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }

func authoredConfig(home string) (string, error) {
	catalog := filepath.Join(home, "synthetic-catalog.json")
	if strings.Contains(catalog, "'") || strings.ContainsAny(catalog, "\r\n") {
		return "", errors.New("unsafe TOML fixture path")
	}
	config := "model = \"synthetic-lifecycle-only\"\nmodel_catalog_json = '" + catalog + "'\ncli_auth_credentials_store = \"file\"\nmcp_oauth_credentials_store = \"file\"\n[skills.bundled]\nenabled = false\n[features]\nplugins = false\n[analytics]\nenabled = false\n[otel]\nexporter = \"none\"\ntrace_exporter = \"none\"\nmetrics_exporter = \"none\"\n"
	return config, nil
}

func TestReviewedRunnerDirectoriesPreserved(t *testing.T) {
	values := map[string]string{}
	entries := []string{}
	for i, name := range reviewedRunnerDirectories {
		value := "  synthetic directory = " + string(rune('A'+i)) + " \u7a7a  "
		if i == 3 {
			value = ""
		} // Presence with an empty value must remain present.
		values[name] = value
		entries = append(entries, name+"="+value)
	}
	if admitEntries(entries) != nil {
		t.Fatal("exact reviewed directory selectors rejected")
	}
	got := preservedEnv(func(name string) (string, bool) { v, ok := values[name]; return v, ok })
	if len(got) != len(values) {
		t.Fatal("directory-selector inventory changed")
	}
	for _, entry := range got {
		name, value, ok := strings.Cut(entry, "=")
		want, present := values[name]
		if !ok || !present || value != want {
			t.Fatal("directory-selector bytes not preserved")
		}
	}
	for _, name := range []string{"AZURE_ACCESS_TOKEN", "AZURE_CLIENT_SECRET", "AZURE_CONFIG_DIR_EXTRA", "AZ_DEVOPS_EXT_PAT", "AZ_DEVOPS_GLOBAL_CONFIG_DIR_EXTRA"} {
		if admitEntries(append(append([]string{}, entries...), name+"=synthetic")) == nil {
			t.Fatal("unreviewed Azure authority variable accepted")
		}
	}
}

// The reference is captured by the workflow's normal KnownFolder API BEFORE
// environment clearing. It is an equality witness, never a fallback path.
func assertNormalProgramData(path string, lookupErr error, expected string) error {
	if lookupErr != nil {
		var hr syscall.Errno
		if errors.As(lookupErr, &hr) {
			return fmt.Errorf("normal ProgramData lookup unavailable (HRESULT=0x%08X)", uint32(hr))
		}
		return errors.New("normal ProgramData lookup unavailable (non-HRESULT error)")
	}
	if !filepath.IsAbs(path) {
		return errors.New("normal ProgramData lookup returned nonabsolute path")
	}
	decoded, e := hex.DecodeString(expected)
	if e != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != expected {
		return errors.New("missing/invalid original KnownFolder reference")
	}
	if digest([]byte(path)) != expected {
		return errors.New("post-clear KnownFolder result differs from original")
	}
	return nil
}
func TestNormalPolicyLocationIdentity(t *testing.T) {
	path, _ := filepath.Abs("synthetic-knownfolder")
	expected := digest([]byte(path))
	if assertNormalProgramData(path, nil, expected) != nil {
		t.Fatal("matching authored reference rejected")
	}
	for _, test := range []struct {
		path, expected string
		err            error
	}{
		{path, "", nil}, {path, "not-a-digest", nil}, {path, digest([]byte(path + "-other")), nil}, {"relative", expected, nil}, {path, expected, syscall.Errno(0x800700cb)},
	} {
		if assertNormalProgramData(test.path, test.err, test.expected) == nil {
			t.Fatal("unproven original policy location accepted")
		}
	}
	e := assertNormalProgramData(path, syscall.Errno(0x800700cb), expected)
	if e == nil || !strings.Contains(e.Error(), "HRESULT=0x800700CB") || strings.Contains(e.Error(), path) {
		t.Fatal("HRESULT diagnostic absent or path disclosed")
	}
	if assertProgramDataVariable(path, path) != nil || assertProgramDataVariable(path, path+"-other") == nil || assertProgramDataVariable(path, "") == nil {
		t.Fatal("ProgramData environment agreement failed")
	}
	values := map[string]string{"ProgramData": "synthetic data directory", "SystemDrive": "synthetic drive", "ALLUSERSPROFILE": ""}
	env := preservedEnv(func(name string) (string, bool) { v, ok := values[name]; return v, ok })
	if len(env) != len(values) {
		t.Fatal("system-directory preservation inventory mismatch")
	}
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		if want, ok := values[k]; !ok || v != want {
			t.Fatal("original system-directory value changed")
		}
	}
}

func assertProgramDataVariable(normal, ambient string) error {
	if !filepath.IsAbs(ambient) || !strings.EqualFold(filepath.Clean(normal), filepath.Clean(ambient)) {
		return errors.New("original ProgramData environment does not match normal KnownFolder")
	}
	return nil
}
