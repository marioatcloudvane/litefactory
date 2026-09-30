package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// SpikeGateActivity measures an investigator's proposal against the limits its
// graph node declares, and returns an outcome only. It never chooses a step.
// When the proposal is small it also saves the worktree's HEAD, the baseline a
// later measurement of the real change is taken against.
type SpikeGateActivity struct {
	Profile  *graphexec.Profile
	Context  func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Worktree func() (*git.Worktree, error)
}

// spikeBaseline is the saved starting point of a change.
type spikeBaseline struct {
	Commit          string `json:"commit"`
	SourceAttemptID int64  `json:"source_attempt_id"`
}

// SpikeGate classifies one proposal against declared limits:
//
//	none     no change is proposed
//	blocked  a change is proposed but too incomplete to measure
//	too_big  more than one defect, more files than maxFiles, or a forbidden kind
//	small    everything else
//
// It is pure: the same proposal and limits always give the same outcome.
func SpikeGate(fix *agentresult.SpikeProposedFix, maxFiles int, forbid []string) string {
	if fix == nil {
		return "none"
	}
	if len(fix.Defects) == 0 || len(fix.Files) == 0 || len(fix.ChangeKinds) == 0 || strings.TrimSpace(fix.RegressionTest) == "" {
		return "blocked"
	}
	if len(fix.Defects) > 1 || len(fix.Files) > maxFiles {
		return "too_big"
	}
	for _, kind := range fix.ChangeKinds {
		if slices.Contains(forbid, kind) {
			return "too_big"
		}
	}
	return "small"
}

// DecodeSpikeGateLimits reads the limits a gate node declares.
func DecodeSpikeGateLimits(attrs map[string]string) (int, []string, error) {
	maxFiles, err := strconv.Atoi(attrs["lf_max_files"])
	if err != nil || maxFiles < 1 || maxFiles > 5 {
		return 0, nil, fmt.Errorf("lf_max_files %q must be a whole number from 1 to 5", attrs["lf_max_files"])
	}
	var forbid []string
	for kind := range strings.SplitSeq(attrs["lf_forbid"], ",") {
		kind = strings.TrimSpace(kind)
		if !slices.Contains(spikeForbidKinds, kind) {
			return 0, nil, fmt.Errorf("lf_forbid element %q is outside the declared kinds", kind)
		}
		forbid = append(forbid, kind)
	}
	return maxFiles, forbid, nil
}

// Execute measures the retained proposal of the node's named source.
func (h SpikeGateActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadSpikeStep(ctx, opts, req, h.Profile, h.Context, SpikeGateAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	maxFiles, forbid, err := DecodeSpikeGateLimits(step.node.Attrs)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if step.candidate.Response != "done" || step.candidate.Result.Payload.Spike == nil {
		err = errors.New("spike gate measures only a finished Spike result")
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	outcome := SpikeGate(step.candidate.Result.Payload.Spike.ProposedFix, maxFiles, forbid)
	if outcome != "small" {
		return ActivityResult{Outcome: outcome}, nil
	}
	// A change that cannot be measured later must not be started: a baseline that
	// cannot be saved is a blocked gate, never a small one.
	baseline, err := h.saveBaseline(ctx, opts, step)
	if err != nil {
		return ActivityResult{Outcome: "blocked", Diagnostic: "spike baseline unavailable: " + err.Error()}, nil //nolint:nilerr // A baseline that cannot be saved is the declared blocked outcome, not a failure to conduct the gate.
	}
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: baseline}, nil
}

// saveBaseline records the worktree's current commit once for this gate pass and
// returns it as the gate's retained output.
func (h SpikeGateActivity) saveBaseline(ctx context.Context, opts Options, step spikeStep) ([]byte, error) {
	if h.Worktree == nil {
		return nil, errors.New("no worktree is admitted")
	}
	worktree, err := h.Worktree()
	if err != nil {
		return nil, err
	}
	commit, err := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return nil, fmt.Errorf("read HEAD: %w", err)
	}
	raw, err := json.Marshal(spikeBaseline{Commit: commit, SourceAttemptID: step.source.ID})
	if err != nil {
		return nil, err
	}
	if err := opts.DB.PutSpikeRecord(ctx, store.SpikeRecord{InstanceID: step.attempt.InstanceID, Node: step.node.Name, Kind: store.SpikeRecordBaseline, Payload: raw, UpdatedAt: spikeNow(opts)}); err != nil {
		return nil, err
	}
	return raw, nil
}

// spikeStep is the verified binding shared by the Spike steps: the pinned
// profile, this reserved attempt, and the retained result of the named source.
type spikeStep struct {
	node      graphexec.Node
	attempt   store.GraphNodeAttempt
	source    store.GraphNodeAttempt
	pin       store.GraphDefinition
	candidate agentresult.ResearchNode
}

func loadSpikeStep(ctx context.Context, opts Options, req ActivityRequest, profile *graphexec.Profile, scopeOf func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), action string) (spikeStep, error) {
	var step spikeStep
	if opts.DB == nil || profile == nil || scopeOf == nil || req.Mode != agent.ModeSpike {
		return step, errors.New("spike step requires admitted Spike context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return step, err
	}
	step.pin, err = opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return step, err
	}
	var ok bool
	step.node, ok = profile.Node(req.Node)
	if !ok || step.node.Action != action || step.pin.Digest != profile.Digest || step.pin.Mode != string(agent.ModeSpike) {
		return step, errors.New("spike step profile binding mismatch")
	}
	step.attempt, err = opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return step, err
	}
	if step.attempt.InstanceID != instance.ID || step.attempt.Node != req.Node || step.attempt.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return step, errors.New("spike step attempt binding mismatch")
	}
	step.source, err = latestResearchAgentAttempt(ctx, opts, profile, instance.ID, step.node.Attrs["lf_source"])
	if err != nil {
		return step, err
	}
	if step.source.ID == 0 {
		return step, errors.New("spike step has no retained source result")
	}
	scope, err := scopeOf(ctx, step.source)
	if err != nil {
		return step, err
	}
	step.candidate, err = SpikeWorkflowPolicy().load(ctx, opts.DB, step.source.ID, scope)
	if err != nil {
		return step, &spikeResultError{err}
	}
	return step, nil
}

// spikeResultError marks a retained source result that cannot be used, as
// distinct from a step that is not bound to admitted context.
type spikeResultError struct{ cause error }

func (e *spikeResultError) Error() string {
	return "the retained result is unusable: " + e.cause.Error()
}
func (e *spikeResultError) Unwrap() error { return e.cause }
