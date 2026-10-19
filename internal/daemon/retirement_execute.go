package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// ErrRetirementStorage distinguishes incomplete durable work from an owner deferral.
var ErrRetirementStorage = errors.New("retirement storage failure")

func retirementStorageError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrRetirementStorage, err)
}

// RetirementGit is the executor's narrow observation/mutation seam. Production
// binds it to a registered clone; tests can retain uncertain external outcomes.
type RetirementGit interface {
	Observe(context.Context) (git.RetirementObservation, error)
	Remove(context.Context, git.RetirementObservation, string) error
	Absent(context.Context, string) (bool, error)
}

// RetirementExecutionOptions binds retirement to existing daemon coordination.
// OpenGit is optional; nil uses the registered Git clone capability.
type RetirementExecutionOptions struct {
	// Issues optionally decorates every closure lookup with request admission.
	// Nil reads through the ticket connection. The same client is used for initial/final reads.
	Issues     RetirementIssues
	CloneGate  turn.CloneGate
	LockDir    string
	RetainDays int
	OpenGit    func(context.Context, store.RetirementOwner) (RetirementGit, error)
}

type retirementClone struct {
	clone *git.Clone
	owner store.RetirementOwner
}

func openRetirementClone(ctx context.Context, owner store.RetirementOwner) (RetirementGit, error) {
	binary, err := git.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	clone, err := binary.Open(ctx, owner.Repo.Path)
	if err != nil {
		return nil, err
	}
	remote, err := clone.RemoteURL(ctx, "origin")
	if err != nil {
		return nil, err
	}
	if remote != owner.Repo.RemoteURL {
		return nil, fmt.Errorf("registered origin changed")
	}
	return retirementClone{clone: clone, owner: owner}, nil
}
func (g retirementClone) Observe(ctx context.Context) (git.RetirementObservation, error) {
	return git.ObserveRetirement(ctx, g.clone, g.owner.Session.WorktreePath, g.owner.Session.Branch, g.owner.Repo.BaseBranch)
}
func (g retirementClone) Remove(ctx context.Context, observation git.RetirementObservation, kind string) error {
	return git.GuardedRetirement(ctx, g.clone, observation, g.owner.Repo.BaseBranch, kind)
}
func (g retirementClone) Absent(ctx context.Context, kind string) (bool, error) {
	return git.RetirementAbsent(ctx, g.clone, g.owner.Session.WorktreePath, g.owner.Session.Branch, kind)
}

// ExecuteRetirement is called only by the daemon's bounded sweep. Preview cannot
// reach this method. All authorization is reread under canonical task ownership,
// managed progress ownership, then the clone gate, in that order.
func (d *Daemon) ExecuteRetirement(ctx context.Context, sweepID string, ownerID int64, now time.Time) (evaluation RetirementEvaluation, err error) {
	return d.executeRetirement(ctx, sweepID, ownerID, now, d.retirement)
}

