package nativepreview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// One request at a time. The only asynchronous surface is a bounded,
// operation-owned notification validator; native server requests never pass.
type nativePeer struct {
	stage                 func(string)
	ctx                   context.Context
	version               string
	input                 io.WriteCloser
	events                <-chan peerEvent
	notification          func(string, json.RawMessage) error
	eventCount, byteCount int
}

func nativeArguments(spec peerSpec, overrides map[string]string) []string {
	args := []string{"app-server", "-c", `model_provider="openai"`}
	if spec.provider == "aihub" {
		args = []string{"app-server", "-c", `model_provider="magpie_preview_aihub"`, "-c",
			`model_providers.magpie_preview_aihub={name="AIHub preview",base_url=` + strconv.Quote(spec.endpoint) + `,env_key="` + keyName + `",wire_api="responses",requires_openai_auth=false}`}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-c", key+"="+overrides[key])
	}
	return args
}

func withNativePeer(parent context.Context, spec peerSpec, timeout time.Duration, overrides map[string]string, experimental bool, operation func(*nativePeer) error) (resultErr error) {
	spec.markStage("admission")
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if _, present := os.LookupEnv("CODEX_WINDOWS_REGISTERED_CORE"); present {
		return ErrUnavailable
	}
	if !safeStateParent(spec.home) || !safeStateParent(spec.codexHome) {
		return ErrUnavailable
	}
	digest, err := executableDigest(spec.command)
	if err != nil || digest != spec.digest || ctx.Err() != nil {
		return ErrUnavailable
	}
	spec.markStage("version")
	version, valid := nativeVersionReport(ctx, spec)
	if !valid {
		return ErrUnavailable
	}
	spec.markStage("start")
	command := proc.CommandContext(context.WithoutCancel(ctx), spec.command, nativeArguments(spec, overrides)...)
	command.Env, command.Dir = nativeEnvironment(spec), spec.home
	input, err := command.StdinPipe()
	if err != nil {
		return ErrUnavailable
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return ErrUnavailable
	}
	if ctx.Err() != nil {
		input.Close()
		output.Close()
		return ErrUnavailable
	}
	tree, err := proc.StartTreeStrict(command)
	if err != nil {
		input.Close()
		output.Close()
		return ErrUnavailable
	}
	events := make(chan peerEvent, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), maxLine+1)
		send := func(event peerEvent) bool {
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for scanner.Scan() {
			if len(scanner.Bytes()) > maxLine {
				send(peerEvent{err: ErrUnavailable})
				return
			}
			if !send(peerEvent{data: bytes.Clone(scanner.Bytes())}) {
				return
			}
		}
		end := io.EOF
		if scanner.Err() != nil {
			end = ErrUnavailable
		}
		send(peerEvent{err: end})
	}()
	defer func() {
		cancel()
		input.Close()
		waited := make(chan error, 1)
		go func() { waited <- tree.Wait() }()
		select {
		case err := <-waited:
			if err != nil {
				if resultErr == nil {
					spec.markStage("cleanup")
				}
				resultErr = ErrUnavailable
			}
		case <-time.After(500 * time.Millisecond):
			if resultErr == nil {
				spec.markStage("cleanup")
			}
			<-tree.Stop(time.Second)
			<-waited
			resultErr = ErrUnavailable
		}
		output.Close()
		<-readerDone
	}()
	peer := &nativePeer{ctx: ctx, version: version, input: input, events: events, stage: spec.stage}
	capabilities := map[string]any{}
	if experimental {
		capabilities["experimentalApi"] = true
	}
	spec.markStage("initialize")
	init, err := peer.rpc("preview-init", "initialize", map[string]any{"clientInfo": map[string]string{"name": "magpie_native_preview", "version": "1"}, "capabilities": capabilities})
	if err != nil {
		return ErrUnavailable
	}
	spec.markStage("initialize-receipt")
	var initialized map[string]json.RawMessage
	if json.Unmarshal(init, &initialized) != nil {
		return ErrUnavailable
	}
	var agent string
	if json.Unmarshal(initialized["userAgent"], &agent) != nil || agent == "" {
		return ErrUnavailable
	}
	if writePeer(ctx, input, []byte(`{"method":"initialized"}`)) != nil {
		return ErrUnavailable
	}
	if operation(peer) != nil {
		return ErrUnavailable
	}
	spec.markStage("close")
	input.Close()
	for {
		envelope, err := peer.next()
		if err == io.EOF {
			return nil
		}
		if err != nil || peer.acceptNotification(envelope) != nil {
			return ErrUnavailable
		}
	}
}

func (p *nativePeer) next() (map[string]json.RawMessage, error) {
	select {
	case <-p.ctx.Done():
		return nil, ErrUnavailable
	case event := <-p.events:
		if event.err != nil {
			return nil, event.err
		}
		p.eventCount++
		p.byteCount += len(event.data)
		if p.eventCount > 4096 || p.byteCount > 16<<20 || !strictJSON(event.data) {
			return nil, ErrUnavailable
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(event.data, &envelope) != nil || envelope == nil {
			return nil, ErrUnavailable
		}
		return envelope, nil
	}
}
func (p *nativePeer) acceptNotification(envelope map[string]json.RawMessage) error {
	if p.notification == nil || len(envelope) != 2 || envelope["method"] == nil || envelope["params"] == nil {
		return ErrUnavailable
	}
	var method string
	if json.Unmarshal(envelope["method"], &method) != nil {
		return ErrUnavailable
	}
	err := p.notification(method, envelope["params"])
	if err != nil && p.stage != nil {
		label := "notification-unsupported"
		switch method {
		case "thread/started", "thread/settings/updated", "thread/status/changed", "thread/tokenUsage/updated", "turn/started", "turn/completed", "item/started", "item/completed":
			label = "notification-" + strings.ReplaceAll(method, "/", "-")
		}
		p.stage(label)
	}
	return err
}
func (p *nativePeer) rpc(id, method string, params any) (json.RawMessage, error) {
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil || writePeer(p.ctx, p.input, request) != nil {
		return nil, ErrUnavailable
	}
	for {
		envelope, err := p.next()
		if err != nil {
			return nil, ErrUnavailable
		}
		if envelope["method"] != nil {
			if p.acceptNotification(envelope) != nil {
				return nil, ErrUnavailable
			}
			continue
		}
		if len(envelope) != 2 || envelope["result"] == nil {
			return nil, ErrUnavailable
		}
		var gotID string
		if json.Unmarshal(envelope["id"], &gotID) != nil || gotID != id {
			return nil, ErrUnavailable
		}
		return envelope["result"], nil
	}
}
