package turn

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugAttemptAction names the approval-bound repair reservation activity.
const BugAttemptAction = "lf_bug_attempt"

// BugAttemptSpec declares the closed repair reservation outcomes.
func BugAttemptSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugAttemptAction, Workflow: agent.WorkflowBug, Outcomes: []string{"reserved", "exhausted", "reshape", "blocked"}}
}

// BugAttemptInput is selected by the factory from current domain records.
type BugAttemptInput struct {
	Request          store.BugRepairRequest
	ExpectedRevision int64
	ReshapeReason    string
	// BlockedReason ends the attempt blocked before any reservation, as when
	// main cannot be merged into the repair branch without conflicts.
	BlockedReason string
}

// BugAttemptActivity reserves one shared lifetime repair slot before the fixer.
type BugAttemptActivity struct {
	Profile *graphexec.Profile
	Resolve func(context.Context, ActivityRequest) (BugAttemptInput, error)
}

// RegisterBugAttempt registers the concrete repair reservation handler.
func RegisterBugAttempt(specs *graphexec.Registry, activities *ActivityRegistry, h BugAttemptActivity) error {
	if specs == nil || activities == nil || h.Profile == nil || h.Resolve == nil {
		return errors.New("bug attempt needs pinned factory bindings")
	}
	if _, ok := activities.Resolve(BugAttemptAction); ok {
		return errors.New("bug attempt already registered")
	}
	if err := specs.Register(BugAttemptSpec()); err != nil {
		return err
	}
	return activities.Register(BugAttemptAction, h)
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (h BugAttemptActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("bug attempt missing factory binding")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != BugAttemptAction || definition.Workflow != "bug" || definition.Mode != "" || definition.Digest != h.Profile.Digest || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("bug attempt profile mismatch")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug attempt graph mismatch")
	}
	input, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if input.Request.SessionID != instance.SessionID || input.Request.GraphAttemptID != req.NodeAttemptID {
		return ActivityResult{}, errors.New("bug attempt source mismatch")
	}
	outcome, reason := "reserved", ""
	var reservation store.BugRepair
	switch {
	case input.BlockedReason != "":
		outcome, reason = "blocked", input.BlockedReason
	case input.ReshapeReason != "":
		outcome, reason = "reshape", input.ReshapeReason
	default:
		reservation, err = opts.DB.ReserveBugRepair(ctx, input.Request, input.ExpectedRevision)
		switch {
		case errors.Is(err, store.ErrBugRepairExhausted):
			outcome, reason = "exhausted", err.Error()
		case errors.Is(err, store.ErrBugRepairUnauthorized), errors.Is(err, store.ErrBugWorkActive):
			outcome, reason = "blocked", err.Error()
		case err != nil:
			return ActivityResult{}, err
		}
	}
	if reason != "" {
		state, _, _, readErr := opts.DB.CurrentBugAnalysis(ctx, instance.SessionID)
		if readErr != nil {
			return ActivityResult{}, readErr
		}
		writeErr := opts.DB.RecordBugFailure(ctx, instance.SessionID, state.Revision, "repair-outcome:"+input.Request.Request, reason, []string{input.Request.Evidence})
		if writeErr != nil {
			return ActivityResult{}, writeErr
		}
	}
	raw, err := json.Marshal(struct {
		Reservation store.BugRepair `json:"reservation"`
		Reason      string          `json:"reason,omitempty"`
	}{reservation, reason})
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}
