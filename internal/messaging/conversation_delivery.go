package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationDeliveryStep retries only the immutable stored head reply. It
// never invokes a model and never applies the ordinary request expiry window.
func (s *Service) ConversationDeliveryStep(ctx context.Context, at time.Time) (bool, error) {
	runtime := s.conversations.Load()
	if runtime == nil {
		return false, nil
	}
	r, err := s.db.ClaimConversationReply(ctx, s.transport, at)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c, err := s.db.ReadConversation(ctx, s.transport, r.ChannelRef)
	if err != nil {
		return true, err
	}
	b := runtime.bindings[r.ChannelRef]
	if !s.channels[r.ChannelRef] || c.OwnerID != s.owner || b.Provider == "" {
		return true, s.db.PauseConversationReply(ctx, r, "conversation disabled or authorization removed; saved reply retained", at)
	}
	if err = conversationAccess(ctx, runtime, r.ChannelRef, at); err != nil {
		if delay := accessRetry(err); delay > 0 {
			return true, s.db.RetryChatReply(ctx, r, at.Add(delay), at)
		}
		return true, s.db.PauseConversationReply(ctx, r, "conversation delivery authorization unavailable; saved reply retained", at)
	}
	// Protected delivery prevents the transport from truncating or replacing the
	// saved bytes. Oversized answers remain retained with a visible health reason.
	deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d := s.sender.Deliver(deliveryCtx, Target{Transport: r.Transport, MessageID: r.MessageID, Channel: r.ChannelRef, Protected: true}, r.ReplyText)
	switch {
	case d.Confirmed:
		return true, s.db.FinishConversationReply(ctx, r, d.PromptID, at)
	case d.Permanent:
		return true, s.db.PauseConversationReply(ctx, r, "Discord permanently refused the saved conversation reply; retained work is paused", at)
	default:
		delay := min(time.Second<<min(max(r.Attempts-1, 0), 10), 15*time.Minute)
		// Honor transport retry guidance in full; unlike ordinary requests, pending
		// conversations have no expiry deadline that can shorten Retry-After.
		delay = max(delay, d.RetryAfter)
		return true, s.db.RetryChatReply(ctx, r, at.Add(delay), at)
	}
}
