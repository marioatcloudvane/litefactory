package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// BugRuntimeForge collects the factory-owned issue and delivery gateways.
type BugRuntimeForge interface {
	BugPRForge
	BugMergeForge
	BugCloseForge
}

// BugRuntimeConfig supplies operator authority and isolation without granting role permissions.
type BugRuntimeConfig struct {
	Forge       BugRuntimeForge
	Permissions func(agent.Kind, string) (BugRolePermissions, error)
	Isolation   bugchecks.Isolation
	OwnerID     int64
}

// BugRuntime binds the shipped standalone parent and its one check child to
// application-owned gateways. Its construction grants no agent permissions.
type BugRuntime struct {
	Child                       *BugTestChild
	opts                        Options
	cfg                         BugRuntimeConfig
	definition, childDefinition *teamsource.WorkflowDefinition
}

// NewBugRuntime captures the complete Bug package and binds its concrete domain handlers.
func NewBugRuntime(ctx context.Context, opts Options, cfg BugRuntimeConfig) (*BugRuntime, error) {
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || opts.MCP == nil || opts.ResearchReporter == nil || cfg.Forge == nil || cfg.Permissions == nil || cfg.OwnerID <= 0 {
		return nil, errors.New("bug runtime requires factory result, report, forge, clone gate, owner and permission bindings")
	}
	registry := graphexec.Registry{Policy: teamsource.ValidateBugTests}
	if err := registry.Register(BugChecksSpec()); err != nil {
		return nil, err
	}
	child, err := teamsource.CaptureBugTests(ctx, registry)
	if err != nil {
		return nil, err
	}
	parentSpecs, err := BugParentSpecs()
	if err != nil {
		return nil, err
	}
	parent, err := teamsource.CaptureBugParent(ctx, parentSpecs)
	if err != nil {
		return nil, err
	}
	r := &BugRuntime{opts: opts, cfg: cfg, definition: parent, childDefinition: child}
	checks := &BugChecksActivity{Profile: child.Profile}
	r.Child, err = NewBugTestChild(BugTestChildOptions{DB: opts.DB, Profile: child.Profile, Checks: checks, Resolve: func(ctx context.Context, c store.BugTestCall, phase string) (BugChecksInput, error) {
		w, e := r.worktree(ctx, c.Request.SessionID)
		if e != nil {
			return BugChecksInput{}, e
		}
		binding := c.PhaseBinding(phase)
		return BugChecksInput{Binding: binding, Worktree: w, Isolation: r.cfg.Isolation, Observe: r.observe}, nil
	}})
	return r, err
}
func bugRuntimeDigest(v any) string {
	raw, _ := json.Marshal(v)
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func (r *BugRuntime) worktree(ctx context.Context, sessionID int64) (*git.Worktree, error) {
	s, e := r.opts.DB.GetSession(ctx, sessionID)
	if e != nil {
		return nil, e
	}
	if s.WorktreePath == "" || s.Branch == "" {
		return nil, errors.New("bug task worktree has not been prepared")
	}
	repo, e := r.opts.DB.GetRepo(ctx, s.RepoID)
	if e != nil {
		return nil, e
	}
	release, e := r.opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return nil, e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return nil, e
	}
	result, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: s.WorktreePath, Branch: s.Branch, BaseBranch: "main"})
	return result.Worktree, e
}
func (r *BugRuntime) identity(ctx context.Context, session int64) (store.BugIdentity, error) {
	s, e := r.opts.DB.GetSession(ctx, session)
	if e != nil {
		return store.BugIdentity{}, e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return store.BugIdentity{}, e
	}
	issue, e := r.cfg.Forge.GetIssue(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, s.IssueNumber)
	if e != nil {
		return store.BugIdentity{}, e
	}
	if issue.ID <= 0 || issue.Number != s.IssueNumber {
		return store.BugIdentity{}, errors.New("bug issue observation identity mismatch")
	}
	return store.BugIdentity{SessionID: session, Repository: ref.Owner + "/" + ref.Repo, IssueID: issue.ID, IssueNumber: int64(issue.Number), OwnerID: r.cfg.OwnerID}, nil
}

