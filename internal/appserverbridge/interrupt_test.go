package appserverbridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Both native fixture backends keep a turn active until its exact interrupt
// arrives. No official backend or model service is involved in this test.
func TestRunInterruptPreservesOtherBackendAndAllowsFollowup(t *testing.T) {
	cfg := fixtureConfig(t)
	for i := range cfg.Backends {
		b := &cfg.Backends[i]
		b.Provider = b.ID + "_provider"
		b.Models = []json.RawMessage{fixtureModel(b.ID+"_model", true)}
		if err := os.WriteFile(filepath.Join(b.Home, "fixture-mode"), []byte("interruptions"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := startFixtureBridge(t, cfg)
	lines := make(chan []byte, 32)
	readerDone := make(chan struct{})
	stopReader := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(lines)
		scanner := bufio.NewScanner(r.output)
		for scanner.Scan() {
			select {
			case lines <- append([]byte(nil), scanner.Bytes()...):
			case <-stopReader:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stopReader)
		r.cancel()
		r.output.Close()
		select {
		case <-readerDone:
		case <-time.After(30 * time.Second):
			t.Error("interrupt fixture reader did not stop")
		}
	})
	notices := []object{}
	requestID := 0
	ask := func(method string, params object, errorCode int) object {
		t.Helper()
		requestID++
		id := raw(requestID)
		if _, err := fmt.Fprintln(r.input, string(raw(object{"id": id, "method": raw(method), "params": raw(params)}))); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatal("bridge ended during interrupt scenario")
				}
				m, err := parseObject(line)
				if err != nil {
					t.Fatal(err)
				}
				if exists(m, "method") {
					notices = append(notices, m)
					continue
				}
				if string(m["id"]) != string(id) {
					t.Fatalf("response ID mismatch: %s", line)
				}
				if errorCode != 0 {
					e, err := child(m, "error")
					if err != nil || string(e["code"]) != fmt.Sprint(errorCode) {
						t.Fatalf("expected error %d, got %s", errorCode, line)
					}
					return e
				}
				result, err := child(m, "result")
				if err != nil || exists(m, "error") {
					t.Fatalf("unexpected response: %s", line)
				}
				return result
			case <-time.After(30 * time.Second):
				t.Fatal("interrupt fixture response timed out")
			}
		}
	}
	ask("initialize", object{"clientInfo": raw(map[string]string{"name": "interrupt-fixture", "version": "1"})}, 0)
	if _, err := fmt.Fprintln(r.input, `{"method":"initialized"}`); err != nil {
		t.Fatal(err)
	}
	ask("model/list", object{}, 0)
	threads := make([]string, len(cfg.Backends))
	turns := make([]string, len(cfg.Backends))
	startTurn := func(i int, suffix string) string {
		t.Helper()
		input := []map[string]any{{"type": "text", "text": "private-" + cfg.Backends[i].ID + "-" + suffix, "text_elements": []any{}}}
		result := ask("turn/start", object{"threadId": raw(threads[i]), "input": raw(input)}, 0)
		turn, err := child(result, "turn")
		if err != nil || text(turn, "status") != "inProgress" {
			t.Fatalf("fixture turn did not start: %s", raw(result))
		}
		return text(turn, "id")
	}
	for i, b := range cfg.Backends {
		result := ask("thread/start", object{"model": raw(alias(b.ID, b.ID+"_model"))}, 0)
		thread, _ := child(result, "thread")
		threads[i] = text(thread, "id")
		turns[i] = startTurn(i, "initial")
	}
	// These must fail locally and must not disclose either invalid ID to a child.
	ask("turn/interrupt", object{"threadId": raw(threads[0]), "turnId": raw("never-seen-turn")}, -32602)
	ask("turn/interrupt", object{"threadId": raw(threads[0]), "turnId": raw(turns[1])}, -32602)
	ask("turn/interrupt", object{"threadId": raw(threads[0]), "turnId": raw(turns[0])}, 0)
	turns[0] = startTurn(0, "followup")
	// A fixture rejects another start while its old turn is active. The other
	// provider must still be active after the first provider was interrupted.
	ask("turn/start", object{"threadId": raw(threads[1]), "input": raw([]any{})}, -32001)
	ask("turn/interrupt", object{"threadId": raw(threads[1]), "turnId": raw(turns[1])}, 0)
	turns[1] = startTurn(1, "followup")
	for i := range cfg.Backends {
		ask("turn/interrupt", object{"threadId": raw(threads[i]), "turnId": raw(turns[i])}, 0)
	}
	r.input.Close()
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	for line := range lines {
		m, err := parseObject(line)
		if err != nil || !exists(m, "method") {
			t.Fatalf("unexpected trailing message: %s", line)
		}
		notices = append(notices, m)
	}
	completed := map[string]int{}
	deltas := map[string]int{}
	for _, notice := range notices {
		p, _ := child(notice, "params")
		switch text(notice, "method") {
		case "turn/completed":
			turn, _ := child(p, "turn")
			if text(turn, "status") != "interrupted" {
				t.Fatalf("wrong final turn status: %s", raw(notice))
			}
			completed[text(p, "threadId")+"/"+text(turn, "id")]++
		case "item/agentMessage/delta":
			deltas[text(p, "threadId")+"/"+text(p, "turnId")]++
		default:
			t.Fatalf("unexpected notification: %s", raw(notice))
		}
	}
	for i, b := range cfg.Backends {
		for n := 1; n <= 2; n++ {
			key := threads[i] + "/" + fmt.Sprintf("%s-turn-%d", b.Provider, n)
			if completed[key] != 1 || deltas[key] != 1 {
				t.Fatalf("missing or duplicate stream/stop event for %s: completed=%d deltas=%d", key, completed[key], deltas[key])
			}
		}
		data, err := os.ReadFile(filepath.Join(b.Home, "fixture-requests"))
		if err != nil {
			t.Fatal(err)
		}
		log := string(data)
		foreign := cfg.Backends[1-i]
		if strings.Contains(log, "never-seen-turn") || strings.Contains(log, foreign.Provider+"-turn") || strings.Contains(log, "private-"+foreign.ID) {
			t.Fatalf("interrupt or input crossed backend ownership: %s", log)
		}
		if strings.Count(log, `"method":"turn/interrupt"`) != 2 {
			t.Fatalf("backend %s received an incorrect interrupt count: %s", b.ID, log)
		}
	}
}
