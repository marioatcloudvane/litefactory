package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// SyncBaseTarget is what one graph binds the generic lf_sync_base step to: the
// branch it works on, the base branch brought into it, and the lock that keeps
// every other writer of the working branch out while the step runs.
type SyncBaseTarget struct {
	Working string
	Base    string
	// ExpectedHead pins the checked repair when the caller requires it.
	ExpectedHead string
	// PreparationReason is a factory-observed retained unpublished checkout.
	// It defers only declared pre-work routes; it cannot waive final synchronization.
	PreparationReason string
	// Lock takes the working branch's lock. It returns ok false, with no error,
	// when another writer holds it; the step then answers retry.
	Lock func(context.Context) (unlock func() error, ok bool, err error)
	// Checkout protects and refreshes a retained agent checkout, when present.
	Checkout SyncBaseCheckout
}

// SyncBaseBinding binds lf_sync_base for one graph. Bind is the one trusted
// source of the target; nothing comes from agent text or node attributes. Repo
// opens the scratch worktree the merge is made in.
type SyncBaseBinding struct {
	Repo ImplementRepo
	// ResolveConflict optionally resolves in the disposable scratch tree.
	// Publication and its expected-head guard remain owned by this activity.
	ResolveConflict func(context.Context, ImplementScratch, SyncBaseTarget) (string, error)
	Bind            func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error)
}

// RegisterSyncBase registers lf_sync_base for one mode in both registries.
func RegisterSyncBase(specs *graphexec.Registry, activities *ActivityRegistry, mode agent.Mode, b SyncBaseBinding, workflow ...agent.Workflow) error {
	if specs == nil || activities == nil || b.Repo == nil || b.Bind == nil {
		return errors.New("the base sync requires registries, a repository and a bind")
	}
	spec := graphexec.SyncBaseSpec(mode)
	if len(workflow) > 1 {
		return errors.New("base sync accepts one workflow")
	}
	if len(workflow) == 1 {
		spec = graphexec.SyncBaseWorkflowSpec(workflow[0], mode)
	}
	if err := specs.Register(spec); err != nil {
		return err
	}
	return activities.Register(spec.Name, SyncBaseActivity{b})
}

// SyncBaseActivity brings the base branch into the working branch in a scratch
// worktree and publishes with an observed-head guard and a non-forced push.
// The server preserves ancestry even if the head moves after observation.
type SyncBaseActivity struct{ b SyncBaseBinding }

