package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

type message struct {
	ID json.RawMessage `json:"id"`
	Method string `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error json.RawMessage `json:"error"`
}

type peer struct {
	tree *proc.Tree
	input, output *os.File
	ctx context.Context
	cancel context.CancelFunc
	messages chan message
	fault chan error
	readDone chan struct{}
	waitDone chan struct{}
	waitErr error
	saved []message
	next int
	unexpected atomic.Int32
	assistants assistantAudit
}

func startPeer(ctx context.Context, binary string, env []string, cwd string) (*peer, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := proc.Command(binary, "app-server")
	cmd.Dir, cmd.Env, cmd.Stderr = cwd, env, nil
	inR, inW, err := os.Pipe()
	if err != nil { cancel(); return nil, failure("process_start_failed") }
	outR, outW, err := os.Pipe()
	if err != nil { inR.Close(); inW.Close(); cancel(); return nil, failure("process_start_failed") }
	cmd.Stdin, cmd.Stdout = inR, outW
	tree, err := proc.StartTreeStrict(cmd)
	inR.Close()
	outW.Close()
	if err != nil {
		inW.Close(); outR.Close(); cancel()
		return nil, failure("process_start_failed")
	}
	p := &peer{tree: tree, input: inW, output: outR, ctx: ctx, cancel: cancel,
		messages: make(chan message, 64), fault: make(chan error, 1),
		readDone: make(chan struct{}), waitDone: make(chan struct{})}
	go p.read()
	go func() { p.waitErr = tree.Wait(); close(p.waitDone) }()
	go func() { select { case <-ctx.Done(): tree.Kill(); inW.Close(); outR.Close(); case <-p.waitDone: } }()
	return p, nil
}

func decodeMessage(line []byte) (message, error) {
	var m message
	var obj map[string]json.RawMessage
	if len(line) > maxMessage || json.Unmarshal(line, &obj) != nil || obj == nil {
		return m, failure("protocol_invalid")
	}
	if json.Unmarshal(line, &m) != nil || (len(m.ID) == 0 && m.Method == "") {
		return m, failure("protocol_invalid")
	}
	if len(m.ID) != 0 && m.Method != "" { return m, failure("unexpected_server_request") }
	if len(m.ID) == 0 && (len(m.Result) != 0 || len(m.Error) != 0) {
		return m, failure("protocol_invalid")
	}
	return m, nil
}

func (p *peer) fail(err error) {
	select { case p.fault <- err: default: }
	p.cancel()
}

func (p *peer) read() {
	defer close(p.readDone)
	scanner := bufio.NewScanner(p.output)
	scanner.Buffer(make([]byte, 64<<10), maxMessage+1)
	total, count := 0, 0
	for scanner.Scan() {
		total += len(scanner.Bytes())
		count++
		if total > 32<<20 || count > 2048 { p.fail(failure("protocol_limit")); return }
		m, err := decodeMessage(scanner.Bytes())
		if err != nil {
			if err == failure("unexpected_server_request") { p.unexpected.Add(1) }
			p.fail(err); return
		}
		if m.Method == "error" { p.fail(failure("native_error")); return }
		if err := checkNotification(m); err != nil { p.fail(err); return }
		if err := p.assistants.observe(m); err != nil { p.fail(err); return }
		select { case p.messages <- m: case <-p.ctx.Done(): return }
	}
	if scanner.Err() != nil { p.fail(failure("protocol_invalid")) }
}

func (p *peer) send(v any) error {
	data := append(jsonBytes(v), '\n')
	if len(data) > maxMessage { return failure("protocol_limit") }
	done := make(chan error, 1)
	go func() {
		n, err := p.input.Write(data)
		if err == nil && n != len(data) { err = io.ErrShortWrite }
		done <- err
	}()
	timer := time.NewTimer(5*time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil { return failure("protocol_write_failed") }
		return nil
	case <-timer.C:
	case <-p.ctx.Done():
	}
	p.input.Close()
	<-done
	return failure("deadline")
}

func (p *peer) wait(match func(message) bool) (message, error) {
	timer := time.NewTimer(45*time.Second)
	defer timer.Stop()
	for {
		select { case err := <-p.fault: return message{}, err; default: }
		for i, m := range p.saved {
			if match(m) { p.saved = append(p.saved[:i], p.saved[i+1:]...); return m, nil }
		}
		select {
		case err := <-p.fault: return message{}, err
		case m := <-p.messages:
			if err := checkNotification(m); err != nil { return message{}, err }
			if match(m) { return m, nil }
			p.saved = append(p.saved, m)
			if len(p.saved) > 512 { return message{}, failure("protocol_limit") }
		case <-p.readDone:
			// Read any final buffered frame before treating EOF as failure.
			select {
			case m := <-p.messages:
				if err := checkNotification(m); err != nil { return message{}, err }
				if match(m) { return m, nil }
				p.saved = append(p.saved, m)
			default:
				select { case err := <-p.fault: return message{}, err; default: }
				return message{}, failure("process_ended")
			}
		case <-p.ctx.Done():
			select { case err := <-p.fault: return message{}, err; default: }
			return message{}, failure("deadline")
		case <-timer.C: return message{}, failure("deadline")
		}
	}
}

func checkNotification(m message) error {
	if m.Method != "item/started" && m.Method != "item/completed" { return nil }
	p, err := object(m.Params)
	if err != nil { return err }
	item, err := object(p["item"])
	if err != nil { return err }
	switch stringField(item, "type") {
	case "agentMessage", "userMessage", "reasoning": return nil
	default: return failure("unexpected_item")
	}
}

func (p *peer) rpc(method string, params any) (json.RawMessage, error) {
	reply, err := p.request(method, params)
	if err != nil { return nil, err }
	if len(reply.Error) != 0 { return nil, failure("native_error") }
	if len(reply.Result) == 0 { return nil, failure("protocol_invalid") }
	return reply.Result, nil
}

func (p *peer) request(method string, params any) (message, error) {
	p.next++
	id := jsonBytes(p.next)
	if err := p.send(map[string]any{"id":p.next, "method":method, "params":params}); err != nil { return message{}, err }
	return p.wait(func(m message) bool { return m.Method == "" && bytes.Equal(m.ID, id) })
}

func (p *peer) close(normal bool) error {
	p.input.Close()
	timer := time.NewTimer(10*time.Second)
	defer timer.Stop()
	if normal {
		select {
		case <-p.waitDone:
		case <-timer.C: normal = false
		case <-p.ctx.Done(): normal = false
		}
	}
	if !normal { p.cancel(); p.tree.Kill() }
	select {
	case <-p.waitDone:
	case <-time.After(5*time.Second):
		p.cancel(); p.output.Close()
		<-p.readDone
		return failure("cleanup_failed")
	}
	select {
	case <-p.readDone:
	case <-time.After(2*time.Second):
		normal = false
		p.cancel()
		p.output.Close()
		<-p.readDone
	}
	p.output.Close()
	p.cancel()
	if !normal || p.waitErr != nil { return failure("shutdown_failed") }
	select { case err := <-p.fault: return err; default: }
	return nil
}

type limitedOutput struct { data []byte }
func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 1024 { return 0, failure("version_invalid") }
	b.data = append(b.data, p...)
	return len(p), nil
}

func verifyVersion(ctx context.Context, binary string, env []string, cwd string) error {
	cmd := proc.Command(binary, "--version")
	cmd.Dir, cmd.Env, cmd.Stderr = cwd, env, nil
	var output limitedOutput
	cmd.Stdout = &output
	cmd.WaitDelay = 2*time.Second
	tree, err := proc.StartTreeStrict(cmd)
	if err != nil { return failure("process_start_failed") }
	done := make(chan error, 1)
	go func() { done <- tree.Wait() }()
	select {
	case err := <-done:
		if err != nil || string(bytes.TrimSpace(output.data)) != nativeVersion { return failure("version_invalid") }
		return nil
	case <-ctx.Done():
	case <-time.After(10*time.Second):
	}
	tree.Kill()
	select { case <-done: case <-time.After(5*time.Second): return failure("cleanup_failed") }
	return failure("deadline")
}

func object(data json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil { return nil, failure("protocol_invalid") }
	return obj, nil
}
func stringField(obj map[string]json.RawMessage, field string) string {
	var value string
	_ = json.Unmarshal(obj[field], &value)
	return value
}
