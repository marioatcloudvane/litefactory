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
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// BugTestGraphAction names the durable child-graph invocation activity.
const BugTestGraphAction = "lf_bug_test_graph"

// BugTestSetupBlocked is a known missing/invalid recipe, distinct from execution
// or persistence failure. It authorizes neither a child pass nor delivery.
type BugTestSetupBlocked struct{ Reason string }

func (e *BugTestSetupBlocked) Error() string { return e.Reason }

// BugTestGraphSpec declares child invocation outcomes without interpreting graph success as passing.
func BugTestGraphSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugTestGraphAction, Workflow: agent.WorkflowBug, Outcomes: []string{"passed", "failed", "blocked", "cancelled"}}
}

// BugTestGraphActivity has only factory-bound call and observation inputs.
// Observe revalidates source, recipe, target and execution context on consumption.
type BugTestGraphActivity struct {
	Resolve func(context.Context, ActivityRequest) (store.BugTestRequest, error)
	Observe func(context.Context, store.BugCheckBinding) error
}

// RegisterBugTestGraph registers the concrete child invocation handler.
func RegisterBugTestGraph(specs *graphexec.Registry, activities *ActivityRegistry, h *BugTestGraphActivity) error {
	if specs == nil || activities == nil || h == nil || h.Resolve == nil || h.Observe == nil {
		return errors.New("incomplete Bug child factory binding")
	}
	if err := specs.Register(BugTestGraphSpec()); err != nil {
		return err
	}
	return activities.Register(BugTestGraphAction, h)
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (h *BugTestGraphActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || opts.MCP == nil || h.Resolve == nil || h.Observe == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("incomplete Bug test call context")
	}
	c, err := opts.DB.BugTestParentCall(ctx, req.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		r, e := h.Resolve(ctx, req)
		if e != nil {
			var blocked *BugTestSetupBlocked
			if errors.As(e, &blocked) {
				instance, readErr := opts.DB.GetGraphInstance(ctx, req.InstanceID)
				if readErr != nil {
					return ActivityResult{}, readErr
				}
				state, _, _, readErr := opts.DB.CurrentBugAnalysis(ctx, instance.SessionID)
				if readErr != nil {
					return ActivityResult{}, readErr
				}
				writeErr := opts.DB.RecordBugFailure(ctx, instance.SessionID, state.Revision, fmt.Sprintf("test-setup:%d", req.NodeAttemptID), blocked.Reason, []string{"factory target recipe validation"})
				raw, marshalErr := json.Marshal(BugChecksOutput{SessionID: instance.SessionID, Reason: blocked.Reason})
				return ActivityResult{Outcome: "blocked", Kind: store.GraphOutputJSON, Output: raw}, errors.Join(writeErr, marshalErr)
			}
			return ActivityResult{}, e
		}
		if r.ParentInstance != req.InstanceID || r.ParentAttempt != req.NodeAttemptID {
			return ActivityResult{}, errors.New("bug parent call attempt mismatch")
		}
		if e = h.Observe(ctx, r.Binding); e != nil {
			return ActivityResult{}, e
		}
		c, err = opts.DB.CreateBugTestCall(ctx, r, opts.MCP.Now())
	}
	if err != nil {
		return ActivityResult{}, err
	}
	if c.Outcome == "pending" {
		return ActivityResult{Suspended: true}, nil
	}
	if c.Request.ParentAttempt != req.NodeAttemptID {
		return ActivityResult{}, errors.New("bug child belongs to another parent attempt")
	}
	if c.Outcome == "passed" && !c.Cancelled && c.Current {
		if e := h.Observe(ctx, c.Request.Binding); e != nil {
			if err = opts.DB.InvalidateBugTestCall(ctx, c.ID, e.Error()); err != nil {
				return ActivityResult{}, err
			}
			c.Current = false
			c.Reason = e.Error()
		}
	}
	outcome := c.Outcome
	if c.Cancelled {
		outcome = "cancelled"
	} else if !c.Current {
		outcome = "blocked"
	}
	raw, err := json.Marshal(struct {
		CallID int64  `json:"call_id"`
		Reason string `json:"reason,omitempty"`
	}{c.ID, c.Reason})
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

// BugTestChild is a bounded executor of one pinned child node per dispatch.
// Resolve builds the normal check activity input under the operator ceiling.
// No agent runner, result service or forge mutation is granted to the child.
type BugTestChild struct {
	db      *store.DB
	profile *graphexec.Profile
	checks  *BugChecksActivity
	resolve func(context.Context, store.BugTestCall, string) (BugChecksInput, error)
}

// BugTestChildOptions binds the pinned child profile, check activity and factory input resolver.
type BugTestChildOptions struct {
	DB      *store.DB
	Profile *graphexec.Profile
	Checks  *BugChecksActivity
	Resolve func(context.Context, store.BugTestCall, string) (BugChecksInput, error)
}

// NewBugTestChild validates the child bindings before constructing its executor.
func NewBugTestChild(o BugTestChildOptions) (*BugTestChild, error) {
	if o.DB == nil || o.Checks == nil || o.Resolve == nil {
		return nil, errors.New("bug child requires store, checks and factory input resolver")
	}
	if err := teamsource.ValidateBugTests(o.Profile); err != nil {
		return nil, err
	}
	profile := *o.Profile
	profile.Attrs = maps.Clone(o.Profile.Attrs)
	profile.Nodes = slices.Clone(o.Profile.Nodes)
	for i := range profile.Nodes {
		profile.Nodes[i].Attrs = maps.Clone(profile.Nodes[i].Attrs)
		profile.Nodes[i].Outcomes = slices.Clone(profile.Nodes[i].Outcomes)
	}
	profile.Edges = slices.Clone(o.Profile.Edges)
	for i := range profile.Edges {
		profile.Edges[i].Attrs = maps.Clone(profile.Edges[i].Attrs)
	}
	return &BugTestChild{db: o.DB, profile: &profile, checks: o.Checks, resolve: o.Resolve}, nil
}

// Advance is called only by a reserved daemon dispatch under task ownership.
// It advances at most one node and leaves the next dispatch durable.
func (h *BugTestChild) Advance(ctx context.Context, dispatch store.GraphDispatch, at time.Time) error {
	if at.IsZero() || dispatch.ID <= 0 {
		return errors.New("bug child dispatch needs identity and timestamp")
	}
	c, err := h.db.BugTestDispatchCall(ctx, dispatch.ID)
	if err != nil {
		return err
	}
	if c.ChildInstance != dispatch.InstanceID || c.Request.SessionID != dispatch.SessionID {
		return errors.New("bug child dispatch mismatch")
	}
	if c.Outcome != "pending" {
		return h.db.FinishGraphDispatch(ctx, dispatch.ID, at)
	}
	claimed, err := h.db.ClaimBugTestDispatch(ctx, dispatch)
	if err != nil || !claimed {
		return err
	}
	instance, err := h.db.GetGraphInstance(ctx, c.ChildInstance)
	if err != nil {
		return err
	}
	pin, err := h.db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	if pin.Digest != h.profile.Digest || pin.Digest != c.Request.DefinitionDigest || pin.Path != "bug_tests.dot" {
		return errors.New("bug child pinned definition mismatch")
	}
	phase := instance.CurrentNode
	if phase != dispatch.Node {
		return errors.New("bug child dispatch is stale for its current node")
	}
	node, ok := h.profile.Node(phase)
	if !ok {
		return errors.New("bug child node missing")
	}
	if node.Terminal {
		_, err = h.db.SettleBugTestCall(ctx, c.ID, at)
		return err
	}
	b := c.PhaseBinding(phase)
	retained, lookupErr := h.db.GetBugCheckPhase(ctx, c.Request.SessionID, b.Operation)
	if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		return lookupErr
	}
	if c.Cancelled {
		if lookupErr == nil {
			if err = h.checks.Cancel(ctx, h.db, c.Request.SessionID, b.Operation); err != nil {
				return err
			}
		}
		_, err = h.reconcileCall(ctx, c, at)
		return err
	}
	if lookupErr == nil {
		var quiet bool
		retained, quiet, err = h.checks.Reconcile(ctx, h.db, c.Request.SessionID, b.Operation)
		if err != nil {
			return err
		}
		if !quiet {
			return h.db.FinishGraphDispatch(ctx, dispatch.ID, at)
		}
	}
	attempt, attemptErr := h.db.LatestGraphNodeAttempt(ctx, c.ChildInstance, phase)
	if attemptErr != nil && !errors.Is(attemptErr, store.ErrNotFound) {
		return attemptErr
	}
	// Retained command completion after a crash finishes the original graph
	// attempt. Starting another command invocation is never a recovery shortcut.
	if attemptErr == nil && attempt.Status == store.GraphRunning {
		if lookupErr != nil || retained.Outcome == "pending" {
			in, e := h.resolveInput(ctx, c, phase)
			if e != nil {
				return e
			}
			if _, e = h.checks.RunPhase(ctx, h.db, in); e != nil {
				return e
			}
			retained, e = h.db.GetBugCheckPhase(ctx, c.Request.SessionID, b.Operation)
			if e != nil {
				return e
			}
		}
		if retained.Outcome == "pending" || !retained.Cleanup {
			return h.db.FinishGraphDispatch(ctx, dispatch.ID, at)
		}
		next, ok := graphexec.Decide(h.profile, phase, retained.Outcome)
		if !ok {
			return errors.New("bug child recovery outcome has no route")
		}
		if err = h.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: retained.Outcome, FinishedAt: at, Transition: &store.GraphTransition{InstanceID: c.ChildInstance, FromNode: phase, ToNode: next, OnOutcome: retained.Outcome, At: at}}); err != nil {
			return err
		}
	} else {
		var activities ActivityRegistry
		if err = activities.Register(BugChecksAction, bugChildPhase{child: h, call: c}); err != nil {
			return err
		}
		_, err = ExecuteNodeAction(ctx, Options{DB: h.db}, GraphNodeRequest{StopOnFailure: true, Profile: h.profile, Activities: activities, InstanceID: c.ChildInstance, At: at})
		if err != nil {
			return err
		}
	}
	settled, err := h.db.SettleBugTestCall(ctx, c.ID, at)
	if err != nil {
		return err
	}
	if settled.Outcome != "pending" {
		return nil
	}
	instance, err = h.db.GetGraphInstance(ctx, c.ChildInstance)
	if err != nil {
		return err
	}
	if instance.CurrentNode == phase {
		return h.db.FinishGraphDispatch(ctx, dispatch.ID, at)
	}
	_, err = h.db.ParkGraphInstanceAfter(ctx, c.ChildInstance, instance.CurrentNode, at, at, dispatch.ID)
	return err
}
func (h *BugTestChild) resolveInput(ctx context.Context, c store.BugTestCall, phase string) (BugChecksInput, error) {
	in, err := h.resolve(ctx, c, phase)
	if err != nil {
		return in, err
	}
	if in.Binding != c.PhaseBinding(phase) {
		return in, errors.New("bug child resolver changed pinned phase inputs")
	}
	return in, nil
}

