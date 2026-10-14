package turn

import (
	"context"
	"errors"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// sessionGraphSyncRepo resolves its authority anew for each factory invocation.
type sessionGraphSyncRepo struct {
	runtime   *BugRuntime
	sessionID int64
}

func (b sessionGraphSyncRepo) Begin(ctx context.Context, branch string) (ImplementScratch, error) {
	s, err := b.runtime.opts.DB.GetSession(ctx, b.sessionID)
	if err != nil {
		return nil, err
	}
	if s.Branch != branch {
		return nil, errors.New("base sync working branch differs from factory session")
	}
	return (bugDeliveryRepo{runtime: b.runtime, session: s}).Begin(ctx, branch)
}

func (r *BugRuntime) graphSyncBaseBinding(sessionID int64) SyncBaseBinding {
	return SyncBaseBinding{Repo: sessionGraphSyncRepo{runtime: r, sessionID: sessionID}, Bind: func(ctx context.Context, _ Options, a ActivityRequest) (SyncBaseTarget, error) {
		if r.opts.DB == nil || r.opts.Git == nil || r.opts.CloneGate == nil || r.definition == nil || r.definition.Profile == nil {
			return SyncBaseTarget{}, errors.New("base sync requires active factory services and a pinned graph")
		}
		identity, err := agent.ResolveWorkflow(a.Workflow, a.Mode)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		expected, err := agent.ResolveWorkflow(r.definition.Profile.Workflow, r.definition.Profile.Mode)
		if err != nil || identity != expected {
			return SyncBaseTarget{}, errors.New("base sync invocation differs from factory workflow")
		}
		instance, err := r.opts.DB.GetGraphInstance(ctx, a.InstanceID)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		if instance.SessionID != sessionID {
			return SyncBaseTarget{}, errors.New("base sync graph does not belong to active factory session")
		}
		pin, err := r.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		if pin.Digest != r.definition.Profile.Digest || pin.TeamSnapshotDigest != r.definition.Digest {
			return SyncBaseTarget{}, errors.New("base sync differs from pinned factory workflow")
		}
		node, ok := r.definition.Profile.Node(a.Node)
		if !ok || node.Action != graphexec.SyncBaseAction {
			return SyncBaseTarget{}, errors.New("base sync node is not in the pinned workflow graph")
		}
		s, err := r.opts.DB.GetSession(ctx, sessionID)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		repo, err := executionRepository(ctx, r.opts.DB, s)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		if s.Branch == "" || repo.BaseBranch == "" || s.WorktreePath == "" {
			return SyncBaseTarget{}, errors.New("base sync requires factory branch, base and retained checkout")
		}
		base, err := r.syncBaseBranch(ctx, s, repo)
		if err != nil {
			return SyncBaseTarget{}, err
		}
		return SyncBaseTarget{Working: s.Branch, Base: base, Checkout: sessionSyncCheckout{runtime: r, session: s}, Lock: func(ctx context.Context) (func() error, bool, error) {
			current, err := r.opts.DB.GetSession(ctx, sessionID)
			if err != nil {
				return nil, false, err
			}
			if current.LockOwner == "" || current.LockOwner != r.opts.InstanceID || current.Branch != s.Branch || current.WorktreePath != s.WorktreePath {
				return nil, false, errors.New("base sync requires current task writer exclusion and unchanged factory checkout")
			}
			return func() error { return nil }, true, nil
		}}, nil
	}}
}

type sessionSyncCheckout struct {
	runtime *BugRuntime
	session store.Session
}

func (b sessionSyncCheckout) withCheckout(ctx context.Context, fn func(GitSyncBaseCheckout) error) error {
	opts := b.runtime.opts
	repo, err := executionRepository(ctx, opts.DB, b.session)
	if err != nil {
		return err
	}
	release, err := opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return err
	}
	defer release()
	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return err
	}
	if err = verifyExecutionClone(ctx, opts, b.session, clone); err != nil {
		return err
	}
	if _, err := os.Stat(b.session.WorktreePath); err != nil {
		return errors.New("retained sync checkout is unavailable; preserve existing work before retrying")
	}
	registered, err := clone.WorktreeList(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, entry := range registered {
		if entry.Path == b.session.WorktreePath {
			found = true
			break
		}
	}
	if !found {
		return errors.New("retained sync checkout is not registered with its factory clone")
	}
	ensured, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: b.session.WorktreePath, Branch: b.session.Branch, BaseBranch: repo.BaseBranch, Remote: opts.remote()})
	if err != nil {
		return err
	}
	author, err := resolveAuthor(ctx, opts, clone)
	if err != nil {
		return err
	}
	return fn(GitSyncBaseCheckout{Worktree: ensured.Worktree, Author: author})
}
func (b sessionSyncCheckout) Guard(ctx context.Context, working, head string) (reason string, err error) {
	err = b.withCheckout(ctx, func(c GitSyncBaseCheckout) error { var e error; reason, e = c.Guard(ctx, working, head); return e })
	return
}
func (b sessionSyncCheckout) Refresh(ctx context.Context, working, commit string) error {
	return b.withCheckout(ctx, func(c GitSyncBaseCheckout) error { return c.Refresh(ctx, working, commit) })
}

func (r *BugRuntime) syncBaseBranch(ctx context.Context, s store.Session, repo store.Repo) (string, error) {
	release, err := r.opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	defer release()
	clone, err := r.opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	if err = verifyExecutionClone(ctx, r.opts, s, clone); err != nil {
		return "", err
	}
	// Preparation selects a story base after fetching. Use the same fresh
	// remote view rather than falling back because tracking refs are stale.
	if err = clone.Fetch(ctx, r.opts.remote()); err != nil {
		return "", err
	}
	return storyBaseBranch(ctx, r.opts, clone, s, repo), nil
}
