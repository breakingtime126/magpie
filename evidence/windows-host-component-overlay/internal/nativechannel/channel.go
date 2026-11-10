// Package nativechannel provides a pinned, session-bound HTTPS channel to an
// injected handler. It authenticates the server, not the caller. The handler
// must retain its normal caller policy and generation checks. This package does
// not establish descriptor provenance or implement the native-provider leg.
package nativechannel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Descriptor contains public trust material. It must come from the owning
// process or a separately verified, owner-controlled handoff, never from the
// endpoint being authenticated. A URL, claimed PID or claimed name is not proof.
// Each new session requires explicit establishment of a new descriptor.
type Descriptor struct {
	URL            string
	CertificateDER []byte
}

// Session owns an ephemeral TLS key and a fresh loopback listener. Closing it
// revokes admission, cancels handler contexts and closes network connections.
type Session struct {
	descriptor Descriptor
	server     *http.Server
	cancel     context.CancelFunc
	revoked    <-chan struct{}
	stopOnce   sync.Once
	mu         sync.Mutex
	closed     bool
	served     bool
	active     int
	done       chan struct{}
	doneOnce   sync.Once
	serveErr   error
}

// Start does not discover or dial a gateway. Pass a generation-bound owned
// handler, not an HTTP forwarding handler that looks up a mutable gateway URL.
func Start(parent context.Context, handler http.Handler) (*Session, error) {
	if parent == nil || handler == nil {
		return nil, errors.New("native channel requires context and handler")
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	certificate, der, err := newCertificate()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Session{descriptor: Descriptor{URL: "https://" + listener.Addr().String(), CertificateDER: der}, cancel: cancel, revoked: ctx.Done(), done: make(chan struct{})}
	s.server = &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if unsupportedTransport(r) {
				http.Error(w, "native channel supports HTTP and SSE only", http.StatusNotImplemented)
				return
			}
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				http.Error(w, "native channel closed", http.StatusServiceUnavailable)
				return
			}
			s.active++
			s.mu.Unlock()
			defer func() {
				s.mu.Lock()
				s.active--
				s.finishLocked()
				s.mu.Unlock()
			}()
			handler.ServeHTTP(httpResponseWriter{writer: w}, r)
		}),
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}})
	go func() {
		err := s.server.Serve(tlsListener)
		s.stop()
		s.mu.Lock()
		if !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			s.serveErr = err
		}
		s.served = true
		s.finishLocked()
		s.mu.Unlock()
	}()
	stopParent := context.AfterFunc(parent, s.stop)
	go func() { <-s.done; stopParent() }()
	return s, nil
}

func unsupportedTransport(r *http.Request) bool {
	if strings.EqualFold(r.Method, http.MethodConnect) || len(r.Header.Values("Upgrade")) != 0 {
		return true
	}
	for _, value := range r.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

// Expose only ordinary HTTP writing and SSE flushing. In particular, neither
// Hijacker nor an Unwrap method may let a handler escape session accounting.
type httpResponseWriter struct{ writer http.ResponseWriter }

func (w httpResponseWriter) Header() http.Header         { return w.writer.Header() }
func (w httpResponseWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w httpResponseWriter) Flush()                      { _ = http.NewResponseController(w.writer).Flush() }
func (w httpResponseWriter) WriteHeader(status int) {
	if status == http.StatusSwitchingProtocols {
		w.writer.Header().Del("Upgrade")
		w.writer.Header().Set("Connection", "close")
		status = http.StatusBadGateway
	}
	w.writer.WriteHeader(status)
}

func (s *Session) finishLocked() {
	if s.closed && s.served && s.active == 0 {
		s.doneOnce.Do(func() { close(s.done) })
	}
}

func (s *Session) stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
		_ = s.server.Close()
	})
}

// Descriptor returns a defensive copy; it contains no private key or token.
func (s *Session) Descriptor() Descriptor {
	return Descriptor{URL: s.descriptor.URL, CertificateDER: bytes.Clone(s.descriptor.CertificateDER)}
}

