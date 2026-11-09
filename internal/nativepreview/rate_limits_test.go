package nativepreview

import (
	"encoding/json"
	"testing"
)

func TestRateLimitsAreValidatedAndDiscarded(t *testing.T) {
	peer := nativePeer{version: "codex-cli 0.167.0-alpha.3.2", notification: func(string, json.RawMessage) error {
		t.Fatal("quota snapshot reached text or UI handler")
		return ErrUnavailable
	}}
	check := func(params string) error {
		return peer.acceptNotification(map[string]json.RawMessage{"method": json.RawMessage(`"account/rateLimits/updated"`), "params": json.RawMessage(params)})
	}
	for _, valid := range []string{
		`{"rateLimits":{}}`,
		`{"rateLimits":{"primary":null,"credits":null,"planType":null}}`,
		`{"rateLimits":{"limitId":"public","limitName":"Public quota","normalModelSlug":"test-alpha","primary":{"usedPercent":101,"windowDurationMins":5,"resetsAt":123},"secondary":{"usedPercent":0},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"individualLimit":{"limit":"10","used":"1","remainingPercent":90,"resetsAt":456},"spendControlReached":false,"planType":"unknown-future-plan","rateLimitReachedType":"rate_limit_reached"}}`,
	} {
		if check(valid) != nil {
			t.Fatal("valid sparse snapshot rejected")
		}
	}
	for _, invalid := range []string{
		`null`, `{}`, `{"rateLimits":null}`, `{"rateLimits":[]}`,
		`{"rateLimits":{},"account":"unknown"}`, `{"rateLimits":{"unknown":null}}`,
		`{"rateLimits":{"primary":{}}}`, `{"rateLimits":{"primary":{"usedPercent":null}}}`,
		`{"rateLimits":{"primary":{"usedPercent":1.5}}}`, `{"rateLimits":{"primary":{"usedPercent":2147483648}}}`,
		`{"rateLimits":{"primary":{"usedPercent":0,"resetsAt":"123"}}}`,
		`{"rateLimits":{"primary":{"usedPercent":0,"unknown":false}}}`,
		`{"rateLimits":{"credits":{"hasCredits":false}}}`, `{"rateLimits":{"credits":{"hasCredits":null,"unlimited":false}}}`,
		`{"rateLimits":{"individualLimit":{"limit":"1","used":"0","remainingPercent":100}}}`,
		`{"rateLimits":{"planType":true}}`, `{"rateLimits":{"spendControlReached":"false"}}`,
		`{"rateLimits":{"rateLimitReachedType":"unknown"}}`,
		`{"rateLimits":{},"rateLimits":{}}`,
	} {
		if check(invalid) != ErrUnavailable {
			t.Fatal("malformed quota snapshot accepted")
		}
	}
	peer.notification = nil
	if check(`{"rateLimits":{}}`) != nil {
		t.Fatal("discovery snapshot rejected")
	}
	peer.version = "codex-cli 0.166.0-alpha.6"
	if check(`{"rateLimits":{}}`) != ErrUnavailable {
		t.Fatal("unreviewed version accepted")
	}
}
