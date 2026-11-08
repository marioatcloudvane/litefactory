package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// ownerBatchLimit bounds one batched answer; the owner wait keeps at most 64 KiB.
const ownerBatchLimit = 65536

// ownerInputQueued reports a workflow that cannot take an owner comment yet: an
// attempt is still running, or its wait already holds another reply. The
// comment stays queued and answers the next question; it is not a failure.
func ownerInputQueued(trigger turn.Trigger, err error) bool {
	return trigger.CommentID > 0 && !store.IsFactoryTrigger(trigger.CommentID) &&
		(errors.Is(err, store.ErrGraphAttemptRunning) || errors.Is(err, store.ErrGraphOwnerWait))
}

// ownerBatch is the owner input that answers one question: trigger and every
// later unconsumed free-text owner comment in this drain's queue, oldest
// first. Comments nothing acts on (the factory's own, third parties') are
// passed over; a command, a doctor request or another author's instruction
// ends the batch so it still runs on its own. upTo, when positive, ends the
// batch at that comment and reports whether it was reached, so a replay
// rebuilds exactly the batch it claimed before.
func (d *Daemon) ownerBatch(ctx context.Context, t *task, trigger turn.Trigger, ownerID, upTo int64) (turn.Trigger, bool, error) {
	if trigger.PlanReply != nil || trigger.CommentID <= 0 || store.IsFactoryTrigger(trigger.CommentID) || trigger.AuthorID != ownerID || !batchable(trigger.Body) {
		return trigger, upTo == 0 || upTo == trigger.CommentID, nil
	}
	members := []github.Comment{{ID: trigger.CommentID, Body: trigger.Body, CreatedAt: trigger.CreatedAt, ReplyToCommentID: trigger.ReplyToCommentID}}
	size := len(trigger.Body)
	for _, c := range t.queue {
		if c.ID <= trigger.CommentID {
			continue
		}
		if upTo > 0 && c.ID > upTo {
			break
		}
		qualified, err := d.qualifyReply(ctx, t.session.ID, c)
		if err != nil {
			return trigger, false, err
		}
		admission, err := d.admitReply(ctx, t.session.ID, c, qualified)
		if err != nil {
			return trigger, false, err
		}
		if !admission.Triggers() {
			continue
		}
		if c.Author.ID != ownerID || !batchable(c.Body) {
			break
		}
		if c, err = d.admittedOrdinaryComment(ctx, t, c); err != nil {
			return trigger, false, err
		}
		if size += len(c.Body) + 64; size > ownerBatchLimit {
			break
		}
		members = append(members, c)
	}
	last := members[len(members)-1]
	if upTo > 0 && last.ID != upTo {
		return trigger, false, nil
	}
	if len(members) == 1 {
		return trigger, true, nil
	}
	var b strings.Builder
	for i, m := range members {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "Owner comment %d (%s):\n%s", m.ID, m.CreatedAt.UTC().Format(time.RFC3339), strings.TrimSpace(m.Body))
	}
	out := trigger
	out.CommentID, out.Body, out.CreatedAt, out.ReplyToCommentID = last.ID, b.String(), last.CreatedAt, last.ReplyToCommentID
	return out, true, nil
}

// batchable reports a comment that is plain owner input rather than a command.
func batchable(body string) bool {
	if turn.ParseCommand(body).Kind == turn.KindDoctor || isCodeAuthorityBody(body) {
		return false
	}
	command := turn.ParseCommand(body)
	return command.Kind == turn.KindFreeText && command.Name == "" && !strings.HasPrefix(strings.TrimSpace(body), "/")
}

// ownerDeadLetterAttempts is how many failed turns one owner comment gets
// before it is set aside with a reply, so later comments run.
const ownerDeadLetterAttempts = 3

// deadLetter consumes an owner comment that failed ownerDeadLetterAttempts
// turns in a row: it answers once with the reason, and the reply path consumes
// the comment after delivery. A failed post is retried in the background
// without holding the comments after it (see [Daemon.settleReportDebt]).
func (d *Daemon) deadLetter(ctx context.Context, t *task, log *slog.Logger, cause error) {
	commentID := t.commentUnderWay
	reason := cause.Error()
	if sanitizer := d.diagnosticRedactor; sanitizer != nil {
		if body, err := sanitizer.Apply(ctx, reason, 1024); err == nil {
			reason = body.String()
		}
	}
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	log.WarnContext(ctx, "owner comment failed every turn; setting it aside with a reply so later comments run",
		slog.Int64("comment_id", commentID), slog.String("reason", reason))
	d.commentFate(ctx, t, commentID, fateIgnored, "not used after repeated failures: "+reason)
	_, err := d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: commentID, Refusal: report.ReplyNotUsed, NotUsedReason: reason})
	if err != nil && !isRecoveryPending(err) {
		log.WarnContext(ctx, "the not-used reply could not be prepared", slog.Any("error", err))
		return
	}
	d.prepFailures.clear(t.session.ID)
}

// reportBackgroundAttempts is the recovery attempt count (zero-based, so the
// third failed post) from which an owed report no longer holds the comments
// after it.
const reportBackgroundAttempts = 2

// consumeWithReportDebt lets the queue move past a comment whose work is done
// and whose report is only waiting to be posted: the post is retried in the
// background ([Daemon.settleReportDebt]) instead of holding every later
// comment. It reports whether the comment was consumed that way.
func (d *Daemon) consumeWithReportDebt(ctx context.Context, t *task, commentID int64) (bool, error) {
	if commentID <= 0 || store.IsFactoryTrigger(commentID) {
		return false, nil
	}
	if admission, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID); err == nil && admission.Workflow == "bug" {
		// Bug owns its own publication and input consumption.
		return false, nil
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	// A post that failed once or twice keeps the comment order; only one that
	// keeps failing moves to the background.
	d.recoveries.mu.Lock()
	row, exists := d.recoveries.items[recoveryKey(t.session.ID, commentID)]
	d.recoveries.mu.Unlock()
	if !exists || row.State == "resolved" || row.State == "stopped" || row.Attempts < reportBackgroundAttempts {
		return false, nil
	}
	_, found, err := d.db.FindPendingReportDelivery(ctx, t.session.ID, commentID)
	if err != nil || !found {
		return false, err
	}
	if _, err := d.db.AdvanceWatermark(ctx, t.session.ID, commentID); err != nil {
		return false, err
	}
	t.reportDebt = true
	t.log.InfoContext(ctx, "comment consumed; its report is posted in the background", slog.Int64("comment_id", commentID))
	return true, nil
}

// settleReportDebt retries, in the background of the comment queue, the posts
// of comments that were already consumed. It never stops the queue; owed posts
// keep the task's backlog flag so a later tick comes back to them.
func (d *Daemon) settleReportDebt(ctx context.Context, t *task, watermark int64) error {
	triggers, err := d.db.PendingCommentReportTriggers(ctx, t.session.ID, watermark)
	if err != nil {
		return err
	}
	for _, trigger := range triggers {
		if _, err := d.resumeReport(ctx, t, trigger); err != nil && !isRecoveryPending(err) {
			return err
		}
		if _, still, err := d.db.FindPendingReportDelivery(ctx, t.session.ID, trigger); err != nil {
			return err
		} else if still {
			t.reportDebt = true
		}
	}
	return nil
}
