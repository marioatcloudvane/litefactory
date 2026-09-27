package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchAgentAction names the registered private Research invocation activity.
const ResearchAgentAction = "lf_agent"

const researchNodeInstructions = "\nFactory invocation: private Research graph node. Deliver the complete application result only through the shared result route: communication preflight, correct every diagnostic, communication submit, then communication receipt for the same request_id (the shared result helper's preflight, submit and receipt methods are the same route). The accepted submission is this step's result, and the factory derives the step's response from it. Do not print a JSON result: terminal text is never read as a result, so end with a short plain-language note. The result is limited to 1 MiB. Submitting retains private progress only; only an explicit graph comment activity may publish it.\n"

// researchNodeGuidance is what every invocation carries in its persistent
// instruction channel, so it holds only what is fatal to get wrong: the route
// and the two shapes the factory routes on. The worked results live in the
// research-evidence skill, which the host loads on demand — they are needed
// once, when the agent composes its answer, not on every resumed pass.
func researchNodeGuidance() string {
	return researchNodeInstructions + researchNodeHardRules
}

const researchNodeHardRules = "\nResult shapes: to ask the owner, submit result.outcome incomplete with a nonblank result.reason and a nonempty result.questions, each naming audience business or technical; when the work is done, submit result.outcome completed with no result.questions. Any other shape fails the step. Approval is the owner's reply, never something you claim. Read the research-evidence skill's node-result resource for the worked result of each shape, the artifact path rules and the correction contract, before you submit.\n"

// ResearchInvocation contains factory-prepared delivery and result authority.
// Close joins the live helper transports before exit-time result validation.
// The caller owns run settlement. Only a refused resume permits one cold fallback;
// other failures never retry, and this handler never publishes.
type ResearchInvocation struct {
	Request     agent.Request
	ColdRequest *agent.Request
	Kind        agent.Kind
	SessionPath string
	Results     *AgentResultService
	Close       func() error
	Release     func() error
	Observe     func(agent.Request)
}

// ResearchAgentActivity prepares a reserved attempt using existing host delivery
// and helper setup. Prepare must not start a subprocess or publish an effect.
type ResearchAgentActivity = AgentActivity

// AgentActivity runs one attempt under an explicit admitted mode policy.
type AgentActivity struct {
	Policy  WorkflowPolicy
	Prepare func(context.Context, Options, ActivityRequest) (ResearchInvocation, error)
}

// RegisterResearchAgent declares one successful private result outcome. ask/done
// are retained response values for subsequent control activities to inspect.
func RegisterResearchAgent(specs *graphexec.Registry, activities *ActivityRegistry, handler ResearchAgentActivity) error {
	handler.Policy = ResearchWorkflowPolicy()
	return RegisterAgent(specs, activities, handler)
}

// RegisterAgent installs the shared invocation activity under an admitted mode policy.
func RegisterAgent(specs *graphexec.Registry, activities *ActivityRegistry, handler AgentActivity) error {
	if specs == nil || activities == nil || handler.Prepare == nil {
		return errors.New("research agent registration requires registries and preparation")
	}
	if err := handler.Policy.Admit(); err != nil {
		return fmt.Errorf("research agent registration: %w", err)
	}
	if _, exists := activities.Resolve(ResearchAgentAction); exists {
		return errors.New("research agent activity already registered")
	}
	if err := specs.Register(graphexec.ActionSpec{Name: ResearchAgentAction, Mode: handler.Policy.Mode, Workflow: handler.Policy.Workflow, Outcomes: []string{"success", "failed"}}); err != nil {
		return err
	}
	return activities.Register(ResearchAgentAction, handler)
}

