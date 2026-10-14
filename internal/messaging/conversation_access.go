package messaging

import (
	"context"
	"errors"
	"time"
)

// ConversationAccessError distinguishes temporary transport checks from lost
// authorization. A temporary check cannot permanently pause a conversation.
type ConversationAccessError struct {
	Permanent  bool
	RetryAfter time.Duration
}

func (e *ConversationAccessError) Error() string { return "conversation access check unavailable" }

func conversationAccess(ctx context.Context, r *conversationRuntime, channel string, at time.Time) error {
	r.accessMu.Lock()
	until := r.accessAfter[channel]
	r.accessMu.Unlock()
	if at.Before(until) {
		return &ConversationAccessError{RetryAfter: until.Sub(at)}
	}
	err := r.check(ctx, channel)
	if err == nil {
		return nil
	}
	var access *ConversationAccessError
	if !errors.As(err, &access) {
		return &ConversationAccessError{Permanent: true}
	}
	if access.Permanent {
		return access
	}
	delay := max(5*time.Second, access.RetryAfter)
	r.accessMu.Lock()
	if until := at.Add(delay); until.After(r.accessAfter[channel]) {
		r.accessAfter[channel] = until
	}
	r.accessMu.Unlock()
	return &ConversationAccessError{RetryAfter: delay}
}
func accessRetry(err error) time.Duration {
	var access *ConversationAccessError
	if errors.As(err, &access) && !access.Permanent {
		return max(time.Second, access.RetryAfter)
	}
	return 0
}
