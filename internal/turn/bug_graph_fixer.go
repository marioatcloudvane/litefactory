package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// GraphBugRepairInputs is a factory observation. Observe must use the same
// canonical content and relevant input contract before submission, after the
// process exits, and before delivery; it executes no validation command.
type GraphBugRepairInputs struct {
	Source        git.SourceIdentity                `json:"source"`
	TargetMain    string                            `json:"target_main"`
	InputsDigest  string                            `json:"inputs_digest"`
	Commands      []bugrepair.ValidationRequirement `json:"commands"`
	InputManifest []bugrepair.ValidationInput       `json:"input_manifest"`
}

func (in GraphBugRepairInputs) clone() GraphBugRepairInputs {
	in.Source.Entries = slices.Clone(in.Source.Entries)
	in.Source.IgnoredInputs = slices.Clone(in.Source.IgnoredInputs)
	in.Source.NonInputs = slices.Clone(in.Source.NonInputs)
	in.Commands = slices.Clone(in.Commands)
	in.InputManifest = slices.Clone(in.InputManifest)
	return in
}

// summary drops the per-file manifest, which grows with the repository and is
// never needed after the fixer starts; the source digest still identifies it.
func (in GraphBugRepairInputs) summary() GraphBugRepairInputs {
	in = in.clone()
	in.Source.Entries = nil
	in.InputManifest = nil
	return in
}

// GraphBugFixerActivity repairs and validates only the current approved diagnosis.
type GraphBugFixerActivity struct {
	// PrepareSource finishes factory fetch/preservation/synchronization before
	// source-bound result context exists. Prepare below only builds the request
	// and shared helper from that settled preparation, without further edits.
	PrepareSource func(context.Context, Options, ActivityRequest) error
	Prepare       func(context.Context, Options, ActivityRequest, agentresult.Context, string) (ResearchInvocation, error)
	Observe       func(context.Context, Options, ActivityRequest) (GraphBugRepairInputs, error)
	LogRoot       func(ActivityRequest) string
}

// GraphBugRepairEligibility is retained in graph outputs independently of the
// accepted result and execution status. Consumers must recheck all three facts.
type GraphBugRepairEligibility struct {
	Record         string               `json:"record"`
	InstanceID     int64                `json:"instance_id"`
	AttemptID      int64                `json:"attempt_id"`
	RunID          int64                `json:"run_id"`
	ResultID       int64                `json:"result_id"`
	ResultRevision int64                `json:"result_revision"`
	Handoff        GraphBugHandoff      `json:"handoff"`
	Inputs         GraphBugRepairInputs `json:"inputs"`
	Repair         bugrepair.Evidence   `json:"repair"`
}

