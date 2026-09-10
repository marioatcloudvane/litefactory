package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// syncTask runs under prepareClone's gate after an authoritative Fetch and
// before base synchronization. It never parks unfinished work or rewrites it.
func syncTask(ctx context.Context, opts Options, req Request, clone *git.Clone, w *git.Worktree, author git.Author, logger *slog.Logger) (syncResult, error) {
	s := syncResult{Phase: "task", Branch: w.Branch(), Base: opts.remote() + "/" + w.Branch(), Outcome: syncOutcomeUpToDate}
	var err error
	s.BranchSHA, err = w.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return s, failuref(err, "task reconciliation could not read local HEAD")
	}
	s.BaseSHA, err = clone.ShowRef(ctx, "refs/remotes/"+s.Base)
	if errors.Is(err, git.ErrRefNotFound) {
		s.Outcome = syncOutcome("absent")
		return s, nil
	}
	if err != nil {
		return s, failuref(err, "task reconciliation could not observe %s; local work was preserved", s.Base)
	}
	d, err := w.Divergence(ctx, s.BaseSHA)
	if err != nil {
		return s, taskRefusal(s, err.Error())
	}
	s.Behind, s.Ahead = d.Behind, d.Ahead
	if req.mode() == "validate" {
		s.Outcome = syncOutcomeSkipped
		s.Reason = "Validate observes task freshness without changing the retained snapshot"
		return s, nil
	}
	if s.Behind == 0 {
		return s, nil
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return s, taskRefusal(s, err.Error())
	}
	if len(changes) > 0 {
		return s, taskRefusal(s, "uncommitted changes must be preserved and finished before incorporating remote task commits")
	}
	if s.Ahead > 0 {
		preview, err := w.MergePreview(ctx, s.BaseSHA)
		if err != nil {
			return s, taskRefusal(s, err.Error())
		}
		if !preview.Clean {
			s.Outcome = syncOutcomeBlocked
			s.Conflicts = preview.Conflicts
			if opts.SuggestConflictResolution && req.Trigger.CommentID != 0 {
				s.ConflictSnapshots, err = w.CollectConflictSnapshots(ctx, preview.TreeSHA, s.BranchSHA, s.BaseSHA, s.Conflicts, opts.ConflictContentMaxBytes)
				if err != nil {
					s.ConflictCollectionReason = err.Error()
					s.ConflictExcluded = append([]string(nil), s.Conflicts...)
				} else {
					s.ConflictFiles = conflictAdvisorFiles(s.ConflictSnapshots)
				}
			}
			return s, taskRefusal(s, "remote task commits conflict with local commits")
		}
	}
	merged, err := w.MergeBase(ctx, s.BaseSHA, git.MergeOptions{Author: author, FastForwardOnly: s.Ahead == 0})
	if err != nil {
		return s, taskRefusal(s, err.Error())
	}
	s.Outcome = syncOutcomeSynced
	s.SyncCommitSHA = merged.Head
	logger.Info("task branch reconciled", "ref", s.Base, "before", s.BranchSHA, "remote", s.BaseSHA, "after", merged.Head)
	return s, nil
}

func taskRefusal(s syncResult, reason string) *syncRefusal {
	s.Outcome = syncOutcomeBlocked
	s.Reason = reason
	return &syncRefusal{phase: "task", observation: &s, branch: s.Branch, base: s.Base, branchSHA: s.BranchSHA, baseSHA: s.BaseSHA,
		conflicts: s.Conflicts, snapshots: s.ConflictSnapshots, files: s.ConflictFiles, excluded: s.ConflictExcluded, collectionReason: s.ConflictCollectionReason}
}

func observation(s syncResult) store.SyncObservation {
	return store.SyncObservation{Phase: s.Phase, Ref: s.Base, LocalSHA: s.BranchSHA, RemoteSHA: s.BaseSHA, Outcome: string(s.Outcome), Behind: s.Behind, Ahead: s.Ahead, ResultSHA: s.SyncCommitSHA, Conflicts: append([]string(nil), s.Conflicts...), Reason: s.Reason}
}

func reportTaskSync(s *syncResult) *report.Sync {
	if s == nil || s.Outcome == syncOutcome("absent") {
		return nil
	}
	return &report.Sync{Phase: "task", Base: s.Base, LocalSHA: s.BranchSHA, RemoteSHA: s.BaseSHA, Outcome: report.SyncOutcome(s.Outcome), Behind: s.Behind, Ahead: s.Ahead, CommitSHA: s.SyncCommitSHA, Conflicts: append([]string(nil), s.Conflicts...), Reason: s.Reason}
}

func taskRefusalDetail(s *syncResult) string {
	if s == nil {
		return ""
	}
	phase := "Task reconciliation"
	if s.Phase == "base" {
		phase = "Base synchronization"
	}
	detail := fmt.Sprintf("%s against %s (local %s, remote %s; %d behind, %d ahead): %s %s. The agent was not started; inspect the retained worktree before retrying.", phase, s.Base, s.BranchSHA, s.BaseSHA, s.Behind, s.Ahead, s.Outcome, s.Reason)
	if s.Task != nil && s.Task.Outcome != syncOutcome("absent") {
		t := s.Task
		detail += fmt.Sprintf(" Task observation against %s: %s; local %s, remote %s, resulting commit %s; %d behind, %d ahead.", t.Base, t.Outcome, t.BranchSHA, t.BaseSHA, t.SyncCommitSHA, t.Behind, t.Ahead)
	}
	return detail
}