// Close waits for the serving loop and admitted handlers. An uncooperative
// handler produces the caller's deadline error, not a claimed clean shutdown.
func (s *Session) Close(ctx context.Context) error {
	s.stop()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.serveErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newCertificate() (tls.Certificate, []byte, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Magpie ephemeral local channel"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, der, nil
}

// Client permits one network connection only. It never reconnects after a
// failed or lost connection; callers must explicitly establish a new client.
// HTTPS through an ambient proxy still authenticates the pinned server before
// transmitting request headers or body. Proxy or certificate failures are final.
// ProxyFromEnvironment's own loopback exemption still applies. This does not
// promise that corporate proxy configuration routes local requests through it.
type Client struct {
	origin    string
	client    *http.Client
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewClient trusts only the supplied descriptor. This is private transport
// trust, not a modification of native, OS or user CA/proxy configuration.
func NewClient(descriptor Descriptor) (*Client, error) {
	certificate, roots, err := validateDescriptor(descriptor)
	if err != nil {
		return nil, err
	}
	der := certificate.Raw
	ctx, cancel := context.WithCancel(context.Background())
	var dialMu sync.Mutex
	dialed := false
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(dialContext context.Context, network, address string) (net.Conn, error) {
			dialMu.Lock()
			if dialed {
				dialMu.Unlock()
				return nil, errors.New("native channel reconnection requires explicit establishment")
			}
			dialed = true
			dialMu.Unlock()
			dialContext, stopDial := context.WithCancel(dialContext)
			stopClose := context.AfterFunc(ctx, stopDial)
			defer stopClose()
			defer stopDial()
			if ctx.Err() != nil {
				stopDial()
			}
			return dialer.DialContext(dialContext, network, address)
		},
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}, VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 || !bytes.Equal(state.PeerCertificates[0].Raw, der) {
				return errors.New("native channel identity changed")
			}
			return nil
		}},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxConnsPerHost:     1,
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
	}
	return &Client{origin: descriptor.URL, transport: transport, ctx: ctx, cancel: cancel, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func validateDescriptor(descriptor Descriptor) (*x509.Certificate, *x509.CertPool, error) {
	u, err := url.Parse(descriptor.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, nil, errors.New("invalid native channel endpoint")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, nil, errors.New("invalid native channel port")
	}
	der := bytes.Clone(descriptor.CertificateDER)
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, errors.New("invalid native channel certificate")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: "127.0.0.1", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, nil, errors.New("native channel trust is invalid or expired")
	}
	return certificate, roots, nil
}

// Do takes ownership of req.Body, including on rejected admission. Callers own
// the returned response body and must close it. Paths may vary; origin may not.
// The request body's Close must unblock a concurrent Read.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.Scheme+"://"+req.URL.Host != c.origin || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Opaque != "" || (req.Host != "" && req.Host != req.URL.Host) || unsupportedTransport(req) {
		if req != nil && req.Body != nil {
			req.Body.Close()
		}
		return nil, errors.New("native channel request destination rejected")
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(c.ctx, cancel)
	clone := req.Clone(ctx)
	var body *ownedBody
	stopBody := func() bool { return false }
	if req.Body != nil {
		body = &ownedBody{ReadCloser: req.Body}
		clone.Body = body
		// Transport cancellation alone may wait for a request-body Read. Close
		// our owned body explicitly so a stalled producer cannot prevent exit.
		stopBody = context.AfterFunc(ctx, func() { _ = body.Close() })
	}
	finish := func() {
		stop()
		cancel()
		if body != nil {
			_ = body.Close()
		}
		stopBody()
	}
	if c.ctx.Err() != nil {
		cancel()
	}
	response, err := c.client.Do(clone)
	if err != nil {
		finish()
		return nil, err
	}
	response.Body = &responseBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

type ownedBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *ownedBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

type responseBody struct {
	io.ReadCloser
	once   sync.Once
	finish func()
}

func (b *responseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.finish)
	}
	return n, err
}

func (b *responseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.finish)
	return err
}

// Close cancels active requests and stops new ones. It does not acquire a fresh
// descriptor, retry requests, or alter the serving generation.
func (c *Client) Close() {
	c.cancel()
	c.transport.CloseIdleConnections()
}
