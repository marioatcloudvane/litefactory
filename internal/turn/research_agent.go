package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchAgentAction names the registered private Research invocation activity.
const ResearchAgentAction = "lf_research_agent"

const researchNodeInstructions = "\nFactory invocation: private Research graph node. Return exactly one JSON object with version=1, response=ask or done, and result containing the complete application result. ask requires an incomplete result and permitted nonempty owner questions; done requires a completed result without questions. The entire object is limited to 1 MiB. Use the existing result helper for application-result preflight/correction/receipts; the terminal node result must match its current retained application candidate. Returning a node result retains private progress only; only an explicit graph comment activity may publish it.\n"

// researchNodeGuidance is what every invocation carries in its persistent
// instruction channel, so it holds only what is fatal to get wrong: the
// envelope's shape in one line and the two pairing rules the decoder enforces.
// The worked envelopes live in the research-evidence skill, which the host
// loads on demand — they are needed once, when the agent composes its answer,
// not on every resumed pass.
func researchNodeGuidance() string {
	return researchNodeInstructions + researchNodeHardRules
}

const researchNodeHardRules = "\nEnvelope: {\"version\":1,\"response\":\"ask\"|\"done\",\"result\":{...}}. ask requires result.outcome incomplete, a nonblank result.reason and a nonempty result.questions; done requires result.outcome completed and no result.questions. Read the research-evidence skill's node-result resource for the worked envelope of each response, the artifact path rules and the correction contract, before you return.\n"

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
type ResearchAgentActivity struct {
	Prepare func(context.Context, Options, ActivityRequest) (ResearchInvocation, error)
}

// RegisterResearchAgent declares one successful private result outcome. ask/done
// are retained response values for subsequent control activities to inspect.
func RegisterResearchAgent(specs *graphexec.Registry, activities *ActivityRegistry, handler ResearchAgentActivity) error {
	if specs == nil || activities == nil || handler.Prepare == nil {
		return errors.New("research agent registration requires registries and preparation")
	}
	if _, exists := activities.Resolve(ResearchAgentAction); exists {
		return errors.New("research agent activity already registered")
	}
	if err := specs.Register(graphexec.ActionSpec{Name: ResearchAgentAction, Mode: agent.ModeResearch, Outcomes: []string{"success", "failed"}}); err != nil {
		return err
	}
	return activities.Register(ResearchAgentAction, handler)
}

// Execute invokes a reserved attempt and retains its validated result without publication.
func (h ResearchAgentActivity) Execute(ctx context.Context, opts Options, a ActivityRequest) (out ActivityResult, returnedErr error) {
	if opts.DB == nil || h.Prepare == nil || a.Mode != agent.ModeResearch || a.RunID < 1 {
		return out, errors.New("research agent requires a real Research run and preparation")
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
	if run.Status != store.RunRunning || run.Mode != "research" {
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
	if invocation.Results == nil || invocation.Results.binding.RunID != a.RunID || invocation.Results.binding.Mode != "research" {
		return out, errors.New("research invocation lacks matching application-result authority")
	}
	req := invocation.Request
	segment, err := opts.DB.GetSegment(ctx, run.SegmentID)
	if err != nil {
		return out, err
	}
	if req.Mode != "research" || req.RunOrdinal != run.Ordinal || req.SegmentOrdinal != segment.Ordinal || req.Session.Ref != segment.SessionRef || req.Session.Resume != segment.ResumeToken || string(invocation.Kind) != run.Agent {
		return out, errors.New("research invocation provider/run binding mismatch")
	}
	// Main-role delivery retains these instructions in the host's persistent
	// instruction channel on cold, resumed and fallback invocations. Keep the
	// user history incremental instead of appending another copy on each pass.
	if req.Delivery == nil || req.Delivery.MainRole == "" {
		req.Prompt += researchNodeGuidance()
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
	trusted, err := invocation.Results.trustedContext(ctx)
	if err != nil {
		return out, err
	}
	node, d := agentresult.DecodeResearchNode([]byte(result.ResultText), trusted)
	if !d.Valid || !d.Complete {
		return out, resultDiagnosticError(d)
	}
	if err := ValidateResearchArtifacts(req.WorktreePath, invocation.SessionPath, node.Result.Artifacts); err != nil {
		return out, err
	}
	application, err := json.Marshal(agentresult.CreateInput{Result: node.Result})
	if err != nil {
		return out, err
	}
	// Reuse current-attempt receipt/correction and terminal validation, without
	// projectFinalWorkflow or the whole-turn save/report path.
	finalizer := preparedMCP{service: invocation.Results}
	if _, err := finalizer.consumeFinalResult(ctx, nil, string(application)); err != nil {
		return out, fmt.Errorf("research application result: %w", err)
	}
	encoded, err := json.Marshal(node)
	if err != nil {
		return out, err
	}
	if len(encoded) > agentresult.MaxResearchNodeBytes {
		return out, errors.New("retained research node exceeds 1 MiB")
	}
	out.Outcome, out.Kind, out.Output = "success", store.GraphOutputJSON, encoded
	return out, nil
}

// LoadResearchNodeOutput reads one previously retained node object within byte
// and row bounds. A missing/ambiguous output is an error, never a loop default.
func LoadResearchNodeOutput(ctx context.Context, db *store.DB, attemptID int64, scope agentresult.Context) (agentresult.ResearchNode, error) {
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
	node, d := agentresult.DecodeResearchNode(raw, scope)
	if !d.Valid || !d.Complete {
		return agentresult.ResearchNode{}, resultDiagnosticError(d)
	}
	return node, nil
}
