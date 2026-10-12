package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

// detailsContent is the visible text of a /details reply; the raw text of the
// previous reply is delivered as raw-details.txt.
const detailsContent = "Raw details of your previous reply."

// detailsRefusal answers /details when no earlier raw text is stored.
const detailsRefusal = "No earlier reply details are available here."

// details returns the stored raw text of the newest delivered reply in the
// message channel. Only the owner's commands are admitted to that channel.
func (s *Service) details(ctx context.Context, m Message, _ time.Time) (string, error) {
	raw, err := s.db.ReadLatestReplyDetails(ctx, m.Transport, m.Channel)
	if errors.Is(err, store.ErrNotFound) {
		return assessed(ctx, detailsRefusal, presentation.Refusal)
	}
	if err != nil {
		return "", err
	}
	return assessed(ctx, raw, presentation.Success)
}