// Execute invokes a reserved attempt and retains its validated result without publication.
func (h AgentActivity) Execute(ctx context.Context, opts Options, a ActivityRequest) (out ActivityResult, returnedErr error) {
	if opts.DB == nil || h.Prepare == nil || a.Mode != h.Policy.Mode || a.RunID < 1 {
		return out, errors.New("research agent requires a real Research run and preparation")
	}
	workflow, err := agent.ResolveWorkflow(h.Policy.Workflow, h.Policy.Mode)
	if err != nil {
		return out, err
	}
	actual, err := agent.ResolveWorkflow(a.Workflow, a.Mode)
	if err != nil || actual != workflow {
		return out, errors.New("agent workflow policy binding mismatch")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, a.NodeAttemptID)
	if err != nil {
		return out, err
	}
	if attempt.InstanceID != a.InstanceID || attempt.RunID != a.RunID || attempt.Node != a.Node || attempt.Status != store.GraphRunning {
		return out, errors.New("research agent attempt binding mismatch")
	}
	run, err := opts.DB.GetRun(ctx, a.RunID)
	if err != nil {
		return out, err
	}
	if run.Status != store.RunRunning || run.Mode != string(h.Policy.Mode) || (workflow == agent.WorkflowBug && run.Workflow != "bug") || (workflow != agent.WorkflowBug && run.Workflow != "") {
		return out, errors.New("research agent run is not active Research")
	}
	invocation, err := h.Prepare(ctx, opts, a)
	if err != nil {
		return out, fmt.Errorf("prepare research invocation: %w", err)
	}
	if invocation.Release != nil {
		defer func() { returnedErr = errors.Join(returnedErr, invocation.Release()) }()
	}
	// Always join helpers, including prelaunch refusal paths.
	closed := false
	defer func() {
		if !closed && invocation.Close != nil {
			returnedErr = errors.Join(returnedErr, invocation.Close())
		}
	}()
	if invocation.Results == nil || invocation.Results.binding.RunID != a.RunID || invocation.Results.binding.Mode != string(h.Policy.Mode) || (workflow == agent.WorkflowBug && invocation.Results.binding.Workflow != "bug") {
		return out, errors.New("research invocation lacks matching application-result authority")
	}
	req := invocation.Request
	requestWorkflow, err := agent.ResolveWorkflow(req.Workflow, agent.Mode(req.Mode))
	if err != nil || requestWorkflow != workflow {
		return out, errors.New("agent request workflow binding mismatch")
	}
	segment, err := opts.DB.GetSegment(ctx, run.SegmentID)
	if err != nil {
		return out, err
	}
	if req.Mode != string(h.Policy.Mode) || req.RunOrdinal != run.Ordinal || req.SegmentOrdinal != segment.Ordinal || req.Session.Ref != segment.SessionRef || req.Session.Resume != segment.ResumeToken || string(invocation.Kind) != run.Agent {
		return out, errors.New("research invocation provider/run binding mismatch")
	}
	// Main-role delivery retains these instructions in the host's persistent
	// instruction channel on cold, resumed and fallback invocations. Keep the
	// user history incremental instead of appending another copy on each pass.
	if req.Delivery == nil || req.Delivery.MainRole == "" {
		req.Prompt += h.Policy.Guidance
	}
	req.Invocation = agent.InvocationResearchNode
	req.Graph = &agent.GraphInvocation{InstanceID: a.InstanceID, AttemptID: a.NodeAttemptID, RunID: a.RunID, Node: a.Node}
	if err := req.Validate(); err != nil {
		return out, err
	}
	runner, ok := opts.Runners[invocation.Kind]
	if !ok || runner == nil {
		return out, errors.New("research runner unavailable")
	}
	if invocation.Observe != nil {
		invocation.Observe(req)
	}
	result, runErr := RunResearchMetered(ctx, opts.DB, invocation.Kind, req, runner)
	if errors.Is(runErr, agent.ErrSessionNotResumable) && invocation.ColdRequest != nil && ctx.Err() == nil {
		cold := *invocation.ColdRequest
		cold.Invocation, cold.Graph = req.Invocation, req.Graph
		cold.Path = agent.PathColdAfterResumeRefused
		if invocation.Observe != nil {
			invocation.Observe(cold)
		}
		result, runErr = RunResearchMetered(ctx, opts.DB, invocation.Kind, cold, runner)
	}
	var closeErr error
	if invocation.Close != nil {
		closeErr = invocation.Close()
	}
	closed = true
	if result == nil {
		return out, errors.Join(errors.New("research invocation produced no process result"), runErr, closeErr)
	}
	out.ProcessResult = result.Clone()
	if runErr != nil || !result.Succeeded() || ctx.Err() != nil {
		out.Diagnostic = result.Reason
		return out, errors.Join(fmt.Errorf("research process outcome: %s", result.Outcome.String()), runErr, closeErr, ctx.Err())
	}
	if closeErr != nil {
		return out, closeErr
	}
	// Printed text is read only by Bug's unchanged confirmation; for every other
	// policy it is never read, so its size cannot fail the step (#410).
	if !h.Policy.routesRetained() && len(result.ResultText) > agentresult.MaxResearchNodeBytes {
		return out, errors.New("research node output exceeds 1 MiB")
	}
	node, revision, err := h.acceptRetained(ctx, invocation.Results, result.ResultText)
	if err != nil {
		return out, err
	}
	if h.Policy.AuthorizeOutput != nil {
		attempt, err := opts.DB.GetGraphNodeAttempt(ctx, a.NodeAttemptID)
		if err != nil {
			return out, err
		}
		// Approval is the owner's, never the agent's: a completed Research result
		// is approved only when this attempt holds the authenticated owner "done"
		// reply to the displayed review of the same candidate. Otherwise it stays
		// done and the graph shows a new review, rather than failing the step.
		if h.Policy.routesRetained() && h.Policy.Mode == agent.ModeResearch && node.Response == "done" {
			approved := node
			approved.Response = "approved"
			if h.Policy.AuthorizeOutput(ctx, opts.DB, attempt, approved) == nil {
				node = approved
			}
		}
		if err := h.Policy.AuthorizeOutput(ctx, opts.DB, attempt, node); err != nil {
			return out, err
		}
	}
	if h.Policy.Artifacts != nil {
		if err := h.Policy.Artifacts(req.WorktreePath, invocation.SessionPath, node.Result); err != nil {
			return out, err
		}
	}
	var encoded []byte
	if h.Policy.Retain != nil {
		encoded, err = h.Policy.Retain(invocation.Results.Binding(), revision, node)
	} else {
		encoded, err = json.Marshal(node)
	}
	if err != nil {
		return out, fmt.Errorf("encode retained node: %w", err)
	}
	if len(encoded) > agentresult.MaxResearchNodeBytes {
		return out, errors.New("retained research node exceeds 1 MiB")
	}
	// The commit message is written from the process result, and a graph node's
	// is a JSON envelope. Committing that put thousands of bytes of questions,
	// findings and evidence arrays — fence included — into the subject line of
	// every graph run's commit. The candidate's own comment is the prose the
	// agent wrote for a reader, which is what the legacy path has always had
	// here, so the retained process result carries that instead.
	narrated := result.Clone()
	narrated.ResultText = researchCommitNarrative(node.Result)
	out.ProcessResult = narrated
	out.Outcome, out.Kind, out.Output = "success", store.GraphOutputJSON, encoded
	return out, nil
}

