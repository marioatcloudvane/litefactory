package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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

const researchNodeHardRules = "\nResult shapes: to ask the owner, submit result.outcome incomplete with a nonblank result.reason and a nonempty result.questions, each naming audience business or technical; when the work is done, submit result.outcome completed with no result.questions. For more investigation without an owner decision, submit incomplete with a reason, cumulative findings and payload.research.continue=true, without questions. Any other shape fails the step. Approval is the owner's reply, never something you claim. Read the research-evidence skill's node-result resource for the worked result of each shape, the artifact path rules and the correction contract, before you submit.\n"

// agentBudgetClock maps a declared time budget to the time the run really gets.
// It is the identity; a test shortens it so a budget can be spent in moments.
var agentBudgetClock = func(d time.Duration) time.Duration { return d }

// ErrAgentTimedOut marks an agent attempt cancelled because its declared time
// budget ran out. The attempt is recorded as timed out before it is returned.
var ErrAgentTimedOut = errors.New("agent run exceeded its time budget")

// ResearchInvocation contains factory-prepared delivery and result authority.
// Close joins the live helper transports before exit-time result validation.
// The caller owns run settlement. Only a refused resume permits one cold fallback;
// capacity refusals retry within this invocation, and this handler never publishes.
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

// AgentActivity prepares a reserved attempt using existing host delivery and
// helper setup and runs it under an explicit admitted mode policy. Prepare must
// not start a subprocess or publish an effect.
type AgentActivity struct {
	Policy WorkflowPolicy
	// WaitCapacity overrides the cancellable backoff in tests.
	WaitCapacity func(context.Context, time.Duration) error
	Prepare      func(context.Context, Options, ActivityRequest) (ResearchInvocation, error)
}

// RegisterAgent declares one successful private result outcome. ask/done
// are retained response values for subsequent control activities to inspect. It installs the shared invocation activity under an admitted mode policy.
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
	outcomes := []string{"success", "failed"}
	if handler.Policy.Workflow == agent.WorkflowBug {
		outcomes = graphexec.OwnerStopAgentOutcomes()
	}
	if err := specs.Register(graphexec.ActionSpec{Name: ResearchAgentAction, Mode: handler.Policy.Mode, Workflow: handler.Policy.Workflow, Outcomes: outcomes, Attributes: graphexec.AgentAttributes()}); err != nil {
		return err
	}
	return activities.Register(ResearchAgentAction, handler)
}

// Execute invokes a reserved attempt and retains its validated result without
// publication. A mode that settles its own failed attempts does so here, after the
// attempt has ended and its resources are released.
func (h AgentActivity) Execute(ctx context.Context, opts Options, a ActivityRequest) (ActivityResult, error) {
	out, err := h.execute(ctx, opts, a)
	if err != nil && h.Policy.AfterAgentFailure != nil {
		failureCtx := ctx
		if errors.Is(err, ErrAgentTimedOut) {
			cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), out.cleanupDeadline)
			defer cancel()
			failureCtx = cleanup
		}
		settled, settleErr := h.Policy.AfterAgentFailure(failureCtx, opts, a, out, err)
		settled.cleanupDeadline = out.cleanupDeadline
		return settled, settleErr
	}

	return out, err
}

