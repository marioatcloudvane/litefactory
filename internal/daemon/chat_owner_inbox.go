package daemon

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/retainedinbox"
)

// ownerInboxReader is a composition bridge to the core-owned retained reader.
type ownerInboxReader struct{ daemon *Daemon }

func (r ownerInboxReader) ReadOwnerInboxTicket(ctx context.Context, decision contracts.InboxDecision) (contracts.OwnerInboxTicketDetail, error) {
	return retainedinbox.New(r.daemon.db, r.daemon.connections).ReadOwnerInboxTicket(ctx, decision)
}
