package nativechannel

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type relayFixture struct {
	relay     *Relay
	server    *httptest.Server
	client    *http.Client
	reference Reference
}

func newRelayFixture(t *testing.T, upstream http.Handler) relayFixture {
	t.Helper()
	s := session(t, upstream)
	reference, err := Publish(filepath.Join(publicationDir(t), "channel.json"), s.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	relay, err := NewRelay(context.Background(), reference, server.Listener.Addr().String())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	server.Config.Handler = relay
	server.Start()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	peer := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := relay.Close(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
		transport.CloseIdleConnections()
	})
	return relayFixture{relay: relay, server: server, client: peer, reference: reference}
}

func TestRelayPreservesCallerOriginAndForwardingPolicy(t *testing.T) {
	seen := make(chan http.Header, 4)
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		if r.Header.Get("Origin") == "null" || r.Header.Get("Forwarded") != "" {
			http.Error(w, "fixture gateway refuses caller", 403)
			return
		}
		io.Copy(w, r.Body)
	}))
	f.relay.proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		t.Errorf("upstream failure: %v", err)
		http.Error(w, "failure", 502)
	}
	for _, mark := range []string{"", "Origin", "Forwarded"} {
		r := request(t, f.server.URL, strings.NewReader("synthetic request"))
		r.Header.Set("Authorization", "Bearer magpie-codex")
		r.Header.Set("User-Agent", "synthetic-native-client")
		if mark == "Origin" {
			r.Header.Set("Origin", "null")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		} else if mark == "Forwarded" {
			for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Cf-Connecting-IP", "True-Client-IP", "Tailscale-User-Login"} {
				r.Header.Set(name, "synthetic-forwarding-value")
			}
		}
		response, err := f.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		want := 200
		if mark != "" {
			want = 403
		}
		if response.StatusCode != want {
			response.Body.Close()
			t.Fatalf("caller policy status: %d", response.StatusCode)
		}
		readResponse(t, response)
		headers := <-seen
		for _, name := range []string{"Authorization", "Origin", "User-Agent", "Sec-Fetch-Site", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "Cf-Connecting-IP", "True-Client-IP", "Tailscale-User-Login"} {
			if headers.Get(name) != r.Header.Get(name) {
				t.Fatalf("changed %s", name)
			}
		}
	}
}

func TestRelayRejectsRouteHostRemoteAndConnectionNomination(t *testing.T) {
	var calls atomic.Int64
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.URL.Path = "/v1/responses/compact" },
		func(r *http.Request) { r.URL.RawQuery = "alternate=1" },
		func(r *http.Request) { r.Method = http.MethodGet },
		func(r *http.Request) { r.Method = http.MethodConnect },
		func(r *http.Request) { r.Host = "foreign.invalid" },
		func(r *http.Request) { r.Header.Set("Connection", "Origin") },
		func(r *http.Request) { r.Header.Set("Connection", "keep-alive, Authorization") },
		func(r *http.Request) { r.Header.Set("Connection", "Forwarded") },
		func(r *http.Request) { r.Header.Set("Upgrade", "websocket") },
	} {
		r := request(t, f.server.URL, strings.NewReader("synthetic secret"))
		r.Header.Set("Origin", "https://foreign.invalid")
		change(r)
		response, err := f.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 403 {
			response.Body.Close()
			t.Fatalf("accepted unsupported request: %d", response.StatusCode)
		}
		readResponse(t, response)
	}
	for _, remote := range []string{"192.0.2.4:1234", "127.0.0.1:bad", "unknown"} {
		r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("synthetic secret"))
		r.Host = f.relay.authority
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		f.relay.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("laundered nonlocal or unknown peer")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected traffic reached upstream")
	}
}

