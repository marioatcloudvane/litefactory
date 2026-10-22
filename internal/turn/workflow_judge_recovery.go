package turn

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// RecoverWorkflowJudges reconciles report-only bookkeeping before general graph
// interruption handling. It runs at quiescent startup, never launches a provider
// or publishes advice, and leaves source attempts and owner gates untouched.
func RecoverWorkflowJudges(ctx context.Context, opts Options, at time.Time) error {
	if opts.Judge == nil {
		return nil
	}
	if opts.DB == nil || at.IsZero() {
		return errors.New("judge recovery requires database and factory timestamp")
	}
	after := int64(0)
	for {
		attempts, err := opts.DB.ListWorkflowFactoryAttempts(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, attempt := range attempts {
			after = attempt.ID
			if attempt.LaunchID == 0 {
				continue
			}
			capture, err := opts.DB.GetWorkflowCapture(ctx, attempt.LaunchID)
			if err != nil {
				return err
			}
			instance, err := opts.DB.GetGraphInstance(ctx, attempt.InstanceID)
			if err != nil {
				return err
			}
			pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
			if err != nil {
				return err
			}
			workflow, err := agent.ResolveWorkflow(agent.Workflow(pin.Workflow), agent.Mode(pin.Mode))
			if err != nil {
				return err
			}
			var specs graphexec.Registry
			if workflow == agent.WorkflowResearch {
				specs, err = ResearchSpecs()
			} else {
				specs, err = PlanSpecs()
			}
			if err != nil {
				return err
			}
			definition, err := restoreWorkflow(ctx, opts.DB, capture, workflow, specs)
			if err != nil {
				return err
			}
			node, ok := definition.Profile.Node(attempt.Node)
			if !ok || node.Action != graphexec.JudgeAction {
				continue
			}
			recoveryAt := at
			if recoveryAt.Before(attempt.StartedAt) {
				recoveryAt = attempt.StartedAt
			}
			step, err := recoverWorkflowJudgeAttempt(ctx, opts, attempt, definition, recoveryAt)
			if err != nil {
				return err
			}
			if step.Deferred {
				if opts.Logger != nil && step.Activity.Diagnostic != "" {
					opts.Logger.InfoContext(ctx, step.Activity.Diagnostic)
				}
				if attempt.Status == store.GraphRunning {
					if err := opts.DB.DeferGraphNodeAttempt(ctx, attempt.ID, recoveryAt); err != nil {
						return err
					}
				}
				if _, err := opts.DB.ParkCapturedGraphInstanceAfter(ctx, instance.ID, attempt.Node, recoveryAt.Add(opts.GraphRetryDelay), recoveryAt, 0, attempt.LaunchID); err != nil {
					return err
				}
				continue
			}
			// A previously deferred recovery owns an owed dispatch at the old
			// node. Replace it after bookkeeping advances, keeping its capture.
			if attempt.Status == store.GraphPending {
				dispatch, err := opts.DB.LatestGraphDispatch(ctx, instance.ID)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				if err == nil && dispatch.Node == attempt.Node && step.Status != store.GraphRunning {
					if err := opts.DB.FinishGraphDispatch(ctx, dispatch.ID, recoveryAt); err != nil {
						return err
					}
				}
				if err == nil && dispatch.Node == attempt.Node && step.NextNode != attempt.Node && step.Status == store.GraphRunning {
					if _, err := opts.DB.ParkCapturedGraphInstanceAfter(ctx, instance.ID, step.NextNode, recoveryAt, recoveryAt, dispatch.ID, attempt.LaunchID); err != nil {
						return err
					}
				}
			}
		}
		if len(attempts) < 100 {
			return nil
		}
	}
}

func recoverWorkflowJudgeAttempt(ctx context.Context, opts Options, attempt store.GraphNodeAttempt, definition *teamsource.WorkflowDefinition, at time.Time) (GraphNodeResult, error) {
	if at.Before(attempt.StartedAt) {
		at = attempt.StartedAt
	}
	handler := JudgeActivity{Profile: definition.Profile, Service: opts.Judge, Context: func(ctx context.Context, a ActivityRequest) (JudgeRequest, error) {
		input, err := workflowJudgeContext(ctx, opts, a)
		if err != nil {
			return input, err
		}
		input.RecoverOnly = true
		// Recovery only retrieves saved reports or records interruption; historical
		// captures need not acquire instructions for a launch that cannot occur.
		input.Instructions, _ = teamsource.WorkflowJudgeCapture(definition)
		return input, nil
	}}
	var activities ActivityRegistry
	if err := activities.Register(graphexec.JudgeAction, handler); err != nil {
		return GraphNodeResult{}, err
	}
	return ExecuteNodeAction(ctx, opts, GraphNodeRequest{LaunchID: attempt.LaunchID, Profile: definition.Profile, Activities: activities, InstanceID: attempt.InstanceID, ResumeAttemptID: attempt.ID, At: at, NextDueAt: at, StopOnFailure: true})
}
