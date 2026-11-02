package messaging

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestConversationProviderObserverTerminalClasses(t *testing.T) {
	service := &Service{}
	var classes []string
	service.SetProviderObserver(func(_ context.Context, provider, conversation, turn, class string) {
		if provider != "codex" || conversation != "conversation" || turn != "turn" {
			t.Error("lost source identity")
		}
		classes = append(classes, class)
	})
	invoke := func(ctx context.Context, result agent.ConversationResult, err error) {
		service.observeConversationProvider(ctx, "codex", "conversation", "turn", result, err)
	}
	invoke(t.Context(), agent.ConversationResult{Answer: []byte("done"), Session: agent.SessionRef{Resume: "session"}}, nil)
	invoke(t.Context(), agent.ConversationResult{}, agent.ConversationError(agent.ErrConversationAuthentication, "terminal", false))
	invoke(t.Context(), agent.ConversationResult{}, agent.ConversationError(agent.ErrConversationRefused, "terminal", false))
	invoke(t.Context(), agent.ConversationResult{}, agent.ConversationError(agent.ErrConversationBusy, "preflight", true))
	invoke(t.Context(), agent.ConversationResult{}, context.Canceled)
	if len(classes) != 3 || classes[0] != "success" || classes[1] != "auth" || classes[2] != "refusal" {
		t.Fatalf("observed %v", classes)
	}
}
