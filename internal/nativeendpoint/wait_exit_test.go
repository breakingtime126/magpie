package nativeendpoint

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func (p *fakeChild) exitError() error { return p.err }

func TestWaitForExitDoesNotTerminateLiveChild(t *testing.T) {
	s := socketFixture()
	p := newChild(true)
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if e.WaitForExit(context.Background()) == nil {
		t.Fatal("unstarted accepted")
	}
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(e.WaitForExit(ctx), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	select {
	case <-p.killed:
		t.Fatal("wait terminated child")
	default:
	}
	p.end()
	if err := e.WaitForExit(context.Background()); err != nil || !s.closed.Load() {
		t.Fatal("successful wait lacks drainage", err)
	}
}

type failedExitChild struct{ *fakeChild }

func (failedExitChild) exitError() error { return errors.New("synthetic root exited 7") }

func TestWaitForExitRejectsFailedStatusAfterSuccessfulDrain(t *testing.T) {
	s := socketFixture()
	p := failedExitChild{newChild(true)}
	s.launch = func(*exec.Cmd) (child, error) { return p, nil }
	e := endpointFixture(t, s)
	if err := e.Start(commandFixture(t)); err != nil {
		t.Fatal(err)
	}
	p.end()
	if err := e.WaitForExit(context.Background()); err == nil {
		t.Fatal("successful cleanup substituted for native success")
	}
	if err := e.Shutdown(context.Background()); err != nil {
		t.Fatal("nonzero process did not drain", err)
	}
}
