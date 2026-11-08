// Command desktop-rpc-observer is a TEST-ONLY Windows stdio observer for disposable CI.
// It is not installed or selected by production bridge packaging. It requires
// MAGPIE_CI_REAL_BRIDGE and MAGPIE_CI_TRACE_FILE to be explicit absolute paths.
// TRACE_FILE is a filename prefix: up to 16 exclusive numbered files (64 KiB
// each) are created. Further invocations forward without observation.
// Never run this diagnostic against a user's account, credentials or live home.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yetone/magpie/internal/proc"
)

const (
	maxTrace   = 64 << 10
	maxLine    = 64 << 10
	maxPending = 256
	activeEnv  = "MAGPIE_CI_OBSERVER_ACTIVE"
)

// event has no field capable of containing a raw RPC payload, ID, argv value,
// path or stderr. Strings are fixed labels or the verified public dictionary's
// digest; dynamic public key matches expose only bounded dictionary indices.
type event struct {
	FixtureReadOnly *fixtureReadOnlyTurn `json:"fixtureReadOnly,omitempty"`

	ConfigBatchWrite *configBatchWriteObservation `json:"configBatchWrite,omitempty"`

	ModelCatalog      *modelCatalogObservation      `json:"modelCatalog,omitempty"`
	OwnedModelCatalog *ownedModelCatalogObservation `json:"ownedModelCatalog,omitempty"`

	Sequence                  uint64   `json:"seq"`
	PID                       int      `json:"pid"`
	Kind                      string   `json:"kind"`
	Direction                 string   `json:"direction,omitempty"`
	Method                    string   `json:"method,omitempty"`
	Correlation               uint64   `json:"correlation,omitempty"`
	Code                      *int64   `json:"code,omitempty"`
	Class                     string   `json:"class,omitempty"`
	RequestAttestation        *bool    `json:"requestAttestation,omitempty"`
	ExplicitGatewayOauth      *bool    `json:"explicitGatewayOauth,omitempty"`
	RequiredHumanApproval     *bool    `json:"requiredHumanApproval,omitempty"`
	ExperimentalAPI           *bool    `json:"experimentalApi,omitempty"`
	TracePresent              *bool    `json:"tracePresent,omitempty"`
	TraceValid                *bool    `json:"traceValid,omitempty"`
	AuthParamsValid           *bool    `json:"authParamsValid,omitempty"`
	IncludeTokenRequested     *bool    `json:"includeTokenRequested,omitempty"`
	RefreshTokenRequested     *bool    `json:"refreshTokenRequested,omitempty"`
	ConfigParamsValid         *bool    `json:"configParamsValid,omitempty"`
	ConfigIncludeLayers       *bool    `json:"configIncludeLayers,omitempty"`
	ConfigCwdProvided         *bool    `json:"configCwdProvided,omitempty"`
	FeatureListParamsValid    *bool    `json:"featureListParamsValid,omitempty"`
	FeatureListCursorProvided *bool    `json:"featureListCursorProvided,omitempty"`
	FeatureListLimitProvided  *bool    `json:"featureListLimitProvided,omitempty"`
	FeatureListThreadProvided *bool    `json:"featureListThreadIdProvided,omitempty"`
	ProjectListParamsValid    *bool    `json:"projectListParamsValid,omitempty"`
	ProjectListCursorProvided *bool    `json:"projectListCursorProvided,omitempty"`
	ProjectListLimitProvided  *bool    `json:"projectListLimitProvided,omitempty"`
	ProjectListSortKey        string   `json:"projectListSortKey,omitempty"`
	ProjectListSortDirection  string   `json:"projectListSortDirection,omitempty"`
	ThreadListProjectIDShape  string   `json:"threadListProjectIdShape,omitempty"`
	ThreadListParentIDShape   string   `json:"threadListParentIdShape,omitempty"`
	ThreadListAncestorIDShape string   `json:"threadListAncestorIdShape,omitempty"`
	ThreadListAttachedIDShape string   `json:"threadListAttachedIdShape,omitempty"`
	ThreadListSectionIDShape  string   `json:"threadListSectionIdShape,omitempty"`
	ThreadListSortKey         string   `json:"threadListSortKey,omitempty"`
	FSReadParamsShapeValid    *bool    `json:"fsReadParamsShapeValid,omitempty"`
	OwnedCatalogPathMatches   *bool    `json:"ownedCatalogPathMatches,omitempty"`
	FSReadPathForm            string   `json:"fsReadPathForm,omitempty"`
	FSReadBasename            string   `json:"fsReadBasename,omitempty"`
	FSReadLexicalRoot         string   `json:"fsReadLexicalRoot,omitempty"`
	Flag                      string   `json:"flag,omitempty"`
	ConfigKey                 string   `json:"configKey,omitempty"`
	ConfigValue               string   `json:"configValue,omitempty"`
	ConfigShape               string   `json:"configShape,omitempty"`
	ConfigFamily              string   `json:"configFamily,omitempty"`
	ConfigPluginShape         string   `json:"configPluginShape,omitempty"`
	ConfigLiteral             string   `json:"configLiteral,omitempty"`
	ConfigLexicalRoot         string   `json:"configLexicalRoot,omitempty"`
	ConfigKeyClasses          []string `json:"configKeyClasses,omitempty"`
	PublicPluginRef           int      `json:"publicPluginRef,omitempty"`
	PublicPluginCatalogSHA256 string   `json:"publicPluginCatalogSHA256,omitempty"`
	ChildPID                  int      `json:"child_pid,omitempty"`
	Exit                      *int     `json:"exit,omitempty"`
}

