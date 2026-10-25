package daemon

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// resumeImplementReady uses the same dependency and capacity admission as the
// coordinator. Discovery also invokes it, so a crash after consuming the reply
// cannot strand eligible work behind a sleeping coordinator or running sibling.
func (d *Daemon) resumeImplementReady(ctx context.Context, parent string, generation int64, at time.Time) error {
	feature, err := d.db.GetManagedFeature(ctx, parent)
	if err != nil {
		return err
	}
	if int64(feature.Generation) != generation || feature.State != "implementing" {
		return nil
	}
	managed, err := d.db.ListManagedStories(ctx, parent, int(generation))
	if err != nil {
		return err
	}
	rows, err := d.db.ListImplementStories(ctx, parent, generation)
	if err != nil {
		return err
	}
	byKey := map[string]store.ManagedStory{}
	for _, story := range managed {
		byKey[story.Key] = story
	}
	stories := make([]turn.ImplementStory, 0, len(rows))
	for _, row := range rows {
		story, ok := byKey[row.Key]
		if !ok {
			continue
		}
		stories = append(stories, turn.ImplementStory{Key: row.Key, Branch: row.Branch, Dependencies: story.Dependencies})
	}
	_, err = turn.ReadyImplementStories(ctx, d.db, parent, generation, stories, at)
	return err
}

func (d *Daemon) readyImplementTimeoutResumes(ctx context.Context, at time.Time) error {
	rows, err := d.db.ListImplementWaitingStories(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Parent] {
			continue
		}
		seen[row.Parent] = true
		if err := d.resumeImplementReady(ctx, row.Parent, row.Generation, at); err != nil {
			return err
		}
	}
	return nil
}

