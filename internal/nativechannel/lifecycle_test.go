package nativechannel

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestSessionRevokedBeforeUncooperativeHandlerDrains(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s, err := Start(context.Background(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error("session cleanup", err)
		}
	}()
	client, err := NewClient(s.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request, _ := http.NewRequest("POST", s.Descriptor().URL+"/v1/responses", nil)
	result := make(chan struct{})
	go func() {
		response, _ := client.Do(request)
		if response != nil {
			response.Body.Close()
		}
		close(result)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler not entered")
	}
	// Simulate serving loss, not parent cancellation. stop must revoke now even
	// though the injected admitted handler ignores its canceled request context.
	if err := s.server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Revoked():
	case <-time.After(time.Second):
		t.Fatal("serving loss did not revoke promptly")
	}
	select {
	case <-s.Done():
		t.Fatal("uncooperative handler falsely drained")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); err == nil {
		t.Fatal("unfinished handler cleanup reported complete")
	}
}
