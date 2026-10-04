package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (d *Daemon) claimResearchReply(ctx context.Context, t *task, trigger turn.Trigger) error {
	return d.claimGraphReply(ctx, t, trigger, "research")
}

// claimGraphReply claims an owner reply for the admitted mode's parked question.
func (d *Daemon) claimGraphReply(ctx context.Context, t *task, trigger turn.Trigger, mode string) error {
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
		if wait.ReplyID == trigger.CommentID && wait.ReplyBody == trigger.Body && wait.ReplyAuthorID == trigger.AuthorID && trigger.AuthorID == d.filter.Identities().OwnerUserID {
			return nil
		}
		return store.ErrGraphOwnerWait
	}
	if wait.State != "waiting" {
		return nil
	}
	_, err = AdmitGraphOwnerReply(ctx, d.db, instance.ID, d.filter.Identities().OwnerUserID, t.session.TaskRef, t.ref.String(), mode, github.Comment{ID: trigger.CommentID, Body: trigger.Body, Author: github.User{ID: trigger.AuthorID, Login: trigger.Author}}, t.tickAt)
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
	return db.ClaimGraphOwnerReply(ctx, store.GraphOwnerReply{InstanceID: instanceID, OwnerID: ownerID, AuthorID: comment.Author.ID, CommentID: comment.ID, QuestionCommentID: delivery.CommentID, TaskRef: taskRef, SourceRef: sourceRef, Mode: mode, Workflow: graphReplyWorkflow(mode), DeliveryKey: wait.DeliveryKey, Body: comment.Body, At: at})
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
func (d *Daemon) recoverGraphReply(ctx context.Context, t *task, trigger turn.Trigger, command turn.Command, mode string) (handled bool, err error) {
	if trigger.PlanReply != nil || t.continuation != nil || command.Kind != turn.KindFreeText || command.Name != "" || trigger.CommentID <= 0 || trigger.AuthorID != d.filter.Identities().OwnerUserID || store.IsGraphTrigger(trigger.CommentID) || store.IsAutomaticTrigger(trigger.CommentID) || store.IsScheduleTrigger(trigger.CommentID) {
		return false, nil
	}
	if consumed, e := d.consumed(ctx, t.session.ID, trigger.CommentID); e != nil || consumed {
		return consumed, e
	}
	admission, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if admission.Workflow != graphReplyWorkflow(mode) {
		return false, nil
	}
	acquired, err := d.db.AcquireSessionLock(ctx, t.session.ID, d.instanceID)
	if err != nil || !acquired {
		return true, err
	}
	defer func() {
		err = errors.Join(err, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), t.session.ID, d.instanceID))
	}()
	instance, err := d.db.LatestWorkflowInstance(ctx, t.session.ID, mode)
	if admission.Workflow == "bug" {
		instance, err = d.db.LatestStandaloneBugInstance(ctx, t.session.ID)
	}
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
	if dispatch.Node != instance.CurrentNode || dispatch.Status != "cancelled" || dispatch.AllocatedRunID != 0 || !instance.NextDueAt.IsZero() {
		return false, nil
	}
	issue, err := d.forge.GetIssue(ctx, t.forgeRepo, t.session.IssueNumber)
	if err != nil || issue.State != github.StateOpen {
		return true, err
	}
	_, err = d.db.ParkGraphInstanceForReply(ctx, instance.ID, instance.CurrentNode, t.tickAt.UTC(), trigger.CommentID)
	return true, err
}
