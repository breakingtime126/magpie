package nativepreview

import (
	"bytes"
	"encoding/json"
)

// These c407 wire types are validated only to discard an informational snapshot.
// No quota, identity, entitlement, or routing state is derived from them.
type previewRateWindow struct {
	UsedPercent        *int32 `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type previewCredits struct {
	HasCredits *bool   `json:"hasCredits"`
	Unlimited  *bool   `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type previewSpendLimit struct {
	Limit            *string `json:"limit"`
	Used             *string `json:"used"`
	RemainingPercent *int32  `json:"remainingPercent"`
	ResetsAt         *int64  `json:"resetsAt"`
}

type previewRateSnapshot struct {
	LimitID              *string            `json:"limitId"`
	LimitName            *string            `json:"limitName"`
	NormalModelSlug      *string            `json:"normalModelSlug"`
	Primary              *previewRateWindow `json:"primary"`
	Secondary            *previewRateWindow `json:"secondary"`
	Credits              *previewCredits    `json:"credits"`
	IndividualLimit      *previewSpendLimit `json:"individualLimit"`
	SpendControlReached  *bool              `json:"spendControlReached"`
	PlanType             *string            `json:"planType"`
	RateLimitReachedType *string            `json:"rateLimitReachedType"`
}

func (p *nativePeer) acceptRateLimits(params json.RawMessage) error {
	if p.version != "codex-cli 0.167.0-alpha.3.2" || !strictJSON(params) {
		return ErrUnavailable
	}
	var notification struct {
		RateLimits *previewRateSnapshot `json:"rateLimits"`
	}
	decoder := json.NewDecoder(bytes.NewReader(params))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&notification) != nil || notification.RateLimits == nil {
		return ErrUnavailable
	}
	rates := notification.RateLimits
	for _, window := range []*previewRateWindow{rates.Primary, rates.Secondary} {
		if window != nil && window.UsedPercent == nil {
			return ErrUnavailable
		}
	}
	if credits := rates.Credits; credits != nil && (credits.HasCredits == nil || credits.Unlimited == nil) {
		return ErrUnavailable
	}
	if limit := rates.IndividualLimit; limit != nil && (limit.Limit == nil || limit.Used == nil || limit.RemainingPercent == nil || limit.ResetsAt == nil) {
		return ErrUnavailable
	}
	if rates.RateLimitReachedType != nil {
		switch *rates.RateLimitReachedType {
		case "rate_limit_reached", "workspace_owner_credits_depleted", "workspace_member_credits_depleted", "workspace_owner_usage_limit_reached", "workspace_member_usage_limit_reached":
		default:
			return ErrUnavailable
		}
	}
	// PlanType uses serde(other): unknown strings are valid unavailable metadata.
	// Percentages are native i32, not a client-invented 0..100 constraint.
	return nil
}
