package turn

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// GraphNodeRequest supplies a validated, frozen profile and activity registry.
// At is the caller's logical timestamp for this bounded step, not measured
// handler duration. NextDueAt is supplied scheduling policy, never computed here.
// Callers serialize steps for an instance; in-flight crash recovery is deferred.
type GraphNodeRequest struct {
	Profile           *graphexec.Profile
	Activities        ActivityRegistry
	InstanceID, RunID int64
	ResumeRef         string
	At, NextDueAt     time.Time
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
// This entry point deliberately has no production caller until mode wiring.
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
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return out, fmt.Errorf("execute graph node definition: %w", err)
	}
	if definition.Digest != req.Profile.Digest || definition.Team != req.Profile.Team || definition.Mode != string(req.Profile.Mode) || definition.ProfileVersion != req.Profile.ProfileVersion {
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
	attempt, err := opts.DB.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: node.Name, RunID: req.RunID, Limit: node.Limit, StartedAt: req.At})
	if errors.Is(err, store.ErrGraphAttemptLimit) {
		out.Activity.Diagnostic = fmt.Sprintf("node %q dispatch limit %d exhausted", node.Name, node.Limit)
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
		Team: req.Profile.Team, Mode: req.Profile.Mode, Node: node.Name, Attrs: node.Attrs,
		InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: req.RunID, ResumeRef: req.ResumeRef,
	})
	activity.Output = slices.Clone(activity.Output)
	out.Activity = activity
	if dispatchErr != nil {
		if out.Activity.Diagnostic == "" {
			out.Activity.Diagnostic = dispatchErr.Error()
		}
		finished, finishErr := finishGraphAction(ctx, opts, req, out, false)
		return finished, errors.Join(fmt.Errorf("execute graph activity %q: %w", node.Action, dispatchErr), finishErr)
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
	completion := store.GraphCompletion{Status: store.GraphFailed, FinishedAt: req.At, InstanceStatus: store.GraphRunning, NextDueAt: req.NextDueAt}
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
	if err := opts.DB.CompleteGraphNodeAttempt(ctx, out.AttemptID, completion); err != nil {
		return out, fmt.Errorf("complete graph activity: %w", err)
	}
	return out, nil
}
