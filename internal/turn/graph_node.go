package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// GraphNodeRequest supplies a validated, frozen profile and activity registry.
// At is the caller's logical timestamp for this bounded step, not measured
// handler duration. NextDueAt is supplied scheduling policy, never computed here.
// Callers serialize steps for an instance; in-flight crash recovery is deferred.
type GraphNodeRequest struct {
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
}

// GraphNodeResult describes the recorded step. AttemptID is zero for terminals
// and exhausted limits. Status is the instance status, not the activity outcome.
type GraphNodeResult struct {
	InstanceID, AttemptID  int64
	Node, NextNode, Status string
	Activity               ActivityResult
}

// ExecuteNodeAction performs at most one activity. Reservation commits before
// dispatch; completion atomically writes its output, transition and progress.
// The Research driver supplies the registered mode activities in production.
func ExecuteNodeAction(ctx context.Context, opts Options, req GraphNodeRequest) (GraphNodeResult, error) {
	out := GraphNodeResult{InstanceID: req.InstanceID}
	if opts.DB == nil || req.Profile == nil || req.InstanceID <= 0 || req.RunID < 0 || req.At.IsZero() {
		return out, errors.New("execute graph node: database, profile, instance and timestamp are required; run ID must be nonnegative")
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
	var attempt store.GraphNodeAttempt
	switch {
	case req.ResumeAttemptID != 0:
		if workflow != agent.WorkflowBug || node.Action == ResearchAgentAction || req.RunID != 0 {
			return out, errors.New("only Bug factory effects may resume an attempt")
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
	handler, ok := req.Activities.Resolve(node.Action)
	if !ok {
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
		InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: req.RunID, ResumeRef: req.ResumeRef,
	})
	activity.Output = slices.Clone(activity.Output)
	out.Activity = activity
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
	completion := store.GraphCompletion{Status: store.GraphFailed, FinishedAt: req.At, InstanceStatus: store.GraphRunning, NextDueAt: req.NextDueAt}
	// Research never retries a failed activity without new owner authority.
	// Terminalizing only graph execution leaves the original run, authored
	// candidate and uncertain comment identity available for report recovery.
	if !succeeded && (req.StopOnFailure || string(req.Profile.Mode) == "research") {
		completion.InstanceStatus = store.GraphFailed
		completion.NextDueAt = time.Time{}
		out.Status = store.GraphFailed
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
	completionCtx := ctx
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
// A failed predecessor starts a fresh owner attempt, ending allowance inheritance.
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
		if predecessor.Status == store.GraphFailed {
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
