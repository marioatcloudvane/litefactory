package messaging

import (
	"context"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

// ReplyTransport delivers final text and raw details in one message operation.
type ReplyTransport interface {
	DeliverReply(context.Context, Target, presentation.Payload) Delivery
}

// assessed preserves the string handler contract while declaring source status.
func assessed(ctx context.Context, text string, status presentation.Status) (string, error) {
	presentation.Assess(ctx, status)
	return text, nil
}
