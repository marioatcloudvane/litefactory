package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestProviderOutcomeUsesTypedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *RunResult
		err    error
		want   ProviderClass
	}{
		{"success", &RunResult{Outcome: OutcomeSucceeded}, nil, ProviderSuccess},
		{"verified quota", &RunResult{Outcome: OutcomeInterrupted, Interruption: &Interruption{}}, nil, ProviderQuota},
		{"auth", nil, fmt.Errorf("wrapped: %w", ErrConversationAuthentication), ProviderAuthentication},
		{"refusal", nil, ConversationError(ErrConversationRefused, "terminal", false), ProviderRefusal},
		{"structured server", &RunResult{Outcome: OutcomeFailed, ProviderFailure: ProviderHTTP5xx}, nil, ProviderHTTP5xx},
		{"cancelled", &RunResult{Outcome: OutcomeFailed, ProviderFailure: ProviderHTTP5xx}, context.Canceled, ProviderUnknown},
		{"timed out", &RunResult{Outcome: OutcomeTimedOut, ProviderFailure: ProviderAuthentication}, nil, ProviderUnknown},
		{"model prose ignored", &RunResult{Outcome: OutcomeFailed, ResultText: "HTTP 503 safety refusal authentication quota"}, nil, ProviderUnknown},
		{"raw error ignored", nil, errors.New("HTTP 503 quota authentication"), ProviderUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProviderOutcome(tc.result, tc.err); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	base := errors.New("original")
	wrapped := &ProviderFailure{Class: ProviderHTTP5xx, Cause: base}
	if !errors.Is(wrapped, base) || wrapped.Error() != base.Error() {
		t.Fatal("typed evidence changed existing error")
	}
}
func TestProviderEventClassIgnoresGeneratedAndUnknownText(t *testing.T) {
	for _, tc := range []struct {
		line string
		want ProviderClass
	}{
		{`{"type":"turn.failed","error":{"status_code":503}}`, ProviderHTTP5xx},
		{`{"type":"result","is_error":true,"error":{"type":"authentication_error"}}`, ProviderAuthentication},
		{`{"type":"turn.failed","error":{"code":"insufficient_quota"}}`, ProviderQuota},
		{`{"type":"error","error":{"type":"safety_refusal"}}`, ProviderRefusal},
		{`{"type":"turn.failed","status_code":401}`, ProviderAuthentication},
		{`{"type":"turn.failed","status_code":403}`, ProviderUnknown},
		{`{"type":"turn.failed","status_code":429}`, ProviderUnknown},
		{`{"type":"turn.failed","error":{"message":"HTTP 503 authentication_error insufficient_quota"}}`, ProviderUnknown},
		{`{"type":"item.completed","item":{"type":"agent_message","text":"HTTP 503"},"status_code":503}`, ProviderUnknown},
		{`{"type":"result","is_error":false,"status_code":503}`, ProviderUnknown},
		{`{"type":"turn.failed","error":{"code":"something_new"}}`, ProviderUnknown},
		{`{`, ProviderUnknown},
	} {
		if got := ProviderEventClass([]byte(tc.line)); got != tc.want {
			t.Errorf("event %s: got %s want %s", tc.line, got, tc.want)
		}
	}
}