// acceptRetained is the runtime result boundary of every graph node. The
// retained accepted shared result is the only source of the node's application
// data and of every effect input derived from it. Terminal text can never supply,
// reconstruct or replace one, so terminal-only output fails here before anything
// is retained or any effect can follow.
//
// Research, Plan and every policy other than Bug take the routing response from
// the retained result too, through the policy's Respond hook or the shared
// ask/done mapping: the terminal text is not read at all, so a broken, partial,
// differing or missing printed copy cannot fail a step whose result was
// accepted (#410).
//
// Bug keeps its own finalization unchanged. Its result carries its response, and
// the policy's decoder still judges a terminal envelope when one is printed:
// prose or broken JSON leaves the retained result standing on its own (#384),
// while an envelope that parses but does not confirm it, or empty output, is
// refused.
func (h AgentActivity) acceptRetained(ctx context.Context, service *AgentResultService, terminal string) (agentresult.ResearchNode, int64, error) {
	snapshot, revision, err := service.retainedCurrent(ctx)
	if err != nil {
		return agentresult.ResearchNode{}, 0, fmt.Errorf("terminal output cannot supply the application result; submit it through the shared result route: %w", err)
	}
	trusted, err := service.trustedContext(ctx)
	if err != nil {
		return agentresult.ResearchNode{}, 0, err
	}
	if d := agentresult.ValidateResult(snapshot.Result, trusted); !d.Valid || !d.Complete {
		return agentresult.ResearchNode{}, 0, fmt.Errorf("retained application result is not complete: %w", resultDiagnosticError(d))
	}
	if h.Policy.routesRetained() {
		response, err := h.Policy.respond(snapshot.Result)
		if err != nil {
			return agentresult.ResearchNode{}, 0, fmt.Errorf("retained application result %s revision %d cannot be routed: %w", snapshot.Meta.ResultID, revision, err)
		}
		return agentresult.ResearchNode{Version: 1, Response: response, Result: snapshot.Result}, revision, nil
	}
	envelope, d := h.Policy.Decode([]byte(terminal), trusted)
	if !d.Valid || !d.Complete {
		if response, ok := retainedResponse(snapshot.Result); ok && strings.TrimSpace(terminal) != "" && !json.Valid([]byte(strings.TrimSpace(terminal))) {
			return agentresult.ResearchNode{Version: 1, Response: response, Result: snapshot.Result}, revision, nil
		}
		return agentresult.ResearchNode{}, 0, fmt.Errorf("terminal envelope is not a valid confirmation of the retained result: %w", resultDiagnosticError(d))
	}
	if !agentresult.SameResult(snapshot.Result, envelope.Result) {
		return agentresult.ResearchNode{}, 0, errors.New("terminal envelope result differs from the retained accepted result; return the result you submitted, or submit the correction first")
	}
	return agentresult.ResearchNode{Version: envelope.Version, Response: envelope.Response, Result: snapshot.Result}, revision, nil
}

