package nativehost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeconnect"
)

func TestHostChannelRechecksAdmissionOnEveryRequest(t *testing.T) {
	cfg, root, launcher, cap, owner := fixture(t)
	var allowed atomic.Bool
	allowed.Store(true)
	h, err := startWithAdmission(context.Background(), cap, cfg, root, dependencies{
		resolve: func() (nativeconnect.PackagedLauncher, error) { return launcher, nil },
		listen:  func(nativebootstrap.Record, nativebootstrap.Peer) (pipeOwner, error) { return owner, nil },
	}, func(context.Context) error {
		if !allowed.Load() {
			return errors.New("private synthetic supplier detail")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeHost(t, h)
	client, err := nativechannel.NewClient(h.session.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	send := func() (int, string) {
		t.Helper()
		req, err := http.NewRequest("POST", h.session.Descriptor().URL+"/v1/responses", strings.NewReader("authored request"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(data)
	}
	if code, body := send(); code != http.StatusOK || body != "authored request" {
		t.Fatal(code, body)
	}
	allowed.Store(false)
	// No Connect, Plan or watcher wake occurs between these requests.
	if code, body := send(); code != http.StatusServiceUnavailable || body != "native supplier admission unavailable\n" {
		t.Fatal("invalid admission leaked or reached gateway", code, body)
	}
	if cap.calls.Load() != 1 {
		t.Fatal("revoked request reached captured gateway", cap.calls.Load())
	}
}

func TestAdmittedCapabilityCancellationAndCapturedValidity(t *testing.T) {
	for _, kind := range []string{"already-canceled", "during-check", "capture-changed"} {
		t.Run(kind, func(t *testing.T) {
			cap := &fakeCapability{}
			cap.valid.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "already-canceled" {
				cancel()
			}
			var calls atomic.Int32
			handler := admittedCapability{captured: cap, admission: func(ctx context.Context) error {
				calls.Add(1)
				if _, ok := ctx.Deadline(); !ok {
					t.Error("admission has no deadline")
				}
				if kind == "during-check" {
					cancel()
				}
				if kind == "capture-changed" {
					cap.valid.Store(false)
				}
				return nil
			}}
			request := httptest.NewRequest("POST", "http://127.0.0.1/v1/responses", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || cap.calls.Load() != 0 {
				t.Fatal("invalid request reached gateway", response.Code)
			}
			if kind == "already-canceled" && calls.Load() != 0 {
				t.Fatal("canceled request invoked supplier")
			}
			if handler.Valid() != cap.Valid() {
				t.Fatal("captured validity changed")
			}
		})
	}
}