func TestRelayBlocksRedirectAndDoesNotRefreshPublication(t *testing.T) {
	var stolen atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stolen.Add(1) }))
	t.Cleanup(foreign.Close)
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", foreign.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	// Existing relay holds its verified descriptor; it never reloads this file.
	if err := os.WriteFile(f.reference.Path, []byte("changed publication"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := f.client.Do(request(t, f.server.URL, strings.NewReader("synthetic secret")))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 502 || response.Header.Get("Location") != "" {
		response.Body.Close()
		t.Fatal("forwarded redirect")
	}
	readResponse(t, response)
	if stolen.Load() != 0 {
		t.Fatal("native-facing client followed upstream redirect")
	}
	if newer, err := NewRelay(context.Background(), f.reference, f.relay.authority); err == nil {
		newer.Close(context.Background())
		t.Fatal("new relay trusted changed publication")
	}
}

func TestRelayStreamsSSEAndCloseCancelsFullCopy(t *testing.T) {
	finished := make(chan struct{})
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Alt-Svc", `h3=":443"`)
		io.WriteString(w, "data: synthetic\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(finished)
	}))
	response, err := f.client.Do(request(t, f.server.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Alt-Svc") != "" {
		response.Body.Close()
		t.Fatal("advertised alternate transport")
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: synthetic\n" {
		response.Body.Close()
		t.Fatal("SSE not flushed", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.relay.Close(ctx); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	io.ReadAll(reader)
	response.Body.Close()
	<-finished
	response, err = f.client.Do(request(t, f.server.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 503 {
		response.Body.Close()
		t.Fatal("closed relay admitted request")
	}
	readResponse(t, response)
}

func TestRelayCloseUnblocksStalledInboundBody(t *testing.T) {
	entered := make(chan struct{})
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); io.Copy(io.Discard, r.Body) }))
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	r := request(t, f.server.URL, reader)
	result := make(chan struct{})
	go func() {
		defer close(result)
		response, _ := f.client.Do(r)
		if response != nil {
			response.Body.Close()
		}
	}()
	wrote := make(chan struct{})
	go func() { defer close(wrote); _, _ = writer.Write([]byte("synthetic prefix")) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.relay.Close(ctx); err != nil {
		reader.Close()
		<-result
		<-wrote
		t.Fatal("stalled relay did not drain", err)
	}
	reader.Close() // The synthetic native client also owns its independent body.
	<-result
	<-wrote
}

func TestRelayRequestCancellationReachesUpstream(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(finished) }))
	r := request(t, f.server.URL, nil)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	result := make(chan struct{})
	go func() {
		defer close(result)
		response, _ := f.client.Do(r.WithContext(ctx))
		if response != nil {
			response.Body.Close()
		}
	}()
	<-entered
	cancel()
	<-finished
	<-result
}

// A custom writer without deadline support cannot be forcibly drained. Close
// must report that fact rather than mistaking upstream cancellation for drain.
type blockedRelayWriter struct {
	header  http.Header
	entered chan struct{}
	release chan struct{}
}

func (w *blockedRelayWriter) Header() http.Header { return w.header }
func (w *blockedRelayWriter) WriteHeader(int)     {}
func (w *blockedRelayWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

func TestRelayCloseReportsUncooperativeWriter(t *testing.T) {
	f := newRelayFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "synthetic response")
	}))
	w := &blockedRelayWriter{header: make(http.Header), entered: make(chan struct{}, 1), release: make(chan struct{})}
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	r.Host = f.relay.authority
	r.RemoteAddr = "127.0.0.1:12345"
	done := make(chan struct{})
	go func() { defer close(done); f.relay.ServeHTTP(w, r) }()
	<-w.entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := f.relay.Close(ctx)
	close(w.release)
	<-done
	if err != context.DeadlineExceeded {
		t.Fatalf("blocked writer reported drained: %v", err)
	}
	if err := f.relay.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRelayBodyEOFSurvivesUnderlyingServerClose(t *testing.T) {
	reader, writer := io.Pipe()
	writer.Close()
	body := &relayBody{ReadCloser: reader, controller: http.NewResponseController(httptest.NewRecorder())}
	buf := make([]byte, 1)
	if _, err := body.Read(buf); err != io.EOF {
		t.Fatal(err)
	}
	reader.Close()
	// Transport performs a final excess-byte probe after copying ContentLength.
	// The HTTP server may already have closed its fully consumed inbound body.
	if _, err := body.Read(buf); err != io.EOF {
		t.Fatalf("lost observed EOF after underlying close: %v", err)
	}
}