type pendingKey struct {
	direction string
	digest    [32]byte
}
type pendingRequest struct {
	correlation          uint64
	method               string
	modelCatalogEligible bool
	ownedCatalogEligible bool
}
type observer struct {
	fixtureGuard        *fixtureReadOnlyGuard
	mu                  sync.Mutex
	out                 io.Writer
	size                int
	sequence            uint64
	nextCorrelation     uint64
	pending             map[pendingKey]pendingRequest
	quarantined         map[pendingKey]bool
	correlationDisabled bool
	stopped             bool
	publicPlugins       *publicPluginCatalog
	readTargetRoots     *readTargetRoots
	ownedModelCatalog   *ownedModelCatalog
}

func newObserver(w io.Writer) *observer {
	return &observer{out: w, pending: make(map[pendingKey]pendingRequest), quarantined: make(map[pendingKey]bool)}
}

// emitLocked never writes part of a record just to fill the remaining budget.
// On trace IO failure observation stops, but forwarding continues unchanged.
func (o *observer) emitLocked(e event) {
	if o.stopped || o.out == nil {
		return
	}
	o.sequence++
	e.Sequence = o.sequence
	e.PID = os.Getpid()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	budget := maxTrace
	if o.fixtureGuard != nil {
		budget = 128 << 10
	}
	if len(b) > budget-o.size {
		o.out = nil
		return
	}
	n, err := o.out.Write(b)
	o.size += n
	if err != nil || n != len(b) {
		o.out = nil
	}
}
func (o *observer) emit(e event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.emitLocked(e)
}

// stop detaches the sink while holding the same lock as every trace write.
// The Windows executable exits immediately after runChild; an inherited stdin
// read need not be cancellable, so it must not hold up that exit or retain a
// usable trace sink after runChild returns.
func (o *observer) stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	o.out = nil
	o.pending = nil
	o.quarantined = nil
}

func allowedMethod(s string) string {
	// These are existing non-client labels, kept outside the client dictionary.
	switch s {
	case "initialized":
		return "initialized"
	case "attestation/generate":
		return "attestation/generate"
	}
	if fixed, known := reviewedClientMethods[s]; known {
		return fixed
	}
	return "unknown"
}
func errorClass(s string) string {
	switch s {
	case "magpie startup probe unavailable":
		return "startup_probe_unavailable"
	case "unsupported authentication capability":
		return "unsupported_authentication_capability"
	case "initialize and initialized are required", "successful initialization of both backends is required":
		return "initialization_required"
	case "config requirements inspection is disabled":
		return "config_requirements_disabled"
	case "config requirements read failed or returned unsupported configuration":
		return "config_requirements_failed"
	case "primary gateway OAuth inspection is disabled":
		return "gateway_oauth_disabled"
	case "method is not supported by this experimental bridge":
		return "unsupported_method"
	case "invalid capabilities":
		return "invalid_capabilities"
	case "invalid initialization message":
		return "invalid_initialization"
	case "already initialized":
		return "already_initialized"
	}
	return "unknown"
}

