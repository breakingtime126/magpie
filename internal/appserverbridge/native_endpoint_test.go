package appserverbridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/proc"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only a copied synthetic test executable runs here, never an official runtime.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "owned-endpoint-descendant" && strings.HasPrefix(filepath.Base(os.Args[0]), "owned-endpoint-fixture") {
		for {
			time.Sleep(time.Hour)
		}
	}

	if len(os.Args) < 2 || os.Args[1] != "app-server" || !strings.HasPrefix(filepath.Base(os.Args[0]), "owned-endpoint-fixture") {
		return
	}
	id := "primary"
	for _, arg := range os.Args {
		if strings.Contains(arg, `model_provider="magpie_shared_aihub"`) {
			id = "secondary"
		}
	}
	report, _ := json.Marshal(struct{ Args, Env []string }{os.Args[1:], os.Environ()})
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), id+".json"), report, 0600)
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), id+"-spawn-descendant")); err == nil {
		command := exec.Command(os.Args[0], "owned-endpoint-descendant")
		command.Stdout = os.Stdout
		if err := command.Start(); err != nil {
			os.Exit(7)
		}
		_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), id+".descendant"), []byte("started"), 0600)
		os.Exit(0)
	}
	// Shared-home production startup now waits for a validated primary
	// initialize reply before starting the owned secondary. This fixture only
	// supplies that real protocol prerequisite; it never claims catalog/chat
	// readiness, and the secondary retains its original drain-only behavior.
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if id != "primary" {
			continue
		}
		message, err := parseObject(scanner.Bytes())
		if err != nil {
			os.Exit(8)
		}
		if text(message, "method") == "initialize" {
			result := object{"userAgent": raw("owned-endpoint-fixture"),
				"codexHome": raw(os.Getenv("CODEX_HOME")),
				"platformFamily": raw("synthetic"), "platformOs": raw("synthetic")}
			if _, err := fmt.Fprintln(os.Stdout, string(raw(object{"id": message["id"], "result": raw(result)}))); err != nil {
				os.Exit(9)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), id+"-stubborn")); err == nil {
		for {
			time.Sleep(time.Hour)
		}
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), id+".exit"), []byte("done"), 0600)
	os.Exit(0)
}

type fixtureEndpoint struct {
	net.Listener
	mu            sync.Mutex
	tree          *proc.Tree
	waited        chan struct{}
	starts, waits atomic.Int32
	once          sync.Once
	fail, partial bool
	shutdownErr   error
	blockStart    bool
	failAccept    bool
	entering      chan struct{}
	stopStart     chan struct{}
	retainedStart chan *exec.Cmd
}

