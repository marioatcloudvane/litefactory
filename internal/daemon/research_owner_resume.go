package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// claimGraphReply claims an owner reply for the admitted mode's parked question.
func (d *Daemon) claimGraphReply(ctx context.Context, t *task, trigger turn.Trigger, mode string) error {
	var ownerID int64
	if isCodeAuthorityBody(trigger.Body) {
		c := github.Comment{ReplyToCommentID: trigger.ReplyToCommentID, ID: trigger.CommentID, Body: trigger.Body, Author: github.User{ID: trigger.AuthorID, Login: trigger.Author}, CreatedAt: trigger.CreatedAt}
		if err := d.authorizeCodeControl(ctx, t.session.ID, c); err != nil {
			return err
		}
		// The gate admitted exactly this author as the task's code authority.
		ownerID = trigger.AuthorID
	} else {
		ownerID = d.ticketIdentities(ctx).OwnerUserID
	}
	// Replaying an already allocated input performs report recovery only. It
	// cannot be reinterpreted as an answer to the question that input produced.
	run, found, err := d.db.GetRunByTrigger(ctx, t.session.ID, trigger.CommentID)
	if err != nil {
		return err
	}
	if found && run.Status != store.RunPrelaunch {
		return nil
	}
	workflow := graphReplyWorkflow(mode)
	instance, err := d.db.LatestWorkflowInstance(ctx, t.session.ID, workflow)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Terminal cancellation is handled as a new authorized successor, not an
	// expired question wait. Restart itself never comes through this path.
	if instance.Status != store.GraphRunning {
		return nil
	}
	wait, err := d.db.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if workflow == "bug" && wait.OriginNode == "" {
		return nil
	}
	if wait.State == "claimed" {
		if wait.ReplySource == "chat" && store.IsGraphTrigger(trigger.CommentID) {
			dispatch, e := d.db.LatestGraphDispatch(ctx, instance.ID)
			if e != nil {
				return e
			}
			if dispatch.TriggerID() == trigger.CommentID && dispatch.InstanceID == wait.InstanceID && dispatch.SessionID == t.session.ID {
				return nil
			}
		}
		if wait.ReplyID == trigger.CommentID && wait.ReplyBody == trigger.Body && wait.ReplyAuthorID == trigger.AuthorID && wait.ReplyToCommentID == trigger.ReplyToCommentID && trigger.AuthorID == ownerID {
			return nil
		}
		return store.ErrGraphOwnerWait
	}
	if wait.State != "waiting" {
		return nil
	}
	_, err = AdmitGraphOwnerReply(ctx, d.db, instance.ID, ownerID, t.session.TaskRef, t.ref.String(), mode, github.Comment{ReplyToCommentID: trigger.ReplyToCommentID, ID: trigger.CommentID, Body: trigger.Body, Author: github.User{ID: trigger.AuthorID, Login: trigger.Author}}, t.tickAt)
	return err
}

// AdmitGraphOwnerReply is the graph driver's authenticated question admission
// boundary. The caller supplies the actual forge source and selected mode; the
// question identity comes from durable wait/publication records, not reply text.
// A claimed reply survives process exit before allocation, with no held worker.
func AdmitGraphOwnerReply(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef, mode string, comment github.Comment, at time.Time) (store.GraphOwnerWait, error) {
	if _, ok := doctor.ParseCommand(comment.Body); ok {
		return store.GraphOwnerWait{}, store.ErrGraphOwnerWait
	}
	if db == nil {
		return store.GraphOwnerWait{}, errors.New("graph admission requires store")
	}
	wait, err := db.GetGraphOwnerWait(ctx, instanceID)
	if err != nil {
		return wait, err
	}
	delivery, err := db.GetReportDelivery(ctx, wait.DeliveryKey)
	if err != nil {
		return wait, err
	}
	return db.ClaimGraphOwnerReply(ctx, store.GraphOwnerReply{InstanceID: instanceID, OwnerID: ownerID, AuthorID: comment.Author.ID, CommentID: comment.ID, ReplyToCommentID: comment.ReplyToCommentID, QuestionCommentID: delivery.CommentID, TaskRef: taskRef, SourceRef: sourceRef, Mode: mode, Workflow: graphReplyWorkflow(mode), DeliveryKey: wait.DeliveryKey, Body: comment.Body, At: at})
}

// AdmitResearchOwnerReply supplies the production Research mode fence.
func AdmitResearchOwnerReply(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef, mode string, comment github.Comment, at time.Time) (store.GraphOwnerWait, error) {
	if mode != "research" {
		return store.GraphOwnerWait{}, errors.New("research owner admission mode mismatch")
	}
	return AdmitGraphOwnerReply(ctx, db, instanceID, ownerID, taskRef, sourceRef, mode, comment, at)
}

// graphReplyWorkflow binds standalone Bug's empty effective mode without
// treating empty mode as a generic continuation grant.
func graphReplyWorkflow(mode string) string {
	if mode == "" {
		return "bug"
	}
	return mode
}

