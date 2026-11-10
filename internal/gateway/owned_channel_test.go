package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativechannel"
)

// This composes the real ownership capability and TLS channel with inert
// handlers. It does not start gateway account workers or a native child.
func TestOwnedConnectionPinnedChannelRequiresNewGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var calls atomic.Int32
	s, old, capability := connectOwnedFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(w, r.Body)
	}))
	open := func(capability *OwnedConnection) (*nativechannel.Session, *nativechannel.Client) {
		t.Helper()
		session, err := nativechannel.Start(ctx, capability)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			closeContext, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := session.Close(closeContext); err != nil {
				t.Error(err)
			}
		})
		// The reference comes directly from this owning publisher, never a
		// discovered URL/PID or sibling digest manifest. Recovering identical
		// bytes must preserve the captured listener generation.
		reference, err := nativechannel.Publish(filepath.Join(t.TempDir(), "channel.json"), session.Descriptor())
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := nativechannel.ReadPublication(reference)
		if err != nil {
			t.Fatal(err)
		}
		client, err := nativechannel.NewClient(recovered)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return session, client
	}
	request := func(session *nativechannel.Session, client *nativechannel.Client) (int, string) {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, "POST", session.Descriptor().URL+"/v1/responses", strings.NewReader("synthetic-body"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}
	session, client := open(capability)
	if status, body := request(session, client); status != http.StatusOK || body != "synthetic-body" {
		t.Fatal("owned request did not reach captured handler")
	}
	_, replacement, _ := newOwnedListenerFixture(t)
	if _, err := replacement.Accept(); err != nil {
		t.Fatal(err)
	}
	s.lnMu.Lock()
	s.ln = replacement
	s.lnMu.Unlock()
	_ = old.Close()
	if status, _ := request(session, client); status != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("old authenticated channel followed a replacement generation")
	}
	fresh, ok := s.OwnedConnection()
	if !ok {
		t.Fatal("explicit fresh capability unavailable")
	}
	freshSession, freshClient := open(fresh)
	if status, body := request(freshSession, freshClient); status != http.StatusOK || body != "synthetic-body" || calls.Load() != 2 {
		t.Fatal("explicit new channel did not use the new generation")
	}
}

// The relay goes through real gateway network/origin/caller guards around an
// inert leaf. No provider resolution, account refresh, or native process runs.
func TestOwnedConnectionRelayPreservesGatewayGuards(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var calls atomic.Int32
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer magpie-codex" {
			t.Error("caller label changed")
		}
		_, _ = io.Copy(w, r.Body)
	})
	s, old, capability := connectOwnedFixture(t, lanGuard(corsGuard(callerGuard(leaf))))
	session, err := nativechannel.Start(ctx, capability)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := session.Close(c); err != nil {
			t.Error(err)
		}
	}()
	reference, err := nativechannel.Publish(filepath.Join(t.TempDir(), "channel.json"), session.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	inbound := httptest.NewUnstartedServer(nil)
	relay, err := nativechannel.NewRelay(ctx, reference, inbound.Listener.Addr().String())
	if err != nil {
		inbound.Close()
		t.Fatal(err)
	}
	inbound.Config.Handler = relay
	inbound.Start()
	defer inbound.Close()
	defer func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := relay.Close(c); err != nil {
			t.Error(err)
		}
	}()
	request := func(host string, headers map[string]string) int {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, "POST", inbound.URL+"/v1/responses", strings.NewReader("synthetic-body"))
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			r.Host = host
		}
		r.Header.Set("Authorization", "Bearer magpie-codex")
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		response, err := inbound.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode
	}
	if code := request("", nil); code != http.StatusOK || calls.Load() != 1 {
		t.Fatal("direct local relay did not reach guarded leaf", code)
	}
	for _, tc := range []struct {
		name, host string
		headers    map[string]string
	}{
		{name: "null origin", headers: map[string]string{"Origin": "null"}},
		{name: "foreign page", headers: map[string]string{"Origin": "https://untrusted.invalid"}},
		{name: "foreign host", host: "untrusted.invalid", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}},
		{name: "forwarded call", headers: map[string]string{"X-Forwarded-For": "198.51.100.9"}},
		{name: "nominated origin", headers: map[string]string{"Origin": "null", "Connection": "Origin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := request(tc.host, tc.headers); code != http.StatusForbidden || calls.Load() != 1 {
				t.Fatal("relay bypassed existing gateway or inbound guard", code)
			}
		})
	}
	_, replacement, _ := newOwnedListenerFixture(t)
	if _, err := replacement.Accept(); err != nil {
		t.Fatal(err)
	}
	s.lnMu.Lock()
	s.ln = replacement
	s.lnMu.Unlock()
	_ = old.Close()
	if code := request("", nil); code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("relay followed expired owned generation", code)
	}
}
