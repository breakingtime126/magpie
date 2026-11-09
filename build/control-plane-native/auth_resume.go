package main

import (
	"encoding/json"
	"strings"
)

// The same genuine initialization contract is used after the one deliberate
// process restart; no cached initialization or account state is synthesized.
func initializeNativePeer(p *peer, controlHome string) error {
	initialized, err := p.rpc("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "magpie_synthetic_ci", "title": "Synthetic isolated CI", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err != nil {
		return err
	}
	initResult, err := object(initialized)
	if err != nil || stringField(initResult, "codexHome") != controlHome {
		return failure("control_home_mismatch")
	}
	if err := emptyDirectory(controlHome); err != nil {
		return err
	}
	if err := p.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	return nil
}

func resumeSyntheticThread(p configReader, thread, name string) error {
	if !auditID(thread) || (name != "alpha" && name != "beta") {
		return failure("ownership_changed")
	}
	selected := "magpie/" + name + "/test-" + name
	raw, err := p.rpc("thread/resume", map[string]any{
		"threadId": thread, "modelProvider": "magpie_bridge", "model": selected,
	})
	if err != nil {
		return err
	}
	obj, err := object(raw)
	if err != nil || stringField(obj, "modelProvider") != "magpie_bridge" || stringField(obj, "model") != selected {
		return failure("ownership_changed")
	}
	returned, err := object(obj["thread"])
	if err != nil || stringField(returned, "id") != thread ||
		stringField(returned, "modelProvider") != "magpie_bridge" {
		return failure("ownership_changed")
	}
	return nil
}

// Native Responses input uses message role/content items (the pinned public
// protocol/models.rs ResponseItem::Message and ContentItem variants). Require
// ordered role-bearing history, not strings hidden in unrelated JSON fields.
func syntheticResumeHistory(raw json.RawMessage, name string) bool {
	var items []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	step := 0
	roles := [3]string{"user", "assistant", "user"}
	types := [3]string{"input_text", "output_text", "input_text"}
	texts := [3]string{"synthetic-" + name + "-turn-1", "fixture-answer-" + name, "synthetic-" + name + "-turn-2"}
	for _, item := range items {
		if item.Type != "message" || item.Role != roles[step] {
			continue
		}
		for _, content := range item.Content {
			if content.Type == types[step] && strings.Contains(content.Text, texts[step]) {
				step++
				if step == len(roles) {
					return true
				}
				break
			}
		}
	}
	return false
}
