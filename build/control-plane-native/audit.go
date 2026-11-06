package main

import (
	"encoding/json"
	"sync"
)

// Audit at the reader boundary, not just when a waiter consumes a notification.
// A successful run accounts for every completion through normal process EOF.
type assistantAudit struct {
	mu sync.Mutex
	owners map[string]string
	expected []assistantTurnKey
	observed []assistantCompletion
	active *assistantPendingTurn
	err error
}
type assistantTurnKey struct { thread, turn string }
type assistantPendingTurn struct { thread, turn, route string }
type assistantCompletion struct {
	key assistantTurnKey
	item string
	claimed bool
}

func auditID(id string) bool { return len(id) > 0 && len(id) <= 256 }

func (a *assistantAudit) reject(err error) error {
	if a.err == nil { a.err = err }
	return a.err
}

func (a *assistantAudit) owner(thread, route string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return a.err }
	if !auditID(thread) || (route != "alpha" && route != "beta") || len(a.owners) >= 2 {
		return a.reject(failure("unexpected_item"))
	}
	if a.owners == nil { a.owners = make(map[string]string, 2) }
	if _, exists := a.owners[thread]; exists { return a.reject(failure("unexpected_item")) }
	for _, previous := range a.owners {
		if previous == route { return a.reject(failure("unexpected_item")) }
	}
	a.owners[thread] = route
	return nil
}

func (a *assistantAudit) begin(thread, route string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return a.err }
	if a.active != nil || (route != "alpha" && route != "beta") || a.owners[thread] != route || len(a.expected) >= 4 {
		return a.reject(failure("unexpected_item"))
	}
	count := 0
	for _, key := range a.expected { if key.thread == thread { count++ } }
	if count >= 2 { return a.reject(failure("unexpected_item")) }
	a.active = &assistantPendingTurn{thread:thread, route:route}
	return nil
}

// Notifications may precede the turn/start reply. Bind that early observation
// to the actual returned turn ID instead of assuming an ordering.
func (a *assistantAudit) bind(thread, turn string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return a.err }
	if a.active == nil || a.active.thread != thread || a.active.turn != "" || !auditID(turn) {
		return a.reject(failure("unexpected_item"))
	}
	key := assistantTurnKey{thread:thread, turn:turn}
	for _, previous := range a.expected {
		if previous == key { return a.reject(failure("unexpected_item")) }
	}
	for _, completion := range a.observed {
		if !completion.claimed && completion.key != key { return a.reject(failure("unexpected_item")) }
	}
	a.active.turn = turn
	a.expected = append(a.expected, key)
	return nil
}

func (a *assistantAudit) observe(m message) error {
	if m.Method != "item/completed" { return nil }
	params, err := object(m.Params)
	if err != nil { return err }
	item, err := object(params["item"])
	if err != nil { return err }
	if stringField(item,"type") != "agentMessage" { return nil }
	thread, turn, id := stringField(params,"threadId"), stringField(params,"turnId"), stringField(item,"id")
	var text string
	if json.Unmarshal(item["text"], &text) != nil { return failure("unexpected_item") }
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return a.err }
	if a.active == nil || a.active.thread != thread || !auditID(thread) || !auditID(turn) ||
		!auditID(id) || len(a.observed) >= 4 ||
		(a.active.turn != "" && a.active.turn != turn) {
		return a.reject(failure("unexpected_item"))
	}
	if text != "fixture-answer-"+a.active.route {
		return a.reject(failure("answer_crossed_route"))
	}
	key := assistantTurnKey{thread:thread, turn:turn}
	for _, previous := range a.observed {
		if previous.key == key || previous.item == id || (!previous.claimed && previous.key.thread == thread) {
			return a.reject(failure("unexpected_item"))
		}
	}
	a.observed = append(a.observed, assistantCompletion{key:key, item:id})
	return nil
}

func (a *assistantAudit) claim(thread, turn, item string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return a.err }
	if a.active == nil || a.active.thread != thread || a.active.turn != turn {
		return a.reject(failure("unexpected_item"))
	}
	key := assistantTurnKey{thread:thread, turn:turn}
	for i := range a.observed {
		completion := &a.observed[i]
		if completion.key == key && completion.item == item && !completion.claimed {
			completion.claimed = true
			a.active = nil
			return nil
		}
	}
	return a.reject(failure("unexpected_item"))
}

func (a *assistantAudit) finish() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil { return 0, a.err }
	if a.active != nil || len(a.owners) != 2 || len(a.expected) != 4 || len(a.observed) != 4 {
		return 0, a.reject(failure("unexpected_item"))
	}
	for _, completion := range a.observed {
		if !completion.claimed { return 0, a.reject(failure("unexpected_item")) }
	}
	for thread := range a.owners {
		count := 0
		for _, key := range a.expected { if key.thread == thread { count++ } }
		if count != 2 { return 0, a.reject(failure("unexpected_item")) }
	}
	return len(a.observed), nil
}