func (d *Daemon) executeRetirement(ctx context.Context, sweepID string, ownerID int64, now time.Time, o RetirementExecutionOptions) (evaluation RetirementEvaluation, err error) {
	defer func() {
		for i := range evaluation.Decisions {
			if evaluation.Decisions[i].Action == "would_remove" {
				evaluation.Decisions[i].Action = "kept"
				evaluation.Decisions[i].Reason = "pass_incomplete"
			}
		}
		LogRetirement(ctx, d.log, sweepID, "outcome", false, evaluation)
		if err != nil {
			attrs := append([]slog.Attr{slog.String("sweep", sweepID), slog.Int64("owner", ownerID)}, retirementErrorAttrs(err)...)
			d.log.LogAttrs(ctx, slog.LevelError, "retirement execution incomplete", attrs...)
		}
	}()
	issues := o.Issues
	if issues == nil {
		issues = retirementTicketIssues{d: d}
	}
	owner, err := d.db.GetRetirementOwner(ctx, ownerID)
	if err != nil {
		return evaluation, retirementStorageError(err)
	}
	evaluation.Owner = owner
	evaluation.ObservedAt = now
	if owner.Session.WorktreePath != "" {
		evaluation.Decisions = append(evaluation.Decisions, RetirementDecision{Kind: "worktree", Resource: owner.Session.WorktreePath, Action: "unknown", Reason: "verification_incomplete"})
	}
	if owner.Session.Branch != "" {
		for _, kind := range []string{"local_branch", "remote_branch"} {
			evaluation.Decisions = append(evaluation.Decisions, RetirementDecision{Kind: kind, Resource: "refs/heads/" + owner.Session.Branch, Action: "unknown", Reason: "verification_incomplete"})
		}
	}
	if sweepID == "" || o.CloneGate == nil || o.LockDir == "" {
		return evaluation, fmt.Errorf("retirement executor is not configured")
	}
	if owner.Canonical && !owner.Active && owner.Pending {
		return d.retirementKept(ctx, sweepID, evaluation, "pending_effects"), nil
	}
	if !owner.Canonical || owner.Active || owner.Pending || !d.enterDoctorGuarded(ctx, ownerID) {
		return d.retirementKept(ctx, sweepID, evaluation, "active_or_uncertain_owner"), nil
	}
	defer d.inflight.leave(ownerID)
	acquired, err := d.db.AcquireSessionLock(ctx, ownerID, d.instanceID)
	if err != nil {
		return evaluation, retirementStorageError(err)
	}
	if !acquired {
		return d.retirementKept(ctx, sweepID, evaluation, "owner_locked"), nil
	}
	defer func() {
		err = errors.Join(err, retirementStorageError(d.db.ReleaseSessionLock(context.WithoutCancel(ctx), ownerID, d.instanceID)))
	}()
	releaseParent, err := d.db.LockStoryProgress(ctx, owner.Session.TaskRef, o.LockDir)
	if errors.Is(err, store.ErrStoryProgressBusy) {
		return d.retirementKept(ctx, sweepID, evaluation, "parent_busy"), nil
	}
	if err != nil {
		return evaluation, err
	}
	defer func() { err = errors.Join(err, releaseParent()) }()
	releaseClone, err := o.CloneGate.Acquire(ctx, owner.Repo.Path)
	if err != nil {
		return evaluation, err
	}
	defer releaseClone()
	owner, err = d.db.GetRetirementOwner(ctx, ownerID)
	if err != nil {
		return evaluation, retirementStorageError(err)
	}
	if owner.Session.LockOwner != d.instanceID {
		return evaluation, fmt.Errorf("retirement task ownership lost")
	}
	owner.Active = owner.BusyWithoutOwnerLock
	openGit := o.OpenGit
	if openGit == nil {
		openGit = openRetirementClone
	}
	gitOps, err := openGit(ctx, owner)
	if err != nil {
		return evaluation, err
	}
	opts := RetirementOptions{Now: now, RetainDays: o.RetainDays, Issues: issues, ObserveGit: func(ctx context.Context, _ store.RetirementOwner) (git.RetirementObservation, error) {
		return gitOps.Observe(ctx)
	}}
	evaluation = EvaluateRetirement(ctx, opts, owner)
	LogRetirement(ctx, d.log, sweepID, "initial", false, evaluation)
	if evaluation.Error != nil {
		return evaluation, evaluation.Error
	}
	pending, err := d.db.PendingRetirementIntents(ctx, ownerID)
	if err != nil {
		return evaluation, retirementStorageError(err)
	}
	for i := range evaluation.Decisions {
		decision := &evaluation.Decisions[i]
		if decision.Action != "would_remove" && decision.Action != "already_absent" {
			continue
		}
		// An old intent is evidence of uncertainty, never authority over a present
		// replacement. Resolve it without deleting that resource during this pass.
		retryPresent := false
		for _, in := range pending {
			if in.Kind != decision.Kind || in.Resource != decision.Resource {
				continue
			}
			absent, confirmErr := gitOps.Absent(ctx, decision.Kind)
			if confirmErr != nil {
				return evaluation, confirmErr
			}
			outcome, reason := "kept", "resource_present_requires_fresh_pass"
			if absent {
				outcome, reason = "already_absent", "reconciled_absence"
			} else {
				retryPresent = true
			}
			if err := d.db.RecordRetirementOutcome(ctx, in.ID, outcome, reason, absent && decision.Kind == "worktree"); err != nil {
				return evaluation, retirementStorageError(err)
			}
		}
		if retryPresent {
			decision.Action = "kept"
			decision.Reason = "resource_present_requires_fresh_pass"
			continue
		}
		// Refresh both activity and Git evidence per resource. Removing a worktree
		// can release its local branch; it cannot authorize another branch tip.
		freshOwner, readErr := d.db.GetRetirementOwner(ctx, ownerID)
		if readErr != nil {
			return evaluation, retirementStorageError(readErr)
		}
		if freshOwner.Session.LockOwner != d.instanceID {
			return evaluation, fmt.Errorf("retirement task ownership lost")
		}
		if freshOwner.Session.Branch != owner.Session.Branch || freshOwner.Session.TaskRef != owner.Session.TaskRef || freshOwner.Repo.Path != owner.Repo.Path || freshOwner.Repo.RemoteURL != owner.Repo.RemoteURL || freshOwner.Repo.BaseBranch != owner.Repo.BaseBranch {
			decision.Action = "kept"
			decision.Reason = "owner_changed"
			continue
		}
		freshOwner.Active = freshOwner.BusyWithoutOwnerLock
		// Keep this attempt's original path to observe the just-removed worktree;
		// a changed nonempty registration is never borrowed by the old attempt.
		if freshOwner.Session.WorktreePath != "" && freshOwner.Session.WorktreePath != owner.Session.WorktreePath {
			decision.Action = "kept"
			decision.Reason = "owner_changed"
			continue
		}
		freshOwner.Session.WorktreePath = owner.Session.WorktreePath
		fresh := EvaluateRetirement(ctx, opts, freshOwner)
		LogRetirement(ctx, d.log, sweepID, "revalidate", false, fresh)
		if fresh.Error != nil {
			return evaluation, fresh.Error
		}
		found := false
		for _, candidate := range fresh.Decisions {
			if candidate.Kind == decision.Kind && candidate.Resource == decision.Resource {
				found = true
				if candidate.ExpectedSHA != decision.ExpectedSHA && candidate.Action != "already_absent" {
					decision.Action = "kept"
					decision.Reason = "head_changed"
				} else {
					*decision = candidate
				}
				break
			}
		}
		if !found || (decision.Action != "would_remove" && decision.Action != "already_absent") {
			continue
		}
		// Last external closure read bypasses conditional cache. No response body or
		// transport error text enters the normal log (either can contain secrets).
		issue, lookupErr := issues.GetIssue(github.WithoutConditionalCache(ctx), github.Repo{Owner: owner.Repo.Owner, Name: owner.Repo.Name}, owner.Session.IssueNumber)
		final := fresh
		final.Decisions = nil
		final.IssueState, final.IssueReason = issue.State, issue.StateReason
		if lookupErr != nil {
			final.IssueError = "issue_lookup_failed"
			final.Error = lookupErr
		}
		if lookupErr == nil && !RetirementIssueClosed(owner.Repo, owner.Session.IssueNumber, issue) {
			final.IssueError = "issue_not_confirmed_closed"
		}
		LogRetirement(ctx, d.log, sweepID, "final_closure", false, final)
		if final.IssueError != "" {
			decision.Action = "unknown"
			decision.Reason = final.IssueError
			if issue.State == github.StateOpen && lookupErr == nil {
				decision.Action = "kept"
				decision.Reason = "issue_reopened"
			}
			if lookupErr != nil {
				return evaluation, lookupErr
			}
			continue
		}
		intentID, writeErr := d.db.RecordRetirementIntent(ctx, store.RetirementIntent{SessionID: ownerID, SweepID: sweepID, Kind: decision.Kind, Resource: decision.Resource, ExpectedSHA: decision.ExpectedSHA, BaseSHA: fresh.Git.BaseSHA, Action: "remove", ObservedAt: now})
		if writeErr != nil {
			decision.Action = "failed"
			decision.Reason = "intent_write_failed"
			return evaluation, retirementStorageError(writeErr)
		}
		wasAbsent := decision.Action == "already_absent"
		// Stale exact worktree registrations need unforced removal even when their
		// directory is already absent. Other absent resources need confirmation only.
		if !wasAbsent || (decision.Kind == "worktree" && fresh.Git.WorktreeRegistered) {
			if removeErr := gitOps.Remove(ctx, fresh.Git, decision.Kind); removeErr != nil {
				if errors.Is(removeErr, git.ErrRetirementChanged) {
					decision.Action = "kept"
					decision.Reason = "head_or_preservation_changed"
					if writeErr := d.db.RecordRetirementOutcome(ctx, intentID, "kept", decision.Reason, false); writeErr != nil {
						decision.Action = "unknown"
						decision.Reason = "outcome_write_failed"
						return evaluation, retirementStorageError(writeErr)
					}
					continue
				}
				decision.Action = "unknown"
				decision.Reason = "mutation_failed_or_uncertain"
				attrs := append([]slog.Attr{slog.String("sweep", sweepID), slog.String("task", owner.Session.TaskRef), slog.String("resource", decision.Resource)}, retirementErrorAttrs(removeErr)...)
				d.log.LogAttrs(ctx, slog.LevelError, "retirement mutation failed", attrs...)
				return evaluation, removeErr
			}
		}
		absent, confirmErr := gitOps.Absent(ctx, decision.Kind)
		if confirmErr != nil || !absent {
			decision.Action = "unknown"
			decision.Reason = "confirmation_failed"
			return evaluation, errors.Join(fmt.Errorf("retirement absence unconfirmed"), confirmErr)
		}
		outcome := "removed"
		if wasAbsent {
			outcome = "already_absent"
		}
		if writeErr := d.db.RecordRetirementOutcome(ctx, intentID, outcome, "confirmed_absence", decision.Kind == "worktree"); writeErr != nil {
			decision.Action = "unknown"
			decision.Reason = "outcome_write_failed"
			return evaluation, retirementStorageError(writeErr)
		}
		decision.Action = outcome
		decision.Reason = "confirmed_absence"
	}
	return evaluation, nil
}

func (d *Daemon) retirementKept(ctx context.Context, sweep string, e RetirementEvaluation, reason string) RetirementEvaluation {
	if len(e.Decisions) == 0 {
		e.Decisions = []RetirementDecision{{Kind: "owner", Resource: e.Owner.Session.TaskRef}}
	}
	for i := range e.Decisions {
		e.Decisions[i].Action = "kept"
		e.Decisions[i].Reason = reason
	}
	LogRetirement(ctx, d.log, sweep, "coordination", false, e)
	return e
}
