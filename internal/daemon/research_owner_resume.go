package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (d *Daemon) claimResearchReply(ctx context.Context, t *task, trigger turn.Trigger) error {
	// Replaying an already allocated input performs report recovery only. It
	// cannot be reinterpreted as an answer to the question that input produced.
	run, found, err := d.db.GetRunByTrigger(ctx, t.session.ID, trigger.CommentID)
	if err != nil {
		return err
	}
	if found && run.Status != store.RunPrelaunch {
		return nil
	}
	instance, err := d.db.LatestResearchInstance(ctx, t.session.ID)
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
	if wait.State == "claimed" {
		if wait.ReplyID == trigger.CommentID && wait.ReplyBody == trigger.Body && wait.ReplyAuthorID == trigger.AuthorID && trigger.AuthorID == d.filter.Identities().OwnerUserID {
			return nil
		}
		return store.ErrGraphOwnerWait
	}
	if wait.State != "waiting" {
		return nil
	}
	_, err = AdmitResearchOwnerReply(ctx, d.db, instance.ID, d.filter.Identities().OwnerUserID, t.session.TaskRef, t.ref.String(), "research", github.Comment{ID: trigger.CommentID, Body: trigger.Body, Author: github.User{ID: trigger.AuthorID, Login: trigger.Author}}, t.tickAt)
	return err
}

// AdmitGraphOwnerReply is the graph driver's authenticated question admission
// boundary. The caller supplies the actual forge source and selected mode; the
// question identity comes from durable wait/publication records, not reply text.
// A claimed reply survives process exit before allocation, with no held worker.
func AdmitGraphOwnerReply(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef, mode string, comment github.Comment, at time.Time) (store.GraphOwnerWait, error) {
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
	return db.ClaimGraphOwnerReply(ctx, store.GraphOwnerReply{InstanceID: instanceID, OwnerID: ownerID, AuthorID: comment.Author.ID, CommentID: comment.ID, QuestionCommentID: delivery.CommentID, TaskRef: taskRef, SourceRef: sourceRef, Mode: mode, DeliveryKey: wait.DeliveryKey, Body: comment.Body, At: at})
}

// AdmitResearchOwnerReply supplies the production Research mode fence.
func AdmitResearchOwnerReply(ctx context.Context, db *store.DB, instanceID, ownerID int64, taskRef, sourceRef, mode string, comment github.Comment, at time.Time) (store.GraphOwnerWait, error) {
	if mode != "research" {
		return store.GraphOwnerWait{}, errors.New("research owner admission mode mismatch")
	}
	return AdmitGraphOwnerReply(ctx, db, instanceID, ownerID, taskRef, sourceRef, mode, comment, at)
}
