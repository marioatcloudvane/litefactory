package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// recoverRepairPublication can repeat only publication of an already captured
// immutable successful repair commit. An uncaptured or dirty save stays unknown;
// it cannot be converted to a second fixer invocation.
func (r *BugRuntime) recoverRepairPublication(ctx context.Context, repair store.BugRepair, w *git.Worktree) error {
	run, e := r.opts.DB.GetRun(ctx, repair.RunID)
	if e != nil {
		return e
	}
	if run.Published {
		return nil
	}
	if run.Status != store.RunSucceeded || run.CommitSHA == "" {
		return errors.New("bug repair save has no successful immutable commit to recover")
	}
	_, analysis, approval, e := r.opts.DB.CurrentBugAnalysis(ctx, repair.SessionID)
	if e != nil {
		return e
	}
	if approval == nil || analysis.Digest != repair.AnalysisDigest || approval.Review != repair.ApprovalReview {
		return store.ErrBugRepairUnauthorized
	}
	tree, e := w.RevParse(ctx, "HEAD^{tree}", git.RevParseOptions{})
	if e != nil {
		return e
	}
	if e = w.ObserveBugCheckSource(ctx, run.CommitSHA, tree); e != nil {
		return e
	}
	remote, e := w.RemoteHead(ctx, r.opts.remote())
	if e != nil {
		return e
	}
	if remote != run.CommitSHA {
		session, e := r.opts.DB.GetSession(ctx, repair.SessionID)
		if e != nil {
			return e
		}
		repo, e := r.opts.DB.GetRepo(ctx, session.RepoID)
		if e != nil {
			return e
		}
		release, e := r.opts.CloneGate.Acquire(ctx, repo.Path)
		if e != nil {
			return e
		}
		defer release()
		// Push is non-forcing; concurrent or divergent task heads are refused by
		// the existing git gateway. Source and authorization are checked again.
		if e = w.ObserveBugCheckSource(ctx, run.CommitSHA, tree); e != nil {
			return e
		}
		if e = w.Push(ctx, r.opts.remote()); e != nil {
			return e
		}
		remote, e = w.RemoteHead(ctx, r.opts.remote())
		if e != nil {
			return e
		}
	}
	if remote != run.CommitSHA {
		return errors.New("bug repair publication remains unconfirmed")
	}
	return r.opts.DB.SetRunPublication(ctx, run.ID, true, fmt.Sprintf("Recovered publication of exact Bug repair commit %s", remote))
}
