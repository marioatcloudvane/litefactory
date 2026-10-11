package turn

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// retainedTicketSession qualifies known history before any compatibility read.
// An unknown destination need not create a task just to publish a linked effect.
func retainedTicketSession(ctx context.Context, db *store.DB, l ticket.Locator) (int64, error) {
	parts := strings.Split(l.Container, "/")
	number, err := strconv.Atoi(l.Key)
	if l.Provider != ticket.GitHub || len(parts) != 2 || err != nil || number < 1 {
		return 0, store.ErrTicketIdentityConflict
	}
	session, err := db.GetSessionByTicketLocator(ctx, l)
	if errors.Is(err, store.ErrNotFound) {
		// Old unmapped rows can still be qualified additively. An existing
		// mapping to another API origin never becomes this fallback's owner.
		legacy, legacyErr := db.FindSessionByIssue(ctx, number, parts[0], parts[1])
		if legacyErr == nil {
			mapping, mappingErr := db.GetSessionTicket(ctx, legacy.ID)
			if mappingErr == nil && mapping.Locator.Origin != "" && mapping.Locator != l {
				return 0, nil
			}
			if mappingErr != nil && !errors.Is(mappingErr, store.ErrNotFound) {
				return 0, mappingErr
			}
			session, err = legacy, nil
		} else if !errors.Is(legacyErr, store.ErrNotFound) {
			return 0, legacyErr
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err := db.BindLegacySessionOrigin(ctx, session.ID, l.Origin); err != nil {
		return 0, err
	}
	return session.ID, nil
}
func bindKnownTicketSession(ctx context.Context, db *store.DB, s ticket.Snapshot) error {
	id, err := retainedTicketSession(ctx, db, s.Ref.Locator)
	if err != nil || id == 0 {
		return err
	}
	_, err = db.BindSessionTicket(ctx, id, s.Ref)
	return err
}
