package nativechannel

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func session(t *testing.T, handler http.Handler) *Session {
	t.Helper()
	s, err := Start(context.Background(), handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}

func client(t *testing.T, descriptor Descriptor) *Client {
	t.Helper()
	c, err := NewClient(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func request(t *testing.T, endpoint string, body io.Reader) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/responses", body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer synthetic-sensitive-value")
	return r
}

func readResponse(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPinnedChannelRetainsInjectedPolicyAndDoesNotRefreshGeneration(t *testing.T) {
	var revoked atomic.Bool
	var bodies atomic.Int64
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if revoked.Load() {
			http.Error(w, "stale injected generation", 503)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies.Add(1)
		w.Write(b)
	}))
	d := s.Descriptor()
	c := client(t, d)
	d.CertificateDER[0] ^= 1 // Client and session both own copies.
	response, err := c.Do(request(t, s.Descriptor().URL, strings.NewReader("synthetic prompt")))
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("first request: %v %v", response, err)
	}
	if got := readResponse(t, response); got != "synthetic prompt" {
		t.Fatal(got)
	}
	revoked.Store(true)
	response, err = c.Do(request(t, s.Descriptor().URL, strings.NewReader("later prompt")))
	if err != nil || response.StatusCode != 503 {
		t.Fatalf("stale generation: %v %v", response, err)
	}
	readResponse(t, response)
	if bodies.Load() != 1 {
		t.Fatal("revoked handler read another body")
	}
}

func TestWrongCertificateReceivesNoHTTPHeadersOrBody(t *testing.T) {
	s := session(t, http.NotFoundHandler())
	var calls atomic.Int64
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	t.Cleanup(foreign.Close)
	d := s.Descriptor()
	d.URL = foreign.URL
	c := client(t, d)
	if response, err := c.Do(request(t, d.URL, strings.NewReader("synthetic sensitive prompt"))); err == nil {
		response.Body.Close()
		t.Fatal("accepted foreign certificate")
	}
	if calls.Load() != 0 {
		t.Fatal("foreign TLS server received HTTP")
	}
}

func TestOldPortReplacementReceivesNoHTTP(t *testing.T) {
	s := session(t, http.NotFoundHandler())
	d := s.Descriptor()
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", strings.TrimPrefix(d.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	certificate, _, err := newCertificate()
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	var calls atomic.Int64
	foreign := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = foreign.Serve(tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}))
	}()
	t.Cleanup(func() { foreign.Close(); <-done })
	c := client(t, d)
	if response, err := c.Do(request(t, d.URL, strings.NewReader("synthetic sensitive prompt"))); err == nil {
		response.Body.Close()
		t.Fatal("old descriptor accepted replacement")
	}
	if calls.Load() != 0 {
		t.Fatal("replacement received HTTP")
	}
}

func TestRedirectAndReconnectAreNotFollowed(t *testing.T) {
	var calls atomic.Int64
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Connection", "close")
		w.Header().Set("Location", "https://127.0.0.1:1/stolen")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	c := client(t, s.Descriptor())
	response, err := c.Do(request(t, s.Descriptor().URL, strings.NewReader("synthetic prompt")))
	if err != nil || response.StatusCode != 307 {
		t.Fatalf("redirect response: %v %v", response, err)
	}
	readResponse(t, response)
	if response, err = c.Do(request(t, s.Descriptor().URL, strings.NewReader("later prompt"))); err == nil {
		response.Body.Close()
		t.Fatal("silently reconnected")
	}
	if calls.Load() != 1 {
		t.Fatal("extra request delivered")
	}
}

