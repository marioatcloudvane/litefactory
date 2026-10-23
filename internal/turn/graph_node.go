package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// GraphNodeRequest supplies a validated, frozen profile and activity registry.
// At is the caller's logical timestamp for this bounded step, not measured
// handler duration. NextDueAt is supplied scheduling policy, never computed here.
// Callers serialize steps for an instance; in-flight crash recovery is deferred.
type GraphNodeRequest struct {
	// RetainCapacity leaves release to the driver after its outer cleanup.
	// The next node must replace the reservation before executing.
	RetainCapacity    bool
	StopOnFailure     bool
	Profile           *graphexec.Profile
	Activities        ActivityRegistry
	InstanceID, RunID int64
	LaunchID          int64
	// ResumeAttemptID is factory-owned recovery of a Bug bookkeeping effect.
	ResumeAttemptID      int64
	OwnerAllowanceSource int64
	ResumeRef            string
	At, NextDueAt        time.Time
	RetryDueAt           time.Time
}

// GraphNodeResult describes the recorded step. AttemptID is zero for terminals
// and exhausted limits. Status is the instance status, not the activity outcome.
type GraphNodeResult struct {
	// Deferred means admission is waiting. AttemptID is zero if no work began.
	Deferred               bool
	InstanceID, AttemptID  int64
	Node, NextNode, Status string
	Activity               ActivityResult
}

// parentSerializedActivity serializes on a Plan parent's story-progress lock,
// which ExecuteNodeAction takes before reserving the attempt.
type parentSerializedActivity interface {
	ActivityHandler
	lockParent(context.Context) (func() error, error)
	withParentLocked() ActivityHandler
}

