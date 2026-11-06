package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func auditMessage(thread, turn, item, text string) message {
	return message{Method: "item/completed", Params: jsonBytes(map[string]any{
		"threadId": thread, "turnId": turn,
		"item": map[string]any{"type": "agentMessage", "id": item, "text": text},
	})}
}

func auditOwners(t *testing.T, a *assistantAudit) {
	t.Helper()
	for _, route := range []string{"alpha", "beta"} {
		if err := a.owner("thread-"+route, route); err != nil {
			t.Fatal(err)
		}
	}
}

func auditedTurn(t *testing.T, a *assistantAudit, route string, round int, early bool) {
	t.Helper()
	thread := "thread-" + route
	turn := fmt.Sprintf("turn-%s-%d", route, round)
	item := fmt.Sprintf("item-%s-%d", route, round)
	if err := a.begin(thread, route); err != nil {
		t.Fatal(err)
	}
	m := auditMessage(thread, turn, item, "fixture-answer-"+route)
	if early {
		if err := a.observe(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.bind(thread, turn); err != nil {
		t.Fatal(err)
	}
	if !early {
		if err := a.observe(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.claim(thread, turn, item); err != nil {
		t.Fatal(err)
	}
}

func fourAuditedTurns(t *testing.T, a *assistantAudit) {
	t.Helper()
	auditOwners(t, a)
	for round := 1; round <= 2; round++ {
		for _, route := range []string{"alpha", "beta"} {
			auditedTurn(t, a, route, round, round == 1)
		}
	}
}

func TestAssistantAuditCountsAllFourIncludingEarlyCompletions(t *testing.T) {
	var a assistantAudit
	fourAuditedTurns(t, &a)
	n, err := a.finish()
	if err != nil || n != 4 {
		t.Fatal("four fully accounted completions rejected")
	}
}

func TestAssistantAuditRejectsWrongAndUnexplainedCompletions(t *testing.T) {
	for _, test := range []struct{ name, thread, turn, item, text string }{
		{"other-thread", "thread-beta", "turn-alpha-1", "item-alpha-1", "fixture-answer-alpha"},
		{"unknown-thread", "unknown", "turn-alpha-1", "item-alpha-1", "fixture-answer-alpha"},
		{"other-turn", "thread-alpha", "other", "item-alpha-1", "fixture-answer-alpha"},
		{"missing-thread", "", "turn-alpha-1", "item-alpha-1", "fixture-answer-alpha"},
		{"missing-turn", "thread-alpha", "", "item-alpha-1", "fixture-answer-alpha"},
		{"missing-item", "thread-alpha", "turn-alpha-1", "", "fixture-answer-alpha"},
		{"wrong-answer", "thread-alpha", "turn-alpha-1", "item-alpha-1", "fixture-answer-beta"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var a assistantAudit
			auditOwners(t, &a)
			if err := a.begin("thread-alpha", "alpha"); err != nil {
				t.Fatal(err)
			}
			if err := a.bind("thread-alpha", "turn-alpha-1"); err != nil {
				t.Fatal(err)
			}
			if err := a.observe(auditMessage(test.thread, test.turn, test.item, test.text)); err == nil {
				t.Fatal("unexplained completion accepted")
			}
			if _, err := a.finish(); err == nil {
				t.Fatal("failed audit recovered as success")
			}
		})
	}
}

func TestAssistantAuditRejectsDuplicateAndSecondItems(t *testing.T) {
	for _, id := range []string{"item-alpha-1", "extra-item"} {
		t.Run(id, func(t *testing.T) {
			var a assistantAudit
			auditOwners(t, &a)
			if err := a.begin("thread-alpha", "alpha"); err != nil {
				t.Fatal(err)
			}
			if err := a.bind("thread-alpha", "turn-alpha-1"); err != nil {
				t.Fatal(err)
			}
			first := auditMessage("thread-alpha", "turn-alpha-1", "item-alpha-1", "fixture-answer-alpha")
			if err := a.observe(first); err != nil {
				t.Fatal(err)
			}
			if err := a.observe(auditMessage("thread-alpha", "turn-alpha-1", id, "fixture-answer-alpha")); err == nil {
				t.Fatal("duplicate or extra completion accepted")
			}
			if err := a.claim("thread-alpha", "turn-alpha-1", "item-alpha-1"); err == nil {
				t.Fatal("duplicate completion disappeared behind waiter claim")
			}
		})
	}
}

func TestAssistantAuditRejectsReusedItemAcrossThreads(t *testing.T) {
	var a assistantAudit
	auditOwners(t, &a)
	auditedTurn(t, &a, "alpha", 1, false)
	if err := a.begin("thread-beta", "beta"); err != nil {
		t.Fatal(err)
	}
	if err := a.bind("thread-beta", "turn-beta-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.observe(auditMessage("thread-beta", "turn-beta-1", "item-alpha-1", "fixture-answer-beta")); err == nil {
		t.Fatal("item ID reused across threads")
	}
}

func TestAssistantAuditBindsEarlyCompletionToActualTurn(t *testing.T) {
	var a assistantAudit
	auditOwners(t, &a)
	if err := a.begin("thread-alpha", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := a.observe(auditMessage("thread-alpha", "wrong-early-turn", "item-alpha-1", "fixture-answer-alpha")); err != nil {
		t.Fatal(err)
	}
	if err := a.bind("thread-alpha", "actual-turn"); err == nil {
		t.Fatal("early completion attached to a different actual turn")
	}
}

func TestAssistantAuditRejectsTwoEarlyCandidates(t *testing.T) {
	var a assistantAudit
	auditOwners(t, &a)
	if err := a.begin("thread-alpha", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := a.observe(auditMessage("thread-alpha", "early-one", "item-one", "fixture-answer-alpha")); err != nil {
		t.Fatal(err)
	}
	if err := a.observe(auditMessage("thread-alpha", "early-two", "item-two", "fixture-answer-alpha")); err == nil {
		t.Fatal("extra early candidate was buffered")
	}
}

func TestAssistantAuditRequiresObservedClaimedCompletion(t *testing.T) {
	var a assistantAudit
	auditOwners(t, &a)
	if err := a.begin("thread-alpha", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := a.bind("thread-alpha", "turn-alpha-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.claim("thread-alpha", "turn-alpha-1", "missing-item"); err == nil {
		t.Fatal("unobserved completion counted")
	}
	if n, err := a.finish(); err == nil || n != 0 {
		t.Fatal("incomplete run counted as four")
	}
}

func TestAssistantAuditRejectsDuplicateActualTurn(t *testing.T) {
	var a assistantAudit
	auditOwners(t, &a)
	auditedTurn(t, &a, "alpha", 1, false)
	if err := a.begin("thread-alpha", "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := a.bind("thread-alpha", "turn-alpha-1"); err == nil {
		t.Fatal("actual turn ID reused")
	}
}

func TestAssistantAuditConcurrentReplyAndCompletion(t *testing.T) {
	for i := 0; i < 20; i++ {
		var a assistantAudit
		auditOwners(t, &a)
		if err := a.begin("thread-alpha", "alpha"); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		done := make(chan error, 2)
		go func() { <-start; done <- a.bind("thread-alpha", "turn-alpha-1") }()
		go func() {
			<-start
			done <- a.observe(auditMessage("thread-alpha", "turn-alpha-1", "item-alpha-1", "fixture-answer-alpha"))
		}()
		close(start)
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		if err := a.claim("thread-alpha", "turn-alpha-1", "item-alpha-1"); err != nil {
			t.Fatal(err)
		}
	}
}

// Regression: no wait call consumes this late frame. Auditing must happen in
// read itself, including frames arriving after the four expected waiters.
func TestPeerReaderRejectsLateExtraWithoutWaiter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	p := &peer{output: r, ctx: ctx, cancel: cancel, messages: make(chan message, 64),
		fault: make(chan error, 1), readDone: make(chan struct{})}
	fourAuditedTurns(t, &p.assistants)
	go p.read()
	late := auditMessage("thread-alpha", "turn-alpha-2", "late-extra", "fixture-answer-alpha")
	if _, err := w.Write(append(jsonBytes(map[string]any{"method": late.Method, "params": late.Params}), '\n')); err != nil {
		t.Fatal(err)
	}
	w.Close()
	select {
	case <-p.readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not stop")
	}
	select {
	case err := <-p.fault:
		if err != failure("unexpected_item") {
			t.Fatal("late completion did not fail closed")
		}
	default:
		t.Fatal("late completion stayed silently buffered")
	}
	if len(p.messages) != 0 {
		t.Fatal("invalid completion reached waiter buffer")
	}
	if n, err := p.assistants.finish(); err == nil || n != 0 {
		t.Fatal("late extra left a successful audit")
	}
}
