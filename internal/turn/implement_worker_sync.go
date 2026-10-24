package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type workerSyncDeferred struct{ working, base, reason string }

func (e *workerSyncDeferred) Error() string { return e.reason }

type workerSyncActivity struct{ SyncBaseActivity }

func (h workerSyncActivity) Execute(ctx context.Context, opts Options, a ActivityRequest) (ActivityResult, error) {
	result, err := h.SyncBaseActivity.Execute(ctx, opts, a)
	var deferred *workerSyncDeferred
	if errors.As(err, &deferred) {
		return implementResult("conflict", syncBaseOutput{Working: deferred.working, Base: deferred.base, Result: "conflict", Reason: deferred.reason})
	}
	return result, err
}

// workerSyncBaseBinding uses the ledger and pinned factory session, never graph refs.
func (rt *implementRuntime) workerSyncBaseBinding() SyncBaseBinding {
	runtime := &BugRuntime{opts: rt.opts, definition: rt.definition}
	binding := runtime.graphSyncBaseBinding(rt.req.SessionID)
	original := binding.Bind
	binding.Bind = func(ctx context.Context, opts Options, a ActivityRequest) (SyncBaseTarget, error) {
		if err := rt.verifyWorkerSyncInvocation(ctx, a); err != nil {
			return SyncBaseTarget{}, err
		}
		if err := rt.prepareWorkerSyncCheckout(ctx, a); err != nil {
			return SyncBaseTarget{}, err
		}
		return original(ctx, opts, a)
	}
	return binding
}

// prepareWorkerSyncCheckout allocates source only: no run, commit or automatic sync.
func (rt *implementRuntime) prepareWorkerSyncCheckout(ctx context.Context, a ActivityRequest) error {
	opts := rt.opts
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || rt.worker == nil {
		return errors.New("worker base sync requires factory services and a story")
	}
	row, err := rt.workerRow(ctx, opts)
	if err != nil {
		return err
	}
	session, err := opts.DB.GetSession(ctx, rt.req.SessionID)
	if err != nil {
		return err
	}
	if row.WorkerSession != session.ID || row.State != store.ImplementRunning || row.Attempts >= store.MaxImplementAttempts || session.LockOwner == "" || session.LockOwner != opts.InstanceID {
		return errors.New("worker base sync requires an eligible running story and current task writer exclusion")
	}
	if session.Branch != "" && session.Branch != row.Branch {
		return errors.New("worker branch differs from trusted story ledger")
	}
	if session.Branch == "" {
		slug := session.FeatureSlug
		if slug == "" {
			revision, revisionErr := implementStoryRevision(ctx, opts.DB, row)
			if revisionErr != nil {
				return revisionErr
			}
			slug, err = naming.Slug(revision.Title)
			if err != nil {
				return err
			}
		}
		if err = naming.ValidateSlug(slug); err != nil {
			return err
		}
		session, err = opts.DB.FreezeSessionSlug(ctx, session.ID, slug, row.Branch)
		if err != nil {
			return err
		}
		if session.Branch != row.Branch {
			return errors.New("worker branch differs from trusted story ledger")
		}
	}
	repo, err := executionRepository(ctx, opts.DB, session)
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
	if err = verifyExecutionClone(ctx, opts, session, clone); err != nil {
		return err
	}
	if err = clone.Fetch(ctx, opts.remote()); err != nil {
		return err
	}
	session, branch, err := freezeSlug(ctx, opts, rt.req, session, repo)
	if err != nil {
		return err
	}
	if branch != row.Branch {
		return errors.New("worker branch differs from trusted story ledger")
	}
	base := storyBaseBranch(ctx, opts, clone, session, repo)
	if _, err = clone.ShowRef(ctx, "refs/remotes/"+opts.remote()+"/"+base); err != nil {
		return fmt.Errorf("selected worker base unavailable: %w", err)
	}
	if session.WorktreePath != "" {
		if _, err := os.Stat(session.WorktreePath); err != nil {
			return errors.New("retained worker checkout is unavailable; preserve existing work before retrying")
		}
	}
	dir := session.WorktreePath
	if dir == "" {
		dir, err = naming.QualifiedWorktreePath(repo.WorktreeRoot, session.IssueNumber, session.TaskRef)
		if err != nil {
			return err
		}
	}
	_, localErr := clone.ShowRef(ctx, "refs/heads/"+branch)
	_, remoteErr := clone.ShowRef(ctx, "refs/remotes/"+opts.remote()+"/"+branch)
	if localErr != nil && !errors.Is(localErr, git.ErrRefNotFound) {
		return localErr
	}
	if remoteErr != nil && !errors.Is(remoteErr, git.ErrRefNotFound) {
		return remoteErr
	}
	ensured, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: base, Remote: opts.remote()})
	if err != nil {
		return err
	}
	if session.WorktreePath != ensured.Worktree.Path() {
		if err = opts.DB.SetSessionWorktree(ctx, session.ID, ensured.Worktree.Path()); err != nil {
			return err
		}
	}
	if remoteErr == nil {
		return nil
	}
	// A retained local branch is never published as if it were first initialization.
	if localErr == nil || !ensured.BranchCreated {
		matches, proofErr := rt.workerInitializationMatches(ctx, a.InstanceID, ensured.Worktree, branch, base)
		if proofErr != nil {
			return proofErr
		}
		if !matches {
			return &workerSyncDeferred{branch, base, "retained working branch has unpublished history; preserve it before retrying base synchronization"}
		}
	}
	head, err := ensured.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return err
	}
	reason, err := ensured.Worktree.CheckSyncCheckout(ctx, branch, head)
	if err != nil {
		return err
	}
	if reason != "" {
		return &workerSyncDeferred{branch, base, reason}
	}

	if ensured.BranchCreated {
		payload, encodeErr := json.Marshal(workerInitialization{Working: branch, Base: base, Commit: head, Session: session.ID})
		if encodeErr != nil {
			return encodeErr
		}
		if _, err = opts.DB.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: a.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: payload, CreatedAt: rt.now()}); err != nil {
			return err
		}
	}
	if err = ensured.Worktree.Push(ctx, opts.remote()); err != nil {
		// A concurrent non-forced publication can be reconciled by the reusable gate.
		if fetchErr := clone.Fetch(ctx, opts.remote()); fetchErr != nil {
			return errors.Join(err, fetchErr)
		}
		if _, lookupErr := clone.ShowRef(ctx, "refs/remotes/"+opts.remote()+"/"+branch); lookupErr != nil {
			return err
		}
	}
	return nil
}