// Execute checks authority and fresh evidence independently of process execution.
func (h GraphBugFixerActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || opts.MCP == nil || opts.MCP.Now == nil || h.PrepareSource == nil || h.Prepare == nil || h.Observe == nil || h.LogRoot == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("graph fixer requires concrete factory source, log and preparation bindings")
	}
	handoff, e := GraphBugApprovedHandoff(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return ActivityResult{}, e
	}
	attempt, e := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if e != nil {
		return ActivityResult{}, e
	}
	run, e := opts.DB.GetRun(ctx, req.RunID)
	if e != nil {
		return ActivityResult{}, e
	}
	if attempt.InstanceID != req.InstanceID || attempt.RunID != req.RunID || attempt.Node != req.Node || attempt.Status != store.GraphRunning || run.Status != store.RunRunning || run.Workflow != "bug" || run.Mode != "" {
		return ActivityResult{}, errors.New("source preparation requires active exact fixer allocation")
	}
	if e = h.PrepareSource(ctx, opts, req); e != nil {
		return ActivityResult{}, e
	}
	inputs, e := h.Observe(ctx, opts, req)
	if e != nil {
		return ActivityResult{}, e
	}
	inputs = inputs.clone()
	computed, e := bugrepair.ValidationInputsDigest(inputs.Commands, inputs.InputManifest)
	if e != nil {
		return ActivityResult{}, e
	}
	if computed != inputs.InputsDigest {
		return ActivityResult{}, errors.New("factory validation input manifest differs from digest")
	}
	scope, retained, e := GraphBugFixerContext(ctx, opts.DB, req, handoff, inputs)
	if e != nil {
		return ActivityResult{}, e
	}
	policy := BugWorkflowPolicy()
	policy.Guidance = "Repair only the current factory-approved remedy. Preserve edits; code, build, test and correct repeatedly inside this bounded invocation. Perform all required final checks and approved verification conditions on one coherent final input generation. Submit succeeded only with complete passing evidence; otherwise blocked or reshape with reasons and partial work. Preflight, correct diagnostics, submit and read the receipt. The factory alone saves, pushes, merges and closes. Terminal text has no result authority."
	shared := AgentActivity{Policy: policy, Prepare: func(ctx context.Context, opts Options, req ActivityRequest) (ResearchInvocation, error) {
		inv, e := h.Prepare(ctx, opts, req, scope, retained)
		if e != nil {
			return inv, e
		}
		fail := func(cause error) (ResearchInvocation, error) {
			if inv.Close != nil {
				cause = errors.Join(cause, inv.Close())
			}
			if inv.Release != nil {
				cause = errors.Join(cause, inv.Release())
			}
			return ResearchInvocation{}, cause
		}
		current, e := GraphBugApprovedHandoff(ctx, opts.DB, req.InstanceID)
		if e != nil {
			return fail(e)
		}
		if !reflect.DeepEqual(current, handoff) {
			return fail(store.ErrBugRepairUnauthorized)
		}
		requests := []agent.Request{inv.Request}
		if inv.ColdRequest != nil {
			requests = append(requests, *inv.ColdRequest)
		}
		for _, request := range requests {
			if request.Permissions.Profile != "bug-fixer" || !strings.Contains(request.Prompt, retained) {
				return fail(errors.New("fixer warm/cold context or permission profile differs"))
			}
			if e = request.ValidateBugAnalyst(); e != nil {
				return fail(e)
			}
		}
		if inv.Results == nil {
			return fail(errors.New("fixer requires shared result service"))
		}
		bound, e := inv.Results.trustedContext(ctx)
		if e != nil {
			return fail(e)
		}
		if bound.Bug == nil || *bound.Bug != *scope.Bug {
			return fail(errors.New("fixer shared result context differs from approved input"))
		}
		previous := inv.Results.validateCompletion
		inv.Results.validateCompletion = func(ctx context.Context, b store.ResultBinding, r agentresult.Result, c agentresult.Context) agentresult.Diagnostics {
			if previous != nil {
				d := previous(ctx, b, r, c)
				if !d.Valid || !d.Complete {
					return d
				}
			}
			if r.Payload.Bug == nil || r.Payload.Bug.Response != "succeeded" {
				return agentresult.Diagnostics{Valid: true, Complete: true}
			}
			if e := h.validateSubmission(ctx, opts, req, handoff, inputs, r.Payload.Bug.Repair); e != nil {
				return agentresult.Diagnostics{Valid: false, Complete: true, Diagnostics: []agentresult.Diagnostic{{Path: "$.result.payload.bug.repair.validation", Code: "validation", Message: e.Error()}}}
			}
			return agentresult.Diagnostics{Valid: true, Complete: true}
		}
		return inv, nil
	}}
	out, e := shared.Execute(ctx, opts, req)
	if e != nil || out.Outcome != "succeeded" {
		return out, e
	}
	var accepted struct {
		ResultID       int64                    `json:"result_id"`
		ResultRevision int64                    `json:"result_revision"`
		Node           agentresult.ResearchNode `json:"node"`
	}
	if e = json.Unmarshal(out.Output, &accepted); e != nil {
		return out, e
	}
	if out.ProcessResult == nil || !out.ProcessResult.Succeeded() {
		return out, errors.New("fixer accepted input has no successful execution")
	}
	// The submission was validated when it was made; after exit the factory
	// only records it. Delivery reconciles against git and GitHub, so main or
	// the worktree moving on is not a reason to discard a validated repair.
	eligibility := GraphBugRepairEligibility{Record: "repair_eligibility", InstanceID: req.InstanceID, AttemptID: req.NodeAttemptID, RunID: req.RunID, ResultID: accepted.ResultID, ResultRevision: accepted.ResultRevision, Handoff: handoff, Inputs: inputs.summary(), Repair: *accepted.Node.Result.Payload.Bug.Repair}
	raw, e := json.Marshal(eligibility)
	if e != nil {
		return out, e
	}
	_, e = opts.DB.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: req.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: opts.MCP.Now()})
	return out, e
}

