package store

import (
	"context"
	"errors"
)

// BugStartAnalystNode names the Bug analyst node in the supported profile.
const BugStartAnalystNode = "analyst"

// StoppedBugLaunch reports the one stopped-start state that only a fresh owner
// reply may continue: the instance is running with nothing owed or live, every
// session run is finished and was launched for this instance, and no analyst
// work was ever recorded. Two shapes qualify: no node attempt and no
// transition at all, or exactly one finished start-node attempt that moved to
// the analyst, which is the current node. The Bug runtime's restart check and
// the bug filing start step both judge with this one rule, so the start step
// never asks for a reply the runtime refuses. Failed reads never count.
func (db *DB) StoppedBugLaunch(ctx context.Context, instance GraphInstance, analyst string) (bool, error) {
	if analyst == "" || instance.Status != GraphRunning || !instance.NextDueAt.IsZero() || !instance.FinishedAt.IsZero() {
		return false, nil
	}
	if _, err := db.GetGraphOwnerWait(ctx, instance.ID); !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if _, err := db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, ErrNotFound) {
		return false, err
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return false, err
	}
	transitions, err := db.ListGraphTransitions(ctx, instance.ID)
	if err != nil {
		return false, err
	}
	switch {
	case len(attempts) == 0 && len(transitions) == 0:
		if instance.CurrentNode == analyst {
			return false, nil
		}
	case len(attempts) == 1 && len(transitions) == 1:
		a, t := attempts[0], transitions[0]
		if a.Node == analyst || a.Status != GraphSucceeded || a.RunID != 0 || instance.CurrentNode != analyst ||
			t.FromNode != a.Node || t.ToNode != analyst || t.OnOutcome != a.Outcome {
			return false, nil
		}
	default:
		return false, nil
	}
	runs, err := db.ListRuns(ctx, instance.SessionID)
	if err != nil || len(runs) == 0 {
		return false, err
	}
	for _, run := range runs {
		if run.Status != RunFailed && run.Status != RunSucceeded && run.Status != RunReported {
			return false, nil
		}
		launched, found, err := db.WorkflowLaunchInstance(ctx, instance.SessionID, run.TriggerCommentID)
		if err != nil || !found || launched != instance.ID {
			return false, err
		}
	}
	comments, err := db.ListGraphComments(ctx, instance.ID, "", 1)
	if err != nil || len(comments) != 0 {
		return false, err
	}
	return true, nil
}
