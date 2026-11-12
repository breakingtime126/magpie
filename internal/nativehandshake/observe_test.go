package nativehandshake

import (
	"context"
	"regexp"
	"testing"
)

func TestHandshakeFixedStagesAndContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	var events []Stage
	ctx := WithObserver(parent, func(s Stage) { events = append(events, s) })
	if !Enabled(ctx) {
		t.Fatal("in-memory observation disabled")
	}
	for i := Stage(0); i < 255; i++ {
		Emit(ctx, i)
	}
	if len(events) != len(names)-1 {
		t.Fatal("unknown stage emitted")
	}
	for _, event := range events {
		if !regexp.MustCompile(`^[a-z-]+$`).MatchString(event.String()) {
			t.Fatal("nonfixed stage")
		}
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("observer changed lifetime")
	}
	Emit(WithObserver(ctx, func(Stage) { panic("PRIVATE_DATA") }), HostListening)
	Emit(WithObserver(ctx, nil), HostListening)
	if Enabled(WithObserver(ctx, nil)) {
		t.Fatal("nil sink should be inert")
	}
}
