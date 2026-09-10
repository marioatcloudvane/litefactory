package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (e turnExecutor) ApplyOwnerException(ctx context.Context, c turn.OwnerExceptionControl) error {
	if e.opts.StoryProgress == nil {
		return fmt.Errorf("owner exception service unavailable")
	}
	return e.opts.StoryProgress.ApplyOwnerException(ctx, c)
}

// handleOwnerException follows the normal numeric filter admission, with an
// explicit second identity guard. No agent dispatch or implicit mode change.
func (d *Daemon) handleOwnerException(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	owner := d.filter.Identities().OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return fmt.Errorf("owner exception requires numeric owner identity")
	}
	applier, ok := d.turn.(interface {
		ApplyOwnerException(context.Context, turn.OwnerExceptionControl) error
	})
	if !ok {
		return d.leave(ctx, t, log, "owner exception service unavailable", fmt.Errorf("executor lacks owner exceptions"))
	}
	err := applier.ApplyOwnerException(ctx, turn.OwnerExceptionControl{Parent: t.ref.String(), CommentID: c.ID, ActorID: c.Author.ID, OwnerID: owner, Body: c.Body})
	if err != nil {
		return d.leave(ctx, t, log, "owner exception remains pending", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not consume owner exception", err)
}
