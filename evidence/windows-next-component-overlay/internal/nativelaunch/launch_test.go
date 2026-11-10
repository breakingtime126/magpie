package nativelaunch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/appserverbridge"
	"github.com/yetone/magpie/internal/nativebootstrap"
	"github.com/yetone/magpie/internal/nativechannel"
	"github.com/yetone/magpie/internal/nativeprofile"
)

type fakeLease struct {
	ctx      context.Context
	record   nativebootstrap.Record
	closed   int
	closeErr error
}

func (l *fakeLease) Context() context.Context       { return l.ctx }
func (l *fakeLease) Record() nativebootstrap.Record { return l.record }
func (l *fakeLease) Close(context.Context) error    { l.closed++; return l.closeErr }

type fakeEndpoint struct {
	runs, stops int
	onRun       func(context.Context, appserverbridge.Config, nativechannel.Reference) error
}

func (e *fakeEndpoint) shutdown(context.Context) error { e.stops++; return nil }
func (e *fakeEndpoint) run(ctx context.Context, cfg appserverbridge.Config, _ appserverbridge.LaunchOptions, r nativechannel.Reference, _ io.ReadCloser, _ io.WriteCloser) error {
	e.runs++
	if e.onRun != nil {
		return e.onRun(ctx, cfg, r)
	}
	return nil
}

type unopenedStream struct{ t *testing.T }

func (s unopenedStream) Read([]byte) (int, error) {
	s.t.Fatal("unexpected pre-entry stream read")
	return 0, io.EOF
}
func (s unopenedStream) Write([]byte) (int, error) {
	s.t.Fatal("unexpected pre-entry stream write")
	return 0, io.EOF
}
func (s unopenedStream) Close() error { s.t.Fatal("unexpected pre-entry stream close"); return nil }
func launchFixture(t *testing.T) (appserverbridge.Config, *nativeprofile.Prepared, nativebootstrap.Record) {
	t.Helper()
	command := filepath.Join(t.TempDir(), "never-run.exe")
	if err := os.WriteFile(command, []byte("inert synthetic executable fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appserverbridge.Config{Backends: []appserverbridge.Backend{
		{ID: "a", Provider: "openai", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()},
		{ID: "b", Provider: "synthetic", Command: command, Home: t.TempDir(), CodexHome: t.TempDir()},
	}}
	p, err := nativeprofile.New(strings.Repeat("1", 32), cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := nativebootstrap.Record{Version: 1, Generation: p.Generation(), ProfileSHA256: p.SHA256(), Reference: nativechannel.Reference{Path: filepath.Join(t.TempDir(), "public-descriptor.json"), SHA256: strings.Repeat("a", 64)}}
	return cfg, p, r
}
func TestUntrustedProfileAndOwnerNeverAllocateOrLaunch(t *testing.T) {
	for _, mode := range []string{"unknown-owner", "tampered-profile", "dead-lease", "wrong-generation", "unknown-format", "invalid-config"} {
		t.Run(mode, func(t *testing.T) {
			cfg, p, r := launchFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := &fakeLease{ctx: ctx, record: r}
			ep := &fakeEndpoint{}
			connections, allocations := 0, 0
			ownerErr := errors.New("unknown expected package owner")
			deps := dependencies{connect: func(context.Context, string) (lease, error) {
				connections++
				if mode == "unknown-owner" {
					return nil, ownerErr
				}
				return l, nil
			}, allocate: func() (endpoint, error) { allocations++; return ep, nil }}
			data := p.Bytes()
			if mode == "tampered-profile" {
				data = bytes.Replace(data, []byte(`"provider":"openai"`), []byte(`"provider":"changed"`), 1)
			}
			if mode == "dead-lease" {
				cancel()
			}
			if mode == "wrong-generation" {
				l.record.Generation = strings.Repeat("2", 32)
			}
			if mode == "unknown-format" {
				data = []byte(`{"backends":[]}`)
			}
			// Config validation would now fail, making owner/hash rejection order visible.
			if err := os.Remove(cfg.Backends[0].Command); err != nil {
				t.Fatal(err)
			}
			err := run(context.Background(), bytes.NewReader(data), appserverbridge.LaunchOptions{}, unopenedStream{t}, unopenedStream{t}, deps)
			if err == nil || allocations != 0 || ep.runs != 0 {
				t.Fatal("untrusted input reached launch", err, allocations, ep.runs)
			}
			if mode == "unknown-owner" && !errors.Is(err, ownerErr) {
				t.Fatal("Config validation preceded owner authentication", err)
			}
			if mode != "unknown-owner" && mode != "unknown-format" && mode != "invalid-config" && !errors.Is(err, ErrUnavailable) {
				t.Fatal("Config validation preceded hash/lease gate", err)
			}
			if mode == "unknown-format" && connections != 0 {
				t.Fatal("unknown explicit format attempted fallback")
			}
		})
	}
}
func TestLeaseCancellationAtRunnerBoundary(t *testing.T) {
	_, p, r := launchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := &fakeLease{ctx: ctx, record: r}
	entered := make(chan struct{})
	ep := &fakeEndpoint{onRun: func(actual context.Context, cfg appserverbridge.Config, ref nativechannel.Reference) error {
		if actual != ctx || ref != r.Reference || len(cfg.Backends) != 2 {
			t.Error("lost trusted runner arguments")
		}
		close(entered)
		<-actual.Done()
		return actual.Err()
	}}
	deps := dependencies{connect: func(context.Context, string) (lease, error) { return l, nil }, allocate: func() (endpoint, error) { return ep, nil }}
	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), bytes.NewReader(p.Bytes()), appserverbridge.LaunchOptions{}, unopenedStream{t}, unopenedStream{t}, deps)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runner not entered")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || l.closed != 1 || ep.runs != 1 {
			t.Fatal(err, l.closed, ep.runs)
		}
	case <-time.After(time.Second):
		t.Fatal("lease cancellation did not reach runner")
	}
}
func TestLeaseLossAfterAllocationClosesUnusedEndpoint(t *testing.T) {
	_, p, r := launchFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := &fakeLease{ctx: ctx, record: r}
	ep := &fakeEndpoint{}
	deps := dependencies{connect: func(context.Context, string) (lease, error) { return l, nil }, allocate: func() (endpoint, error) { cancel(); return ep, nil }}
	if err := run(context.Background(), bytes.NewReader(p.Bytes()), appserverbridge.LaunchOptions{}, unopenedStream{t}, unopenedStream{t}, deps); err == nil || ep.runs != 0 || ep.stops != 1 || l.closed != 1 {
		t.Fatal(err, ep)
	}
}
func TestLeaseCleanupFailureIsReturned(t *testing.T) {
	_, p, r := launchFixture(t)
	failure := errors.New("lease close failed")
	l := &fakeLease{ctx: context.Background(), record: r, closeErr: failure}
	ep := &fakeEndpoint{}
	deps := dependencies{connect: func(context.Context, string) (lease, error) { return l, nil }, allocate: func() (endpoint, error) { return ep, nil }}
	if err := run(context.Background(), bytes.NewReader(p.Bytes()), appserverbridge.LaunchOptions{}, unopenedStream{t}, unopenedStream{t}, deps); !errors.Is(err, failure) || ep.runs != 1 {
		t.Fatal(err)
	}
}
