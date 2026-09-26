package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugAnalystInput is selected by the factory, never by role or graph text.
// InputDigest binds the complete immutable invocation input; replay keeps both
// the logical invocation and its digest. A new investigation needs a new ID.
type BugAnalystInput struct {
	SessionID, ExpectedRevision int64
	Invocation, InputDigest     string
}

// BugAnalystActivity adds the lifetime reservation and dispatch claim around
// the shared retained-result runner. Prepare must supply the same retained
// suffix to warm and cold requests and create only read-only native delivery.
// The complete workflow package must register this concrete handler before use.
type BugAnalystActivity struct {
	Resolve func(context.Context, ActivityRequest) (BugAnalystInput, error)
	Prepare func(context.Context, Options, ActivityRequest, agentresult.Context, string) (ResearchInvocation, error)
}

// Execute reserves and claims one logical analysis before invoking the shared runner.
func (h BugAnalystActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (out ActivityResult, err error) {
	if opts.DB == nil || h.Resolve == nil || h.Prepare == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return out, errors.New("bug analyst requires concrete factory preparation")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return out, err
	}
	input, err := h.Resolve(ctx, req)
	if err != nil {
		return out, err
	}
	if input.SessionID != instance.SessionID {
		return out, errors.New("bug analyst task mismatch")
	}
	pass, err := opts.DB.ReserveBugAnalysisPass(ctx, store.BugAnalysisPass{SessionID: input.SessionID, Invocation: input.Invocation, InputDigest: input.InputDigest, GraphAttemptID: req.NodeAttemptID}, input.ExpectedRevision)
	if err != nil {
		return out, err
	}
	// Recovery of already dispatched work is bookkeeping, never a second launch.
	if pass.State != "reserved" {
		return out, fmt.Errorf("bug invocation %s requires reconciliation of %s execution", pass.Invocation, pass.State)
	}
	defer func() {
		if err != nil {
			state := "failed"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			err = errors.Join(err, opts.DB.FinishBugAnalysisPass(context.WithoutCancel(ctx), input.SessionID, input.Invocation, state, err.Error()))
		}
	}()
	scope, retained, err := BugAnalystContext(ctx, opts.DB, input.SessionID, input.Invocation)
	if err != nil {
		return out, err
	}
	shared := AgentActivity{
		Policy: BugWorkflowPolicy(),
		Prepare: func(ctx context.Context, opts Options, req ActivityRequest) (ResearchInvocation, error) {
			invocation, err := h.Prepare(ctx, opts, req, scope, retained)
			if err != nil {
				return invocation, err
			}
			closeFailed := func(cause error) (ResearchInvocation, error) {
				if invocation.Close != nil {
					cause = errors.Join(cause, invocation.Close())
				}
				if invocation.Release != nil {
					cause = errors.Join(cause, invocation.Release())
				}
				return ResearchInvocation{}, cause
			}
			if !strings.Contains(invocation.Request.Prompt, retained) || (invocation.ColdRequest != nil && !strings.Contains(invocation.ColdRequest.Prompt, retained)) {
				return closeFailed(errors.New("bug analyst warm/cold context is incomplete"))
			}
			if invocation.Request.Workflow != agent.WorkflowBug || (invocation.ColdRequest != nil && invocation.ColdRequest.Workflow != agent.WorkflowBug) {
				return closeFailed(errors.New("bug analyst invocation changed workflow identity"))
			}
			if err := invocation.Request.ValidateBugAnalyst(); err != nil {
				return closeFailed(err)
			}
			if invocation.ColdRequest != nil {
				if err := invocation.ColdRequest.ValidateBugAnalyst(); err != nil {
					return closeFailed(err)
				}
			}
			if invocation.Results == nil {
				return closeFailed(errors.New("bug analyst missing scoped result service"))
			}
			bound, err := invocation.Results.trustedContext(ctx)
			if err != nil {
				return closeFailed(err)
			}
			if bound.Bug == nil || scope.Bug == nil || *bound.Bug != *scope.Bug {
				return closeFailed(errors.New("bug analyst result context changed"))
			}
			claimed, err := opts.DB.ClaimBugAnalysisDispatch(ctx, input.SessionID, input.Invocation, input.InputDigest, req.RunID)
			if err != nil {
				return closeFailed(err)
			}
			if !claimed {
				return closeFailed(errors.New("bug analyst dispatch already claimed"))
			}
			return invocation, nil
		},
	}
	out, err = shared.Execute(ctx, opts, req)
	// Success remains dispatched until the enclosing graph/run settlement is
	// durable. Record application or restart reconciliation calls the method below.
	return out, err
}

// ReconcileBugAnalystSuccess performs no provider execution. A successful shared
// receipt alone is insufficient: FinishBugAnalysisPass verifies the matching
// completed run, and ApplyBugAnalysis separately verifies graph consistency.
func ReconcileBugAnalystSuccess(ctx context.Context, db *store.DB, session int64, invocation string) error {
	if db == nil {
		return errors.New("bug reconciliation requires store")
	}
	return db.FinishBugAnalysisPass(ctx, session, invocation, "succeeded", "")
}
