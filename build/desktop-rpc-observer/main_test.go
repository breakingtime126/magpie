package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

func events(t *testing.T, b []byte) []event {
	t.Helper()
	var result []event
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		result = append(result, e)
	}
	return result
}

func TestObservationCorrelatesWithoutPayloads(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(`{"id":"SECRET_ID","method":"initialize","params":{"token":"SECRET_TOKEN","capabilities":{"requestAttestation":true,"explicitGatewayOauth":false,"requiredHumanApproval":true,"experimentalApi":false,"other":"SECRET_PROOF"}}}`))
	o.line("from_bridge", []byte(`{"id":"SECRET_ID","error":{"code":-32602,"message":"unsupported authentication capability","data":"SECRET_ACCOUNT"}}`))
	es := events(t, log.Bytes())
	if len(es) != 2 || es[0].Correlation == 0 || es[0].Correlation != es[1].Correlation ||
		es[1].Method != "initialize" || es[1].Code == nil || *es[1].Code != -32602 ||
		es[1].Class != "unsupported_authentication_capability" {
		t.Fatalf("missing correlated error: %+v", es)
	}
	if es[0].RequestAttestation == nil || !*es[0].RequestAttestation ||
		es[0].ExplicitGatewayOauth == nil || *es[0].ExplicitGatewayOauth ||
		es[0].RequiredHumanApproval == nil || !*es[0].RequiredHumanApproval ||
		es[0].ExperimentalAPI == nil || *es[0].ExperimentalAPI {
		t.Fatal("initialize booleans lost")
	}
	if strings.Contains(log.String(), "SECRET") {
		t.Fatal("raw payload leaked")
	}
	if es[0].Sequence >= es[1].Sequence {
		t.Fatal("sequence not monotonic")
	}
}

func TestBothRequestDirectionsAndEscapedIDs(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.line("to_bridge", []byte(`{"id":"a","method":"account/read","params":{}}`))
	o.line("from_bridge", []byte(`{"id":"a","method":"attestation/generate","params":{"proof":"SECRET"}}`))
	o.line("to_bridge", []byte(`{"id":"\u0061","error":{"code":-32000,"message":"SECRET"}}`))
	o.line("from_bridge", []byte(`{"id":"a","result":{"token":"SECRET"}}`))
	es := events(t, log.Bytes())
	if es[0].Correlation != es[3].Correlation || es[1].Correlation != es[2].Correlation ||
		es[0].Correlation == es[1].Correlation {
		t.Fatal("directions collided")
	}
	if es[2].Class != "unknown" || strings.Contains(log.String(), "SECRET") {
		t.Fatal("unknown error leaked")
	}
}

func TestExactErrorClassificationOnly(t *testing.T) {
	if errorClass("magpie startup probe unavailable") != "startup_probe_unavailable" {
		t.Fatal("exact startup probe error not classified")
	}
	for _, message := range []string{
		"magpie startup probe unavailable: SECRET",
		"SECRET magpie startup probe unavailable",
		"magpie startup probe unavailable ",
	} {
		if errorClass(message) != "unknown" {
			t.Fatal("non-exact startup probe error classified")
		}
	}
	if errorClass("unsupported authentication capability: SECRET") != "unknown" {
		t.Fatal("error classification used substring matching")
	}
	if allowedMethod("configRequirements/read/SECRET") != "unknown" {
		t.Fatal("method classification used prefix matching")
	}
}

func TestArgumentValuesNeverRecorded(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.argv([]string{"app-server", "--listen=SECRET", "--startup-client-info", "SECRET",
		"-c", "features.code_mode_host=SECRET",
		"--config=features.prefer_mxc=SECRET_MXC",
		"-c", "features.windows_sandbox_service=SECRET_SANDBOX",
		"--config", "cli_auth_credentials_store=SECRET_STORE",
		"--config=SECRET_KEY=SECRET",
		"--SECRET_FLAG=SECRET", "SECRET", "--check"})
	es := events(t, log.Bytes())
	keys := make(map[string]bool)
	var check bool
	for _, e := range es {
		if e.ConfigKey != "" {
			keys[e.ConfigKey] = true
		}
		check = check || e.Flag == "--check"
	}
	for _, key := range []string{"features.code_mode_host", "features.prefer_mxc",
		"features.windows_sandbox_service", "cli_auth_credentials_store"} {
		if !keys[key] {
			t.Fatalf("missing known config key %s", key)
		}
	}
	if len(keys) != 4 || !check || strings.Contains(log.String(), "SECRET") {
		t.Fatal("argv metadata unsafe or incomplete")
	}
}