func (e *fixtureEndpoint) Start(cmd *exec.Cmd) error {
	if e.starts.Add(1) != 1 {
		return errors.New("duplicate start")
	}
	if cmd.Cancel != nil {
		return errors.New("independent cancellation")
	}
	for _, f := range []any{cmd.Stdin, cmd.Stdout, cmd.Stderr} {
		if f != nil {
			if _, ok := f.(*os.File); !ok {
				return errors.New("non-file stdio")
			}
		}
	}
	if e.blockStart {
		close(e.entering)
		<-e.stopStart
		return errors.New("synthetic canceled startup")
	}
	if e.fail {
		return errors.New("synthetic start failure")
	}
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.tree = tree
	e.mu.Unlock()
	go func() { e.waits.Add(1); _ = tree.Wait(); close(e.waited); _ = e.Listener.Close() }()
	if e.retainedStart != nil {
		e.retainedStart <- cmd
	}
	if e.partial {
		return errors.New("synthetic partial start failure")
	}
	return nil
}
func (e *fixtureEndpoint) Shutdown(ctx context.Context) error {
	e.once.Do(func() {
		if e.stopStart != nil {
			close(e.stopStart)
		}
		_ = e.Listener.Close()
		e.mu.Lock()
		tree := e.tree
		e.mu.Unlock()
		if tree != nil && e.retainedStart == nil {
			tree.Kill()
		}
	})
	e.mu.Lock()
	tree := e.tree
	e.mu.Unlock()
	if tree != nil {
		select {
		case <-e.waited:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return e.shutdownErr
}
func (e *fixtureEndpoint) Accept() (net.Conn, error) {
	if e.failAccept {
		return nil, errors.New("synthetic serve failure")
	}
	return e.Listener.Accept()
}
func (e *fixtureEndpoint) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return e.Shutdown(ctx)
}

type endpointRunFixture struct {
	cfg        Config
	endpoint   *fixtureEndpoint
	reference  nativechannel.Reference
	streamDone chan struct{}
}

func newEndpointRunFixture(t *testing.T) endpointRunFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	codex := filepath.Join(root, "native")
	control := filepath.Join(root, "control")
	for _, p := range []string{home, codex, control} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(root, "owned-endpoint-fixture.exe")
	if err = os.WriteFile(command, data, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	catalog := filepath.Join(control, "synthetic-catalog.json")
	if err = os.WriteFile(catalog, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ControlPlane: ControlPlaneRouting167, ControlHome: control, SharedNativeRoute: &SharedNativeRoute167{Mode: SharedNativeHome167, GatewayURL: "invalid-before-owned-binding", GatewayEnvKey: "MAGPIE_SYNTHETIC_GATEWAY_CALLER", GatewayAuth: SharedNativeLocalGateway167, SecondaryCatalog: catalog}}
	for i, id := range []string{"primary", "secondary"} {
		provider := "openai"
		if i == 1 {
			provider = sharedSecondaryProvider167
		}
		cfg.Backends = append(cfg.Backends, Backend{ID: id, Provider: provider, Command: command, Home: home, CodexHome: codex, StartupProbeSHA256: hex.EncodeToString(sum[:])})
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &fixtureEndpoint{Listener: listener, waited: make(chan struct{})}
	t.Cleanup(func() {
		if endpoint.retainedStart != nil {
			endpoint.mu.Lock()
			tree := endpoint.tree
			endpoint.mu.Unlock()
			if tree != nil {
				tree.Kill()
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = endpoint.Shutdown(ctx)
	})
	streamDone := make(chan struct{})
	session, err := nativechannel.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Synthetic-Stream") == "yes" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: synthetic\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(streamDone)
			return
		}
		_, _ = io.Copy(w, r.Body)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := session.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	ref, err := nativechannel.Publish(filepath.Join(control, "publication.json"), session.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	return endpointRunFixture{cfg, endpoint, ref, streamDone}
}
func waitEndpointFile(t *testing.T, path string) []byte {
	t.Helper()
	limit := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if b, err := os.ReadFile(path); err == nil {
			return b
		}
		select {
		case <-limit:
			t.Fatal("synthetic process report missing")
		case <-ticker.C:
		}
	}
}
func runEndpointFixture(t *testing.T, f endpointRunFixture) (*io.PipeWriter, context.CancelFunc, <-chan error) {
	t.Helper()
	return runEndpointFixtureInitialize(t, f, true)
}
func runEndpointFixtureInitialize(t *testing.T, f endpointRunFixture, initialize bool) (*io.PipeWriter, context.CancelFunc, <-chan error) {
	t.Helper()
	in, writer := io.Pipe()
	out, output := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runWithTrustedEndpoint(ctx, f.cfg, LaunchOptions{}, f.endpoint, f.reference, in, output)
		close(done)
	}()
	// An asynchronous send also permits admission/Start failures before the
	// client scanner exists. Cleanup closes the pipe and joins this writer.
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		if initialize {
			_, _ = fmt.Fprintln(writer, `{"id":"owned-endpoint-init","method":"initialize","params":{"clientInfo":{"name":"owned-endpoint-fixture","version":"1"}}}`)
		}
	}()
	t.Cleanup(func() {
		cancel()
		writer.Close()
		out.Close()
		select {
		case <-sent:
		case <-time.After(5 * time.Second):
			t.Error("synthetic initialize writer did not drain")
		}
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("owned runner cleanup did not complete")
		}
	})
	return writer, cancel, done
}
func waitEndpointRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("owned runner did not drain")
		return nil
	}
}
func TestOwnedEndpointRunnerBindsBeforeValidationAndStopsOnEOF(t *testing.T) {
	f := newEndpointRunFixture(t)
	writer, _, done := runEndpointFixture(t, f)
	secondary := waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "secondary.json"))
	primary := waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "primary.json"))
	authority := f.endpoint.Addr().String()
	if !strings.Contains(string(secondary), authority) || strings.Contains(string(primary), authority) {
		t.Fatal("endpoint route not confined to secondary")
	}
	if !strings.Contains(string(secondary), "magpie-codex") || strings.Contains(string(primary), "magpie-codex") {
		t.Fatal("caller label not confined to secondary")
	}
	if f.cfg.SharedNativeRoute.GatewayURL != "invalid-before-owned-binding" {
		t.Fatal("mutated caller config")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Post("http://"+authority+"/v1/responses", "application/json", strings.NewReader("synthetic request"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(body) != "synthetic request" {
		t.Fatal("relay composition failed")
	}
	writer.Close()
	if err := waitEndpointRun(t, done); err != nil {
		t.Fatal(err)
	}
	if f.endpoint.starts.Load() != 1 || f.endpoint.waits.Load() != 1 {
		t.Fatal("secondary did not have exactly one Start/Wait owner")
	}
}
func TestOwnedEndpointRunnerFailuresCloseOwnedResources(t *testing.T) {
	for _, mode := range []string{"validation", "pin", "start", "partial-start", "cancel", "secondary-exit", "shutdown-error", "cancel-start", "serve-failure", "primary-start"} {
		t.Run(mode, func(t *testing.T) {
			f := newEndpointRunFixture(t)
			switch mode {
			case "cancel-start":
				f.endpoint.blockStart = true
				f.endpoint.entering = make(chan struct{})
				f.endpoint.stopStart = make(chan struct{})
			case "serve-failure":
				f.endpoint.failAccept = true
			case "primary-start":
				bad := filepath.Join(t.TempDir(), "invalid-native.exe")
				data := []byte{0, 0, 1}
				if err := os.WriteFile(bad, data, 0700); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(data)
				f.cfg.Backends[0].Command = bad
				f.cfg.Backends[0].StartupProbeSHA256 = hex.EncodeToString(sum[:])
			case "validation":
				f.cfg.Backends[0].Provider = "wrong"
			case "pin":
				f.reference.SHA256 = strings.Repeat("0", 64)
			case "start":
				f.endpoint.fail = true
			case "partial-start":
				f.endpoint.partial = true
			case "shutdown-error":
				f.endpoint.shutdownErr = errors.New("synthetic unproven drain")
			}
			writer, cancel, done := runEndpointFixture(t, f)
			if mode == "cancel-start" {
				select {
				case <-f.endpoint.entering:
					cancel()
				case <-time.After(10 * time.Second):
					t.Fatal("startup gate not reached")
				}
			}
			if mode == "cancel" || mode == "secondary-exit" || mode == "shutdown-error" {
				waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "secondary.json"))
				switch mode {
				case "cancel":
					cancel()
				case "secondary-exit":
					f.endpoint.mu.Lock()
					tree := f.endpoint.tree
					f.endpoint.mu.Unlock()
					tree.Kill()
				case "shutdown-error":
					writer.Close()
				}
			}
			if err := waitEndpointRun(t, done); err == nil {
				t.Fatal("failed lifetime claimed success")
			}
			if f.endpoint.waits.Load() > 1 {
				t.Fatal("double Wait")
			}
			conn, err := net.DialTimeout("tcp", f.endpoint.Addr().String(), 100*time.Millisecond)
			if err == nil {
				conn.Close()
				t.Fatal("owned listener survived cleanup")
			}
		})
	}
}