// ExecuteNodeAction performs at most one activity. Reservation commits before
// dispatch; completion atomically writes its output, transition and progress.
// The Research driver supplies the registered mode activities in production.
func ExecuteNodeAction(ctx context.Context, opts Options, req GraphNodeRequest) (out GraphNodeResult, err error) {
	out = GraphNodeResult{InstanceID: req.InstanceID}
	if opts.DB == nil || req.Profile == nil || req.InstanceID <= 0 || req.RunID < 0 || req.At.IsZero() {
		return out, errors.New("execute graph node: database, profile, instance and timestamp are required; run ID must be nonnegative")
	}
	if capacity := stepCapacity(ctx); capacity != nil && !req.RetainCapacity {
		defer capacity.Release()
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return out, fmt.Errorf("execute graph node: %w", err)
	}
	out.Node, out.NextNode, out.Status = instance.CurrentNode, instance.CurrentNode, instance.Status
	if instance.Status != store.GraphRunning {
		return out, fmt.Errorf("execute graph node: %w", store.ErrGraphInstanceTerminal)
	}
	wait, waitErr := opts.DB.GetGraphOwnerWait(ctx, instance.ID)
	if waitErr != nil && !errors.Is(waitErr, store.ErrNotFound) {
		return out, waitErr
	}
	if waitErr == nil && wait.State == "waiting" {
		out.Status = "waiting_owner"
		return out, nil
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return out, fmt.Errorf("execute graph node definition: %w", err)
	}
	workflow, bindingErr := agent.ResolveWorkflow(req.Profile.Workflow, req.Profile.Mode)
	storedWorkflow, storedErr := agent.ResolveWorkflow(agent.Workflow(definition.Workflow), agent.Mode(definition.Mode))
	if bindingErr != nil || storedErr != nil || workflow != storedWorkflow {
		return out, errors.New("execute graph node: profile does not match pinned definition workflow")
	}
	admission, admissionErr := opts.DB.GetTaskWorkflowAdmission(ctx, instance.SessionID)
	if admissionErr != nil && !errors.Is(admissionErr, store.ErrNotFound) {
		return out, admissionErr
	}
	if admissionErr == nil && ((admission.Family == "standalone" && workflow != agent.WorkflowBug) || (admission.Family == "mode" && workflow == agent.WorkflowBug)) {
		return out, errors.New("execute graph node: workflow differs from task admission")
	}
	if workflow == agent.WorkflowBug && admissionErr != nil {
		return out, errors.New("execute graph node: missing Bug admission")
	}
	if (workflow != agent.WorkflowResearch && definition.Digest != req.Profile.Digest) || definition.Team != req.Profile.Team || definition.Mode != string(req.Profile.Mode) || (workflow != agent.WorkflowResearch && definition.ProfileVersion != req.Profile.ProfileVersion) {
		return out, errors.New("execute graph node: profile does not match pinned definition")
	}
	node, ok := req.Profile.Node(instance.CurrentNode)
	if !ok {
		return out, fmt.Errorf("execute graph node: unknown current node %q", instance.CurrentNode)
	}
	if node.Terminal {
		if err := opts.DB.SetGraphInstanceStatus(ctx, instance.ID, store.GraphSucceeded, time.Time{}, req.At); err != nil {
			return out, fmt.Errorf("complete graph terminal: %w", err)
		}
		out.Status = store.GraphSucceeded
		return out, nil
	}
	admitted, diagnostic, capacityErr := admitGraphCapacity(ctx, opts, instance.ID, req.LaunchID, node.Name, string(workflow))
	if capacityErr != nil {
		return out, capacityErr
	}
	if !admitted {
		out.Deferred = true
		out.Activity.Diagnostic = diagnostic
		return out, nil
	}
	handler, registered := req.Activities.Resolve(node.Action)
	if suite, ok := handler.(ValidateSuiteActivity); ok {
		unlock, lockErr := suite.b.DB.LockSuite(ctx, suite.b.LockDir)
		if errors.Is(lockErr, store.ErrSuiteBusy) {
			out.Deferred = true
			return out, nil
		}
		if lockErr != nil {
			return out, fmt.Errorf("admit validate suite: %w", lockErr)
		}
		defer func() { err = errors.Join(err, unlock()) }()
		handler = admittedValidateSuite{suite}
	}
	// A busy Plan parent is temporary contention: take its story-progress lock
	// before reserving, so waiting spends no node attempt or revision budget.
	if serialized, ok := handler.(parentSerializedActivity); ok && workflow == agent.WorkflowPlan && node.Action == graphexec.PlanStoryAction {
		unlock, lockErr := serialized.lockParent(ctx)
		if errors.Is(lockErr, store.ErrStoryProgressBusy) {
			out.Deferred = true
			return out, nil
		}
		if lockErr != nil {
			return out, fmt.Errorf("admit plan story parent: %w", lockErr)
		}
		defer func() { err = errors.Join(err, unlock()) }()
		handler = serialized.withParentLocked()
	}
	if review, ok := handler.(ValidateReviewActivity); ok {
		admitted, release, admissionErr := review.admit(ctx, opts, ActivityRequest{InstanceID: instance.ID, Node: node.Name, Attrs: node.Attrs})
		if admissionErr != nil {
			return out, admissionErr
		}
		if !admitted {
			out.Deferred = true
			out.Activity.Outcome = "retry"
			out.Activity.Diagnostic = review.capacityDiagnostic(ctx, req.Profile, node.Name)
			return out, nil
		}
		if release != nil {
			defer release()
			review.initialRelease = release
			handler = review
		}
	}
	var attempt store.GraphNodeAttempt
	var deferredAttempt store.GraphNodeAttempt
	deferredErr := store.ErrNotFound
	reviewResume := workflow == agent.WorkflowValidate && node.Action == graphexec.ValidateReviewAction
	judgeResume := req.ResumeAttemptID != 0 && (workflow == agent.WorkflowResearch || workflow == agent.WorkflowPlan) && node.Action == graphexec.JudgeAction
	if reviewResume || judgeResume {
		deferredAttempt, deferredErr = opts.DB.ResumeDeferredGraphNodeAttempt(ctx, instance.ID, node.Name, req.At)
		if deferredErr == nil && judgeResume && deferredAttempt.ID != req.ResumeAttemptID {
			return out, errors.New("judge capacity resume attempt mismatch")
		}
	}
	if deferredErr != nil && !errors.Is(deferredErr, store.ErrNotFound) {
		return out, deferredErr
	}
	switch {
	case deferredErr == nil:
		attempt = deferredAttempt

	case req.ResumeAttemptID != 0:
		judgeRecovery := (workflow == agent.WorkflowResearch || workflow == agent.WorkflowPlan) && node.Action == graphexec.JudgeAction
		if (!judgeRecovery && workflow != agent.WorkflowBug) || node.Action == ResearchAgentAction || req.RunID != 0 {
			return out, errors.New("only bounded factory effects may resume an attempt")
		}
		attempt, err = opts.DB.GetGraphNodeAttempt(ctx, req.ResumeAttemptID)
		if err == nil && (attempt.InstanceID != instance.ID || attempt.Node != node.Name || attempt.RunID != 0 || (attempt.Status != store.GraphRunning && attempt.Status != store.GraphPending)) {
			return out, errors.New("bug effect recovery attempt mismatch")
		}
	default:
		var ancestors map[int64]string
		if workflow == agent.WorkflowResearch {
			ancestors, err = researchBudgetAncestors(ctx, opts.DB, instance.ID, node)
			if err != nil {
				return out, err
			}
		}
		var prerequisite *store.BugSyncPrerequisite
		if gate, exists := graphexec.BugFixerSyncGate(req.Profile); exists {
			if fixer, valid := graphexec.Decide(req.Profile, gate.Name, "synced"); valid {
				prerequisite = &store.BugSyncPrerequisite{SyncNode: gate.Name, FixerNode: fixer}
			}
		}
		attempt, err = opts.DB.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{BugSyncPrerequisite: prerequisite, ResearchPriorNodes: ancestors, InstanceID: instance.ID, LaunchID: req.LaunchID, Node: node.Name, RunID: req.RunID, OwnerAllowanceSource: req.OwnerAllowanceSource, Limit: node.Limit, StartedAt: req.At})
	}
	if errors.Is(err, store.ErrGraphAttemptLimit) {
		out.Activity.Diagnostic = fmt.Sprintf("node %q dispatch limit %d exhausted", node.Name, node.Limit)
		if workflow == agent.WorkflowBug && node.Action == graphexec.SyncBaseAction {
			out.Activity.Diagnostic, err = graphBugSyncExhaustionDiagnostic(context.WithoutCancel(ctx), opts.DB, instance.ID, node.Name, out.Activity.Diagnostic)
			if err != nil {
				return out, err
			}
		}
		if graphexec.ParksOutcome(node, "exhausted") {
			return parkGraphAction(ctx, opts, req, out, "exhausted", false)
		}
		if err := opts.DB.SetGraphInstanceStatus(ctx, instance.ID, store.GraphFailed, time.Time{}, req.At); err != nil {
			return out, fmt.Errorf("record exhausted graph limit: %w", err)
		}
		out.Status = store.GraphFailed
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("reserve graph node: %w", err)
	}
	out.AttemptID = attempt.ID
	if !registered {
		out.Activity.Diagnostic = fmt.Sprintf("unknown graph activity %q", node.Action)
		return finishGraphAction(ctx, opts, req, out, false)
	}
	// Options' service pointers are intentionally shared dependencies. Clone its
	// mutable configuration maps before passing the value to a handler.
	handlerOpts := opts
	handlerOpts.Runners = maps.Clone(opts.Runners)
	handlerOpts.Tiers.ModelByTier = maps.Clone(opts.Tiers.ModelByTier)
	for kind, tiers := range handlerOpts.Tiers.ModelByTier {
		handlerOpts.Tiers.ModelByTier[kind] = maps.Clone(tiers)
	}
	activity, dispatchErr := handler.Execute(ctx, handlerOpts, ActivityRequest{
		Team: req.Profile.Team, Mode: req.Profile.Mode, Workflow: workflow, Node: node.Name, Attrs: node.Attrs,
		InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: req.RunID, ResumeRef: req.ResumeRef, Recovery: req.ResumeAttemptID != 0,
	})
	activity.Output = slices.Clone(activity.Output)
	out.Activity = activity
	if activity.Deferred && dispatchErr == nil {
		if err := opts.DB.DeferGraphNodeAttempt(context.WithoutCancel(ctx), attempt.ID, req.At); err != nil {
			return out, err
		}
		out.Deferred, out.NextNode = true, node.Name
		return out, nil
	}
	if workflow == agent.WorkflowBug && node.Action == graphexec.SyncBaseAction && (dispatchErr != nil || (activity.Outcome != "synced" && activity.Outcome != "current")) {
		out.Activity.Diagnostic = graphBugSyncDiagnostic(activity)
	}
	// Owner cancellation must park even when private result submission completed
	// before process cleanup; a stopped run cannot enter successful delivery.
	if workflow == agent.WorkflowBug && req.RunID != 0 {
		run, err := opts.DB.GetRun(context.WithoutCancel(ctx), req.RunID)
		if err != nil {
			return out, errors.Join(dispatchErr, err)
		}
		if _, stopped := run.OwnerStop(); stopped {
			out.Activity.Diagnostic = store.ReasonOwnerStop
			finished, err := parkGraphAction(ctx, opts, req, out, "failed", false)
			return finished, errors.Join(dispatchErr, err)
		}
	}
	// Owner-conversation activities atomically complete and park themselves,
	// because their retained review and wait belong to the same boundary.
	if activity.Suspended && workflow == agent.WorkflowBug && node.Action == graphexec.ResearchLoopAction && node.Attrs["lf_contract"] == "owner_conversation" {
		wait, waitErr := opts.DB.GetGraphOwnerWait(ctx, instance.ID)
		if waitErr != nil || wait.State != "waiting" || wait.SourceAttemptID != attempt.ID || wait.OriginNode != node.Name {
			return out, errors.Join(errors.New("conversation suspension lacks durable origin wait"), waitErr)
		}
		out.Status, out.NextNode = "waiting_owner", node.Name
		return out, dispatchErr
	}
	// A local discovery-budget deferral inside a Plan activity is transient
	// scheduling pressure: the failed attempt stays bounded by the node limit,
	// and the instance stays at the node for a later retry instead of failing.
	if dispatchErr != nil && workflow == agent.WorkflowPlan && errors.Is(dispatchErr, ticket.ErrDiscoveryDeferred) {
		if out.Activity.Diagnostic == "" {
			out.Activity.Diagnostic = dispatchErr.Error()
		}
		req.StopOnFailure, req.NextDueAt = false, req.RetryDueAt
		completion := store.GraphCompletion{Status: store.GraphFailed, FinishedAt: req.At, InstanceStatus: store.GraphRunning, NextDueAt: req.NextDueAt}
		completion.Outputs = append(completion.Outputs, store.GraphOutput{Kind: store.GraphOutputDiagnostic, Payload: []byte(out.Activity.Diagnostic)})
		if err := opts.DB.CompleteGraphNodeAttempt(ctx, out.AttemptID, completion); err != nil {
			return out, fmt.Errorf("complete deferred graph activity: %w", err)
		}
		out.Deferred, out.NextNode = true, node.Name
		return out, nil
	}
	if dispatchErr != nil {
		if out.Activity.Diagnostic == "" {
			out.Activity.Diagnostic = dispatchErr.Error()
		} else if graphexec.ParksOwner(node) {
			out.Activity.Diagnostic += "\nFactory dispatch diagnostic: " + dispatchErr.Error()
		}
		finished, finishErr := finishGraphAction(ctx, opts, req, out, false)
		return finished, errors.Join(fmt.Errorf("execute graph activity %q: %w", node.Action, dispatchErr), finishErr)
	}
	if activity.Suspended {
		return out, errors.New("graph suspension lacks a supported durable owner wait")
	}
	if graphexec.ParksOutcome(node, activity.Outcome) {
		return parkGraphAction(ctx, opts, req, out, activity.Outcome, true)
	}
	next, decided := graphexec.Decide(req.Profile, node.Name, activity.Outcome)
	if !decided {
		diagnostic := fmt.Sprintf("activity %q returned undeclared or ambiguous outcome %q", node.Action, activity.Outcome)
		if out.Activity.Diagnostic != "" {
			diagnostic = out.Activity.Diagnostic + "\n" + diagnostic
		}
		out.Activity.Diagnostic = diagnostic
		return finishGraphAction(ctx, opts, req, out, false)
	}
	if _, exists := req.Profile.Node(next); !exists {
		out.Activity.Diagnostic = fmt.Sprintf("activity %q targets unknown node %q", node.Action, next)
		return finishGraphAction(ctx, opts, req, out, false)
	}
	out.NextNode = next
	if activity.Outcome == "retry" && next == node.Name && req.RetryDueAt.After(req.At) {
		req.NextDueAt = req.RetryDueAt
	}
	return finishGraphAction(ctx, opts, req, out, true)
}