type bugChildPhase struct {
	child *BugTestChild
	call  store.BugTestCall
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (h bugChildPhase) Execute(ctx context.Context, _ Options, r ActivityRequest) (ActivityResult, error) {
	in, err := h.child.resolveInput(ctx, h.call, r.Node)
	if err != nil {
		return ActivityResult{}, err
	}
	result, err := h.child.checks.RunPhase(ctx, h.child.db, in)
	if err != nil {
		return result, err
	}
	phase, e := h.child.db.GetBugCheckPhase(ctx, h.call.Request.SessionID, in.Binding.Operation)
	if e != nil {
		return result, e
	}
	if phase.Outcome == "pending" || !phase.Cleanup {
		return ActivityResult{Suspended: true}, nil
	}
	return result, nil
}
func (h *BugTestChild) reconcileCall(ctx context.Context, c store.BugTestCall, at time.Time) (store.BugTestCall, error) {
	for _, phase := range []string{"regression", "focused", "full"} {
		b := c.PhaseBinding(phase)
		_, err := h.db.GetBugCheckPhase(ctx, c.Request.SessionID, b.Operation)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			return c, err
		}
		_, quiet, err := h.checks.Reconcile(ctx, h.db, c.Request.SessionID, b.Operation)
		if err != nil {
			return c, err
		}
		if !quiet {
			return c, nil
		}
	}
	instance, err := h.db.GetGraphInstance(ctx, c.ChildInstance)
	if err != nil {
		return c, err
	}
	if instance.Status == store.GraphFailed {
		reason := c.Reason
		if reason == "" {
			reason = "test child graph failed before retained terminal evidence"
		}
		if err = h.db.FailBugTestCall(ctx, c.ID, reason, at); err != nil {
			return c, err
		}
		return h.db.GetBugTestCall(ctx, c.ID)
	}
	return h.db.SettleBugTestCall(ctx, c.ID, at)
}

