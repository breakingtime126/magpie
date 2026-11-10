package gateway

import (
	"net/http"
	"testing"
)

func TestOwnedConnectionValidNeverReacquiresGeneration(t *testing.T) {
	var empty *OwnedConnection
	if empty.Valid() || (&OwnedConnection{}).Valid() {
		t.Fatal("empty capability valid")
	}
	s, old, c := connectOwnedFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if !c.Valid() {
		t.Fatal("serving capability unavailable")
	}
	_, next, _ := newOwnedListenerFixture(t)
	if _, e := next.Accept(); e != nil {
		t.Fatal(e)
	}
	s.lnMu.Lock()
	s.ln = next
	s.lnMu.Unlock()
	if c.Valid() {
		t.Fatal("old capability followed replacement")
	}
	fresh, ok := s.OwnedConnection()
	if !ok || !fresh.Valid() {
		t.Fatal("explicit fresh capability unavailable")
	}
	old.Close()
	next.Close()
	if fresh.Valid() {
		t.Fatal("closed generation stayed valid")
	}
}
