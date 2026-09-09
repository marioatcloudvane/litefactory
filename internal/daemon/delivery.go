package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

// ApplyDelivery routes explicit owner controls without starting an agent.
func (e turnExecutor) ApplyDelivery(ctx context.Context, c turn.DeliveryControl) error {
	if e.opts.StoryProgress == nil {
		return fmt.Errorf("delivery service unavailable")
	}
	return e.opts.StoryProgress.ApplyDelivery(ctx, c)
}
func (d *Daemon) handleDelivery(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	owner := d.filter.Identities().OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return fmt.Errorf("delivery requires numeric owner identity")
	}
	applier, ok := d.turn.(interface {
		ApplyDelivery(context.Context, turn.DeliveryControl) error
	})
	if !ok {
		return d.leave(ctx, t, log, "delivery service unavailable", fmt.Errorf("executor lacks delivery controls"))
	}
	err := applier.ApplyDelivery(ctx, turn.DeliveryControl{Parent: t.ref.String(), CommentID: c.ID, ActorID: c.Author.ID, OwnerID: owner, Body: c.Body})
	if err != nil {
		return d.leave(ctx, t, log, "delivery remains pending", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not consume delivery control", err)
}