type trackedBody struct {
	*bytes.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error { b.closed.Store(true); return nil }

func TestRejectsOriginAndHostOverrideAndClosesBody(t *testing.T) {
	s := session(t, http.NotFoundHandler())
	c := client(t, s.Descriptor())
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.URL.Host = "127.0.0.1:1" },
		func(r *http.Request) { r.Host = "foreign.invalid" },
		func(r *http.Request) { r.URL.Scheme = "http" },
	} {
		body := &trackedBody{Reader: bytes.NewReader([]byte("synthetic prompt"))}
		r := request(t, s.Descriptor().URL, body)
		change(r)
		if _, err := c.Do(r); err == nil || !body.closed.Load() {
			t.Fatalf("rejection did not close body: %v", err)
		}
	}
}

func TestSessionCloseCancelsAndDrainsHandlers(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(finished)
	}))
	c := client(t, s.Descriptor())
	result := make(chan error, 1)
	go func() {
		r, err := c.Do(request(t, s.Descriptor().URL, nil))
		if r != nil {
			r.Body.Close()
		}
		result <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before handler finished")
	}
	// A cooperative handler may finish its response while Close cancels it.
	// Draining, rather than a particular racing transport error, is guaranteed.
	<-result
}

func TestSelectedProxyRefusalSendsNoSensitiveHeaders(t *testing.T) {
	s := session(t, http.NotFoundHandler())
	seen := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Clone(context.Background())
		http.Error(w, "proxy policy refusal", http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	c := client(t, s.Descriptor())
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Force a synthetic selected proxy without changing process environment.
	// Production always uses the standard ProxyFromEnvironment selection.
	c.transport.Proxy = http.ProxyURL(proxyURL)
	if response, err := c.Do(request(t, s.Descriptor().URL, strings.NewReader("synthetic sensitive prompt"))); err == nil {
		response.Body.Close()
		t.Fatal("bypassed refusing proxy")
	}
	r := <-seen
	if r.Method != "CONNECT" || r.Header.Get("Authorization") != "" || r.ContentLength > 0 {
		t.Fatal("sensitive request reached proxy before TLS authentication")
	}
}

func TestParentCancellationClosesSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, http.NotFoundHandler())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	deadline, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := s.Close(deadline); err != nil {
		t.Fatal(err)
	}
	if next, err := Start(ctx, http.NotFoundHandler()); err == nil {
		next.Close(deadline)
		t.Fatal("started after parent cancellation")
	}
}

func TestCloseReportsUncooperativeHandler(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release }))
	c := client(t, s.Descriptor())
	result := make(chan struct{})
	go func() {
		defer close(result)
		r, _ := c.Do(request(t, s.Descriptor().URL, nil))
		if r != nil {
			r.Body.Close()
		}
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("claimed drained: %v", err)
	}
	close(release)
	<-result
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRequestsAndClientClose(t *testing.T) {
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(w, r.Body) }))
	c := client(t, s.Descriptor())
	var workers sync.WaitGroup
	for range 8 {
		r := request(t, s.Descriptor().URL, strings.NewReader("synthetic prompt"))
		workers.Add(1)
		go func() {
			defer workers.Done()
			response, _ := c.Do(r)
			if response != nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
		}()
	}
	c.Close()
	workers.Wait()
	if response, err := c.Do(request(t, s.Descriptor().URL, nil)); err == nil {
		response.Body.Close()
		t.Fatal("closed client accepted request")
	}
}

func TestClientCloseUnblocksStreamingUpload(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		io.Copy(io.Discard, r.Body)
		close(finished)
	}))
	c := client(t, s.Descriptor())
	reader, writer := io.Pipe()
	t.Cleanup(func() { reader.Close(); writer.Close() })
	r := request(t, s.Descriptor().URL, reader)
	result := make(chan error, 1)
	go func() {
		response, err := c.Do(r)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()
	wrote := make(chan struct{})
	go func() { defer close(wrote); _, _ = writer.Write([]byte("synthetic prefix")) }()
	<-entered
	c.Close()
	<-wrote
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled streaming upload succeeded")
		}
	case <-time.After(5 * time.Second):
		reader.Close()
		<-result
		<-finished
		t.Fatal("client cancellation did not close stalled request body")
	}
	if _, err := writer.Write([]byte("must not be sent")); err == nil {
		t.Fatal("request body remained open")
	}
	<-finished
}

