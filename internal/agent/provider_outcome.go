package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ProviderClass is safe terminal evidence, never the provider's raw diagnostic.
type ProviderClass string

// Canonical provider classes are used by observation policy only.
const (
	ProviderUnknown        ProviderClass = "unknown"
	ProviderSuccess        ProviderClass = "success"
	ProviderAuthentication ProviderClass = "auth"
	ProviderQuota          ProviderClass = "quota"
	ProviderRefusal        ProviderClass = "refusal"
	ProviderHTTP5xx        ProviderClass = "http_5xx"
)

// ProviderFailure attaches typed evidence without changing the underlying error.
type ProviderFailure struct {
	Class ProviderClass
	Cause error
}

func (e *ProviderFailure) Error() string { return e.Cause.Error() }
func (e *ProviderFailure) Unwrap() error { return e.Cause }

// ProviderOutcome normalizes one completed operation. Cancellation is never
// failure or success evidence, and unknown results break consecutive counts.
func ProviderOutcome(result *RunResult, err error) ProviderClass {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ProviderUnknown
	}
	if result != nil && (result.Outcome == OutcomeAbandoned || result.Outcome == OutcomeTimedOut) {
		return ProviderUnknown
	}
	if errors.Is(err, ErrConversationAuthentication) {
		return ProviderAuthentication
	}
	if errors.Is(err, ErrConversationRefused) {
		return ProviderRefusal
	}
	var classified *ProviderFailure
	if errors.As(err, &classified) {
		return classified.Class
	}
	if result != nil {
		if result.Interruption != nil {
			return ProviderQuota
		}
		if result.Succeeded() && err == nil {
			return ProviderSuccess
		}
		if result.ProviderFailure != "" {
			return result.ProviderFailure
		}
	}
	return ProviderUnknown
}

// ProviderEventClass accepts only structured provider-owned terminal metadata.
// Generated text and diagnostic messages are deliberately not inspected.
func ProviderEventClass(line []byte) ProviderClass {
	var event struct {
		Type       string          `json:"type"`
		IsError    bool            `json:"is_error"`
		Code       string          `json:"code"`
		ErrorType  string          `json:"error_type"`
		Status     int             `json:"status_code"`
		HTTPStatus int             `json:"http_status"`
		Error      json.RawMessage `json:"error"`
	}
	if json.Unmarshal(line, &event) != nil {
		return ProviderUnknown
	}
	if event.Type != "turn.failed" && event.Type != "error" && (event.Type != "result" || !event.IsError) {
		return ProviderUnknown
	}
	var detail struct {
		Type, Code string
		Status     int `json:"status_code"`
		HTTPStatus int `json:"http_status"`
	}
	_ = json.Unmarshal(event.Error, &detail)
	for _, code := range []string{event.Code, event.ErrorType, detail.Code, detail.Type} {
		switch strings.ToLower(code) {
		case "authentication_error", "invalid_api_key", "auth_expired", "unauthorized", "token_expired":
			return ProviderAuthentication
		case "quota_exceeded", "insufficient_quota", "usage_limit_reached", "usage_limit_exceeded":
			return ProviderQuota
		case "safety_refusal", "content_filter", "refusal", "safety_filter":
			return ProviderRefusal
		}
	}
	for _, status := range []int{event.Status, event.HTTPStatus, detail.Status, detail.HTTPStatus} {
		if status == 401 {
			return ProviderAuthentication
		}
		if status >= 500 && status < 600 {
			return ProviderHTTP5xx
		}
	}
	return ProviderUnknown
}
