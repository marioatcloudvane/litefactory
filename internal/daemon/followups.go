package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (e turnExecutor) ApplyFollowup(ctx context.Context, c turn.FollowupControl) error {
	if e.opts.StoryProgress == nil {
		return fmt.Errorf("follow-up service unavailable")
	}
	return e.opts.StoryProgress.ApplyFollowup(ctx, c)
}

// handleFollowup follows the normal numeric filter admission, with an
// explicit second identity guard. No agent dispatch or implicit mode change.
func (d *Daemon) handleFollowup(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	owner := d.ticketIdentities(ctx).OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return fmt.Errorf("follow-up requires numeric owner identity")
	}
	applier, ok := d.turn.(interface {
		ApplyFollowup(context.Context, turn.FollowupControl) error
	})
	if !ok {
		return d.leave(ctx, t, log, "follow-up service unavailable", fmt.Errorf("executor lacks follow-ups"))
	}
	err := applier.ApplyFollowup(ctx, turn.FollowupControl{Parent: t.ref.String(), CommentID: c.ID, ActorID: c.Author.ID, OwnerID: owner, Body: c.Body})
	if err != nil {
		return d.leave(ctx, t, log, "follow-up remains pending", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not consume follow-up", err)
}
