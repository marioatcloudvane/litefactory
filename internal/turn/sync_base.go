package turn

import (
	"context"
	"errors"
	"fmt"
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
	Working          string   `json:"working"`
	Base             string   `json:"base"`
	Result           string   `json:"result"`
	Commit           string   `json:"commit,omitempty"`
	Conflicts        []string `json:"conflicts,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	Publication      string   `json:"publication,omitempty"`
	CheckoutVerified bool     `json:"checkout_verified,omitempty"`
}

// Execute returns
//
//	synced    the base was merged and the working branch was pushed
//	current   the working branch already holds the base; nothing was pushed
//	conflict  the base conflicts with the working branch, which is unchanged;
//	          the output names the conflicting paths
//	retry     retained work is unsafe, the lock is busy or the branch moved
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
			out.Result, out.Reason = "retry", reason
			return implementResult("retry", out)
		}
	}

	head, merged, err := scratch.Merge(ctx, target.Base)
	switch {
	case errors.Is(err, git.ErrMergeConflict):
		out.Result, out.Reason = "conflict", fmt.Sprintf("%s conflicts with %s; the working branch is unchanged", target.Base, target.Working)
		out.Conflicts = syncConflictPaths(ctx, scratch, target.Base)
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
		out.Result, out.Reason = "retry", "reserved factory transport differs from the fetched base; retained content was preserved"
		return implementResult("retry", out)
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
			out.Result, out.Reason = "retry", reason
			return implementResult("retry", out)
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