func (h AgentActivity) execute(ctx context.Context, opts Options, a ActivityRequest) (out ActivityResult, returnedErr error) {
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

	if h.Policy.Mode == agent.ModeImplement {
		instance, err := opts.DB.GetGraphInstance(ctx, a.InstanceID)
		if err != nil {
			return out, err
		}
		row, err := opts.DB.ImplementStoryByWorkerSession(ctx, instance.SessionID)
		if err == nil && row.TimeoutAttemptID != 0 {
			return ActivityResult{Outcome: "failed", Diagnostic: row.LastFailure}, nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return out, err
		}
	}

	minutes, budgeted, err := graphexec.AgentBudget(a.Attrs)
	if err != nil {
		return out, err
	}
	allowance := time.Duration(opts.WorkerTimeoutSeconds) * time.Second
	source := opts.WorkerTimeoutSource
	if source == "" {
		source = "default"
	}
	if allowance <= 0 {
		allowance, source = 1800*time.Second, "default"
	}
	if budgeted {
		allowance, source = time.Duration(minutes)*time.Minute, "node"
	}
	started := spikeNow(opts)
	deadline := started.Add(agentBudgetClock(allowance))
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline, source = parent, "parent"
		if parent.After(started) {
			allowance = parent.Sub(started)
		}
	}
	timing, err := opts.DB.EnsureAttemptTiming(ctx, store.AttemptTiming{NodeAttemptID: a.NodeAttemptID, StartedAt: started, Deadline: deadline, Allowance: allowance, Source: source})
	if err != nil {
		return out, err
	}
	runCtx, cancel := agent.WithWorkerDeadline(ctx, timing.Deadline)
	defer cancel()
	var timeoutOnce sync.Once
	timeoutRecorded := false
	var timeoutRecordErr error
	var cleanupDeadline time.Time
	recordTimeout := func() {
		if !errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return
		}
		timeoutOnce.Do(func() {
			timeoutRecorded = true
			cleanup, stop := agent.EvidenceContext(runCtx)
			defer stop()
			cleanupDeadline, _ = cleanup.Deadline()
			timeoutRecordErr = opts.DB.RecordAttemptTimeout(cleanup, store.AttemptTimeout{NodeAttemptID: a.NodeAttemptID, Budget: timing.Allowance, TimedOutAt: spikeNow(opts)})
		})
	}
	// Deadline authority is persisted independently of provider evidence drains.
	// Joining the observer before returning prevents late settlement of an old run.
	observerDone := make(chan struct{})
	observerStop := make(chan struct{})
	go func() {
		defer close(observerDone)
		select {
		case <-runCtx.Done():
			recordTimeout()
		case <-observerStop:
		}
	}()
	defer func() {
		close(observerStop)
		<-observerDone
		recordTimeout()
		out.cleanupDeadline = cleanupDeadline
		if !timeoutRecorded {
			return
		}
		out.Diagnostic = fmt.Sprintf("The worker exceeded its effective limit of %g seconds (deadline %s). Available work and submission/receipt state are retained.", timing.Allowance.Seconds(), timing.Deadline.UTC().Format(time.RFC3339Nano))
		if out.ProcessResult == nil {
			out.Diagnostic += " Process evidence unavailable."
		}
		if timeoutRecordErr != nil {
			out.Diagnostic += " Timeout evidence collection failed: " + timeoutRecordErr.Error()
		}
		if returnedErr != nil {
			out.Diagnostic += " Collection or execution detail: " + returnedErr.Error()
		}
		if out.ProcessResult != nil {
			out.ProcessResult = out.ProcessResult.Clone()
			out.ProcessResult.Outcome = agent.OutcomeTimedOut
			out.ProcessResult.Reason = "Worker attempt exceeded its effective deadline; available work is retained."
			out.ProcessResult.Transient = nil
		}
		if h.Policy.AcceptAfterProcessFailure && returnedErr == nil && out.Kind == store.GraphOutputJSON && len(out.Output) > 0 && timeoutRecordErr == nil {
			return
		}
		returnedErr = errors.Join(ErrAgentTimedOut, returnedErr, timeoutRecordErr)
	}()
	invocation, err := h.Prepare(runCtx, opts, a)
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
			recordTimeout()
			returnedErr = errors.Join(returnedErr, closeInvocation(runCtx, invocation.Close))
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
	if err := validateStackCommandInputs(opts, req); err != nil {
		return out, err
	}
	if stackEngineeringRequest(req, a.Attrs) {
		guidance, err := stackEngineeringGuidance(req)
		if err != nil {
			return out, err
		}
		req.Prompt += guidance
	}
	attemptGuidance, err := implementAttemptGuidance(req, a.Attrs, effectiveWorkerLimit(timing.Allowance))
	if err != nil {
		return out, err
	}
	ownerInput, err := graphOwnerInputSection(ctx, opts.DB, a.NodeAttemptID)
	if err != nil {
		return out, err
	}
	attemptGuidance += fmt.Sprintf("\nWorker timing: effective limit %g seconds; deadline %s.\n", timing.Allowance.Seconds(), timing.Deadline.UTC().Format(time.RFC3339Nano))
	req.Prompt += attemptGuidance + ownerInput
	if invocation.Observe != nil {
		invocation.Observe(req)
	}
	if _, err := runStackSetup(runCtx, opts, req, a.Attrs); err != nil {
		return out, err
	}
	result, retries, runErr := h.runCapacity(runCtx, opts, invocation, req, runner, recordTimeout)
	if retries == 0 && (result == nil || result.Transient == nil) && errors.Is(runErr, agent.ErrSessionNotResumable) && invocation.ColdRequest != nil && runCtx.Err() == nil {
		cold := *invocation.ColdRequest
		cold.Invocation, cold.Graph = req.Invocation, req.Graph
		cold.StackProfile = req.StackProfile
		if stackEngineeringRequest(req, a.Attrs) {
			guidance, err := stackEngineeringGuidance(req)
			if err != nil {
				return out, err
			}
			cold.Prompt += guidance
		}
		cold.Prompt += attemptGuidance + ownerInput
		cold.Path = agent.PathColdAfterResumeRefused
		if invocation.Observe != nil {
			invocation.Observe(cold)
		}
		result, _, runErr = h.runCapacity(runCtx, opts, invocation, cold, runner, recordTimeout)
	}
	recordTimeout()
	evidenceCtx, cancelEvidence := agent.EvidenceContext(runCtx)
	defer cancelEvidence()
	var closeErr error
	if invocation.Close != nil {
		closeErr = closeInvocation(runCtx, invocation.Close)
	}
	closed = true
	if result == nil {
		return out, errors.Join(errors.New("research invocation produced no process result"), runErr, closeErr)
	}
	if !errors.Is(ctx.Err(), context.Canceled) && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result = result.Clone()
		result.Outcome, result.Reason, result.Transient = agent.OutcomeTimedOut, "agent invocation exceeded its wall-clock limit", nil
		runErr = errors.Join(runErr, context.DeadlineExceeded)
	}
	out.ProcessResult = result.Clone()
	failed := runErr != nil || !result.Succeeded() || ctx.Err() != nil
	var node agentresult.ResearchNode
	var revision int64
	accepted := false
	// An accepted submission is the agent's answer. How the process exited
	// afterwards (a non-zero exit, a timeout after submitting, a transport
	// close) does not discard it where the policy opts in; only a factory
	// shutdown interrupts it.
	if failed && h.Policy.AcceptAfterProcessFailure && !errors.Is(ctx.Err(), context.Canceled) {
		if n, r, e := h.acceptRetained(evidenceCtx, invocation.Results); e == nil && n.Response != "continue" {
			node, revision, accepted, failed = n, r, true, false
		}
	}
	if failed {
		out.Diagnostic = result.Reason
		failureErr := errors.Join(fmt.Errorf("research process outcome: %s", result.Outcome.String()), runErr, closeErr, ctx.Err())
		if errors.Is(runErr, errCapacityRetriesExhausted) {
			failureErr = errors.Join(runErr, closeErr, ctx.Err())
			out.Diagnostic = runErr.Error()
		}
		if h.Policy.Workflow == agent.WorkflowBug || h.Policy.Mode == agent.ModeResearch {
			retained, retainErr := h.retainFailedSubmission(evidenceCtx, invocation.Results)
			out.Kind, out.Output = retained.Kind, retained.Output
			if retained.Diagnostic != "" {
				out.Diagnostic = retained.Diagnostic + "\nFactory execution failure: " + failureErr.Error()
			}
			failureErr = errors.Join(failureErr, retainErr)
		}
		return out, failureErr
	}
	if closeErr != nil && !accepted {
		return out, closeErr
	}
	if !accepted {
		node, revision, err = h.acceptRetained(evidenceCtx, invocation.Results)
		if err != nil {
			out.Outcome = "invalid"
			out.Diagnostic = err.Error()
			return out, err
		}
	}
	if h.Policy.AuthorizeOutput != nil {
		attempt, err := opts.DB.GetGraphNodeAttempt(evidenceCtx, a.NodeAttemptID)
		if err != nil {
			return out, err
		}
		if err := h.Policy.AuthorizeOutput(evidenceCtx, opts.DB, attempt, node); err != nil {
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
	if a.Attrs["lf_response"] == "result" {
		out.Outcome = node.Response
		if node.Response == "blocked" || node.Response == "failed" {
			out.Diagnostic = strings.TrimSpace(node.Result.Reason)
			if out.Diagnostic == "" {
				out.Diagnostic = strings.TrimSpace(node.Result.Comment)
			}
		}
	}
	return out, nil
}

// retainFailedSubmission preserves current accepted input for audit only. It
// creates neither a successful activity outcome nor authority to follow an edge.
func (h AgentActivity) retainFailedSubmission(ctx context.Context, service *AgentResultService) (ActivityResult, error) {
	var out ActivityResult
	snapshot, revision, err := service.retainedCurrent(ctx)
	if errors.Is(err, errNoRetainedResult) {
		out.Diagnostic = "No accepted application result was retained for the failed execution."
		return out, nil
	}
	if err != nil {
		return out, err
	}
	scope, err := service.trustedContext(ctx)
	if err != nil {
		return out, err
	}
	if d := agentresult.ValidateResult(snapshot.Result, scope); !d.Valid || !d.Complete {
		return out, resultDiagnosticError(d)
	}
	response, err := h.Policy.respond(snapshot.Result)
	if err != nil {
		return out, err
	}
	node := agentresult.ResearchNode{Version: 1, Response: response, Result: snapshot.Result}
	if h.Policy.Retain != nil {
		out.Output, err = h.Policy.Retain(service.Binding(), revision, node)
	} else {
		out.Output, err = json.Marshal(node)
	}
	if err != nil {
		return out, err
	}
	out.Kind = store.GraphOutputJSON
	out.Diagnostic = strings.TrimSpace(snapshot.Result.Reason)
	if out.Diagnostic == "" {
		out.Diagnostic = strings.TrimSpace(snapshot.Result.Comment)
	}
	return out, nil
}

// acceptRetained reads only the current attempt's accepted shared submission.
// Terminal text is deliberately ignored, including empty or conflicting JSON.
// Process failure and interruption are checked separately before this boundary.
func (h AgentActivity) acceptRetained(ctx context.Context, service *AgentResultService) (agentresult.ResearchNode, int64, error) {
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
	response, err := h.Policy.respond(snapshot.Result)
	if err != nil {
		return agentresult.ResearchNode{}, 0, fmt.Errorf("retained application result %s revision %d cannot be routed: %w", snapshot.Meta.ResultID, revision, err)
	}
	return agentresult.ResearchNode{Version: 1, Response: response, Result: snapshot.Result}, revision, nil
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

// implementDelegationRequest is the owner's standing request to the Implement
// story worker. It travels in the user prompt, outside the untrusted handoff
// note, because a host may withdraw delegation granted only in the role text.
const implementDelegationRequest = "\nThe owner explicitly asks you to delegate this story's tasks to subagents (spawn_agent or your subagent tool) and to run them in parallel.\n"

// implementAttemptGuidance is the fixed factory text an Implement agent node
// gets in its user prompt: the story worker's delegation request, and for every
// Implement node the attempt's wall-clock limit and that there is no other
// budget. Runtime supplies the persisted effective allowance; compatibility
// callers may supply a node override and a runner fallback.
func implementAttemptGuidance(req agent.Request, attrs map[string]string, runner any) (string, error) {
	if req.Mode != string(agent.ModeImplement) || req.Workflow == agent.WorkflowBug {
		return "", nil
	}
	var b strings.Builder
	role := attrs["lf_agent_markdown"]
	if req.Delivery != nil && req.Delivery.MainRole != "" {
		role = req.Delivery.MainRole
	}
	if role == "story-worker" {
		b.WriteString(implementDelegationRequest)
	}
	var limit time.Duration
	if timed, ok := runner.(interface{ Timeout() time.Duration }); ok {
		limit = timed.Timeout()
	}
	minutes, budgeted, err := graphexec.AgentBudget(attrs)
	if err != nil {
		return "", err
	}
	if budget := time.Duration(minutes) * time.Minute; budgeted {
		if _, resolved := runner.(effectiveWorkerLimit); !resolved {
			limit = budget
		}
	}
	if limit > 0 {
		fmt.Fprintf(&b, "\nThis attempt has a wall-clock limit of %g seconds; there is no other budget (no token, context or cost budget), so do not stop early for budget reasons.\n", limit.Seconds())
	} else {
		b.WriteString("\nThis attempt has a wall-clock limit; there is no other budget (no token, context or cost budget), so do not stop early for budget reasons.\n")
	}
	return b.String(), nil
}

const capacityRetriesExhausted = "provider capacity retries exhausted"

var errCapacityRetriesExhausted = errors.New(capacityRetriesExhausted)

// runCapacity keeps helper/result authority live and the graph attempt unsettled.
// Only proven absence of this attempt's accepted result permits another process.
func (h AgentActivity) runCapacity(ctx context.Context, opts Options, invocation ResearchInvocation, req agent.Request, runner agent.Runner, onStopped ...func()) (*agent.RunResult, int, error) {
	var stopped func()
	if len(onStopped) > 0 {
		stopped = onStopped[0]
	}
	result, err := runResearchMetered(ctx, opts.DB, invocation.Kind, req, runner, stopped)
	retries := 0
	for ; result != nil && result.Outcome == agent.OutcomeFailed && result.Transient != nil && result.Interruption == nil && err == nil; retries++ {
		if ctx.Err() != nil {
			return result, retries, ctx.Err()
		}
		if _, _, readErr := invocation.Results.retainedCurrent(ctx); !errors.Is(readErr, errNoRetainedResult) {
			return result, retries, readErr
		}
		if retries == 3 {
			return result, retries, fmt.Errorf("%w: %s model %.120s: %s; retried %d times", errCapacityRetriesExhausted, invocation.Kind, result.Transient.Model, result.Transient.Cause, retries)
		}
		wait := h.WaitCapacity
		if wait == nil {
			wait = waitCapacity
		}
		if waitErr := wait(ctx, (30*time.Second)<<retries); waitErr != nil {
			return result, retries, waitErr
		}
		if ctx.Err() != nil {
			return result, retries, ctx.Err()
		}
		if _, _, readErr := invocation.Results.retainedCurrent(ctx); !errors.Is(readErr, errNoRetainedResult) {
			return result, retries, readErr
		}
		run, readErr := opts.DB.GetRun(ctx, req.Graph.RunID)
		if readErr != nil {
			return result, retries, readErr
		}
		if _, stopped := run.OwnerStop(); stopped {
			return result, retries, errors.New("agent invocation stopped by owner")
		}
		result, err = runResearchMetered(ctx, opts.DB, invocation.Kind, req, runner, stopped)
	}
	return result, retries, err
}

func waitCapacity(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type effectiveWorkerLimit time.Duration

func (d effectiveWorkerLimit) Timeout() time.Duration { return time.Duration(d) }

// closeInvocation bounds the caller's wait for owned helper teardown after the
// process joins. Existing accepted-publication recovery keeps its own authority;
// this wait neither invents receipts nor starts remote notice delivery.
func closeInvocation(ctx context.Context, closeFn func() error) error {
	cleanup, cancel := agent.EvidenceContext(ctx)
	defer cancel()
	done := make(chan error, 1)
	// One waiter ends when this owned teardown returns. Graph execution joins one
	// teardown; compatibility paths join at most three per subprocess. An admitted
	// recovery operation can outlive this wait without gaining new worker authority.
	go func() { done <- closeFn() }()
	select {
	case err := <-done:
		return err
	case <-cleanup.Done():
		return fmt.Errorf("factory helper cleanup unavailable: %w", cleanup.Err())
	}
}
