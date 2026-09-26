package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugChecksAction names the trusted recipe phase activity.
const BugChecksAction = "lf_bug_checks"

// BugChecksSpec declares passing, ordinary failure and blocked phase outcomes.
func BugChecksSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugChecksAction, Workflow: agent.WorkflowBug, Outcomes: []string{"passed", "failed", "blocked", "cancelled"}, Attributes: map[string]graphexec.AttributeSpec{"lf_phase": {Required: true, Values: []string{"regression", "focused", "full"}}}}
}

// BugChecksInput binds trusted recipe execution to source, target and isolation observations.
type BugChecksInput struct {
	Binding   store.BugCheckBinding
	Worktree  *git.Worktree
	Isolation bugchecks.Isolation
	// Observe validates target repository/branch/tip and environment/permission
	// revisions through factory-owned observations. It must not trust graph attrs.
	Observe func(context.Context, store.BugCheckBinding) error
}

// BugChecksOutput reports a phase outcome with retained evidence references.
type BugChecksOutput struct {
	SessionID int64  `json:"session_id"`
	Operation string `json:"operation"`
	Reason    string `json:"reason,omitempty"`
}

// BugChecksActivity owns active cancellation handles. The store retains intent
// across restart; no recovered operation is relaunched based on a bare PID.
type BugChecksActivity struct {
	// ObserveStopped optionally supplies an operator-owned process observer. Nil
	// selects the concrete process-group observer; graph input cannot set it.
	ObserveStopped func(context.Context, string) (bool, error)
	Profile        *graphexec.Profile
	Resolve        func(context.Context, ActivityRequest) (BugChecksInput, error)
	mu             sync.Mutex
	active         map[string]context.CancelFunc
}

// RegisterBugChecks registers the concrete trusted recipe phase handler.
func RegisterBugChecks(specs *graphexec.Registry, activities *ActivityRegistry, h *BugChecksActivity) error {
	if specs == nil || activities == nil || h == nil || h.Profile == nil || h.Resolve == nil {
		return errors.New("bug checks require complete factory bindings")
	}
	if _, ok := activities.Resolve(BugChecksAction); ok {
		return errors.New("bug checks already registered")
	}
	if err := specs.Register(BugChecksSpec()); err != nil {
		return err
	}
	return activities.Register(BugChecksAction, h)
}
func bugCheckKey(session int64, operation string) string {
	return fmt.Sprintf("%d:%s", session, operation)
}