type fragmentedReader struct {
	b    []byte
	step int
}

func (r *fragmentedReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := r.step
	if n > len(r.b) {
		n = len(r.b)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, r.b[:n])
	r.b = r.b[n:]
	return n, nil
}
func TestForwardingIsByteExactAcrossChunksAndOversizedLines(t *testing.T) {
	payload := append([]byte("not JSON SECRET\x00\xff\r\n"), bytes.Repeat([]byte("SECRET"), maxLine)...)
	payload = append(payload, []byte("\n{\"id\":1,\"method\":\"configRequirements/read\",\"params\":{}}\npartial SECRET")...)
	for _, step := range []int{1, 7, 8192} {
		var forwarded, log bytes.Buffer
		o := newObserver(&log)
		p := &lineObserver{o: o, direction: "to_bridge"}
		if err := pump(&forwarded, &fragmentedReader{append([]byte(nil), payload...), step}, p); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, forwarded.Bytes()) {
			t.Fatal("wire bytes changed")
		}
		if len(p.buf) > maxLine || cap(p.buf) > maxLine*2 {
			t.Fatal("line buffer grew without bound")
		}
		if strings.Contains(log.String(), "SECRET") {
			t.Fatal("oversized/invalid payload leaked")
		}
		var oversized, request, partial, eof bool
		for _, e := range events(t, log.Bytes()) {
			oversized = oversized || e.Class == "oversized"
			request = request || e.Method == "configRequirements/read"
			partial = partial || e.Class == "unterminated"
			eof = eof || e.Class == "eof"
		}
		if !oversized || !request || !partial || !eof {
			t.Fatal("stream classifications missing")
		}
	}
}
func TestTraceAndPendingCaps(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	for i := 0; i < 3000; i++ {
		b, _ := json.Marshal(map[string]any{"id": i, "method": "configRequirements/read", "params": map[string]any{"secret": "SECRET"}})
		o.line("to_bridge", b)
	}
	if log.Len() > maxTrace || len(o.pending) > maxPending {
		t.Fatal("cap exceeded")
	}
	events(t, log.Bytes()) // Budget never leaves a truncated JSON record.
	if strings.Contains(log.String(), "SECRET") {
		t.Fatal("trace leaked payload")
	}
}
func TestConcurrentTraceSequence(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				o.emit(event{Kind: "test"})
			}
		}()
	}
	wg.Wait()
	var previous uint64
	for _, e := range events(t, log.Bytes()) {
		if e.Sequence <= previous {
			t.Fatal("sequence reordered")
		}
		previous = e.Sequence
	}
}

func TestTraceSlotsBoundAggregateAndNeverOverwrite(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "trace")
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := openTrace(prefix)
			if err != nil {
				t.Error(err)
				return
			}
			if f != nil {
				f.WriteString("sentinel")
				f.Close()
			}
		}()
	}
	wg.Wait()
	files, err := filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 16 {
		t.Fatalf("slot count %d, error %v", len(files), err)
	}
	f, err := openTrace(prefix)
	if err != nil || f != nil {
		t.Fatal("exhausted budget must disable observation")
	}
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil || string(b) != "sentinel" {
			t.Fatal("existing trace overwritten")
		}
	}
}

