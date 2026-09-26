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

const researchNodeInstructions = "\nFactory invocation: private Research graph node. Return exactly one JSON object with version=1, response=ask or done, and result containing the complete application result. ask requires an incomplete result and permitted nonempty owner questions; done requires a completed result without questions. The entire object is limited to 1 MiB. Use the existing result helper for application-result preflight/correction/receipts, and submit the result first: the terminal node result only confirms the retained accepted result and never supplies it, so one with no accepted submission, or with a different result, fails the attempt. Returning a node result retains private progress only; only an explicit graph comment activity may publish it.\n"

// researchNodeGuidance is what every invocation carries in its persistent
// instruction channel, so it holds only what is fatal to get wrong: the
// envelope's shape in one line and the two pairing rules the decoder enforces.
// The worked envelopes live in the research-evidence skill, which the host
// loads on demand — they are needed once, when the agent composes its answer,
// not on every resumed pass.
func researchNodeGuidance() string {
	return researchNodeInstructions + researchNodeHardRules
}

const researchNodeHardRules = "\nEnvelope: {\"version\":1,\"response\":\"ask\"|\"done\",\"result\":{...}}. ask requires result.outcome incomplete, a nonblank result.reason and a nonempty result.questions, each naming audience business or technical; done requires result.outcome completed and no result.questions. Read the research-evidence skill's node-result resource for the worked envelope of each response, the artifact path rules and the correction contract, before you return.\n"

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
	if len(result.ResultText) > agentresult.MaxResearchNodeBytes {
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
// data and of every effect input derived from it. The runner's terminal text is
// parsed for one purpose: to confirm completion and consistency against that
// retained result. It cannot supply, reconstruct or replace one, so terminal-only
// output and output that differs from the retained result both fail here, each
// with its own cause, before anything is retained or any effect can follow.
//
// The policy's decoder judges the terminal envelope, and it does so under the
// mode's own trusted context, so a mode with a richer result contract is held to
// it here too. Only the response of a consistent envelope is carried over.
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
	envelope, d := h.Policy.Decode([]byte(terminal), trusted)
	if !d.Valid || !d.Complete {
		return agentresult.ResearchNode{}, 0, fmt.Errorf("terminal envelope is not a valid confirmation of the retained result: %w", resultDiagnosticError(d))
	}
	if !agentresult.SameResult(snapshot.Result, envelope.Result) {
		return agentresult.ResearchNode{}, 0, errors.New("terminal envelope result differs from the retained accepted result; return the result you submitted, or submit the correction first")
	}
	return agentresult.ResearchNode{Version: envelope.Version, Response: envelope.Response, Result: snapshot.Result}, revision, nil
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