// retainedResponse is the routing response a retained result carries itself.
// Only Bug results do; other workflows derive theirs through WorkflowPolicy.respond.
func retainedResponse(r agentresult.Result) (string, bool) {
	if r.Payload.Bug != nil && r.Payload.Bug.Response != "" {
		return r.Payload.Bug.Response, true
	}
	return "", false
}

// researchCommitNarrative is what a reader of the repository history sees for
// one graph node. It prefers the agent's own comment and falls back to naming
// the outcome, because an empty subject is worse than a terse one.
func researchCommitNarrative(r agentresult.Result) string {
	if comment := strings.TrimSpace(r.Comment); comment != "" {
		return comment
	}
	if reason := strings.TrimSpace(r.Reason); reason != "" {
		return reason
	}
	return "Research node produced a " + r.Outcome + " result."
}

// LoadResearchNodeOutput reads one previously retained node object within byte
// and row bounds. A missing/ambiguous output is an error, never a loop default.
func LoadResearchNodeOutput(ctx context.Context, db *store.DB, attemptID int64, scope agentresult.Context) (agentresult.ResearchNode, error) {
	return ResearchWorkflowPolicy().load(ctx, db, attemptID, scope)
}

func loadNodeOutput(ctx context.Context, db *store.DB, attemptID int64, scope agentresult.Context, decode func([]byte, agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics)) (agentresult.ResearchNode, error) {
	if db == nil {
		return agentresult.ResearchNode{}, errors.New("research output database is required")
	}
	attempt, err := db.GetGraphNodeAttempt(ctx, attemptID)
	if err != nil {
		return agentresult.ResearchNode{}, err
	}
	if attempt.Status != store.GraphSucceeded {
		return agentresult.ResearchNode{}, errors.New("research attempt has no successful retained result")
	}
	outputs, err := db.ListGraphOutputsBounded(ctx, attemptID, agentresult.MaxResearchNodeBytes, 16)
	if err != nil {
		return agentresult.ResearchNode{}, err
	}
	var raw []byte
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		if raw != nil {
			return agentresult.ResearchNode{}, errors.New("research attempt has ambiguous node outputs")
		}
		raw = output.Payload
	}
	if raw == nil {
		return agentresult.ResearchNode{}, errors.New("research attempt has no node output")
	}
	node, d := decode(raw, scope)
	if !d.Valid || !d.Complete {
		return agentresult.ResearchNode{}, resultDiagnosticError(d)
	}
	return node, nil
}