func finishGraphAction(ctx context.Context, opts Options, req GraphNodeRequest, out GraphNodeResult, succeeded bool) (GraphNodeResult, error) {
	if !succeeded {
		if node, ok := req.Profile.Node(out.Node); ok && graphexec.ParksOwner(node) {
			cause := "failed"
			if out.Activity.Outcome == "invalid" {
				cause = "invalid"
			}
			return parkGraphAction(ctx, opts, req, out, cause, false)
		}
	}
	completionCtx := ctx
	ownerStopped := false
	if !succeeded && req.RunID != 0 && ctx.Err() != nil {
		// An owner stop cancels the worker context; settle the attempt anyway so
		// the instance is not left running at this node until restart recovery.
		run, err := opts.DB.GetRun(context.WithoutCancel(ctx), req.RunID)
		if err != nil {
			return out, fmt.Errorf("read graph run: %w", err)
		}
		if _, ownerStopped = run.OwnerStop(); ownerStopped {
			completionCtx = context.WithoutCancel(ctx)
			if out.Activity.Diagnostic == "" {
				out.Activity.Diagnostic = store.ReasonOwnerStop
			} else if !strings.Contains(out.Activity.Diagnostic, store.ReasonOwnerStop) {
				out.Activity.Diagnostic = store.ReasonOwnerStop + "\n" + out.Activity.Diagnostic
			}
		}
	}
	completion := store.GraphCompletion{Status: store.GraphFailed, FinishedAt: req.At, InstanceStatus: store.GraphRunning, NextDueAt: req.NextDueAt}
	// Research never retries a failed activity without new owner authority.
	// Terminalizing only graph execution leaves the original run, authored
	// candidate and uncertain comment identity available for report recovery.
	if !succeeded && (req.StopOnFailure || string(req.Profile.Mode) == "research") {
		completion.InstanceStatus = store.GraphFailed
		completion.NextDueAt = time.Time{}
		out.Status = store.GraphFailed
	}
	// Match restart reconciliation: an owner-stopped Implement story is
	// cancelled, not failed, so the stop does not count as a story failure.
	if ownerStopped && req.Profile.Mode == agent.ModeImplement {
		completion.InstanceStatus = store.GraphCancelled
		completion.NextDueAt = time.Time{}
		out.Status = store.GraphCancelled
	}
	if succeeded {
		completion.Status, completion.Outcome = store.GraphSucceeded, out.Activity.Outcome
		completion.Transition = &store.GraphTransition{InstanceID: out.InstanceID, FromNode: out.Node, ToNode: out.NextNode, OnOutcome: out.Activity.Outcome, At: req.At}
	}
	if out.Activity.Kind != "" || out.Activity.Output != nil {
		kind := out.Activity.Kind
		if kind == "" {
			kind = store.GraphOutputText
		}
		completion.Outputs = append(completion.Outputs, store.GraphOutput{Kind: kind, Payload: slices.Clone(out.Activity.Output)})
	}
	if out.Activity.Diagnostic != "" {
		completion.Outputs = append(completion.Outputs, store.GraphOutput{Kind: store.GraphOutputDiagnostic, Payload: []byte(out.Activity.Diagnostic)})
	}
	if node, ok := req.Profile.Node(out.Node); !succeeded && ok && node.Action == graphexec.SyncBaseAction {
		// Preserve commit evidence from a possibly published sync despite cancellation.
		completionCtx = context.WithoutCancel(ctx)
	}
	if err := opts.DB.CompleteGraphNodeAttempt(completionCtx, out.AttemptID, completion); err != nil {
		return out, fmt.Errorf("complete graph activity: %w", err)
	}
	return out, nil
}