// AdmitBugOwnerReply resumes the published current stop only; it creates no
// diagnosis approval. Command precedence is enforced before ordinary admission.
func AdmitBugOwnerReply(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef string, comment github.Comment, at time.Time) (store.GraphOwnerWait, error) {
	command := turn.ParseCommand(comment.Body)
	if command.Name != "" {
		return store.GraphOwnerWait{}, store.ErrGraphOwnerWait
	}
	return AdmitGraphOwnerReply(ctx, db, instanceID, ownerID, taskRef, sourceRef, "", comment, at)
}

// recoverGraphReply is a fallback for a cancelled scheduling intent, after
// command and admission checks. Published waits retain their normal handlers.
// The target is the session's latest instance whatever workflow the reply's
// mode resolves to, so a Validate stopped under an Implement-labelled parent
// resumes too. Stopped work that cannot resume is answered with the reason.
func (d *Daemon) recoverGraphReply(ctx context.Context, t *task, trigger turn.Trigger, command turn.Command) (handled bool, err error) {
	if trigger.PlanReply != nil || t.continuation != nil || command.Kind != turn.KindFreeText || command.Name != "" || trigger.CommentID <= 0 || trigger.AuthorID != d.ticketIdentities(ctx).OwnerUserID || store.IsGraphTrigger(trigger.CommentID) || store.IsAutomaticTrigger(trigger.CommentID) || store.IsScheduleTrigger(trigger.CommentID) {
		return false, nil
	}
	if consumed, e := d.consumed(ctx, t.session.ID, trigger.CommentID); e != nil || consumed {
		if consumed {
			d.commentFate(ctx, t, trigger.CommentID, fateIgnored, "ignored, stopped graph reply was already consumed")
		}
		return consumed, e
	}
	if _, err = d.db.GetTaskWorkflowAdmission(ctx, t.session.ID); errors.Is(err, store.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	acquired, err := d.db.AcquireSessionLock(ctx, t.session.ID, d.instanceID)
	if err != nil {
		return true, err
	}
	if !acquired {
		d.commentFate(ctx, t, trigger.CommentID, fateDeferred, "deferred, stopped graph reply recovery is waiting for the session lock")
		t.recordWorkerStatus(workerDeferred)
		return true, nil
	}
	defer func() {
		err = errors.Join(err, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), t.session.ID, d.instanceID))
	}()
	instance, err := d.db.LatestSessionGraphInstance(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil || instance.Status != store.GraphRunning {
		return false, err
	}
	wait, err := d.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if err == nil && (wait.State == "waiting" || wait.State == "claimed") {
		return false, nil
	}
	dispatch, err := d.db.LatestGraphDispatch(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if dispatch.Node != instance.CurrentNode || dispatch.Status != "cancelled" || dispatch.AllocatedRunID != 0 {
		return false, nil
	}
	if !instance.NextDueAt.IsZero() {
		return true, d.refuseGraphResume(ctx, t, trigger, instance.CurrentNode, "it is already scheduled to continue")
	}
	issue, err := d.readIssue(ctx, t.forgeRepo, t.session.IssueNumber)
	if err != nil {
		return true, err
	}
	if issue.State != github.StateOpen {
		d.commentFate(ctx, t, trigger.CommentID, fateIgnored, "ignored, stopped graph reply cannot resume a closed issue")
		return true, nil
	}
	_, err = d.db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, t.tickAt.UTC(), trigger.CommentID)
	if refusal := (*store.GraphReplyResumeRefusal)(nil); errors.As(err, &refusal) {
		return true, d.refuseGraphResume(ctx, t, trigger, instance.CurrentNode, refusal.Reason)
	}
	if err == nil {
		d.commentFate(ctx, t, trigger.CommentID, fateQueued, "queued, stopped graph reply scheduled recovery at node "+instance.CurrentNode)
	}
	return true, err
}

// refuseUnresumableReply releases a claimed owner reply the store refused to
// allocate, so the question waits for a new reply, then answers and consumes
// the comment once, naming the step the question waits on. It handles nothing
// when the trigger is not the claimed reply, such as a scheduled dispatch.
func (d *Daemon) refuseUnresumableReply(ctx context.Context, t *task, trigger turn.Trigger, mode string, cause error) (bool, error) {
	bookkeeping := context.WithoutCancel(ctx)
	instance, err := d.db.LatestWorkflowInstance(bookkeeping, t.session.ID, graphReplyWorkflow(mode))
	if err != nil {
		return false, errors.Join(cause, err)
	}
	node, err := d.db.ReleaseGraphOwnerReply(bookkeeping, instance.ID, trigger.CommentID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errors.Join(cause, err)
	}
	t.log.InfoContext(ctx, "owner reply cannot resume the step its question waits on; released it and refusing it with a reply", slog.Int64("instance_id", instance.ID), slog.Any("error", cause))
	return true, d.refuseGraphResume(ctx, t, trigger, node, "the step the open question waits on cannot be continued with this reply")
}

// refuseGraphResume answers and consumes a reply that could not resume the
// work stopped at node, naming the reason.
func (d *Daemon) refuseGraphResume(ctx context.Context, t *task, trigger turn.Trigger, node, reason string) error {
	t.log.InfoContext(ctx, "owner reply could not resume stopped work; refusing it with a reply", slog.String("node", node), slog.String("reason", reason))
	_, err := d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: trigger.CommentID, Refusal: report.ReplyGraphResumeRefused, ResumeNode: node, ResumeReason: reason})
	return err
}
