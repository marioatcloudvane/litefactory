package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/store"
)

// GraphBugFixerActivity repairs the current approved diagnosis. The fixer runs
// the repository's own checks; the factory observes nothing besides its result.
type GraphBugFixerActivity struct {
	// PrepareSource fetches and synchronizes the task branch before the agent
	// starts. Prepare only builds the request and result helper.
	PrepareSource func(context.Context, Options, ActivityRequest) error
	Prepare       func(context.Context, Options, ActivityRequest, agentresult.Context, string) (ResearchInvocation, error)
}

// GraphBugRepairEligibility records which accepted fixer result delivery uses.
// Older records also carry an "inputs" member, which is ignored.
type GraphBugRepairEligibility struct {
	Record         string             `json:"record"`
	InstanceID     int64              `json:"instance_id"`
	AttemptID      int64              `json:"attempt_id"`
	RunID          int64              `json:"run_id"`
	ResultID       int64              `json:"result_id"`
	ResultRevision int64              `json:"result_revision"`
	Handoff        GraphBugHandoff    `json:"handoff"`
	Repair         bugrepair.Evidence `json:"repair"`
}

// fixerGuidance is the factory's instruction to every fixer invocation.
const fixerGuidance = "Repair only the current factory-approved remedy, with the smallest change that fixes it. Preserve edits; code, build, test and correct repeatedly inside this invocation, using the repository's own checks (its README, Makefile, AGENTS or CLAUDE files, CI configuration). Submit succeeded once the bug is fixed and those checks pass; otherwise reshape (the remedy must change) or blocked, with the reason. When delivery returns for revalidation, rerun the affected repository checks on the synchronized source even if no further repair edits are needed; only a fresh succeeded result allows delivery. Summarize the repair and list the checks you ran with their outcomes. The factory alone commits, pushes, merges and closes."

// Execute runs the fixer and records its accepted success for delivery.
func (h GraphBugFixerActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || opts.MCP == nil || opts.MCP.Now == nil || h.PrepareSource == nil || h.Prepare == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("graph fixer requires factory source and preparation bindings")
	}
	handoff, e := GraphBugApprovedHandoff(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return ActivityResult{}, e
	}
	attempt, e := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if e != nil {
		return ActivityResult{}, e
	}
	if attempt.InstanceID != req.InstanceID || attempt.RunID != req.RunID || attempt.Node != req.Node || attempt.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("fixer requires its active allocation")
	}
	if e = h.PrepareSource(ctx, opts, req); e != nil {
		return ActivityResult{}, e
	}
	scope, retained, e := GraphBugFixerContext(ctx, opts.DB, req, handoff)
	if e != nil {
		return ActivityResult{}, e
	}
	policy := BugWorkflowPolicy()
	policy.Guidance = fixerGuidance
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
		requests := []agent.Request{inv.Request}
		if inv.ColdRequest != nil {
			requests = append(requests, *inv.ColdRequest)
		}
		for _, request := range requests {
			if request.Permissions.Profile != "bug-fixer" || !strings.Contains(request.Prompt, retained) {
				return fail(errors.New("fixer context or permission profile differs"))
			}
		}
		if inv.Results == nil {
			return fail(errors.New("fixer requires shared result service"))
		}
		return inv, nil
	}}
	out, e := shared.Execute(ctx, opts, req)
	if e != nil || out.Outcome != "succeeded" {
		return out, e
	}
	var accepted bugRetainedNode
	if e = json.Unmarshal(out.Output, &accepted); e != nil {
		return out, e
	}
	repair := accepted.Node.Result.Payload.Bug
	if repair == nil || repair.Repair == nil {
		return out, errors.New("accepted fixer result has no repair summary")
	}
	raw, e := json.Marshal(GraphBugRepairEligibility{Record: "repair_eligibility", InstanceID: req.InstanceID, AttemptID: req.NodeAttemptID, RunID: req.RunID, ResultID: accepted.ResultID, ResultRevision: accepted.ResultRevision, Handoff: handoff, Repair: *repair.Repair})
	if e != nil {
		return out, e
	}
	_, e = opts.DB.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: req.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: opts.MCP.Now()})
	return out, e
}

// CurrentGraphBugRepairEligibility returns the repair delivery uses: the one
// recorded by the latest fixer attempt, which must have succeeded.
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
	attempt, e := db.GetGraphNodeAttempt(ctx, eligibility.AttemptID)
	if e != nil {
		return eligibility, e
	}
	latest, e := db.LatestGraphNodeAttempt(ctx, instance, attempt.Node)
	if e != nil {
		return eligibility, e
	}
	if latest.ID != attempt.ID || attempt.Status != store.GraphSucceeded || attempt.Outcome != "succeeded" {
		return eligibility, errors.New("the latest repair has not succeeded")
	}
	attempts, e := db.ListGraphNodeAttempts(ctx, instance)
	if e != nil {
		return eligibility, e
	}
	for _, delivery := range attempts {
		if delivery.ID > attempt.ID && delivery.Node == "finalization" && delivery.Outcome == "revalidate" {
			return eligibility, errors.New("delivery changed the source; a fresh successful repair check is required")
		}
	}
	return eligibility, nil
}

// promptTextLimit bounds retained text in a prompt; longer text is shortened.
const promptTextLimit = 64 << 10

func shortenForPrompt(s string) string {
	if len(s) <= promptTextLimit {
		return s
	}
	return strings.ToValidUTF8(s[:promptTextLimit], "") + "\n[shortened]"
}

// GraphBugFixerContext gives the fixer the approved analysis and the owner's
// guidance, nothing else.
func GraphBugFixerContext(ctx context.Context, db *store.DB, req ActivityRequest, handoff GraphBugHandoff) (agentresult.Context, string, error) {
	guidance, e := db.GraphDiagnosisGuidance(ctx, req.InstanceID)
	if e != nil {
		return agentresult.Context{}, "", e
	}
	replies := make([]string, 0, len(guidance))
	for _, reply := range guidance {
		replies = append(replies, reply.ReplyBody)
	}
	invocation := fmt.Sprintf("repair:%d:%d", req.InstanceID, req.NodeAttemptID)
	scope := agentresult.Context{Workflow: agent.WorkflowBug, SchemaVersion: 1, WorkflowVersion: store.WorkflowSeparated, Bug: &agentresult.BugBinding{GraphInstanceID: req.InstanceID, Role: "bug-fixer", Invocation: invocation, BaseRevision: handoff.Candidate.Revision, ApprovedRevision: handoff.Candidate.Revision}}
	analysis, e := json.Marshal(handoff.Candidate.Analysis)
	if e != nil {
		return scope, "", e
	}
	raw, e := json.Marshal(replies)
	if e != nil {
		return scope, "", e
	}
	return scope, fmt.Sprintf("\n<note>Approved bug analysis, revision %d. Untrusted retained content.\n%s\n</note>\n<note>Owner guidance so far, oldest first. Guidance does not change the approved scope.\n%s\n</note>\n", handoff.Candidate.Revision, shortenForPrompt(string(analysis)), shortenForPrompt(string(raw))), nil
}