func TestOwnedEndpointRunnerEOFDrainsSSE(t *testing.T) {
	f := newEndpointRunFixture(t)
	writer, _, done := runEndpointFixture(t, f)
	waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "secondary.json"))
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request, err := http.NewRequest(http.MethodPost, "http://"+f.endpoint.Addr().String()+"/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Synthetic-Stream", "yes")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: synthetic\n" {
		t.Fatal("SSE did not stream", err)
	}
	writer.Close()
	if err := waitEndpointRun(t, done); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream SSE outlived completed runner")
	}
}

// The synthetic producer retains its real stdout write end after reporting an
// unproven process drain. Only the copied synthetic fixture executable runs.
func TestOwnedEndpointRunnerFailedDrainQuarantinesBlockedStdout(t *testing.T) {
	f := newEndpointRunFixture(t)
	f.endpoint.retainedStart = make(chan *exec.Cmd, 1)
	if err := os.WriteFile(filepath.Join(f.cfg.Backends[0].Home, "secondary-stubborn"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	f.endpoint.shutdownErr = errors.New("synthetic producer drain unproven")
	writer, _, done := runEndpointFixture(t, f)
	var command *exec.Cmd
	select {
	case command = <-f.endpoint.retainedStart:
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic retained startup missing")
	}
	_ = command // Exact process was launched only by the synthetic endpoint.
	waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "secondary.json"))
	f.endpoint.mu.Lock()
	tree := f.endpoint.tree
	f.endpoint.mu.Unlock()
	defer tree.Kill()
	writer.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unproven producer reported successful drain")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runner blocked closing retained stdout or joining its reader")
	}
	// Release only after runner returned a failure. The quarantined scanner may
	// now exit and its retained cleanup owner can close the parent pipe ends.
	tree.Kill()
	<-f.endpoint.waited
}