// handleImplementTimeoutReply precedes generic Plan-story routing. Admission has
// already authenticated the owner; the exact child identity comes only from
// retained managed records and the current held notice.
func (d *Daemon) handleImplementTimeoutReply(ctx context.Context, t *task, c github.Comment) (bool, error) {
	command := turn.ParseCommand(c.Body)
	if command.Kind != turn.KindFreeText || command.Name != "" || c.ID <= 0 {
		return false, nil
	}
	parent, err := d.db.ManagedParentForChild(ctx, t.repo.ID, t.issue.Number)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	source := t.ref.Forge
	replay, err := d.db.HasImplementTimeoutReply(ctx, source, t.ref.String(), strconv.FormatInt(c.ID, 10))
	if err != nil {
		return true, err
	}
	if replay {
		_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
		return true, err
	}
	feature, err := d.db.GetManagedFeature(ctx, parent)
	if err != nil {
		return true, err
	}
	if feature.State != "implementing" {
		return false, nil
	}
	stories, err := d.db.ListManagedStories(ctx, parent, feature.Generation)
	if err != nil {
		return true, err
	}
	var key string
	for _, story := range stories {
		if story.IssueNumber == t.issue.Number {
			key = story.Key
			break
		}
	}
	if key == "" {
		return false, nil
	}
	row, err := d.db.GetImplementStory(ctx, parent, int64(feature.Generation), key)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if row.TimeoutAttemptID == 0 {
		return false, nil
	}
	if c.Author.ID != d.ticketIdentities(ctx).OwnerUserID {
		return true, store.ErrImplementConflict
	}
	// A reply during an unconfirmed write cannot bypass the unknown-write hold.
	// It remains unconsumed and is reconsidered after delivery reconciliation.
	if row.TimeoutCommentID == "" || row.TimeoutDeliveryKey == "" {
		return true, &recoveryPending{errors.New("timeout notice publication is not confirmed")}
	}
	replyTo := ""
	if c.ReplyToCommentID > 0 {
		replyTo = strconv.FormatInt(c.ReplyToCommentID, 10)
	}
	// The provider's publication time closes the remote-write/local-confirm
	// gap: an owner can reply while the confirmed receipt is still settling.
	comments, err := d.readComments(ctx, t.forgeRepo, t.issue.Number, 0)
	if err != nil {
		return true, err
	}
	var noticeAt time.Time
	noticeIndex, replyIndex := -1, -1
	for i, comment := range comments {
		if comment.ID == c.ID {
			replyIndex = i
		}
		if strconv.FormatInt(comment.ID, 10) == row.TimeoutCommentID {
			noticeAt = comment.CreatedAt
			noticeIndex = i
		}
	}
	if noticeAt.IsZero() {
		return true, &recoveryPending{errors.New("confirmed timeout notice is unavailable from provider")}
	}
	_, _, err = d.db.ResumeImplementTimeout(ctx, store.ImplementTimeoutReply{
		Source: source, Task: t.ref.String(), CommentID: strconv.FormatInt(c.ID, 10),
		AuthorID: strconv.FormatInt(c.Author.ID, 10), OwnerID: strconv.FormatInt(d.ticketIdentities(ctx).OwnerUserID, 10),
		Parent: parent, Generation: row.Generation, Key: row.Key, TimeoutAttemptID: row.TimeoutAttemptID,
		ParkRevision: row.TimeoutParkRevision, DeliveryKey: row.TimeoutDeliveryKey, NoticeCommentID: row.TimeoutCommentID,
		NoticeCreatedAt: noticeAt, NoticePrecedesReply: noticeIndex >= 0 && replyIndex > noticeIndex, ReplyToCommentID: replyTo, Guidance: c.Body, At: c.CreatedAt,
	})
	if errors.Is(err, store.ErrImplementConflict) {
		return true, d.refuseGraphResume(ctx, t, turn.Trigger{CommentID: c.ID}, "worker_agent", "this reply does not answer the current confirmed timeout notice")
	}
	if err != nil {
		return true, err
	}
	if err = d.resumeImplementReady(ctx, parent, row.Generation, t.tickAt); err != nil {
		return true, err
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	if err == nil {
		t.implementTimeoutResumed = true
	}
	return true, err
}

// recoverImplementTimeoutForTask settles an interrupted notice on the child's
// own worker, outside discovery and the independent evidence-cleanup allowance.
func (d *Daemon) recoverImplementTimeoutForTask(ctx context.Context, t *task) (returnedErr error) {
	recovery, ok := d.turn.(interface {
		RecoverImplementTimeout(context.Context, store.ImplementStory) error
	})
	if !ok {
		return nil
	}
	row, err := d.db.ImplementStoryByWorkerSession(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.TimeoutAttemptID == 0 {
		return nil
	}
	instance, err := d.db.LatestWorkflowInstance(ctx, row.WorkerSession, "implement")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && instance.Status == store.GraphRunning {
		acquired, err := d.db.AcquireSessionLock(ctx, t.session.ID, d.instanceID)
		if err != nil {
			return err
		}
		if !acquired {
			return &recoveryPending{errors.New("timeout settlement awaits the worker session lock")}
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			returnedErr = errors.Join(returnedErr, d.db.ReleaseSessionLock(cleanup, t.session.ID, d.instanceID))
		}()
		session, err := d.db.GetSession(ctx, t.session.ID)
		if err != nil {
			return err
		}
		if session.State == store.SessionRunning {
			return &recoveryPending{errors.New("timed-out worker has not finished stopping")}
		}
		// The timeout hold is the authority to stop orphaned graph bookkeeping.
		// This retains effect receipts and never opens a provider invocation.
		if err = d.db.CancelWorkflowGraphs(ctx, t.session.ID, "implement", "Durable worker timeout: factory-only park settlement.", t.tickAt); err != nil {
			return err
		}
	}
	return recovery.RecoverImplementTimeout(ctx, row)
}

// graphImplementTimeoutReplies admits held children as comment-processing tasks.
// Generic managed-child discovery routes to the parent, so it cannot carry the
// story's authenticated reply while a sibling owns the parent's batch.
func (d *Daemon) graphImplementTimeoutReplies(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	rows, err := d.db.ListImplementTimeoutHolds(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.WorkerSession == 0 {
			continue
		}
		session, err := d.db.GetSession(ctx, row.WorkerSession)
		if err != nil {
			return err
		}
		q := byRepo[session.RepoID]
		if q == nil {
			continue
		}
		if prior := seen[session.ID]; prior != nil {
			prior.addSource("implement_timeout_reply")
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(session, ref)
		c.addSource("implement_timeout_reply")
		q.candidates = append(q.candidates, c)
		seen[session.ID] = c
	}
	return nil
}
