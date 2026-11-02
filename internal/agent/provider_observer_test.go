package agent

import (
	"context"
	"errors"
	"testing"
)

func TestProviderObserverTerminalCopyAndCancellation(t *testing.T) {
	var observer ProviderObserver
	calls := 0
	observer.Set(func(_ context.Context, _ Request, result *RunResult, _ error) {
		calls++
		if result != nil {
			result.Outcome = OutcomeFailed
			result.ResultText = "changed"
		}
	})
	original := &RunResult{Outcome: OutcomeSucceeded, ResultText: "original"}
	observer.Observe(t.Context(), Request{}, original, nil)
	if calls != 1 || original.Outcome != OutcomeSucceeded || original.ResultText != "original" {
		t.Fatal("observer mutated result")
	}
	observer.Observe(t.Context(), Request{}, nil, errors.New("not started"))
	observer.Observe(t.Context(), Request{}, &RunResult{Outcome: OutcomeTimedOut}, nil)
	observer.Observe(t.Context(), Request{}, original, context.Canceled)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	observer.Observe(ctx, Request{}, original, nil)
	if calls != 1 {
		t.Fatalf("unstarted/cancelled operations emitted: %d", calls)
	}
	observer.Observe(t.Context(), Request{}, nil, ErrConversationAuthentication)
	if calls != 2 {
		t.Fatal("typed terminal error not observed")
	}
}