func (rt *implementRuntime) verifyWorkerSyncHandoff(ctx context.Context, instance store.GraphInstance) error {
	if rt.worker == nil || rt.definition == nil {
		return nil
	}
	profile := rt.definition.Profile
	gate, gated := graphexec.ImplementWorkerSyncNode(profile)
	if !gated {
		return nil
	}
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return err
	}
	found := false
	for i := len(attempts) - 1; i >= 0; i-- {
		node, ok := profile.Node(attempts[i].Node)
		if !ok {
			return errors.New("worker handoff contains an unpinned attempt")
		}
		if node.Action == ResearchAgentAction {
			break
		}
		if node.Name == gate.Name {
			found = true
			break
		}
	}
	if !found {
		return errors.New("worker entry lacks fresh base synchronization")
	}
	return verifySyncHandoffBeforeAgent(ctx, rt.opts, profile, instance, rt.workerSyncBaseBinding())
}

func (rt *implementRuntime) registerWorkerSync(specs *graphexec.Registry, activities *ActivityRegistry) error {
	if _, gated := graphexec.ImplementWorkerSyncNode(rt.definition.Profile); !gated {
		return nil
	}
	if err := RegisterSyncBase(specs, activities, agent.ModeImplement, rt.workerSyncBaseBinding()); err != nil {
		return err
	}
	activities.handlers[graphexec.SyncBaseAction] = workerSyncActivity{SyncBaseActivity{rt.workerSyncBaseBinding()}}
	return nil
}

// Verify the invocation before allocating or publishing any branch resources.
func (rt *implementRuntime) verifyWorkerSyncInvocation(ctx context.Context, a ActivityRequest) error {
	if rt.definition == nil || rt.definition.Profile == nil || rt.opts.DB == nil {
		return errors.New("worker sync requires a pinned factory graph")
	}
	profile := rt.definition.Profile
	identity, err := agent.ResolveWorkflow(a.Workflow, a.Mode)
	if err != nil {
		return err
	}
	expected, err := agent.ResolveWorkflow(profile.Workflow, profile.Mode)
	if err != nil || identity != expected {
		return errors.New("worker sync invocation differs from factory workflow")
	}
	instance, err := rt.opts.DB.GetGraphInstance(ctx, a.InstanceID)
	if err != nil {
		return err
	}
	if instance.SessionID != rt.req.SessionID {
		return errors.New("worker sync graph belongs to another session")
	}
	pin, err := rt.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	if pin.Digest != profile.Digest {
		return errors.New("worker sync graph differs from pinned definition")
	}
	attempt, err := rt.opts.DB.GetGraphNodeAttempt(ctx, a.NodeAttemptID)
	if err != nil {
		return err
	}
	if attempt.InstanceID != instance.ID || attempt.Node != a.Node {
		return errors.New("worker sync attempt differs from pinned factory invocation")
	}
	if ok, err := pinnedContent(ctx, rt.opts.DB, pin, instance.ID, attempt.ID, rt.definition.Digest); err != nil || !ok {
		return errors.Join(errors.New("worker sync graph differs from pinned definition"), err)
	}
	node, ok := profile.Node(a.Node)
	gate, gated := graphexec.ImplementWorkerSyncNode(profile)
	if !ok || !gated || node.Name != gate.Name || node.Action != graphexec.SyncBaseAction {
		return errors.New("worker sync invocation is not the pinned worker gate")
	}
	return nil
}

// Initialization evidence distinguishes interrupted publication from retained worker commits.
type workerInitialization struct {
	Working string `json:"initial_working"`
	Base    string `json:"initial_base"`
	Commit  string `json:"initial_commit"`
	Session int64  `json:"initial_session"`
}

func (rt *implementRuntime) workerInitializationMatches(ctx context.Context, instanceID int64, wt *git.Worktree, working, base string) (bool, error) {
	head, err := wt.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return false, err
	}
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return false, err
	}
	gate, gated := graphexec.ImplementWorkerSyncNode(rt.definition.Profile)
	if !gated {
		return false, nil
	}
	for _, attempt := range attempts {
		if attempt.Node != gate.Name {
			continue
		}
		outputs, err := rt.opts.DB.ListGraphOutputsBounded(ctx, attempt.ID, 1<<20, 16)
		if err != nil {
			return false, err
		}
		for _, output := range outputs {
			var init workerInitialization
			if output.Kind == store.GraphOutputJSON && json.Unmarshal(output.Payload, &init) == nil && init.Working == working && init.Base == base && init.Session == rt.req.SessionID && init.Commit == head {
				return true, nil
			}
		}
	}
	return false, nil
}
