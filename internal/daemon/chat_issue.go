package daemon

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// ChatIssueForge retains the primitive forge interface for trusted composition.
type ChatIssueForge = featuredraft.IssueForge

// ChatIssueCreator adapts the factory's bounded result to existing chat presentation.
// Persistence is supplied only by daemon composition and never reaches the consumer.
type ChatIssueCreator struct {
	DB        *store.DB
	Forge     ChatIssueForge
	BotUserID int64
	Redactor  *redact.Redactor
	Now       func() time.Time
}

// Confirm presents the core-owned confirmation outcome without reinterpreting policy.
func (c *ChatIssueCreator) Confirm(ctx context.Context, transport, channel, message, code string, at time.Time) (string, error) {
	service := featuredraft.Creator{DB: c.DB, Forge: c.Forge, BotUserID: c.BotUserID, Redactor: c.Redactor, Now: c.Now}
	result, err := service.Confirm(ctx, transport, channel, message, code, at)
	presentation.Assess(ctx, presentation.Status(result.Status))
	return result.Text, err
}