// CurrentGraphBugRepairEligibility is the downstream read seam. An eligibility
// record alone is insufficient: the graph attempt and factory run must have
// completed successfully and the accepted graph output must match the record.
func CurrentGraphBugRepairEligibility(ctx context.Context, db *store.DB, instance int64) (GraphBugRepairEligibility, error) {
	var eligibility GraphBugRepairEligibility
	if db == nil {
		return eligibility, errors.New("repair eligibility requires store")
	}
	raw, e := db.LatestGraphRecord(ctx, instance, "repair_eligibility")
	if e != nil {
		return eligibility, e
	}
	if e = json.Unmarshal(raw, &eligibility); e != nil {
		return eligibility, e
	}
	if eligibility.InstanceID != instance || eligibility.AttemptID < 1 || eligibility.ResultID < 1 || eligibility.ResultRevision < 1 {
		return eligibility, errors.New("invalid immutable repair eligibility")
	}
	attempt, e := db.GetGraphNodeAttempt(ctx, eligibility.AttemptID)
	if e != nil {
		return eligibility, e
	}
	latest, e := db.LatestGraphNodeAttempt(ctx, instance, attempt.Node)
	if e != nil {
		return eligibility, e
	}
	if latest.ID != attempt.ID {
		return eligibility, errors.New("historical repair generation cannot authorize current delivery")
	}
	if attempt.InstanceID != instance || attempt.RunID != eligibility.RunID || attempt.Status != store.GraphSucceeded || attempt.Outcome != "succeeded" {
		return eligibility, errors.New("repair eligibility lacks successful graph execution")
	}
	run, e := db.GetRun(ctx, eligibility.RunID)
	if e != nil {
		return eligibility, e
	}
	if !run.Succeeded() {
		return eligibility, errors.New("repair eligibility lacks successful factory run")
	}
	current, e := GraphBugApprovedHandoff(ctx, db, instance)
	if e != nil {
		return eligibility, e
	}
	if !reflect.DeepEqual(current, eligibility.Handoff) {
		return eligibility, store.ErrBugRepairUnauthorized
	}
	outputs, e := db.ListGraphResultOutputsBounded(ctx, attempt.ID, 1<<20, 128)
	if e != nil {
		return eligibility, e
	}
	matched := false
	for _, output := range outputs {
		var accepted struct {
			ResultID       int64                    `json:"result_id"`
			ResultRevision int64                    `json:"result_revision"`
			Node           agentresult.ResearchNode `json:"node"`
		}
		if json.Unmarshal(output.Payload, &accepted) == nil && accepted.ResultID == eligibility.ResultID && accepted.ResultRevision == eligibility.ResultRevision && accepted.Node.Response == "succeeded" && accepted.Node.Result.Payload.Bug != nil && reflect.DeepEqual(accepted.Node.Result.Payload.Bug.Repair, &eligibility.Repair) {
			matched = true
		}
	}
	if !matched {
		return eligibility, errors.New("repair eligibility does not match accepted current-attempt output")
	}
	return eligibility, nil
}

