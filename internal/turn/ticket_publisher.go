package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/store"
)

func retainedTicketPublisher(ctx context.Context, db *store.DB, sessionID, fallback int64) (int64, error) {
	if sessionID == 0 {
		return fallback, nil
	}
	snapshot, err := db.GetExecutionSnapshot(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	return snapshot.ActorID, nil
}

func (p *StoryProgress) ticketPublisherID(ctx context.Context, parent string) (int64, error) {
	session, err := p.db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return p.botID, nil
	}
	if err != nil {
		return 0, err
	}
	return retainedTicketPublisher(ctx, p.db, session.ID, p.botID)
}
