package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
)

// These listeners never bind a socket, contact a service, or start a gateway's
// account-refresh workers. Accept returns one end of an in-memory connection.
type ownedListenerFixture struct {
	addr      net.Addr
	conn      net.Conn
	acceptErr error
	closeErr  error
	closed    bool
}

func (ln *ownedListenerFixture) Accept() (net.Conn, error) { return ln.conn, ln.acceptErr }
func (ln *ownedListenerFixture) Addr() net.Addr            { return ln.addr }
func (ln *ownedListenerFixture) Close() error {
	ln.closed = true
	return ln.closeErr
}

func newOwnedListenerFixture(t *testing.T) (*Server, *ownedListener, *ownedListenerFixture) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	f := &ownedListenerFixture{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3425}, conn: a}
	ln := &ownedListener{Listener: f, unshared: true, plain: true}
	s := &Server{ln: ln, listenContext: context.Background()}
	return s, ln, f
}

func requireOwnedListener(t *testing.T, s *Server, want bool) {
	t.Helper()
	u, ok := s.OwnedLoopbackURL()
	if ok != want || (want && u != "http://127.0.0.1:3425/v1") || (!want && u != "") {
		t.Fatalf("OwnedLoopbackURL() = %q, %v; want available %v", u, ok, want)
	}
}

func TestOwnedLoopbackListenerRequiresServing(t *testing.T) {
	s, ln, _ := newOwnedListenerFixture(t)
	requireOwnedListener(t, s, false)
	if _, err := ln.Accept(); err != nil {
		t.Fatal(err)
	}
	requireOwnedListener(t, s, true)
	s.listenContext = nil
	requireOwnedListener(t, s, false)
}

func TestOwnedLoopbackListenerClosedAndFailed(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("close uncertain")} {
		s, ln, f := newOwnedListenerFixture(t)
		if _, err := ln.Accept(); err != nil {
			t.Fatal(err)
		}
		f.closeErr = closeErr
		_ = ln.Close()
		if !f.closed {
			t.Fatal("underlying listener was not closed")
		}
		requireOwnedListener(t, s, false)
		// A late Accept completion cannot revive a closed listener.
		_, _ = ln.Accept()
		requireOwnedListener(t, s, false)
	}
	s, ln, f := newOwnedListenerFixture(t)
	if _, err := ln.Accept(); err != nil {
		t.Fatal(err)
	}
	f.acceptErr = errors.New("listener failed")
	_, _ = ln.Accept()
	requireOwnedListener(t, s, false)
}

func TestOwnedLoopbackListenerReplacement(t *testing.T) {
	s, old, _ := newOwnedListenerFixture(t)
	if _, err := old.Accept(); err != nil {
		t.Fatal(err)
	}
	requireOwnedListener(t, s, true)
	_ = old.Close()
	// A failed Relisten leaving the old pointer is still unavailable.
	requireOwnedListener(t, s, false)
	_, replacement, _ := newOwnedListenerFixture(t)
	s.ln = replacement
	requireOwnedListener(t, s, false)
	if _, err := replacement.Accept(); err != nil {
		t.Fatal(err)
	}
	requireOwnedListener(t, s, true)
	_ = old.Close()
	requireOwnedListener(t, s, true)
}

func TestOwnedLoopbackListenerAdmission(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Server, *ownedListener, *ownedListenerFixture)
	}{
		{"untracked", func(s *Server, ln *ownedListener, f *ownedListenerFixture) { s.ln = f }},
		{"inherited or shared", func(s *Server, ln *ownedListener, f *ownedListenerFixture) { ln.unshared = false }},
		{"TLS", func(s *Server, ln *ownedListener, f *ownedListenerFixture) { ln.plain = false }},
		{"wildcard", func(s *Server, ln *ownedListener, f *ownedListenerFixture) {
			f.addr = &net.TCPAddr{IP: net.IPv4zero, Port: 3425}
		}},
		{"IPv6", func(s *Server, ln *ownedListener, f *ownedListenerFixture) {
			f.addr = &net.TCPAddr{IP: net.IPv6loopback, Port: 3425}
		}},
		{"zero port", func(s *Server, ln *ownedListener, f *ownedListenerFixture) {
			f.addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
		}},
		{"canceled", func(s *Server, ln *ownedListener, f *ownedListenerFixture) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			s.listenContext = ctx
		}},
		{"handoff", func(s *Server, ln *ownedListener, f *ownedListenerFixture) { s.HandOff() }},
		{"stop accepting", func(s *Server, ln *ownedListener, f *ownedListenerFixture) { s.stopAccepting() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ln, f := newOwnedListenerFixture(t)
			if _, err := ln.Accept(); err != nil {
				t.Fatal(err)
			}
			tc.change(s, ln, f)
			requireOwnedListener(t, s, false)
		})
	}
}

func TestOwnedLoopbackListenerPortChangeNeverTrustsOldAddress(t *testing.T) {
	s, old, _ := newOwnedListenerFixture(t)
	if _, err := old.Accept(); err != nil {
		t.Fatal(err)
	}
	oldURL, ok := s.OwnedLoopbackURL()
	if !ok {
		t.Fatal("initial fixture listener unavailable")
	}
	_, replacement, replacementSocket := newOwnedListenerFixture(t)
	replacementSocket.addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4343}
	s.ln = replacement
	_ = old.Close()
	// Another server now accepts on the exact former address. Its address
	// and claimed identity cannot confer ownership on this server.
	foreign, foreignListener, _ := newOwnedListenerFixture(t)
	if _, err := foreignListener.Accept(); err != nil {
		t.Fatal(err)
	}
	foreignURL, ok := foreign.OwnedLoopbackURL()
	if !ok || foreignURL != oldURL {
		t.Fatal("fixture did not model reuse of the old address")
	}
	requireOwnedListener(t, s, false) // replacement has not begun serving
	if _, err := replacement.Accept(); err != nil {
		t.Fatal(err)
	}
	url, ok := s.OwnedLoopbackURL()
	if !ok || url != "http://127.0.0.1:4343/v1" || url == oldURL {
		t.Fatal("listener observation reused a stale address after port change")
	}
	_ = replacement.Close()
	if _, ok := s.OwnedLoopbackURL(); ok {
		t.Fatal("foreign reuse made a closed owned listener look usable")
	}
	// This test proves a fresh observation, not safety of any caller that
	// caches oldURL. Production Prepare remains disabled until launch-time
	// ownership is bound to the actual connection; no HTTP request is made.
}