// Cancel records intent before stopping the locally active command.
func (h *BugChecksActivity) Cancel(ctx context.Context, db *store.DB, session int64, operation string) error {
	if db == nil {
		return errors.New("bug check cancellation requires store")
	}
	if err := db.CancelBugCheckPhase(ctx, session, operation); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cancel := h.active[bugCheckKey(session, operation)]; cancel != nil {
		cancel()
	}
	return nil
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (h *BugChecksActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("bug checks require standalone factory context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != BugChecksAction || pin.Workflow != "bug" || pin.Mode != "" || pin.Digest != h.Profile.Digest || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("bug check graph binding mismatch")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug check attempt binding mismatch")
	}
	in, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if in.Binding.SessionID != instance.SessionID || in.Binding.Phase != node.Attrs["lf_phase"] {
		return ActivityResult{}, errors.New("bug check task/phase mismatch")
	}
	return h.RunPhase(ctx, opts.DB, in)
}
func checkActivityResult(b store.BugCheckBinding, outcome, reason string) (ActivityResult, error) {
	raw, err := json.Marshal(BugChecksOutput{b.SessionID, b.Operation, reason})
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

// RunPhase runs the existing generation once. It does not allocate children,
// generations or repair slots and performs no retries.
func (h *BugChecksActivity) RunPhase(ctx context.Context, db *store.DB, in BugChecksInput) (ActivityResult, error) {
	b := in.Binding
	if db == nil || in.Worktree == nil || in.Observe == nil || b.PermissionRevision != in.Isolation.Revision {
		return ActivityResult{}, errors.New("incomplete Bug check factory input")
	}
	key := bugCheckKey(b.SessionID, b.Operation)
	runCtx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	if h.active == nil {
		h.active = map[string]context.CancelFunc{}
	}
	if h.active[key] != nil {
		h.mu.Unlock()
		cancel()
		return ActivityResult{}, errors.New("bug check already executing")
	}
	h.active[key] = cancel
	h.mu.Unlock()
	defer func() { cancel(); h.mu.Lock(); delete(h.active, key); h.mu.Unlock() }()
	observe := func() error {
		if err := in.Worktree.ObserveBugCheckSource(runCtx, b.SourceCommit, b.SourceTree); err != nil {
			return err
		}
		return in.Observe(runCtx, b)
	}
	// Replays resolve the retained recipe, never the mutable repair branch.
	retained, err := db.GetBugCheckPhase(runCtx, b.SessionID, b.Operation)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	var recipe []byte
	if err == nil {
		recipe = retained.Recipe
	} else {
		recipe, err = in.Worktree.ReadBugCheckRecipe(runCtx, b.TargetCommit)
		if err != nil {
			return retainCheckSetupBlock(runCtx, db, b, err)
		}
	}
	parsed, err := config.ParseBugCheckRecipe(recipe)
	if err != nil {
		return retainCheckSetupBlock(runCtx, db, b, err)
	}
	phase, err := parsed.Phase(b.Phase)
	if err != nil {
		return ActivityResult{}, err
	}
	retained, err = db.EnsureBugCheckPhase(runCtx, b, recipe)
	if err != nil {
		return ActivityResult{}, err
	}
	if !retained.Current {
		return checkActivityResult(b, "blocked", "retained check evidence is stale or unreliable")
	}
	if retained.Outcome != "pending" {
		if retained.Outcome == "passed" {
			if err = observe(); err != nil {
				if invalidErr := db.InvalidateBugCheckPhase(runCtx, b.SessionID, b.Operation, err.Error()); invalidErr != nil {
					return ActivityResult{}, invalidErr
				}
				return retainCheckSetupBlock(runCtx, db, b, err)
			}
		}
		return checkActivityResult(b, retained.Outcome, retained.Reason)
	}
	claimed, err := db.ClaimBugWork(runCtx, b.SessionID, b.Operation, "checks")
	if err != nil {
		return ActivityResult{}, err
	}
	if !claimed {
		return checkActivityResult(b, "blocked", "worktree is owned by repair, save or another child")
	}
	finish := func(outcome, reason string, current bool) (ActivityResult, error) {
		cleanupCtx := context.WithoutCancel(ctx)
		if err := db.FinishBugCheckPhase(cleanupCtx, b.SessionID, b.Operation, outcome, reason, current); err != nil {
			return ActivityResult{}, err
		}
		saved, err := db.GetBugCheckPhase(cleanupCtx, b.SessionID, b.Operation)
		if err != nil {
			return ActivityResult{}, err
		}
		if saved.Cleanup {
			if err = db.ReleaseBugWork(cleanupCtx, b.SessionID, b.Operation); err != nil {
				return ActivityResult{}, err
			}
		}
		return checkActivityResult(b, outcome, reason)
	}
	if retained.Cancelled {
		return finish("cancelled", "check cancellation requested", false)
	}
	if retained.Unreliable {
		return finish("blocked", "known unreliable checks require a changed binding", false)
	}
	if err = in.Isolation.Validate(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	if err = observe(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	for ordinal, command := range phase.Commands {
		if ordinal < len(retained.Commands) {
			old := retained.Commands[ordinal]
			if old.Ordinal != ordinal {
				return ActivityResult{}, errors.New("bug check command ordinal mismatch")
			}
			if old.State != "completed" {
				return checkActivityResult(b, "blocked", "command launch or cleanup remains uncertain; reconciliation required")
			}
			if old.Observation.Outcome != "passed" {
				return finish(old.Observation.Outcome, old.Observation.Reason, false)
			}
			continue
		}
		if err = observe(); err != nil {
			return finish("blocked", err.Error(), false)
		}
		latest, e := db.GetBugCheckPhase(runCtx, b.SessionID, b.Operation)
		if e != nil {
			return ActivityResult{}, e
		}
		if latest.Cancelled {
			return finish("cancelled", "check cancellation requested", false)
		}
		launched, e := db.ClaimBugCheckCommand(runCtx, b.SessionID, b.Operation, ordinal)
		if e != nil {
			return ActivityResult{}, e
		}
		if !launched {
			return checkActivityResult(b, "blocked", "command claim refused or already retained")
		}
		observation := in.Isolation.Run(runCtx, in.Worktree.Path(), command, func(o bugchecks.CommandResult) error {
			return db.ObserveBugCheckCommand(runCtx, b.SessionID, b.Operation, ordinal, false, checkObservation(o))
		})
		cleanupCtx := context.WithoutCancel(ctx)
		if runCtx.Err() != nil {
			if e = db.CancelBugCheckPhase(cleanupCtx, b.SessionID, b.Operation); e != nil {
				return ActivityResult{}, e
			}
		}
		if e = db.ObserveBugCheckCommand(cleanupCtx, b.SessionID, b.Operation, ordinal, true, checkObservation(observation)); e != nil {
			return ActivityResult{}, e
		}
		if observation.Outcome != "passed" {
			return finish(observation.Outcome, observation.Reason, false)
		}
		if e = observe(); e != nil {
			return finish("blocked", e.Error(), false)
		}
	}
	if err = observe(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	return finish("passed", "", true)
}
func checkObservation(o bugchecks.CommandResult) store.BugCheckObservation {
	return store.BugCheckObservation{ExecutableDigest: o.ExecutableDigest, Executable: o.Executable, CWD: o.CWD, Environment: o.Environment, Started: o.Started, Ended: o.Ended, Exit: o.Exit, Outcome: o.Outcome, Reason: o.Reason, Output: o.Output, CaptureComplete: o.CaptureComplete, CleanupConfirmed: o.CleanupConfirmed, ProcessID: o.ProcessID, ProcessIdentity: o.ProcessIdentity}
}
func retainCheckSetupBlock(ctx context.Context, db *store.DB, b store.BugCheckBinding, cause error) (ActivityResult, error) {
	state, _, _, err := db.CurrentBugAnalysis(ctx, b.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	reason := cause.Error()
	_, err = db.UpdateBugFailureReferences(ctx, b.SessionID, state.Revision, "checks:"+b.Operation+":"+config.BugRecipeDigest([]byte(reason)), store.BugFailureReferences{Reasons: []string{reason}, Evidence: []string{b.Operation}})
	if err != nil {
		return ActivityResult{}, err
	}
	return checkActivityResult(b, "blocked", reason)
}