// IDs are kept only as in-memory digests. Neither their bytes nor hashes enter
// the trace. Typed string decoding makes escaped spellings correlate correctly.
func digestID(raw json.RawMessage) ([32]byte, bool) {
	var zero [32]byte
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return zero, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return sha256.Sum256([]byte("s:" + s)), true
	}
	var n json.Number
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return zero, false
	}
	n, ok := v.(json.Number)
	if !ok {
		return zero, false
	}
	return sha256.Sum256([]byte("n:" + n.String())), true
}
func boolField(m map[string]json.RawMessage, key string) *bool {
	b := m[key]
	if !bytes.Equal(b, []byte("true")) && !bytes.Equal(b, []byte("false")) {
		return nil
	}
	v := bytes.Equal(b, []byte("true"))
	return &v
}
func (o *observer) line(direction string, b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return
	}
	e := event{Kind: "rpc", Direction: direction}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m == nil {
		e.Class = "unrecognized"
		o.emitLocked(e)
		return
	}
	var method string
	if raw, hasMethod := m["method"]; hasMethod {
		if json.Unmarshal(raw, &method) != nil {
			e.Class = "unrecognized"
			o.emitLocked(e)
			return
		}
		e.Method = allowedMethod(method)
		e.Class = "notification"
		if id, ok := digestID(m["id"]); ok {
			o.nextCorrelation++
			e.Correlation = o.nextCorrelation
			e.Class = "request"
			key := pendingKey{direction, id}
			if o.correlationDisabled {
				e.Class = "correlation_limit"
			} else if o.quarantined[key] {
				e.Class = "duplicate_id"
			} else if _, exists := o.pending[key]; exists {
				// Never reuse this ID again in this invocation: a third request
				// must not steal the response to either earlier request.
				delete(o.pending, key)
				o.quarantined[key] = true
				e.Class = "duplicate_id"
			} else if len(o.pending)+len(o.quarantined) < maxPending {
				o.pending[key] = pendingRequest{correlation: e.Correlation, method: e.Method,
					modelCatalogEligible: direction == "to_bridge" && method == "model/list" && modelCatalogRequestEligible(b),
					ownedCatalogEligible: direction == "to_bridge" && method == "config/read" && ownedCatalogRequestEligible(b)}
			} else {
				// Losing tracking state must not create later false matches.
				o.correlationDisabled = true
				clear(o.pending)
				clear(o.quarantined)
				e.Class = "correlation_limit"
			}
		}
		if direction == "to_bridge" && e.Class == "request" &&
			(method == "getAuthStatus" || method == "configRequirements/read" ||
				method == "config/read" || method == "experimentalFeature/list" ||
				method == "project/list" || method == "fs/readFile") {
			e.TracePresent, e.TraceValid = observedRequestTrace(b, method)
		}
		if direction == "to_bridge" && method == "getAuthStatus" && e.Class == "request" {
			e.AuthParamsValid, e.IncludeTokenRequested, e.RefreshTokenRequested = observedGetAuthStatus(b)
		}
		if direction == "to_bridge" && e.Class == "request" {
			switch method {
			case "thread/list":
				observedThreadList(b, &e)
			case "project/list":
				o.observedProjectList(b, &e)
			case "fs/readFile":
				o.observedFSReadFile(b, &e)
			case "config/batchWrite":
				e.ConfigBatchWrite = observedConfigBatchWrite(b, o.readTargetRoots)
			case "config/read":
				e.ConfigParamsValid, e.ConfigIncludeLayers, e.ConfigCwdProvided = observedConfigRead(b)
			case "experimentalFeature/list":
				e.FeatureListParamsValid, e.FeatureListCursorProvided, e.FeatureListLimitProvided,
					e.FeatureListThreadProvided = observedExperimentalFeatureList(b)
			}
		}
		if method == "initialize" {
			var p, c map[string]json.RawMessage
			if json.Unmarshal(m["params"], &p) == nil && json.Unmarshal(p["capabilities"], &c) == nil {
				e.RequestAttestation = boolField(c, "requestAttestation")
				e.ExplicitGatewayOauth = boolField(c, "explicitGatewayOauth")
				e.RequiredHumanApproval = boolField(c, "requiredHumanApproval")
				e.ExperimentalAPI = boolField(c, "experimentalApi")
			}
		}
	} else {
		_, result := m["result"]
		rawError, failure := m["error"]
		if result == failure {
			e.Class = "unrecognized"
			o.emitLocked(e)
			return
		}
		e.Class = "unmatched_response"
		opposite := "to_bridge"
		if direction == opposite {
			opposite = "from_bridge"
		}
		if id, ok := digestID(m["id"]); ok {
			key := pendingKey{opposite, id}
			if p, found := o.pending[key]; found {
				e.Correlation, e.Method = p.correlation, p.method
				e.Class = "response"
				delete(o.pending, key)
				if direction == "from_bridge" && result && p.modelCatalogEligible {
					e.ModelCatalog = observedModelCatalog(b)
				}
				if direction == "from_bridge" && result && p.ownedCatalogEligible && o.ownedModelCatalog != nil {
					e.OwnedModelCatalog = o.ownedModelCatalog.observeConfig(b)
				}
			}
		}
		if failure {
			var failureObject struct {
				Code    *int64 `json:"code"`
				Message string `json:"message"`
			}
			e.Class = "unknown"
			if json.Unmarshal(rawError, &failureObject) == nil {
				e.Code = failureObject.Code
				e.Class = errorClass(failureObject.Message)
			}
			if direction == "from_bridge" && e.Method == "thread/list" && e.Class == "unknown" {
				e.Class = observedThreadListRejection(b)
			}
			e.Kind = "rpc_error"
		}
	}
	o.emitLocked(e)
}

