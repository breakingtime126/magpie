package gateway

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
)

// ownedListener observes the actual HTTP listener lifecycle. A bound socket
// alone is not ready: Serve must have begun accepting on that same socket.
type ownedListener struct {
	net.Listener
	unshared  bool
	plain     bool
	accepting atomic.Bool
	closed    atomic.Bool
	exported  atomic.Bool
}

func (ln *ownedListener) Accept() (net.Conn, error) {
	ln.accepting.Store(true)
	conn, err := ln.Listener.Accept()
	if err != nil {
		if temporary, ok := err.(net.Error); !ok || !temporary.Temporary() {
			ln.accepting.Store(false)
			ln.closed.Store(true)
		}
	}
	return conn, err
}

func (ln *ownedListener) Close() error {
	// Invalidate before Close can block or fail. A failed close is uncertain,
	// never evidence that this endpoint is safe to hand to another process.
	ln.closed.Store(true)
	return ln.Listener.Close()
}

// OwnedLoopbackURL observes this server's current plain, unshared IPv4
// loopback listener, only after HTTP serving begins. Unshared means no known
// inheritance/export, not kernel address exclusivity (notably on Windows). It does not probe another
// process, change gateway policy, or promise ownership at a future launch.
// The public caller token is not authentication or ownership evidence.
func (s *Server) OwnedLoopbackURL() (string, bool) {
	s.lnMu.Lock()
	defer s.lnMu.Unlock()
	return s.ownedLoopbackURLLocked()
}

func (s *Server) ownedLoopbackURLLocked() (string, bool) {
	if s.listenContext == nil || s.listenContext.Err() != nil || s.handedOff.Load() || s.notAccepting.Load() || s.ownedStopped {
		return "", false
	}
	ln, ok := s.ln.(*ownedListener)
	if !ok || !ln.unshared || !ln.plain || ln.exported.Load() || ln.closed.Load() || !ln.accepting.Load() {
		return "", false
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Zone != "" || !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) || addr.Port < 1 || addr.Port > 65535 {
		return "", false
	}
	return "http://127.0.0.1:" + strconv.Itoa(addr.Port) + "/v1", true
}

// OwnedConnection is a process-local capability for one listener generation.
// Its fields cannot be supplied by a peer or reconstructed from a saved URL.
// It dispatches through the exact owned HTTP stack without dialing any address.
// Keep the capability, not OwnedLoopbackURL, for a connection's whole lifetime.
// It cannot itself authorize a separate native process to use a loopback URL.
type OwnedConnection struct {
	server   *Server
	listener *ownedListener
	handler  http.Handler
}

// OwnedConnection captures the current serving generation. A replacement must
// be acquired explicitly; an old capability never follows Relisten or handoff.
func (s *Server) OwnedConnection() (*OwnedConnection, bool) {
	s.lnMu.Lock()
	defer s.lnMu.Unlock()
	if _, ok := s.ownedLoopbackURLLocked(); !ok || s.ownedHTTP == nil {
		return nil, false
	}
	return &OwnedConnection{server: s, listener: s.ln.(*ownedListener), handler: s.ownedHTTP}, true
}

// Valid observes only the captured listener generation. It does not probe a
// URL or acquire a replacement capability. Hosts may poll it to cancel their
// independent channel lifetime; ServeHTTP still rechecks admission atomically.
func (c *OwnedConnection) Valid() bool {
	if c == nil || c.server == nil || c.listener == nil || c.handler == nil {
		return false
	}
	s := c.server
	s.lnMu.Lock()
	defer s.lnMu.Unlock()
	_, ready := s.ownedLoopbackURLLocked()
	return ready && s.ln == c.listener
}

// ServeHTTP admits each request against the captured generation, before the
// handler can read its body. Rejected requests never reach any handler. Already
// admitted requests may finish on the original stack, just as Relisten allows
// existing requests to drain. No lock is held while a handler streams or blocks.
// Closing, replacing or exporting the listener cannot redirect such a request
// to another process, even if that process immediately reuses its old port.
func (c *OwnedConnection) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if c == nil || c.server == nil || c.listener == nil || c.handler == nil {
		http.Error(w, "owned gateway connection unavailable", http.StatusServiceUnavailable)
		return
	}
	s := c.server
	s.lnMu.Lock()
	_, ready := s.ownedLoopbackURLLocked()
	ready = ready && s.ln == c.listener
	if !ready || r.Context().Err() != nil {
		s.lnMu.Unlock()
		http.Error(w, "owned gateway connection expired or canceled", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	call := &ownedRequest{cancel: cancel}
	if len(s.ownedRequests) == 0 {
		s.ownedDrained = make(chan struct{})
	}
	if s.ownedRequests == nil {
		s.ownedRequests = make(map[*ownedRequest]struct{})
	}
	s.ownedRequests[call] = struct{}{}
	stop := context.AfterFunc(s.listenContext, cancel)
	s.lnMu.Unlock()
	defer func() {
		stop()
		cancel()
		s.lnMu.Lock()
		delete(s.ownedRequests, call)
		if len(s.ownedRequests) == 0 {
			close(s.ownedDrained)
		}
		s.lnMu.Unlock()
	}()
	c.handler.ServeHTTP(w, r.WithContext(ctx))
}

// ownedRequest is registered before admission releases lnMu. Cancellation
// belongs to the serving run, not a listener generation: Relisten drains calls,
// while a complete shutdown cancels them even on an old generation.
type ownedRequest struct{ cancel context.CancelFunc }

// stopOwnedRequests rejects further admission, cancels every admitted request,
// and returns a channel closed once their handlers have returned. Cancellation
// runs outside lnMu. A handler must honor its request context, as with net/http.
func (s *Server) stopOwnedRequests() <-chan struct{} {
	s.lnMu.Lock()
	s.ownedStopped = true
	if s.ownedDrained == nil {
		s.ownedDrained = make(chan struct{})
		close(s.ownedDrained)
	}
	done := s.ownedDrained
	cancels := make([]context.CancelFunc, 0, len(s.ownedRequests))
	for call := range s.ownedRequests {
		cancels = append(cancels, call.cancel)
	}
	s.lnMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return done
}

func (s *Server) drainOwnedRequests(ctx context.Context) error {
	done := s.stopOwnedRequests()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
