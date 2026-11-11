package nativeprepdiag

import (
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestObserverFixedProtocolAndLifetime(t *testing.T) {
	var lines []string
	request, cancel := context.WithCancel(context.Background())
	ctx, finish := Begin(request, func(s string) { lines = append(lines, s) })
	lifetime := Inherit(context.Background(), ctx)
	outer := Track(ctx, RuntimePreparation)
	inner := Track(lifetime, RuntimeHomes)
	inner.Next(RuntimeVersion)
	inner.End()
	outer.End()
	finish(false)
	cancel()
	if lifetime.Err() != nil || ctx.Err() != context.Canceled {
		t.Fatal("observer changed cancellation")
	}
	Track(lifetime, HostPreparation).End()
	if len(lines) != 8 {
		t.Fatalf("events after attempt closed or missing: %v", lines)
	}
	pattern := regexp.MustCompile(`^native-connect-preparation-v1 attempt=([1-9][0-9]*) sequence=([1-9][0-9]*) stage=([a-z-]+) outcome=(entered|completed|failed)$`)
	var stack []string
	var attempt string
	for i, line := range lines {
		match := pattern.FindStringSubmatch(line)
		if match == nil || match[2] != fmt.Sprint(i+1) {
			t.Fatal("nonfixed protocol", line)
		}
		if i == 0 {
			attempt = match[1]
		}
		if match[1] != attempt {
			t.Fatal("attempt changed")
		}
		if match[4] == "entered" {
			stack = append(stack, match[3])
		} else {
			if len(stack) == 0 || stack[len(stack)-1] != match[3] {
				t.Fatal("not nested", lines)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 || !strings.HasSuffix(lines[len(lines)-1], "stage=connect outcome=failed") {
		t.Fatal(lines)
	}
}
func TestObserverFailureAndBounds(t *testing.T) {
	ctx, finish := Begin(context.Background(), func(string) { panic("SECRET-PATH-KEY") })
	Track(ctx, SupplierMetadata).End()
	finish(false)
	var lines []string
	ctx, finish = Begin(context.Background(), func(s string) { lines = append(lines, s) })
	Track(ctx, Stage(255)).End()
	for i := 0; i < MaxEvents; i++ {
		Track(ctx, RuntimeVersion).Complete()
	}
	finish(true)
	if len(lines) != MaxEvents {
		t.Fatal(len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "SECRET") || strings.Contains(line, "stage= outcome") {
			t.Fatal("unsafe output")
		}
	}
	if strings.Contains(lines[len(lines)-1], "stage=connect") {
		t.Fatal("overflow fabricated terminal success")
	}
}
func TestObserverAbsentAndAttemptIsolation(t *testing.T) {
	Track(nil, RuntimeVersion).End()
	Track(context.Background(), RuntimeVersion).Complete()
	var a, b []string
	ca, fa := Begin(context.Background(), func(s string) { a = append(a, s) })
	cb, fb := Begin(context.Background(), func(s string) { b = append(b, s) })
	Track(ca, SupplierMetadata).Complete()
	Track(cb, RuntimeVersion).End()
	fa(true)
	fb(false)
	if len(a) != 4 || len(b) != 4 || strings.Contains(strings.Join(a, "\n"), "runtime-version") || strings.Contains(strings.Join(b, "\n"), "supplier-metadata") {
		t.Fatal(a, b)
	}
	if strings.Fields(a[0])[1] == strings.Fields(b[0])[1] {
		t.Fatal("reused attempt")
	}
}

func TestObserverDoesNoSinkWorkDuringPreparationAndFinishOnce(t *testing.T) {
	calls := 0
	ctx, finish := Begin(context.Background(), func(string) { calls++ })
	tr := Track(ctx, RuntimeHomes)
	tr.Complete()
	if calls != 0 {
		t.Fatal("sink called during operation")
	}
	finish(true)
	count := calls
	finish(false)
	if count != 4 || calls != count {
		t.Fatal("finish replayed records", calls)
	}
}

func TestObserverConcurrentFinishAndEmitsAreBounded(t *testing.T) {
	var lines []string
	ctx, finish := Begin(context.Background(), func(s string) { lines = append(lines, s) })
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); tr := Track(ctx, RuntimeVersion); tr.Complete() }()
	}
	workers.Wait()
	var finishers sync.WaitGroup
	for i := 0; i < 8; i++ {
		finishers.Add(1)
		go func() { defer finishers.Done(); finish(true) }()
	}
	finishers.Wait()
	if len(lines) != 42 {
		t.Fatal("lost/duplicated records", len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, fmt.Sprintf(" sequence=%d ", i+1)) {
			t.Fatal("reordered records", line)
		}
	}
}

type failedLogWriter struct{ calls int }

func (w *failedLogWriter) Write([]byte) (int, error) { w.calls++; return 0, io.ErrClosedPipe }
func TestObserverFailedLogWriterDoesNotChangeCaller(t *testing.T) {
	writer := &failedLogWriter{}
	logger := log.New(writer, "", log.LstdFlags)
	ctx, finish := Begin(context.Background(), func(s string) { logger.Print(s) })
	Track(ctx, RuntimeVersion).Complete()
	if writer.calls != 0 {
		t.Fatal("log touched before operation returned")
	}
	finish(true)
	if writer.calls != 4 {
		t.Fatal("unexpected writes", writer.calls)
	}
	// The writer accepted no bytes. A collector has no attempt evidence; no
	// success/failure claim or retry can be derived from a successful return here.
}