func TestSetupRejectsRelativeRecursiveAndScriptTargets(t *testing.T) {
	t.Setenv(activeEnv, "")
	t.Setenv("MAGPIE_CI_TRACE_FILE", filepath.Join(t.TempDir(), "trace"))
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", "relative")
	if _, _, err := setup(); err == nil {
		t.Fatal("relative executable accepted")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", self)
	if _, _, err := setup(); err == nil {
		t.Fatal("self recursion accepted")
	}
	script := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", script)
	if _, _, err := setup(); err == nil {
		t.Fatal("script target accepted")
	}
	t.Setenv(activeEnv, "1")
	if _, _, err := setup(); err == nil {
		t.Fatal("active observer accepted")
	}
}

type captureCloser struct {
	bytes.Buffer
	closed bool
}

func (w *captureCloser) Close() error { w.closed = true; return nil }

// The real native test executable is the child fixture, with no shell/network.
// It checks argv, echoes stdin exactly, writes secret-looking stderr unchanged,
// then exits nonzero. None of the fixture's payload may enter the metadata.
func TestObserverChildFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_TEST_CHILD") != "1" {
		return
	}
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "SECRET_ARG" || args[len(args)-1] != "space value" {
		os.Exit(41)
	}
	if os.Getenv(activeEnv) != "1" {
		os.Exit(42)
	}
	_, err := io.Copy(os.Stdout, os.Stdin)
	if err != nil {
		os.Exit(43)
	}
	os.Stderr.WriteString("SECRET_STDERR\x00\xff\r\n")
	os.Exit(23)
}
func TestChildLifecycleAndByteForwarding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MAGPIE_OBSERVER_TEST_CHILD", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("{\"id\":\"SECRET_ID\",\"method\":\"configRequirements/read\",\"params\":{\"secret\":\"SECRET\"}}\r\n\x00\xff")
	var output, diagnostics captureCloser
	var log bytes.Buffer
	o := newObserver(&log)
	code := runChild(self, []string{"-test.run=^TestObserverChildFixture$", "--", "SECRET_ARG", "space value"},
		io.NopCloser(bytes.NewReader(payload)), &output, &diagnostics, o)
	if code != 23 {
		t.Fatalf("exit = %d, want 23", code)
	}
	if !bytes.Equal(output.Bytes(), payload) || diagnostics.String() != "SECRET_STDERR\x00\xff\r\n" {
		t.Fatal("stdout or stderr changed")
	}
	if !output.closed || !diagnostics.closed {
		t.Fatal("child EOF not propagated")
	}
	if strings.Contains(log.String(), "SECRET") {
		t.Fatal("child data entered trace")
	}
	var start, exit bool
	for _, e := range events(t, log.Bytes()) {
		start = start || e.Kind == "child_start" && e.ChildPID > 0
		exit = exit || e.Kind == "child_exit" && e.Exit != nil && *e.Exit == 23
	}
	if !start || !exit {
		t.Fatal("child lifecycle metadata missing")
	}
}

func TestEarlyExitFixture(t *testing.T) {
	if os.Getenv("MAGPIE_OBSERVER_EARLY_EXIT") != "1" {
		return
	}
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "SECRET_ARG" ||
		os.Args[len(os.Args)-1] != "space value" {
		os.Exit(41)
	}
	os.Stdout.WriteString("SECRET_STDOUT\x00\xff\r\n")
	os.Stderr.WriteString("SECRET_STDERR\x00\xff\r\n")
	os.Exit(19)
}

// A generous deadline is only a hang guard, not a timing assertion. Both the
// offline build and observer are owned/reaped even when this guard fires.
func runOwnedTestCommand(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- tree.Wait() }()
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		tree.Kill()
		<-done
		t.Fatal("owned test command did not exit before hang guard")
		return nil
	}
}

