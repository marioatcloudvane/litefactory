package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/turn"
)

// dispatchJudgeChild keeps an advisory child command outside parent Plan input.
// Ordinary comment admission authenticates the actor against the child's qualified identity.
func (d *Daemon) dispatchJudgeChild(ctx context.Context, parent *task, child github.Issue, comment github.Comment) (handled bool, returnedErr error) {
	if turn.ParseCommand(comment.Body).Kind != turn.KindJudge {
		return false, nil
	}
	session, err := d.managedChildSession(ctx, parent.repo, parent.ref.String(), child.Number, child.ID)
	if err != nil {
		return true, err
	}
	if err = d.bindObservedIssue(ctx, session.ID, parent.forgeRepo, child); err != nil {
		return true, err
	}
	if err = d.db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
		return true, err
	}
	if !d.inflight.enter(session.ID) {
		return true, errors.New("child advisory command waits for its current polling worker")
	}
	defer d.inflight.leave(session.ID)
	acquired, err := d.db.AcquireSessionLock(ctx, session.ID, d.instanceID)
	if err != nil {
		return true, err
	}
	if !acquired {
		return true, errors.New("child advisory command waits for its current session lock owner")
	}
	defer func() {
		returnedErr = errors.Join(returnedErr, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), session.ID, d.instanceID))
	}()
	consumed, err := d.consumed(ctx, session.ID, comment.ID)
	if err != nil || consumed {
		return true, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return true, err
	}
	childTask := *parent
	childTask.ref, childTask.session, childTask.issue = ref, session, child
	childTask.refining = false
	childTask.commentUnderWay = 0
	if childTask.session.WorktreePath == "" {
		childTask.session.WorktreePath = parent.session.WorktreePath
	}
	if err = d.handleComment(ctx, &childTask, comment); err != nil {
		return true, err
	}
	consumed, err = d.consumed(ctx, session.ID, comment.ID)
	if err != nil {
		return true, err
	}
	if !consumed {
		return true, errors.New("child advisory delivery remains pending")
	}
	return true, nil
}
