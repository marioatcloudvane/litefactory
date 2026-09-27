package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// handleBugAnalysisApproval consumes the authenticated exact revision command
// mechanically. It invokes neither an analyst nor mode selection, so an eighth
// pass review remains approvable while the full parent package is unavailable.
func (d *Daemon) handleBugAnalysisApproval(ctx context.Context, t *task, c github.Comment, command turn.Command, log *slog.Logger) error {
	admission, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && admission.Workflow != "bug") {
		return d.reply(ctx, t, c, turn.Command{Kind: turn.KindUnknown, Name: command.Name, Tagged: command.Tagged}, log)
	}
	if err != nil {
		return d.leave(ctx, t, log, "Bug approval admission unavailable", err)
	}
	owner := d.filter.Identities().OwnerUserID
	if owner <= 0 || c.Author.ID != owner {
		return d.leave(ctx, t, log, "Bug approval requires authenticated owner", fmt.Errorf("numeric owner identity mismatch"))
	}
	state, review, wait, err := d.db.CurrentBugAnalysisReview(ctx, t.session.ID)
	if err != nil {
		return d.leave(ctx, t, log, "Bug approval requires current published analysis review", err)
	}
	_, err = d.db.ApproveBugAnalysis(ctx, t.session.ID, review.Review, store.GraphOwnerReply{
		InstanceID: wait.InstanceID, OwnerID: owner, AuthorID: c.Author.ID, CommentID: c.ID,
		QuestionCommentID: review.CommentID, TaskRef: t.ref.String(), SourceRef: t.ref.String(),
		DeliveryKey: review.DeliveryKey, Body: c.Body, At: t.tickAt,
	}, state.Revision)
	if err != nil {
		return d.leave(ctx, t, log, "Bug analysis approval remains refused", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not acknowledge retained Bug analysis approval", err)
}

// AdmitBugAnalysisAnswers is the factory question-continuation boundary. The
// caller supplies the observed source and selected question IDs; the published
// wait and immutable analysis come from storage. It retains owner text unchanged
// and creates no investigation or authority. Parent coordination separately
// schedules a budgeted analyst continuation.
func AdmitBugAnalysisAnswers(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef string, comment github.Comment, questionIDs []string, at time.Time) error {
	if db == nil {
		return errors.New("bug answer admission requires store")
	}
	instance, err := db.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	wait, err := db.GetGraphOwnerWait(ctx, instanceID)
	if err != nil {
		return err
	}
	delivery, err := db.GetReportDelivery(ctx, wait.DeliveryKey)
	if err != nil {
		return err
	}
	state, _, _, err := db.CurrentBugAnalysis(ctx, instance.SessionID)
	if err != nil {
		return err
	}
	return db.ConsumeBugAnalysisAnswers(ctx, instance.SessionID, store.GraphOwnerReply{InstanceID: instanceID, OwnerID: ownerID, AuthorID: comment.Author.ID, CommentID: comment.ID, QuestionCommentID: delivery.CommentID, TaskRef: taskRef, SourceRef: sourceRef, DeliveryKey: wait.DeliveryKey, Body: comment.Body, At: at}, questionIDs, state.Revision)
}
