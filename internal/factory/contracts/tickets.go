package contracts

import (
	"context"
	"github.com/cloudvane/litefactory/internal/ticket"
	"time"
)

// TicketDiscovery observes qualified ticket snapshots and ordered reply pages.
// Provider credentials and pagination mechanics remain in provider adapters.
type TicketDiscovery interface {
	Discover(context.Context, string, time.Time) ([]ticket.Snapshot, error)
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
}

// TicketCommentDiscovery identifies qualified tickets with changed replies.
type TicketCommentDiscovery interface {
	DiscoverCommented(context.Context, string, time.Time) ([]ticket.Locator, error)
}