type endpointGatedReader struct{ entered, release, ended, closed chan struct{} }

func (r *endpointGatedReader) Read([]byte) (int, error) {
	close(r.entered)
	<-r.release
	close(r.ended)
	return 0, io.EOF
}
func (r *endpointGatedReader) Close() error { <-r.ended; close(r.closed); return nil }

type endpointGatedWriter struct{ entered, release, ended, closed chan struct{} }

func (w *endpointGatedWriter) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	close(w.ended)
	return len(p), nil
}
func (w *endpointGatedWriter) Close() error { <-w.ended; close(w.closed); return nil }
func TestOwnedEndpointRunnerQuarantineRetainsReadAndWrite(t *testing.T) {
	r := &endpointGatedReader{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	w := &endpointGatedWriter{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	s := newOwnedEndpointStreams(w, r)
	go func() { _, _ = s.Read(make([]byte, 1)) }()
	writeDone := make(chan struct{})
	go func() { defer close(writeDone); _, _ = s.Write([]byte("synthetic private buffer")) }()
	<-r.entered
	<-w.entered
	s.retainCleanup()
	select {
	case <-writeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("wrapper Write cancellation blocked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.waitReleased(ctx) == nil {
		t.Fatal("blocked I/O reported released")
	}
	select {
	case <-r.closed:
		t.Fatal("closed raw reader before I/O ended")
	default:
	}
	select {
	case <-w.closed:
		t.Fatal("closed raw writer before I/O ended")
	default:
	}
	close(r.release)
	close(w.release)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.waitReleased(ctx); err != nil {
		t.Fatal(err)
	}
	<-r.closed
	<-w.closed
}

type endpointDiscardWriter struct{}

func (endpointDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (endpointDiscardWriter) Close() error                { return nil }
func TestOwnedEndpointRunnerCancelQuarantinesBlockingFrontend(t *testing.T) {
	f := newEndpointRunFixture(t)
	input := &endpointGatedReader{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runWithTrustedEndpoint(ctx, f.cfg, LaunchOptions{}, f.endpoint, f.reference, input, endpointDiscardWriter{})
	}()
	select {
	case <-input.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("frontend scanner not started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked frontend incorrectly drained")
		}
	case <-time.After(8 * time.Second):
		close(input.release)
		t.Fatal("runner blocked in frontend Close/Read")
	}
	select {
	case <-input.closed:
		t.Fatal("closed synchronous frontend under outstanding read")
	default:
	}
	close(input.release)
	select {
	case <-input.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("frontend quarantine not released after I/O ended")
	}
}

func TestOwnedEndpointRunnerShutdownFailureDoesNotCloseRawOutput(t *testing.T) {
	r := &endpointGatedReader{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	go func() { _, _ = r.Read(make([]byte, 1)) }()
	<-r.entered
	failure := errors.New("synthetic drain failure")
	done := make(chan error, 1)
	go func() {
		done <- shutdownChildren(map[string]*childServer{"owned": {input: endpointDiscardWriter{}, output: r, shutdown: func() error { return failure }}})
	}()
	select {
	case err := <-done:
		close(r.release)
		<-r.ended
		if !errors.Is(err, failure) {
			t.Fatal("lost failed drain")
		}
		// This test owns the retained resource; production ownedStreams does this.
		_ = r.Close()
	case <-time.After(time.Second):
		close(r.release)
		<-r.ended
		<-done
		t.Fatal("failed owner cleanup called blocking raw stdout Close")
	}
}

func TestOwnedEndpointRunnerNaturalRootExitWithDescendantStdout(t *testing.T) {
	for _, role := range []string{"primary", "secondary"} {
		t.Run(role, func(t *testing.T) {
			f := newEndpointRunFixture(t)
			if err := os.WriteFile(filepath.Join(f.cfg.Backends[0].Home, role+"-spawn-descendant"), []byte("synthetic"), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, done := runEndpointFixture(t, f)
			waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, role+".descendant"))
			if err := waitEndpointRun(t, done); err == nil {
				t.Fatal("natural selected root exit claimed healthy bridge")
			}
		})
	}
}

type endpointErrorCloser struct{ err error }

func (c endpointErrorCloser) Close() error { return c.err }
func TestOwnedEndpointRunnerClosePreservesJoinedFailures(t *testing.T) {
	failure := errors.New("synthetic cleanup failure")
	for _, err := range []error{failure, errors.Join(os.ErrClosed, failure), fmt.Errorf("context: %w", os.ErrClosed), &os.PathError{Op: "close", Path: "synthetic", Err: errors.Join(os.ErrClosed, failure)}} {
		if got := closeEndpointPipe(endpointErrorCloser{err}); got != err {
			t.Fatal("suppressed nontrivial cleanup error")
		}
	}
	for _, err := range []error{nil, os.ErrClosed, &os.PathError{Op: "close", Path: "synthetic", Err: os.ErrClosed}} {
		if got := closeEndpointPipe(endpointErrorCloser{err}); got != nil {
			t.Fatal("ordinary closed handle not normalized")
		}
	}
}

type endpointCloseTrackedReader struct{ closed atomic.Int32 }

func (r *endpointCloseTrackedReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *endpointCloseTrackedReader) Close() error             { r.closed.Add(1); return nil }

type endpointCloseTrackedWriter struct{ closed atomic.Int32 }

func (w *endpointCloseTrackedWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *endpointCloseTrackedWriter) Close() error                { w.closed.Add(1); return nil }
func TestOwnedEndpointRunnerEarlyRejectionClosesTransferredStreams(t *testing.T) {
	for _, mode := range []string{"route", "pin", "missing-input", "missing-output"} {
		t.Run(mode, func(t *testing.T) {
			f := newEndpointRunFixture(t)
			reader := &endpointCloseTrackedReader{}
			writer := &endpointCloseTrackedWriter{}
			var input io.ReadCloser = reader
			var output io.WriteCloser = writer
			switch mode {
			case "route":
				f.cfg.SharedNativeRoute = nil
			case "pin":
				f.reference.SHA256 = strings.Repeat("0", 64)
			case "missing-input":
				input = nil
			case "missing-output":
				output = nil
			}
			if err := runWithTrustedEndpoint(context.Background(), f.cfg, LaunchOptions{}, f.endpoint, f.reference, input, output); err == nil {
				t.Fatal("invalid admission succeeded")
			}
			if input != nil && reader.closed.Load() != 1 {
				t.Fatal("transferred input was not closed once")
			}
			if output != nil && writer.closed.Load() != 1 {
				t.Fatal("transferred output was not closed once")
			}
			if f.endpoint.starts.Load() != 0 {
				t.Fatal("early rejection started a child")
			}
		})
	}
}

func TestOwnedEndpointRunnerClientEOFBeforeInitializeNeverStartsSecondary(t *testing.T) {
	f := newEndpointRunFixture(t)
	writer, _, done := runEndpointFixtureInitialize(t, f, false)
	waitEndpointFile(t, filepath.Join(f.cfg.Backends[0].Home, "primary.json"))
	writer.Close()
	if err := waitEndpointRun(t, done); err != nil {
		t.Fatal(err)
	}
	if f.endpoint.starts.Load() != 0 || f.endpoint.waits.Load() != 0 {
		t.Fatal("secondary started without primary initialization")
	}
	if _, err := os.Stat(filepath.Join(f.cfg.Backends[0].Home, "secondary.json")); !os.IsNotExist(err) {
		t.Fatal("secondary fixture ran without initialization")
	}
	conn, err := net.DialTimeout("tcp", f.endpoint.Addr().String(), 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("unstarted owned listener survived client EOF")
	}
}
