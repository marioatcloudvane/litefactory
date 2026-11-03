package daemon

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"time"
)

// RecoverOwnerActionFeature delegates reconciliation to the factory owner.
func (c *ChatIssueCreator) RecoverOwnerActionFeature(ctx context.Context, transport, channel, code string, at time.Time) (string, bool, error) {
	if c == nil {
		return "", false, errors.New("feature recovery unavailable")
	}
	service := featuredraft.Creator{DB: c.DB, Forge: c.Forge, BotUserID: c.BotUserID, Redactor: c.Redactor, Now: c.Now}
	return service.RecoverOwnerActionFeature(ctx, transport, channel, code, at)
}