// lineObserver is a bounded side tap, not a framing layer. Incomplete and
// oversized lines are still forwarded immediately, including arbitrary binary
// bytes; only their observation is omitted/classified.
type lineObserver struct {
	o         *observer
	direction string
	buf       []byte
	oversized bool
}

func (p *lineObserver) feed(b []byte) {
	for _, c := range b {
		if c == '\n' {
			if p.oversized {
				p.o.emit(event{Kind: "rpc", Direction: p.direction, Class: "oversized"})
			} else if len(bytes.TrimSpace(p.buf)) > 0 {
				p.o.line(p.direction, p.buf)
			}
			p.buf = p.buf[:0]
			p.oversized = false
			continue
		}
		if !p.oversized {
			if len(p.buf) == maxLine {
				p.buf = p.buf[:0]
				p.oversized = true
			} else {
				p.buf = append(p.buf, c)
			}
		}
	}
}
func (p *lineObserver) end(eof bool) {
	if p.oversized {
		p.o.emit(event{Kind: "rpc", Direction: p.direction, Class: "oversized"})
	} else if len(p.buf) != 0 {
		p.o.emit(event{Kind: "rpc", Direction: p.direction, Class: "unterminated"})
	}
	class := "read_error"
	if eof {
		class = "eof"
	}
	p.o.emit(event{Kind: "stream_end", Direction: p.direction, Class: class})
}

