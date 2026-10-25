package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// conversationDraftModel supplies generation through the already selected
// native session. It exposes neither factory readers nor execution authority.
type conversationDraftModel struct {
	invoke func(context.Context, string) ([]byte, error)
}

func (m conversationDraftModel) Answer(ctx context.Context, _ int64, prompt string) ([]byte, error) {
	return m.invoke(ctx, prompt)
}

// conversationDraft keeps the existing draft decoder, protection, saved preview
// identity and separate confirmation gate. The factory-rendered preview is the
// terminal reply and must not be rewritten by outcome narration.
func (s *Service) conversationDraft(ctx context.Context, c store.Conversation, _ ConversationBinding, t store.ConversationTurn, inv Invocation, at time.Time, invoke func(context.Context, string) ([]byte, error)) (string, error) {
	if s.drafter == nil || invoke == nil {
		return "Drafting is unavailable; no issue was created. Ask the factory operator to inspect drafting service availability using `litefactory logs --all` on the factory host.", nil
	}
	if inv.Command != "draft" || c.OwnerID != s.owner || !s.channels[c.ChannelRef] || c.Transport != s.transport {
		return "", errors.New("conversation draft authorization unavailable")
	}
	drafter := *s.drafter
	drafter.Model = conversationDraftModel{invoke: invoke}
	// Project selection was authorized by the proposal handler. Avoid introducing
	// a legacy chat-reply binding or history-based reconstruction of owner content.
	drafter.Bind = nil
	drafter.History = nil
	text, _, err := drafter.Reply(ctx, c.Transport, c.ChannelRef, "conversation:"+t.ID, inv.Arguments["project"], inv.Arguments["note"], at)
	return text, err
}