// Reconcile runs before generic orphan handling; it never restarts a command.
func (h *BugTestChild) Reconcile(ctx context.Context, at time.Time) error {
	calls, err := h.db.ListUnresolvedBugTests(ctx)
	if err != nil {
		return err
	}
	var faults []error
	for _, c := range calls {
		settled, e := h.reconcileCall(ctx, c, at)
		if e != nil {
			faults = append(faults, fmt.Errorf("bug child %d: %w", c.ID, e))
			continue
		}
		if settled.Outcome != "pending" {
			continue
		}
		instance, e := h.db.GetGraphInstance(ctx, c.ChildInstance)
		if e != nil {
			faults = append(faults, e)
			continue
		}
		b := c.PhaseBinding(instance.CurrentNode)
		phase, e := h.db.GetBugCheckPhase(ctx, c.Request.SessionID, b.Operation)
		if e == nil && !phase.Cleanup {
			continue
		}
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			faults = append(faults, e)
			continue
		}
		e = h.db.RecoverBugTestDispatch(ctx, c.ID, at)
		if e != nil {
			faults = append(faults, e)
		}
	}
	return errors.Join(faults...)
}

// Cancel persists cancellation and propagates it to active child processes.
func (h *BugTestChild) Cancel(ctx context.Context, callID int64, at time.Time) error {
	if err := h.db.CancelBugTestCall(ctx, callID); err != nil {
		return err
	}
	c, err := h.db.GetBugTestCall(ctx, callID)
	if err != nil {
		return err
	}
	for _, phase := range []string{"regression", "focused", "full"} {
		b := c.PhaseBinding(phase)
		_, e := h.db.GetBugCheckPhase(ctx, c.Request.SessionID, b.Operation)
		if errors.Is(e, store.ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		if e = h.checks.Cancel(ctx, h.db, c.Request.SessionID, b.Operation); e != nil {
			return e
		}
	}
	_, err = h.reconcileCall(ctx, c, at)
	return err
}