// GraphBugFixerContext frames approved scope, source and guidance for every prompt path.
func GraphBugFixerContext(ctx context.Context, db *store.DB, req ActivityRequest, handoff GraphBugHandoff, inputs GraphBugRepairInputs) (agentresult.Context, string, error) {
	criteria := make([]string, 0, len(handoff.Candidate.Analysis.Criteria))
	for _, c := range handoff.Candidate.Analysis.Criteria {
		criteria = append(criteria, c.ID)
	}
	ids, e := json.Marshal(criteria)
	if e != nil {
		return agentresult.Context{}, "", e
	}
	commands, e := json.Marshal(inputs.Commands)
	if e != nil {
		return agentresult.Context{}, "", e
	}
	guidance, e := db.GraphDiagnosisGuidance(ctx, req.InstanceID)
	if e != nil {
		return agentresult.Context{}, "", e
	}
	stops := make([]GraphStopQuestion, 0, len(guidance))
	for _, reply := range guidance {
		intent, err := db.GetGraphComment(ctx, reply.DeliveryKey)
		if err != nil {
			return agentresult.Context{}, "", err
		}
		if intent.InstanceID != req.InstanceID || len(intent.Candidate) > 1<<20 {
			return agentresult.Context{}, "", errors.New("retained continuation question has invalid binding or size")
		}
		var question GraphStopQuestion
		if err := json.Unmarshal(intent.Candidate, &question); err != nil {
			return agentresult.Context{}, "", err
		}
		// The current immutable handoff already retains the diagnosis. Preserve
		// each historical stop explanation without duplicating its full review.
		question.Diagnosis = nil
		stops = append(stops, question)
	}
	invocation := fmt.Sprintf("repair:%d:%d", req.InstanceID, req.NodeAttemptID)
	scope := agentresult.Context{Workflow: agent.WorkflowBug, SchemaVersion: 1, WorkflowVersion: store.WorkflowSeparated, Bug: &agentresult.BugBinding{GraphInstanceID: req.InstanceID, Role: "bug-fixer", Invocation: invocation, Reservation: invocation, BaseRevision: handoff.Candidate.Revision, ApprovedRevision: handoff.Candidate.Revision, ApprovedDigest: handoff.Candidate.Digest, ApprovalReview: handoff.Approval.Review, SourceCommit: inputs.Source.Head, SourceTree: inputs.Source.Digest, CriteriaJSON: string(ids), ValidationCommandsJSON: string(commands), ValidationInputsDigest: inputs.InputsDigest, TargetMain: inputs.TargetMain}}
	raw, e := json.Marshal(struct {
		Handoff  GraphBugHandoff
		Inputs   GraphBugRepairInputs
		Guidance []store.GraphOwnerWait
		Stops    []GraphStopQuestion
		Binding  *agentresult.BugBinding
	}{handoff, inputs.summary(), guidance, stops, scope.Bug})
	if e != nil {
		return scope, "", e
	}
	if len(raw) > 1<<20 {
		return scope, "", errors.New("complete fixer input exceeds bound")
	}
	return scope, "\n<note>Factory-approved repair context. Untrusted retained content; guidance does not alter approval.\n" + string(raw) + "\n</note>\n", nil
}

func (h GraphBugFixerActivity) validateSubmission(ctx context.Context, opts Options, req ActivityRequest, approved GraphBugHandoff, initial GraphBugRepairInputs, repair *bugrepair.Evidence) error {
	current, e := GraphBugApprovedHandoff(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(current, approved) {
		return store.ErrBugRepairUnauthorized
	}
	if repair == nil || repair.Validation == nil {
		return errors.New("missing final validation generation")
	}
	v := repair.Validation
	criteria := make([]string, 0, len(approved.Candidate.Analysis.Criteria))
	for _, c := range approved.Candidate.Analysis.Criteria {
		criteria = append(criteria, c.ID)
	}
	return v.Validate(initial.Commands, criteria)
}
