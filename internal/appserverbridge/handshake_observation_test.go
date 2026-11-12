package appserverbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/nativehandshake"
)

type handshakeEvents struct {
	sync.Mutex
	events  []nativehandshake.Stage
	changed chan struct{}
}

func (e *handshakeEvents) record(s nativehandshake.Stage) {
	e.Lock()
	e.events = append(e.events, s)
	e.Unlock()
	if e.changed != nil {
		select {
		case e.changed <- struct{}{}:
		default:
		}
	}
}
func (e *handshakeEvents) has(s nativehandshake.Stage) bool {
	e.Lock()
	defer e.Unlock()
	for _, v := range e.events {
		if v == s {
			return true
		}
	}
	return false
}
func (e *handshakeEvents) wait(t *testing.T, s nativehandshake.Stage) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for !e.has(s) {
		select {
		case <-e.changed:
		case <-timeout:
			t.Fatal("missing fixed handshake event", s)
		}
	}
}

type handshakeWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mode    string
}

func (w *handshakeWriter) Write(b []byte) (int, error) {
	close(w.entered)
	<-w.release
	switch w.mode {
	case "short":
		return len(b) - 1, nil
	case "error", "cancel":
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}
func (w *handshakeWriter) Close() error { w.once.Do(func() { close(w.release) }); return nil }

func TestHandshakeActualRunRequiresCompletedOutputWrite(t *testing.T) {
	for _, mode := range []string{"complete", "short", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cfg := fixtureConfig(t)
			input, client := io.Pipe()
			output := &handshakeWriter{entered: make(chan struct{}), release: make(chan struct{}), mode: mode}
			events := &handshakeEvents{changed: make(chan struct{}, 64)}
			ctx, cancel := context.WithCancel(nativehandshake.WithObserver(context.Background(), events.record))
			done := make(chan error, 1)
			go func() { done <- Run(ctx, cfg, input, output) }()
			t.Cleanup(func() {
				cancel()
				client.Close()
				output.Close()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
					t.Error("owned run did not drain")
				}
			})
			if _, err := fmt.Fprintln(client, `{"id":"PRIVATE_REQUEST_ID","method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`); err != nil {
				t.Fatal(err)
			}
			select {
			case <-output.entered:
			case <-time.After(15 * time.Second):
				t.Fatal("initialize writer not entered")
			}
			if !events.has(nativehandshake.InitializationReady) || events.has(nativehandshake.InitializeOutputWritten) {
				t.Fatal("ready or pending write claimed delivery")
			}
			if mode == "cancel" {
				cancel()
			} else {
				output.Close()
			}
			if mode == "complete" {
				events.wait(t, nativehandshake.InitializeOutputWritten)
				client.Close()
			} else {
				events.wait(t, nativehandshake.StreamWriteFailed)
				if events.has(nativehandshake.InitializeOutputWritten) {
					t.Fatal("failed write claimed delivery")
				}
			}
		})
	}
}