// pump logs observations before handing bytes to the other process, so a fast
// response cannot arrive before its request's correlation was registered.
func pump(dst io.Writer, src io.Reader, p *lineObserver) error {
	buf := make([]byte, 8192)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if p != nil {
				p.feed(buf[:n])
			}
			written, err := dst.Write(buf[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr != nil {
			if p != nil {
				p.end(readErr == io.EOF)
			}
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func (o *observer) argv(args []string) {
	for i := 0; i < len(args); i++ {
		name, _, _ := strings.Cut(args[i], "=")
		switch name {
		case "app-server", "--check", "--help", "-h", "--version", "-V",
			"--analytics-default-enabled", "--stdio", "--print-startup-config",
			"--startup-sign-out":
			o.emit(event{Kind: "argv", Flag: name})
		case "--listen", "--startup-client-info":
			o.emit(event{Kind: "argv", Flag: name})
			if name == args[i] && i+1 < len(args) {
				i++
			}
		case "-c", "--config":
			o.emit(event{Kind: "argv", Flag: name})
			value := ""
			if name == args[i] {
				if i+1 < len(args) {
					i++
					value = args[i]
				}
			} else {
				_, value, _ = strings.Cut(args[i], "=")
			}
			e := observedConfigArgument(value)
			if e.Class == "unknown" && o.publicPlugins != nil {
				if matched, known := o.publicPlugins.argument(value); known {
					e = matched
				}
			}
			if e.Class == "unknown" {
				if key, _, hasEquals := strings.Cut(value, "="); hasEquals {
					e.ConfigPluginShape = observedPluginPathShape(key)
				}
			}
			o.emit(e)
		default:
			o.emit(event{Kind: "argv", Class: "unknown"})
		}
	}
}

func setup() (string, *os.File, error) {
	bad := errors.New("invalid test-only observer environment")
	real, prefix := os.Getenv("MAGPIE_CI_REAL_BRIDGE"), os.Getenv("MAGPIE_CI_TRACE_FILE")
	if os.Getenv(activeEnv) != "" || !filepath.IsAbs(real) || !filepath.IsAbs(prefix) {
		return "", nil, bad
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		return "", nil, bad
	}
	target, err := os.Stat(resolved)
	if err != nil || !target.Mode().IsRegular() {
		return "", nil, bad
	}
	if runtime.GOOS != "windows" && target.Mode().Perm()&0111 == 0 {
		return "", nil, bad
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", nil, bad
	}
	var header [2]byte
	_, readErr := io.ReadFull(f, header[:])
	f.Close()
	ext := strings.ToLower(filepath.Ext(resolved))
	if readErr != nil || string(header[:]) == "#!" || ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		return "", nil, bad
	}
	self, err := os.Executable()
	if err != nil {
		return "", nil, bad
	}
	current, err := os.Stat(self)
	if err != nil || os.SameFile(target, current) {
		return "", nil, bad
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(prefix))
	if err != nil {
		return "", nil, bad
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", nil, bad
	}
	base := filepath.Base(prefix)
	if base == "." || base == string(filepath.Separator) || strings.ContainsAny(base, "*?:<>|\"") {
		return "", nil, bad
	}
	trace, err := openTrace(filepath.Join(parent, base))
	if err != nil {
		return "", nil, bad
	}
	return resolved, trace, nil
}

// Exclusive fixed slots give a concurrency-safe aggregate cap of 1 MiB per
// prefix. Existing files (including symlinks) are never opened or overwritten.
// The harness must use one prefix in a fresh private directory for the run.
func openTrace(prefix string) (*os.File, error) {
	for i := 1; i <= 16; i++ {
		f, err := os.OpenFile(fmt.Sprintf("%s.%02d.jsonl", prefix, i), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		return f, err
	}
	return nil, nil // Observation exhausted; do not alter bridge behavior.
}

func childEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, activeEnv) || strings.EqualFold(key, publicPluginCatalogEnv) ||
			strings.EqualFold(key, readTargetRootsEnv) || strings.EqualFold(key, ownedModelCatalogEnv) ||
			strings.EqualFold(key, fixtureReadOnlyGuardEnv) {
			continue
		}
		env = append(env, value)
	}
	return append(env, activeEnv+"=1")
}

// runChild owns the launched bridge with the existing strict process-tree API.
// The production bridge retains its own nested child containment. There is no
// breakaway, relaxed job fallback, or change to security settings.
// Separate OS pipes let stdout/stderr EOF propagate before process exit.
func runChild(real string, args []string, input io.ReadCloser, output, diagnostics io.WriteCloser, o *observer) int {
	inR, inW, err := os.Pipe()
	if err != nil {
		o.emit(event{Kind: "setup_error"})
		return 125
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		o.emit(event{Kind: "setup_error"})
		return 125
	}
	defer outR.Close()
	defer outW.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		o.emit(event{Kind: "setup_error"})
		return 125
	}
	defer errR.Close()
	defer errW.Close()
	// Use the repository launch path and retain the exact argument vector.
	cmd := proc.Command(real, args...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = childEnvironment(), inR, outW, errW
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil {
		o.emit(event{Kind: "child_start_failed"})
		return 125
	}
	o.emit(event{Kind: "child_start", ChildPID: cmd.Process.Pid})
	inR.Close()
	outW.Close()
	errW.Close()
	var guardFailed atomic.Bool
	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		var err error
		if o.fixtureGuard == nil {
			err = pump(inW, input, &lineObserver{o: o, direction: "to_bridge"})
		} else {
			err = pumpFixtureGuard(inW, input, &lineObserver{o: o, direction: "to_bridge"}, o.fixtureGuard, "to_bridge")
			if err != nil {
				guardFailed.Store(true)
				o.emit(event{Kind: "fixture_guard_rejected", Direction: "to_bridge"})
				tree.Kill()
			}
		}
		if err != nil {
			o.emit(event{Kind: "copy_error", Direction: "to_bridge"})
		}
		inW.Close()
	}()
	go func() {
		defer copies.Done()
		var err error
		if o.fixtureGuard == nil {
			err = pump(output, outR, &lineObserver{o: o, direction: "from_bridge"})
		} else {
			err = pumpFixtureGuard(output, outR, &lineObserver{o: o, direction: "from_bridge"}, o.fixtureGuard, "from_bridge")
			if err != nil {
				guardFailed.Store(true)
				o.emit(event{Kind: "fixture_guard_rejected", Direction: "from_bridge"})
				tree.Kill()
			}
		}
		if err != nil {
			o.emit(event{Kind: "copy_error", Direction: "from_bridge"})
		}
		outR.Close()
		output.Close()
	}()
	go func() {
		defer copies.Done()
		// stderr is forwarded but never parsed or stored, even if it is JSON.
		_, err := io.Copy(diagnostics, errR)
		class := "eof"
		if err != nil {
			class = "copy_error"
		}
		o.emit(event{Kind: "stream_end", Direction: "stderr", Class: class})
		errR.Close()
		diagnostics.Close()
	}()
	err = tree.Wait()
	code := 0
	if err != nil {
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		} else {
			code = 125
		}
	}
	o.emit(event{Kind: "child_exit", ChildPID: cmd.Process.Pid, Exit: &code})
	// Inherited Windows stdin may be a synchronous handle whose Read cannot
	// be released by Close. Do not close or join it here. The executable exits
	// immediately after this function returns, releasing that reader and handle.
	// Output pumps are joined so every child byte and EOF is delivered first.
	inW.Close()
	copies.Wait()
	code = fixtureGuardExitCode(code, guardFailed.Load())
	o.stop()
	return code
}