// researchAncestorLimit bounds the successor walk; real chains are a handful long.
const researchAncestorLimit = 64

// researchBudgetAncestors carries the existing per-stage allowance across graph
// successors. Renamed agent nodes match their captured role; effects match names.
// A failed or completed predecessor starts a fresh owner attempt, ending
// allowance inheritance.
func researchBudgetAncestors(ctx context.Context, db *store.DB, instance int64, node graphexec.Node) (map[int64]string, error) {
	out := make(map[int64]string)
	for range researchAncestorLimit { // bounded walk, not a retry loop
		prior, _, err := db.WorkflowPredecessor(ctx, instance)
		if err != nil || prior == 0 {
			return out, err
		}
		if _, cycle := out[prior]; cycle {
			return nil, errors.New("research successor cycle")
		}
		predecessor, err := db.GetGraphInstance(ctx, prior)
		if err != nil {
			return nil, err
		}
		if predecessor.Status == store.GraphFailed || predecessor.Status == store.GraphSucceeded {
			return out, nil
		}
		name := node.Name
		raw, err := db.ResearchLaunchFiles(ctx, prior)
		if err != nil {
			return nil, err
		}
		var files map[string][]byte
		if err := json.Unmarshal(raw, &files); err != nil {
			return nil, err
		}
		graphFile, err := retainedGraphFile(ctx, db, prior)
		if err != nil {
			return nil, err
		}
		if dot, found := files[graphFile]; found {
			profile, err := graphexec.ParseProfile(dot)
			if err != nil {
				return nil, err
			}
			if node.Action == ResearchAgentAction {
				for _, old := range profile.Nodes {
					if old.Action == node.Action && old.Attrs["lf_agent_markdown"] == node.Attrs["lf_agent_markdown"] {
						name = old.Name
						break
					}
				}
			}
		}
		out[prior] = name
		instance = prior
	}
	return nil, errors.New("research successor chain exceeds its bound")
}
