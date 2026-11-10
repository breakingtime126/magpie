package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func connectOwnedFixture(t *testing.T, h http.Handler) (*Server, *ownedListener, *OwnedConnection) {
	t.Helper()
	s, ln, _ := newOwnedListenerFixture(t)
	s.ownedHTTP = h
	if _, err := ln.Accept(); err != nil {
		t.Fatal(err)
	}
	c, ok := s.OwnedConnection()
	if !ok {
		t.Fatal("owned connection unavailable")
	}
	return s, ln, c
}

func TestOwnedConnectionRejectsExpiredGeneration(t *testing.T) {
	for _, kind := range []string{"close", "replace", "cancel", "handoff", "stop", "export", "end"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			s, ln, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.Copy(w, r.Body)
			}))
			request := func() int {
				w := httptest.NewRecorder()
				c.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", strings.NewReader("synthetic-secret")))
				return w.Code
			}
			if request() != http.StatusOK || calls.Load() != 1 {
				t.Fatal("initial request did not reach owned stack")
			}
			switch kind {
			case "close":
				ln.Close()
			case "replace":
				_, next, _ := newOwnedListenerFixture(t)
				next.Accept()
				s.lnMu.Lock()
				s.ln = next // a new generation at even the exact same address
				s.lnMu.Unlock()
			case "cancel":
				ctx, cancel := context.WithCancel(context.Background())
				s.listenContext = ctx
				cancel()
			case "handoff":
				s.HandOff()
			case "stop":
				s.stopAccepting()
			case "export":
				// Even failed/uncertain export cannot restore ownership.
				_, _ = s.ListenerFile()
			case "end":
				s.listenContext = nil
			}
			if request() != http.StatusServiceUnavailable || calls.Load() != 1 {
				t.Fatal("expired connection reached handler")
			}
		})
	}
}

func TestOwnedConnectionPortReuseNeverReceivesSensitiveRequest(t *testing.T) {
	var ownedCalls, foreignCalls atomic.Int32
	s, old, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ownedCalls.Add(1) }))
	oldURL, _ := s.OwnedLoopbackURL()
	foreign, _, _ := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls.Add(1) }))
	foreignURL, _ := foreign.OwnedLoopbackURL()
	if foreignURL != oldURL {
		t.Fatal("fixture did not reuse address")
	}
	_, next, socket := newOwnedListenerFixture(t)
	socket.addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4343}
	s.lnMu.Lock()
	s.ln = next
	s.lnMu.Unlock()
	old.Close()
	next.Accept()
	// A hostile URL and Host still cannot select a network destination.
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest("POST", foreignURL+"/responses", strings.NewReader("synthetic-secret")))
	if w.Code != http.StatusServiceUnavailable || ownedCalls.Load() != 0 || foreignCalls.Load() != 0 {
		t.Fatal("old generation delivered request after port reuse")
	}
}

func TestOwnedConnectionInflightStaysOnCapturedStack(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s, ln, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		io.Copy(w, r.Body)
	}))
	w := httptest.NewRecorder()
	go func() {
		defer close(done)
		c.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", strings.NewReader("synthetic-secret")))
	}()
	<-entered
	// Close and replacement must not wait on a streaming handler's lock.
	ln.Close()
	_, next, _ := newOwnedListenerFixture(t)
	s.lnMu.Lock()
	s.ln = next
	s.lnMu.Unlock()
	close(release)
	<-done
	if w.Code != http.StatusOK || w.Body.String() != "synthetic-secret" {
		t.Fatal("admitted request did not finish on original handler")
	}
}

func TestOwnedConnectionZeroAndUnstartedFailClosed(t *testing.T) {
	s, _, _ := newOwnedListenerFixture(t)
	if _, ok := s.OwnedConnection(); ok {
		t.Fatal("unstarted connection issued")
	}
	for _, c := range []*OwnedConnection{nil, {}} {
		w := httptest.NewRecorder()
		c.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("empty capability accepted")
		}
	}
}

func TestOwnedConnectionConcurrentReplacement(t *testing.T) {
	var calls atomic.Int32
	s, old, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	start, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		<-start
		for i := 0; i < 100; i++ {
			c.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
		}
	}()
	close(start)
	_, next, _ := newOwnedListenerFixture(t)
	next.Accept()
	s.lnMu.Lock()
	s.ln = next
	s.lnMu.Unlock()
	old.Close()
	<-done
	before := calls.Load()
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		c.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("stale generation admitted")
		}
	}
	if calls.Load() != before {
		t.Fatal("stale request reached original stack")
	}
	fresh, ok := s.OwnedConnection()
	if !ok {
		t.Fatal("new generation unavailable")
	}
	w := httptest.NewRecorder()
	fresh.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", nil))
	if w.Code != http.StatusOK || calls.Load() != before+1 {
		t.Fatal("fresh generation did not dispatch")
	}
}

func TestOwnedConnectionCanceledRequestDoesNotReachHandler(t *testing.T) {
	var calls atomic.Int32
	_, _, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader("synthetic-secret")).WithContext(ctx)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatal("canceled request reached owned stack")
	}
}

func TestOwnedConnectionShutdownCancelsAndWaits(t *testing.T) {
	entered, canceled, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	s, _, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
		<-release
	}))
	go func() {
		defer close(finished)
		c.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	}()
	<-entered
	done := s.stopOwnedRequests()
	<-canceled
	select {
	case <-done:
		t.Fatal("shutdown completed before owned handler returned")
	default:
	}
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("shutdown admitted another request")
	}
	close(release)
	<-finished
	<-done
	if err := s.drainOwnedRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedConnectionShutdownBoundedForUncooperativeHandler(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s, _, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release // deliberately ignores cancellation until the fixture releases it
	}))
	go func() {
		defer close(finished)
		c.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // deterministic expired budget; no timing assumption
	err := s.drainOwnedRequests(ctx)
	close(release)
	<-finished
	if err != context.Canceled {
		t.Fatalf("drain returned %v; want expired budget", err)
	}
	if err := s.drainOwnedRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedConnectionServingCancellationReachesOldGeneration(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	s, old, c := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.listenContext = ctx
	go func() {
		defer close(finished)
		c.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	}()
	<-entered
	_, next, _ := newOwnedListenerFixture(t)
	s.lnMu.Lock()
	s.ln = next
	s.lnMu.Unlock()
	old.Close()
	select {
	case <-finished:
		t.Fatal("Relisten canceled an admitted request")
	default:
	}
	cancel()
	<-finished
	if err := s.drainOwnedRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
}