type syncBaseOutput struct {
	Working           string   `json:"working"`
	Base              string   `json:"base"`
	Result            string   `json:"result"`
	Commit            string   `json:"commit,omitempty"`
	Conflicts         []string `json:"conflicts,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	Publication       string   `json:"publication,omitempty"`
	CheckoutVerified  bool     `json:"checkout_verified,omitempty"`
	CheckoutUnchanged bool     `json:"checkout_unchanged,omitempty"`
}

// Execute returns
//
//	synced    the base was merged and the working branch was pushed
//	current   the working branch already holds the base; nothing was pushed
//	conflict  merge conflict or retained-work safety blockage; reason is specific
//	          and paths are included only for merge conflicts
//	retry     the lock is busy or the branch moved
//	deferred  known preparation remains unresolved, on a declared pre-work route
//
// Healthy retained checkouts report current without refreshing their edits.
// Final synchronization keeps its strict published-head and checkout checks.
//
// A cancelled context, a git outage or a forge outage is an error.
func (h SyncBaseActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	target, err := h.b.Bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if strings.TrimSpace(target.Working) == "" || strings.TrimSpace(target.Base) == "" || target.Working == target.Base || target.Lock == nil {
		return ActivityResult{}, errors.New("the base sync needs a working branch, a different base branch and a lock")
	}
	allowDeferred := slices.ContainsFunc(strings.Split(req.Attrs["lf_outcomes"], ","), func(outcome string) bool { return strings.TrimSpace(outcome) == "deferred" })
	out := syncBaseOutput{Working: target.Working, Base: target.Base}
	unlock, ok, err := target.Lock(ctx)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("base sync lock: %w", err)
	}
	if !ok {
		out.Result, out.Reason = "retry", "another change to the working branch is in progress"
		return implementResult("retry", out)
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if target.PreparationReason != "" {
		if allowDeferred && knownWorkerPreparation(target.PreparationReason) {
			return deferredSyncResult(ctx, target, out, target.PreparationReason)
		}
		out.Result, out.Reason = "conflict", target.PreparationReason
		return implementResult("conflict", out)
	}
	scratch, err := h.b.Repo.Begin(ctx, target.Working)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("prepare base sync: %w", err)
	}
	defer func() {
		if closeErr := scratch.Close(context.WithoutCancel(ctx)); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("discard scratch worktree: %w", closeErr))
		}
	}()

	if target.ExpectedHead != "" && scratch.Head() != target.ExpectedHead {
		out.Result, out.Reason = "retry", "the working branch differs from the checked repair head"
		return implementResult("retry", out)
	}

	if target.Checkout != nil {
		reason, guardErr := target.Checkout.Guard(ctx, target.Working, scratch.Head())
		if guardErr != nil {
			return ActivityResult{}, fmt.Errorf("inspect retained checkout: %w", guardErr)
		}
		if reason != "" {
			if allowDeferred && knownWorkerPreparation(reason) {
				return deferredSyncResult(ctx, target, out, reason)
			}
			out.Result, out.Reason = "conflict", reason
			return implementResult("conflict", out)
		}
	}

	if allowDeferred && opts.AutoSyncMaxBehind > 0 {
		behind, measured, measureErr := syncBehind(ctx, scratch, target.Base)
		if measureErr != nil {
			return ActivityResult{}, measureErr
		}
		if measured && !withinSyncCap(opts, behind) {
			return deferredSyncResult(ctx, target, out, fmt.Sprintf("automatic base merge skipped: %d commits behind exceeds the limit of %d", behind, opts.AutoSyncMaxBehind))
		}
	}
	head, merged, err := scratch.Merge(ctx, target.Base)
	switch {
	case errors.Is(err, git.ErrMergeConflict):
		out.Result, out.Reason = "conflict", fmt.Sprintf("%s conflicts with %s; the working branch is unchanged", target.Base, target.Working)
		out.Conflicts = syncConflictPaths(ctx, scratch, target.Base)
		if allowDeferred {
			return deferredSyncResult(ctx, target, out, out.Reason)
		}
		if h.b.ResolveConflict == nil {
			return implementResult("conflict", out)
		}
		head, err = h.b.ResolveConflict(ctx, scratch, target)
		if err != nil {
			out.Reason += ": " + err.Error()
			return implementResult("conflict", out)
		}
		merged = true
		out.Reason, out.Conflicts = "", nil
	case errors.Is(err, git.ErrReservedTransportChange) && target.Checkout != nil:
		out.Result, out.Reason = "conflict", "reserved factory transport differs from the fetched base; retained content was preserved"
		return implementResult("conflict", out)
	case err != nil:
		return ActivityResult{}, fmt.Errorf("merge %s into %s: %w", target.Base, target.Working, err)
	}
	if !merged {
		out.Result, out.Commit = "current", scratch.Head()
		return finishSyncCheckout(ctx, target, out)
	}
	if target.Checkout != nil {
		reason, guardErr := target.Checkout.Guard(ctx, target.Working, head)
		if guardErr != nil {
			return ActivityResult{}, fmt.Errorf("inspect proposed checkout handoff: %w", guardErr)
		}
		if reason != "" {
			out.Result, out.Reason = "conflict", reason
			return implementResult("conflict", out)
		}
	}
	out.Commit = head
	// Publication can succeed even when its transport response is lost. Keep
	// the proposed commit as evidence and require Git reconciliation on reentry.
	out.Publication = "uncertain"
	err = scratch.Publish(ctx, target.Working, scratch.Head())
	if errors.Is(err, git.ErrIntegrationHeadMoved) {
		out.Result, out.Reason = "retry", "the working branch moved while the base was being merged"
		out.Publication = "not_published"
		return implementResult("retry", out)
	}
	if err != nil {
		out.Result, out.Reason = "error", "publication could not be confirmed; reconcile the working branch before continuing"
		res, encodeErr := implementResult("", out)
		return res, errors.Join(fmt.Errorf("publish the merge of %s into %s: %w", target.Base, target.Working, err), encodeErr)
	}
	out.Result, out.Commit, out.Publication = "synced", head, "published"
	return finishSyncCheckout(ctx, target, out)
}

// syncConflictPaths names the paths the base conflicts on, read without
// touching the scratch tree. A scratch tree that cannot say reports none; the
// conflict itself is already established.
func syncConflictPaths(ctx context.Context, scratch ImplementScratch, base string) []string {
	if gated, ok := scratch.(gatedScratch); ok {
		scratch = gated.ImplementScratch
	}
	lister, ok := scratch.(interface {
		conflicts(context.Context, string) ([]string, error)
	})
	if !ok {
		return nil
	}
	paths, err := lister.conflicts(ctx, base)
	if err != nil {
		return nil
	}
	return paths
}

// conflicts previews the merge of a remote branch into the scratch head and
// returns the paths that conflict. It changes no file and no ref.
func (s *gitScratch) conflicts(ctx context.Context, branch string) ([]string, error) {
	preview, err := s.wt.MergePreview(ctx, "refs/remotes/"+s.repo.Remote+"/"+branch)
	if err != nil {
		return nil, err
	}
	return preview.Conflicts, nil
}

// finishSyncCheckout makes success include the checkout the next agent sees.
func finishSyncCheckout(ctx context.Context, target SyncBaseTarget, out syncBaseOutput) (ActivityResult, error) {
	if target.Checkout != nil {
		if err := target.Checkout.Refresh(ctx, target.Working, out.Commit); err != nil {
			out.Result, out.Reason = "error", "working commit is retained, but checkout handoff failed; reconcile and verify before continuing"
			res, encodeErr := implementResult("", out)
			return res, errors.Join(fmt.Errorf("refresh retained checkout at %s: %w", out.Commit, err), encodeErr)
		}
		out.CheckoutVerified = true
	}
	return implementResult(out.Result, out)
}

// A declared deferred route continues only known preparation conditions.
func knownRetainedPreparation(reason string) bool {
	return reason == "retained checkout has uncommitted or untracked changes" || reason == "retained checkout has a merge in progress"
}

func deferredSyncResult(ctx context.Context, target SyncBaseTarget, out syncBaseOutput, reason string) (ActivityResult, error) {
	out.Result, out.Reason = "deferred", reason
	if target.Checkout != nil {
		retained, ok := target.Checkout.(interface {
			DeferredHead(context.Context, string) (string, error)
		})
		if !ok {
			return ActivityResult{}, errors.New("preparation deferral requires retained checkout identity verification")
		}
		head, err := retained.DeferredHead(ctx, target.Working)
		if err != nil {
			return ActivityResult{}, err
		}
		out.Commit, out.CheckoutVerified = head, true
		if observer, ok := target.Checkout.(interface {
			PreparationFreshness(context.Context, string, string) (string, int, error)
		}); ok {
			observed, behind, observeErr := observer.PreparationFreshness(ctx, target.Working, target.Base)
			if observeErr != nil {
				return ActivityResult{}, observeErr
			}
			if observed != head {
				return ActivityResult{}, errors.New("retained checkout changed while observing preparation freshness")
			}
			if behind == 0 {
				out.Result, out.Reason, out.Conflicts = "current", "", nil
				out.CheckoutUnchanged = true
				return implementResult("current", out)
			}
		}
	}
	return implementResult("deferred", out)
}

func syncBehind(ctx context.Context, scratch ImplementScratch, base string) (int, bool, error) {
	if gated, ok := scratch.(gatedScratch); ok {
		scratch = gated.ImplementScratch
	}
	actual, ok := scratch.(*gitScratch)
	if !ok {
		return 0, false, nil
	}
	d, err := actual.wt.Divergence(ctx, "refs/remotes/"+actual.repo.Remote+"/"+base)
	return d.Behind, true, err
}

func knownWorkerPreparation(reason string) bool {
	return knownRetainedPreparation(reason) || reason == "retained working branch has unpublished history; preserve it before retrying base synchronization" || reason == "retained local commits are not contained in synchronization commit"
}
