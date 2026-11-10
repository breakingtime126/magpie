package nativechannel

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Relay forwards only POST /v1/responses over the pinned HTTP/SSE channel.
// It does not create or authenticate a native-facing listener. Its authority
// must be supplied by that listener's owner, never by an incoming HTTP request.
type Relay struct {
	authority string
	client    *Client
	proxy     *httputil.ReverseProxy
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	active    int
	drained   chan struct{}
	stopOnce  sync.Once
	stopWatch func() bool
}

// NewRelay reads the exact independently trusted publication once. It never
// reloads a changed file, discovers a replacement, or dials until a request is
// admitted. The caller still owns runtime, publication and listener provenance.
func NewRelay(parent context.Context, reference Reference, inboundAuthority string) (*Relay, error) {
	if parent == nil || parent.Err() != nil || !validRelayAuthority(inboundAuthority) {
		return nil, errors.New("native relay admission unavailable")
	}
	descriptor, err := ReadPublication(reference)
	if err != nil {
		return nil, err
	}
	client, err := NewClient(descriptor)
	if err != nil {
		return nil, err
	}
	target, _ := url.Parse(descriptor.URL) // Validated by ReadPublication/NewClient.
	ctx, cancel := context.WithCancel(parent)
	r := &Relay{authority: inboundAuthority, client: client, ctx: ctx, cancel: cancel, drained: make(chan struct{})}
	r.proxy = &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: "/v1/responses"}
			p.Out.Host = ""
			// Rewrite strips these by default. Retain exactly what the caller
			// supplied, so the gateway still treats forwarded calls as such.
			// Do not SetXForwarded: an admitted direct loopback call stays direct.
			for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if values, ok := p.In.Header[name]; ok {
					p.Out.Header[name] = append([]string(nil), values...)
				}
			}
		},
		Transport:     relayTransport{client: client},
		FlushInterval: -1,
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode >= 300 && response.StatusCode < 400 || response.StatusCode == http.StatusSwitchingProtocols {
				return errors.New("native relay does not forward redirects or upgrades")
			}
			response.Header.Del("Alt-Svc")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "native relay upstream unavailable", http.StatusBadGateway)
		},
		// Transport errors may contain URLs. Never log caller headers or
		// backend diagnostics; streaming failures still abort the HTTP stream.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	r.stopWatch = context.AfterFunc(parent, r.stop)
	return r, nil
}

type relayTransport struct{ client *Client }

func (t relayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.RequestURI = "" // Client.Do, unlike RoundTrip, requires client form.
	return t.client.Do(clone)
}

func validRelayAuthority(authority string) bool {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number <= 65535 && strconv.Itoa(number) == port
}

func (r *Relay) admits(request *http.Request) bool {
	if request == nil || request.URL == nil || request.Method != http.MethodPost || request.Host != r.authority || unsupportedTransport(request) {
		return false
	}
	u := request.URL
	if u.Path != "/v1/responses" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || len(request.Trailer) != 0 {
		return false
	}
	host, port, err := net.SplitHostPort(request.RemoteAddr)
	ip := net.ParseIP(host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return false
	}
	// Do not let hop-by-hop stripping erase Origin, Authorization, browser
	// markers or forwarding headers before the original gateway judges them.
	for _, value := range request.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if token = strings.ToLower(strings.TrimSpace(token)); token != "close" && token != "keep-alive" {
				return false
			}
		}
	}
	return true
}

func (r *Relay) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if !r.admits(request) {
		http.Error(w, "native relay request rejected", http.StatusForbidden)
		return
	}
	r.mu.Lock()
	if r.closed || r.ctx.Err() != nil {
		r.mu.Unlock()
		http.Error(w, "native relay closed", http.StatusServiceUnavailable)
		return
	}
	r.active++
	r.mu.Unlock()
	ctx, cancel := context.WithCancel(request.Context())
	stopRelay := context.AfterFunc(r.ctx, cancel)
	controller := http.NewResponseController(w)
	aborted := make(chan struct{})
	stopAbort := context.AfterFunc(ctx, func() {
		defer close(aborted)
		// Actual HTTP server bodies/writers can block on a peer. Expiring
		// both directions unblocks them. Unsupported custom writers may not
		// cooperate; Close then reports its deadline instead of claiming drain.
		_ = controller.SetReadDeadline(time.Now())
		_ = controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		stopRelay()
		if !stopAbort() {
			<-aborted
		}
		cancel()
		r.mu.Lock()
		r.active--
		if r.closed && r.active == 0 {
			close(r.drained)
		}
		r.mu.Unlock()
	}()
	clone := request.Clone(ctx)
	if clone.Body != nil && clone.Body != http.NoBody {
		clone.Body = &relayBody{ReadCloser: clone.Body, controller: controller}
	}
	r.proxy.ServeHTTP(w, clone)
}

// Closing an unread server body can itself drain/block. Expire its read side
// before closing unless EOF was observed; healthy completed requests keep their
// inbound keep-alive connection. The native endpoint must support read deadlines.
type relayBody struct {
	io.ReadCloser
	controller *http.ResponseController
	eof        atomic.Bool
	once       sync.Once
	closeErr   error
}

func (b *relayBody) Read(p []byte) (int, error) {
	if b.eof.Load() {
		return 0, io.EOF
	}
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof.Store(true)
	}
	return n, err
}

func (b *relayBody) Close() error {
	b.once.Do(func() {
		if !b.eof.Load() {
			_ = b.controller.SetReadDeadline(time.Now())
		}
		b.closeErr = b.ReadCloser.Close()
	})
	return b.closeErr
}

func (r *Relay) stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		if r.active == 0 {
			close(r.drained)
		}
		r.mu.Unlock()
		r.cancel()
		r.client.Close()
	})
}

// Close cancels and waits for full forwarding handlers, including SSE copies.
// The owner must separately close its native-facing listener. No listener trust
// or guarantee about arbitrary ResponseWriter implementations is inferred here.
func (r *Relay) Close(ctx context.Context) error {
	r.stop()
	r.stopWatch()
	select {
	case <-r.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