func TestClientCloseUnblocksStreamingResponse(t *testing.T) {
	finished := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(finished)
		<-release // Do not race cancellation against an ordinary successful EOF.
	}))
	c := client(t, s.Descriptor())
	response, err := c.Do(request(t, s.Descriptor().URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("canceled stream reported complete")
	}
	response.Body.Close()
	<-finished
}

func TestMalformedDescriptorsFailClosed(t *testing.T) {
	s := session(t, http.NotFoundHandler())
	for _, endpoint := range []string{"http://127.0.0.1:443", "https://localhost:443", "https://127.0.0.1:0", "https://127.0.0.1:443/v1", "https://user@127.0.0.1:443", "https://127.0.0.1:443?x=1"} {
		d := s.Descriptor()
		d.URL = endpoint
		if c, err := NewClient(d); err == nil {
			c.Close()
			t.Fatal("accepted malformed descriptor", endpoint)
		}
	}
	d := s.Descriptor()
	d.CertificateDER = []byte("not a certificate")
	if c, err := NewClient(d); err == nil {
		c.Close()
		t.Fatal("accepted malformed certificate")
	}
}

func TestHealthySequentialRequestsReusePinnedConnection(t *testing.T) {
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(w, r.Body) }))
	c := client(t, s.Descriptor())
	for _, input := range []string{"first synthetic prompt", "second synthetic prompt", "third synthetic prompt"} {
		response, err := c.Do(request(t, s.Descriptor().URL, strings.NewReader(input)))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || readResponse(t, response) != input {
			t.Fatal("sequential request failed")
		}
	}
}

func TestHandlerCannotHijackAndSSEStillFlushes(t *testing.T) {
	result := make(chan error, 1)
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			result <- errors.New("handler received http.Hijacker")
			http.Error(w, "unexpected hijacker", 500)
			return
		}
		conn, _, err := http.NewResponseController(w).Hijack()
		if conn != nil {
			conn.Close()
		}
		if !errors.Is(err, http.ErrNotSupported) {
			result <- errors.New("ResponseController could escape session")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: synthetic\n\n")
		result <- http.NewResponseController(w).Flush()
	}))
	c := client(t, s.Descriptor())
	response, err := c.Do(request(t, s.Descriptor().URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	if got := readResponse(t, response); got != "data: synthetic\n\n" {
		t.Fatal(got)
	}
}

func TestUpgradeAndConnectRejectedAtBothBoundaries(t *testing.T) {
	var calls atomic.Int64
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Method = http.MethodConnect },
		func(r *http.Request) { r.Header.Set("Upgrade", "websocket") },
		func(r *http.Request) { r.Header.Set("Connection", "keep-alive, UpGrAdE") },
	} {
		c := client(t, s.Descriptor())
		body := &trackedBody{Reader: bytes.NewReader([]byte("synthetic prompt"))}
		r := request(t, s.Descriptor().URL, body)
		change(r)
		if response, err := c.Do(r); err == nil || !body.closed.Load() {
			if response != nil {
				response.Body.Close()
			}
			t.Fatal("client accepted unsupported transport or retained its body")
		}
		// Bypass only the client admission wrapper to exercise the actual
		// server boundary with the same pinned TLS transport.
		r = request(t, s.Descriptor().URL, nil)
		change(r)
		response, err := c.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusNotImplemented {
			response.Body.Close()
			t.Fatal("server accepted unsupported transport")
		}
		readResponse(t, response)
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported transport reached injected handler")
	}
}

func TestHandlerCannotReturnProtocolSwitch(t *testing.T) {
	s := session(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upgrade", "websocket")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	c := client(t, s.Descriptor())
	response, err := c.Do(request(t, s.Descriptor().URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || response.Header.Get("Upgrade") != "" {
		t.Fatal("handler switched protocol")
	}
}