func TestWindowsExecutableExitsWithInheritedStdinOpen(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("actual observer entry point is Windows-only")
	}
	work := t.TempDir()
	// Keep Go caches pinned in the invoking environment; build offline before
	// switching to the fixture's private home.
	observerExe := filepath.Join(work, "observer.exe")
	build := proc.Command("go", "build", "-o", observerExe, ".")
	build.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	var buildLog bytes.Buffer
	build.Stdout, build.Stderr = &buildLog, &buildLog
	if err := runOwnedTestCommand(t, build); err != nil {
		t.Fatalf("offline observer build failed: %v\n%s", err, buildLog.String())
	}
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv(activeEnv, "")
	t.Setenv("MAGPIE_OBSERVER_EARLY_EXIT", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGPIE_CI_REAL_BRIDGE", self)
	prefix := filepath.Join(work, "trace")
	t.Setenv("MAGPIE_CI_TRACE_FILE", prefix)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close() // Keep the inherited input open for the entire observer run.
	cmd := proc.Command(observerExe, "-test.run=^TestEarlyExitFixture$", "--", "SECRET_ARG", "space value")
	cmd.Stdin = r
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runOwnedTestCommand(t, cmd)
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 19 {
		t.Fatalf("observer exit = %v, want child exit 19", err)
	}
	// Cmd.Wait has joined both actual output pipes: bytes and EOF propagated.
	if stdout.String() != "SECRET_STDOUT\x00\xff\r\n" ||
		stderr.String() != "SECRET_STDERR\x00\xff\r\n" {
		t.Fatal("observer changed stdout/stderr or added diagnostics")
	}
	files, err := filepath.Glob(prefix + ".*.jsonl")
	if err != nil || len(files) != 1 {
		t.Fatalf("trace count %d: %v", len(files), err)
	}
	log, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(log) > maxTrace || bytes.Contains(log, []byte("SECRET")) {
		t.Fatal("unsafe trace")
	}
	var exited, outputEOF, errorEOF bool
	for _, e := range events(t, log) {
		exited = exited || e.Kind == "child_exit" && e.Exit != nil && *e.Exit == 19
		outputEOF = outputEOF || e.Kind == "stream_end" && e.Direction == "from_bridge" && e.Class == "eof"
		errorEOF = errorEOF || e.Kind == "stream_end" && e.Direction == "stderr" && e.Class == "eof"
	}
	if !exited || !outputEOF || !errorEOF {
		t.Fatal("missing child exit or output EOF metadata")
	}
}

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestShortWritesAreNotRetriedOrDuplicated(t *testing.T) {
	err := pump(shortWriter{}, strings.NewReader("bytes"), nil)
	if err != io.ErrShortWrite {
		t.Fatalf("error = %v", err)
	}
}

func TestDuplicateIDRemainsQuarantinedAfterThirdRequest(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	for _, method := range []string{"account/read", "model/list", "configRequirements/read"} {
		b, _ := json.Marshal(map[string]any{"id": "SECRET", "method": method, "params": map[string]any{}})
		o.line("to_bridge", b)
	}
	o.line("from_bridge", []byte(`{"id":"SECRET","result":{}}`))
	es := events(t, log.Bytes())
	if len(es) != 4 || es[1].Class != "duplicate_id" || es[2].Class != "duplicate_id" ||
		es[3].Correlation != 0 || es[3].Method != "" || es[3].Class != "unmatched_response" {
		t.Fatal("third request stole an earlier response")
	}
}

func TestCorrelationCapacityDisablesFurtherMatching(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	for i := 0; i <= maxPending; i++ {
		b, _ := json.Marshal(map[string]any{"id": i, "method": "account/read", "params": map[string]any{}})
		o.line("to_bridge", b)
	}
	if !o.correlationDisabled || len(o.pending) != 0 || len(o.quarantined) != 0 {
		t.Fatal("capacity did not permanently disable tracking")
	}
	o.line("from_bridge", []byte(`{"id":0,"result":{}}`))
	o.line("to_bridge", []byte(`{"id":0,"method":"model/list","params":{}}`))
	o.line("from_bridge", []byte(`{"id":0,"result":{}}`))
	es := events(t, log.Bytes())
	for _, e := range es[len(es)-3:] {
		if e.Kind == "rpc" && e.Class == "response" {
			t.Fatal("matching resumed after lost state")
		}
	}
	if es[len(es)-1].Correlation != 0 {
		t.Fatal("response received false attribution")
	}
}

func TestStoppedObservationCannotWriteFromLateInput(t *testing.T) {
	var log bytes.Buffer
	o := newObserver(&log)
	o.emit(event{Kind: "test"})
	o.stop()
	before := log.String()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				o.line("to_bridge", []byte(`{"id":"SECRET","method":"initialize","params":{}}`))
				o.emit(event{Kind: "copy_error", Direction: "to_bridge"})
			}
		}()
	}
	wg.Wait()
	if log.String() != before {
		t.Fatal("late stdin wrote after trace finalization")
	}
}