func TestHandshakeInitialCatalogBarrierAndRefusals(t *testing.T) {
	for _, mode := range []string{"complete", "error", "malformed", "catalog-error"} {
		t.Run(mode, func(t *testing.T) {
			r := initialCatalogRouter167(t)
			events := &handshakeEvents{}
			o := newHandshakeObservation(nativehandshake.WithObserver(context.Background(), events.record))
			before := r.phase
			line := []byte(`{"id":"init","method":"initialize","params":{"clientInfo":{"name":"fixture","version":"1"}}}`)
			out, err := r.Client(line)
			if err != nil {
				t.Fatal(err)
			}
			o.client(r, before, line, out)
			replyObserved := func(d Delivery, result json.RawMessage, isError bool) []Delivery {
				m, _ := parseObject(d.Message)
				response := object{"id": m["id"]}
				if isError {
					response["error"] = result
				} else {
					response["result"] = result
				}
				b := raw(response)
				pending := o.pending(r, d.Backend, b)
				prior := r.phase
				next, err := r.Server(d.Backend, b)
				o.routed(r, prior, d.Backend, b, pending, err)
				for _, v := range next {
					o.wrote(r, v, nil)
				}
				return next
			}
			for _, d := range out {
				o.wrote(r, d, nil)
			}
			for i, d := range out {
				result := raw(object{"userAgent": raw("synthetic"), "codexHome": raw(r.byID[d.Backend].CodexHome), "platformFamily": raw("fixture"), "platformOs": raw("fixture")})
				if i == 1 && mode == "error" {
					replyObserved(d, raw(object{"code": raw(-32000), "message": raw("PRIVATE_ERROR")}), true)
					break
				}
				if i == 1 && mode == "malformed" {
					result = raw(object{})
				}
				next := replyObserved(d, result, false)
				if i == 0 && events.has(nativehandshake.InitializationReady) {
					t.Fatal("one backend established readiness")
				}
				if i == 1 && (mode == "complete" || mode == "catalog-error") {
					if events.has(nativehandshake.InitializationReady) || events.has(nativehandshake.InitializeOutputWritten) {
						t.Fatal("native replies bypassed catalog barrier")
					}
					prior := r.phase
					notification := []byte(`{"method":"initialized"}`)
					delivered, _ := r.Client(notification)
					o.client(r, prior, notification, delivered)
					o.routed(r, prior, "", notification, false, nil)
					if events.has(nativehandshake.InitializationReady) {
						t.Fatal("optional notification claimed readiness")
					}
					for j, catalog := range next {
						if j == 1 && mode == "catalog-error" {
							replyObserved(catalog, raw(object{"code": raw(-32000), "message": raw("PRIVATE_ERROR")}), true)
						} else {
							replyObserved(catalog, raw(object{"data": raw([]json.RawMessage{fixtureModel(fmt.Sprint(j), true)}), "nextCursor": raw(nil)}), false)
						}
					}
				}
			}
			if events.has(nativehandshake.InitializeOutputWritten) != (mode == "complete") {
				t.Fatal("handshake result changed", mode)
			}
		})
	}
}

func TestHandshakeWrongDeliveryNeverAcknowledges(t *testing.T) {
	r := testRouter(t, false)
	if r.phase != "ready" {
		t.Fatal("fixture must exercise a genuinely ready router")
	}
	events := &handshakeEvents{}
	o := newHandshakeObservation(nativehandshake.WithObserver(context.Background(), events.record))
	o.id = raw("expected")
	o.ready = true
	for _, line := range []string{`{"id":"other","result":{}}`, `{"id":"expected","error":{}}`, `{"id":"expected","method":"initialized","result":{}}`} {
		o.wrote(r, Delivery{Message: []byte(line)}, nil)
	}
	o.wrote(r, Delivery{Message: []byte(`{"id":"expected","result":{}}`)}, errors.New("private"))
	if events.has(nativehandshake.InitializeOutputWritten) {
		t.Fatal("unrelated or failed write acknowledged")
	}
	o.wrote(r, Delivery{Message: []byte(`{"id":"expected","result":{}}`)}, nil)
	if !events.has(nativehandshake.InitializeOutputWritten) {
		t.Fatal("matching completed write was not observed")
	}
}

func TestHandshakeLocalInitializeRejectionIsObserved(t *testing.T) {
	for _, caps := range []string{`{"requestAttestation":true}`, `{"explicitGatewayOauth":true}`, `"invalid"`} {
		r, err := NewRouter([]Backend{{ID: "primary", Provider: "openai"}, {ID: "secondary", Provider: "custom"}})
		if err != nil {
			t.Fatal(err)
		}
		events := &handshakeEvents{}
		o := newHandshakeObservation(nativehandshake.WithObserver(context.Background(), events.record))
		line := []byte(`{"id":1,"method":"initialize","params":{"capabilities":` + caps + `}}`)
		previous := r.phase
		out, err := r.Client(line)
		o.client(r, previous, line, out)
		o.routed(r, previous, "", line, false, err)
		if !events.has(nativehandshake.InitializeReceived) || !events.has(nativehandshake.InitializationRejected) || events.has(nativehandshake.InitializeOutputWritten) {
			t.Fatal("valid rejected initialize was invisible")
		}
	}
	for _, line := range []string{`{"method":"initialized"}`, `not json`} {
		r, _ := NewRouter([]Backend{{ID: "primary", Provider: "openai"}, {ID: "secondary", Provider: "custom"}})
		events := &handshakeEvents{}
		o := newHandshakeObservation(nativehandshake.WithObserver(context.Background(), events.record))
		previous := r.phase
		out, err := r.Client([]byte(line))
		o.client(r, previous, []byte(line), out)
		o.routed(r, previous, "", []byte(line), false, err)
		if events.has(nativehandshake.InitializeReceived) || events.has(nativehandshake.InitializeOutputWritten) {
			t.Fatal("notification or malformed input fabricated initialize")
		}
	}
}