// Reconcile recovers child and parent operations before generic orphan finalization.
func (r *BugRuntime) Reconcile(ctx context.Context) error {
	// The child owns process identities and process-group cleanup. Agent and remote
	// effect journals are reconciled only when their exact parent node is owed.
	if e := r.Child.Reconcile(ctx, r.opts.MCP.Now()); e != nil {
		return e
	}
	if e := r.opts.DB.ReconcileBugParentGraphs(ctx, r.opts.MCP.Now()); e != nil {
		return e
	}
	if e := r.reconcileReports(ctx); e != nil {
		return e
	}
	pending, e := r.opts.DB.PendingBugRemoteContinuations(ctx)
	if e != nil {
		return e
	}
	for _, c := range pending {
		c.At = r.opts.MCP.Now()
		if _, e = r.opts.DB.ClaimBugContinuation(ctx, c); e != nil {
			return e
		}
	}
	return nil
}
func (r *BugRuntime) pin(ctx context.Context, d *teamsource.WorkflowDefinition, path string) (store.GraphDefinition, error) {
	return r.opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Workflow: "bug", Team: "builtin", ProfileVersion: d.Profile.ProfileVersion, Path: path, Digest: d.Profile.Digest, TeamSnapshotDigest: r.definition.Digest})
}
func (r *BugRuntime) admission(ctx context.Context, req Request) (store.WorkflowAdmission, error) {
	identity, e := r.identity(ctx, req.SessionID)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	if e = r.opts.DB.EnsureBugRecord(ctx, identity); e != nil {
		return store.WorkflowAdmission{}, e
	}
	life, e := r.opts.DB.GetBugLifecycle(ctx, req.SessionID)
	if e == nil {
		i, e := r.opts.DB.GetGraphInstance(ctx, life.ParentInstance)
		if e != nil {
			return store.WorkflowAdmission{}, e
		}
		pin, e := r.opts.DB.GetGraphDefinition(ctx, i.DefinitionID)
		if e != nil {
			return store.WorkflowAdmission{}, e
		}
		// Only the graph is pinned: a task keeps the topology it started on,
		// while role text and skills follow the current package like Research
		// and Plan. Pinning the whole package stranded every open Bug task on
		// any instruction change (#401).
		if pin.Digest != r.definition.Profile.Digest {
			return store.WorkflowAdmission{}, errors.New("bug package differs from retained definition")
		}
		if i.Status != store.GraphRunning && life.Disposition == "pending-effect" {
			pending, pe := r.opts.DB.PendingBugRemoteContinuations(ctx)
			if pe != nil {
				return store.WorkflowAdmission{}, pe
			}
			for _, c := range pending {
				if c.SessionID == req.SessionID {
					c.At = r.opts.MCP.Now()
					next, ce := r.opts.DB.ClaimBugContinuation(ctx, c)
					return store.WorkflowAdmission{Instance: next}, ce
				}
			}
		}
		if req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) {
			_, exists, runErr := r.opts.DB.GetRunByTrigger(ctx, req.SessionID, req.Trigger.CommentID)
			if runErr == nil && !exists {
				next, e := r.continueOwner(ctx, req, life)
				return store.WorkflowAdmission{Instance: next}, e
			}
			if runErr != nil {
				return store.WorkflowAdmission{}, runErr
			}
		}
		return store.WorkflowAdmission{Instance: i}, nil
	}
	if !errors.Is(e, store.ErrNotFound) {
		return store.WorkflowAdmission{}, e
	}
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		return store.WorkflowAdmission{}, errors.New("bug dispatch has no retained parent")
	}
	if _, e = r.opts.DB.AdmitWorkflowPath(ctx, req.SessionID, "graph"); e != nil {
		return store.WorkflowAdmission{}, e
	}
	p, e := r.pin(ctx, r.definition, "bug/bug.dot")
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	c, e := r.pin(ctx, r.childDefinition, "bug_tests.dot")
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	topology, e := graphexec.SemanticDigest(r.definition.Profile)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	raw, e := json.Marshal(r.definition.Files)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	a, e := r.opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: p.ID, Start: r.definition.Profile.Start, ContentDigest: r.definition.Digest, TopologyDigest: topology, Files: raw, Reply: req.Trigger.Body})
	if e != nil {
		return a, e
	}
	_, e = r.opts.DB.EnsureBugLifecycle(ctx, req.SessionID, a.Instance.ID, c.ID)
	return a, e
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (r *BugRuntime) Execute(ctx context.Context, req Request, result Result) (Result, error) {
	result.ResearchGraph = true
	if e := r.reconcileBugProjection(ctx, req.SessionID); e != nil {
		return result, e
	}
	a, e := r.admission(ctx, req)
	if errors.Is(e, store.ErrGraphReplyPredatesQuestion) {
		// The daemon replies and consumes the comment.
		return result, e
	}
	if e != nil {
		reporter := BugParentPolicy{Reporter: r.opts.ResearchReporter}
		reportErr := reporter.PublishEvent(context.WithoutCancel(ctx), req.SessionID, 0, fmt.Sprintf("bug-admission:%d:%d", req.SessionID, req.Trigger.CommentID), "pre-dispatch", bugAdmissionComment(e))
		return result, errors.Join(e, reportErr)
	}
	if !store.IsGraphTrigger(req.Trigger.CommentID) {
		if _, e = r.opts.ResearchReporter.Consume(ctx, req.SessionID, req.Trigger.CommentID); e != nil {
			return result, e
		}
	}
	w := &researchWorkflow{opts: r.opts, req: req, result: result, definition: r.definition, admission: a, policy: BugWorkflowPolicy()}
	var specs graphexec.Registry
	var activities ActivityRegistry
	policy := r.parentPolicy(w)
	if e = policy.Register(&specs, &activities); e != nil {
		return r.finishDispatch(ctx, w, e)
	}
	for {
		if e = ctx.Err(); e != nil {
			bookkeeping := context.WithoutCancel(ctx)
			e = errors.Join(e, r.opts.DB.CancelBugLifecycle(bookkeeping, req.SessionID))
			child, ce := r.opts.DB.LatestBugTestCall(bookkeeping, req.SessionID)
			if ce == nil && child.Outcome == "pending" {
				e = errors.Join(e, r.Child.Cancel(bookkeeping, child.ID, r.opts.MCP.Now()))
			}
			return r.finishDispatch(ctx, w, e)
		}
		i, e := r.opts.DB.GetGraphInstance(ctx, a.Instance.ID)
		if e != nil {
			return r.finishDispatch(ctx, w, e)
		}
		if i.Status != store.GraphRunning {
			return r.finishDispatch(ctx, w, nil)
		}
		wait, we := r.opts.DB.GetGraphOwnerWait(ctx, i.ID)
		if we == nil && wait.State == "waiting" {
			return r.finishDispatch(ctx, w, nil)
		}
		if we != nil && !errors.Is(we, store.ErrNotFound) {
			return r.finishDispatch(ctx, w, we)
		}
		node, ok := r.definition.Profile.Node(i.CurrentNode)
		if !ok {
			return r.finishDispatch(ctx, w, errors.New("bug current node absent from pinned parent"))
		}
		observed, oe := r.cfg.Forge.GetIssue(ctx, github.Repo{Owner: result.Ref.Owner, Name: result.Ref.Repo}, result.Ref.Issue)
		if oe != nil {
			return r.finishDispatch(ctx, w, oe)
		}
		if observed.State != "open" && node.Name != "close" && node.Name != "report" && !node.Terminal {
			return r.finishDispatch(ctx, w, errors.New("bug issue is not open; new investigation, repair and delivery are suppressed"))
		}
		if node.Action == ResearchAgentAction {
			role := node.Attrs["lf_agent_markdown"]
			permissions, e := r.cfg.Permissions(req.Agent, role)
			if e != nil {
				return r.finishDispatch(ctx, w, e)
			}
			nr := req
			nr.Tools, nr.AllowedTools, nr.CodexSandbox, nr.ToolProfile = permissions.Tools, permissions.AllowedTools, permissions.Sandbox, role
			nr.researchDefinition, nr.researchNode, nr.researchInstance, nr.workflowPolicy = r.definition, node.Name, i.ID, &w.policy
			noSync := false
			nr.SyncOverride = &noSync
			w.prep, e = prepare(ctx, r.opts, nr, result.Session, result.Ref, r.opts.Logger)
			if e != nil {
				return r.finishDispatch(ctx, w, e)
			}
			w.prep.mcpRuntime = r.opts.MCP
			started, e := r.opts.DB.StartRun(ctx, w.prep.run.ID)
			if e != nil {
				return r.finishDispatch(ctx, w, e)
			}
			w.prep.run = started
			w.result.Run, w.result.Session = started, w.prep.session
			w.result.WorktreePath, w.result.Branch, w.result.SHABefore = w.prep.worktree.Path(), w.prep.session.Branch, w.prep.shaBefore
			w.result.LogPath, w.result.MetaPath = started.LogPath, w.prep.metaPath
		}
		resume := int64(0)
		if node.Action != ResearchAgentAction && w.result.Run.ID == 0 {
			pending, pe := r.opts.DB.CurrentBugFactoryAttempt(ctx, i.ID, node.Name)
			if pe == nil {
				resume = pending.ID
			} else if !errors.Is(pe, store.ErrNotFound) {
				return r.finishDispatch(ctx, w, pe)
			}
		}
		step, e := ExecuteNodeAction(ctx, r.opts, GraphNodeRequest{StopOnFailure: true, Profile: r.definition.Profile, Activities: activities, InstanceID: i.ID, RunID: w.result.Run.ID, ResumeAttemptID: resume, At: r.opts.MCP.Now()})
		if step.Activity.ProcessResult != nil {
			w.process = step.Activity.ProcessResult
		}
		if e != nil {
			return r.finishDispatch(ctx, w, e)
		}
		if node.Action == ResearchAgentAction {
			next, e := r.opts.DB.GetGraphInstance(ctx, i.ID)
			if e != nil {
				return r.finishDispatch(ctx, w, e)
			}
			if next.Status == store.GraphRunning {
				return r.finishBugAgent(ctx, w, next)
			}
			return r.finishDispatch(ctx, w, nil)
		}
		if step.Status == store.GraphFailed {
			return r.finishDispatch(ctx, w, fmt.Errorf("bug node failed: %s", step.Activity.Diagnostic))
		}
		if step.Status == store.GraphSucceeded || step.Status == "waiting_owner" || step.Status == "waiting_child" {
			return r.finishDispatch(ctx, w, nil)
		}
	}
}
func (r *BugRuntime) finishDispatch(ctx context.Context, w *researchWorkflow, cause error) (Result, error) {
	if cause == nil {
		instance, e := r.opts.DB.GetGraphInstance(ctx, w.admission.Instance.ID)
		if e != nil {
			cause = e
		} else if instance.Status == store.GraphSucceeded {
			closure, ce := r.opts.DB.GetBugClosure(ctx, w.req.SessionID)
			if ce == nil && closure.Outcome == "confirmed" {
				cause = r.opts.DB.CompleteBugLifecycle(ctx, w.req.SessionID)
			} else if ce != nil && !errors.Is(ce, store.ErrNotFound) {
				cause = ce
			}
		}
	}
	if cause != nil {
		bookkeeping := context.WithoutCancel(ctx)
		if ctx.Err() != nil {
			cause = errors.Join(cause, r.opts.DB.CancelBugLifecycle(bookkeeping, w.req.SessionID))
			child, ce := r.opts.DB.LatestBugTestCall(bookkeeping, w.req.SessionID)
			if ce == nil && child.Outcome == "pending" {
				cause = errors.Join(cause, r.Child.Cancel(bookkeeping, child.ID, r.opts.MCP.Now()))
			}
		}
		instance, e := r.opts.DB.GetGraphInstance(bookkeeping, w.admission.Instance.ID)
		if e == nil {
			source, se := r.opts.DB.LatestGraphNodeAttempt(bookkeeping, instance.ID, instance.CurrentNode)
			kind, attempt := "pre-dispatch", int64(0)
			if se == nil && source.Status == store.GraphFailed {
				kind, attempt = "failure", source.ID
			}
			event := fmt.Sprintf("bug-error:%d:%s:%d", instance.ID, instance.CurrentNode, attempt)
			policy := r.parentPolicy(w)
			cause = errors.Join(cause, policy.PublishEvent(bookkeeping, w.req.SessionID, attempt, event, kind, bugFailureComment(instance.CurrentNode, cause)))
		} else {
			cause = errors.Join(cause, e)
		}
	}
	if id, ok := store.GraphTriggerDispatch(w.req.Trigger.CommentID); ok {
		cause = errors.Join(cause, r.opts.DB.FinishGraphDispatch(context.WithoutCancel(ctx), id, r.opts.MCP.Now()))
	}
	return w.finish(ctx, cause)
}

// fetchTarget updates only the registered clone's remote objects under its
// existing gate; it never merges target changes into an approved repair.
func (r *BugRuntime) fetchTarget(ctx context.Context, sessionID int64) error {
	session, e := r.opts.DB.GetSession(ctx, sessionID)
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
	clone, e := r.opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return e
	}
	return clone.Fetch(ctx, r.opts.remote())
}