// emitStart records provenance before optional guard activation evidence.
// The guard is already configured in memory; this ordering changes no launch
// or wire admission. The parent requires the actual per-process catalog event
// before any event other than observer_start.
func (o *observer) emitStart() {
	o.emit(event{Kind: "observer_start", Class: "test_only"})
	if o.publicPlugins != nil {
		o.emit(event{Kind: "public_plugin_catalog", PublicPluginCatalogSHA256: o.publicPlugins.digest})
	}
	if o.fixtureGuard != nil {
		o.emit(event{Kind: "fixture_readonly_guard_started"})
	}
}

func main() {
	// This diagnostic has no Unix signal relay and must not imply Unix process
	// transparency. Pure metadata/pump tests remain portable.
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "desktop-rpc-observer: Windows CI only")
		os.Exit(125)
	}
	if len(reviewedClientMethods) != 183 {
		fmt.Fprintln(os.Stderr, "desktop-rpc-observer: invalid test-only setup")
		os.Exit(125)
	}
	var catalog *publicPluginCatalog
	if value, present := os.LookupEnv(publicPluginCatalogEnv); present {
		loaded, err := parsePublicPluginCatalog(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "desktop-rpc-observer: invalid test-only setup")
			os.Exit(125)
		}
		catalog = loaded
	}
	var roots *readTargetRoots
	if value, present := os.LookupEnv(readTargetRootsEnv); present {
		loaded, err := parseReadTargetRoots(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "desktop-rpc-observer: invalid test-only setup")
			os.Exit(125)
		}
		roots = loaded
	}
	var owned *ownedModelCatalog
	if value, present := os.LookupEnv(ownedModelCatalogEnv); present {
		loaded, err := parseOwnedModelCatalog(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "desktop-rpc-observer: invalid test-only setup")
			os.Exit(125)
		}
		owned = loaded
	}
	var fixtureGuard *fixtureReadOnlyGuard
	if value, present := os.LookupEnv(fixtureReadOnlyGuardEnv); present {
		loaded, err := parseFixtureReadOnlyGuard(value)
		_, registeredCorePresent := os.LookupEnv("CODEX_WINDOWS_REGISTERED_CORE")
		probe := false
		if err == nil {
			probe, err = loaded.argv(os.Args[1:], registeredCorePresent)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "desktop-rpc-observer: fixture_readonly_guard_rejected")
			os.Exit(125)
		}
		if !probe {
			fixtureGuard = loaded
		}
	}
	real, trace, err := setup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "desktop-rpc-observer: invalid test-only setup")
		os.Exit(125)
	}
	var sink io.Writer
	if trace != nil {
		sink = trace
	}
	o := newObserver(sink)
	o.publicPlugins = catalog
	o.readTargetRoots = roots
	o.ownedModelCatalog = owned
	o.fixtureGuard = fixtureGuard
	if fixtureGuard != nil {
		fixtureGuard.emit = o.emit
	}
	o.emitStart()
	o.argv(os.Args[1:])
	code := runChild(real, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, o)
	if trace != nil {
		trace.Close()
	}
	os.Exit(code)
}
