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
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// PlanFailureNode is the shipped failure comment node every failing Plan path
// reaches. Publication under this node is what lets the daemon treat the owner's
// triggering comment as handled after a failed run.
const PlanFailureNode = graphexec.PlanFailureNode

// maxFailureReasonRunes bounds the cause quoted to the owner. A joined error
// chain can be long; the owner needs the cause, not the whole chain.
const maxFailureReasonRunes = 1200

// planFailureCandidate authors the owner-facing account of a Plan step that
// failed, conflicted, was uncertain or was stopped. It claims no story work:
// the outcome is blocked and the reason carries the actual cause, which is what
// the failure publication contract requires. It is Plan's counterpart of the
// Research failure builder and is what every failure path publishes.
func planFailureCandidate(_ Request, cause error) agentresult.Result {
	reason := "Plan stopped before it produced a reportable result."
	if cause != nil {
		if text := strings.TrimSpace(cause.Error()); text != "" {
			reason = boundedFailureReason(text)
		}
	}
	return agentresult.Result{
		Outcome: "blocked",
		Comment: "Plan did not finish this step. No story work is claimed for it; the cause is recorded below.",
		Reason:  reason,
		Payload: agentresult.Payload{Plan: &agentresult.Plan{}},
	}
}

func boundedFailureReason(text string) string {
	runes := []rune(text)
	if len(runes) <= maxFailureReasonRunes {
		return text
	}
	return string(runes[:maxFailureReasonRunes]) + "..."
}

// renderPlanFailure states the cause. Exhausted automatic capacity retries
// do not ask the owner to send a reply merely to retry.
func renderPlanFailure(r agentresult.Result, _ string, _ naming.Ref, _ string) string {
	var out strings.Builder
	out.WriteString("Plan could not finish this step\n\n")
	out.WriteString(strings.TrimSpace(r.Comment))
	out.WriteString("\n\nCause: ")
	out.WriteString(strings.TrimSpace(r.Reason))
	if !strings.Contains(r.Reason, capacityRetriesExhausted) {
		out.WriteString("\n\nTo retry, reply on this issue with `/plan Continue the unfinished step from the saved Plan state.` Your reply starts a new Plan attempt from the saved Plan state; the earlier step's result is not reused. Existing approvals are retained only while their review basis remains current. Independent eligible stories can continue through normal admission.")
	} else {
		out.WriteString("\n\nAutomatic provider-capacity retries have ended for this attempt. Saved Plan work and approval rules remain intact. No verified reset time or safe immediate retry is established by this report; the reported capacity limit must clear before a later Plan request can proceed. This report does not queue another attempt or approve a stage.")
	}
	return out.String()
}

// failureCommentNode names the profile's shipped failure comment node. Exactly
// one must exist: two would make the destination of a failure ambiguous.
func failureCommentNode(p *graphexec.Profile) (string, error) {
	if p == nil {
		return "", errors.New("workflow profile is required to route a failure")
	}
	found := ""
	for _, n := range p.Nodes {
		if n.Action != ResearchCommentAction || n.Attrs["lf_kind"] != "failure" {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("workflow declares two failure comment nodes, %q and %q", found, n.Name)
		}
		found = n.Name
	}
	if found == "" {
		return "", errors.New("workflow declares no failure comment node")
	}
	return found, nil
}

// ReportGraphFailure publishes one failure comment for a step of an admitted
// workflow, under the graph's failure comment node. The source is the attempt
// the comment reports: a failed one is retained as a failure, and one that
// finished with a failing outcome (a conflict, an uncertain effect) as a final
// publication, because the store binds the failure kind to failed attempts only.
//
// The comment's identity is derived from the workflow, instance, source and
// node, and its bytes are retained before delivery, so a repeat, a report
// recovery and a replay of the run all deliver the one comment. A repeat with
// different cause text reuses the retained bytes rather than correcting them.
func (r *Reporter) ReportGraphFailure(ctx context.Context, req Request, policy WorkflowPolicy, source store.GraphNodeAttempt, node string, cause error) (Posted, error) {
	if policy.Failure == nil || policy.Render == nil {
		return Posted{}, errors.New("workflow policy defines no failure comment content builder")
	}
	kind := "final"
	switch source.Status {
	case store.GraphFailed:
		kind = "failure"
	case store.GraphSucceeded:
	default:
		return Posted{}, fmt.Errorf("failure source attempt %d is %s; only a finished attempt can be reported", source.ID, source.Status)
	}
	if source.RunID < 1 || node == "" {
		return Posted{}, errors.New("failure comment requires a run-bound source attempt and a failure node")
	}
	bookkeeping := context.WithoutCancel(ctx)
	instance, err := r.db.GetGraphInstance(bookkeeping, source.InstanceID)
	if err != nil {
		return Posted{}, err
	}
	session, err := r.db.GetSession(bookkeeping, instance.SessionID)
	if err != nil {
		return Posted{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return Posted{}, err
	}
	run, err := r.db.GetRun(bookkeeping, source.RunID)
	if err != nil {
		return Posted{}, err
	}
	key := fmt.Sprintf("%s-failure:%d:%d:%s", policy.Mode, source.InstanceID, source.ID, node)
	intent := r.deliveryIntent(ref, ref, session.ID, run.TriggerCommentID, run.ID, key)
	var text string
	saved, err := r.db.GetGraphComment(bookkeeping, intent.Key)
	switch {
	case err == nil:
		var retained planPublication
		if err = json.Unmarshal(saved.Candidate, &retained); err != nil {
			return Posted{}, fmt.Errorf("retained failure comment: %w", err)
		}
		if retained.Intent.Key != intent.Key || retained.Text == "" {
			return Posted{}, errors.New("retained failure comment identity mismatch")
		}
		intent, text = retained.Intent, retained.Text
	case errors.Is(err, store.ErrNotFound):
		candidate := policy.Failure(req, cause)
		scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: string(policy.Mode), WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
		if d := agentresult.ValidateResult(candidate, scope); !d.Valid || !d.Complete {
			return Posted{}, fmt.Errorf("failure comment content is not a valid result: %w", resultDiagnosticError(d))
		}
		if candidate.Outcome == "completed" || strings.TrimSpace(candidate.Reason) == "" || len(candidate.Questions) != 0 {
			return Posted{}, errors.New("failure comment must state a cause, claim no completion and ask nothing")
		}
		text = policy.Render(candidate, "", ref, session.Branch)
		raw, marshalErr := json.Marshal(planPublication{Intent: intent, Text: text})
		if marshalErr != nil {
			return Posted{}, marshalErr
		}
		if err = r.db.RetainGraphComment(bookkeeping, store.GraphCommentIntent{Key: intent.Key, InstanceID: source.InstanceID, SourceAttemptID: source.ID, Node: node, Kind: kind, Candidate: raw}); err != nil {
			return Posted{}, err
		}
	default:
		return Posted{}, err
	}
	if savedDelivery, readErr := r.db.GetReportDelivery(bookkeeping, intent.Key); readErr == nil {
		return r.dispatchDelivery(bookkeeping, savedDelivery)
	} else if !errors.Is(readErr, store.ErrNotFound) {
		return Posted{}, readErr
	}
	text, err = r.appendGraphResultReview(bookkeeping, source, text)
	if err != nil {
		return Posted{}, err
	}
	return r.deliverFullText(bookkeeping, intent, text)
}

// failureSource resolves the attempt a failed or stopped step is reported
// against. The failing attempt is preferred; a step that never reserved one
// (a stopped run between nodes, an exhausted node limit) falls back to the
// node's latest attempt and then to the instance's latest.
func failureSource(ctx context.Context, db *store.DB, instanceID int64, step GraphNodeResult) (store.GraphNodeAttempt, error) {
	if step.AttemptID > 0 {
		return db.GetGraphNodeAttempt(ctx, step.AttemptID)
	}
	if step.Node != "" {
		attempt, err := db.LatestGraphNodeAttempt(ctx, instanceID, step.Node)
		if err == nil {
			return attempt, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.GraphNodeAttempt{}, err
		}
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return store.GraphNodeAttempt{}, err
	}
	if len(attempts) == 0 {
		return store.GraphNodeAttempt{}, errors.New("no graph step ran, so there is nothing to attach a failure comment to")
	}
	return attempts[len(attempts)-1], nil
}

// reportStepFailure routes one failed, stopped or uncertain step of a mode with
// its own handlers to the shipped failure comment. It returns only the
// publication's own error, for the caller to join to the original cause, so a
// failure that could not be reported is never upgraded to a clean one.
func (w *researchWorkflow) reportStepFailure(ctx context.Context, step GraphNodeResult, cause error) error {
	if w.opts.ResearchReporter == nil || w.policy.Failure == nil {
		return errors.New("failure comment cannot be published: no reporter or content builder")
	}
	node, err := failureCommentNode(w.definition.Profile)
	if err != nil {
		return err
	}
	if step.Node == node {
		// The failure comment itself failed. Its bytes were retained before
		// delivery, so report recovery redelivers it; there is nothing further
		// to route it to.
		return nil
	}
	source, err := failureSource(context.WithoutCancel(ctx), w.opts.DB, w.admission.Instance.ID, step)
	if err != nil {
		return err
	}
	_, err = w.opts.ResearchReporter.ReportGraphFailure(ctx, w.req, w.policy, source, node, cause)
	return err
}

// failed settles a failed or stopped graph step. A mode with its own handlers
// reports every one, dispatch errors included, so nothing fails silently; a mode
// without publishes where the graph declares a failure edge, and that includes
// an agent step whose activity returned an error (#410): a Research investigate
// or shape step that errors reaches the owner through the declared failure
// comment instead of ending silently. Other dispatch errors keep their graph
// routing. The result always carries the original cause.
func (w *researchWorkflow) failed(ctx context.Context, scope func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), step GraphNodeResult, cause error, dispatched bool) error {
	if w.prep != nil {
		run, err := w.opts.DB.GetRun(context.WithoutCancel(ctx), w.prep.run.ID)
		if err != nil {
			return errors.Join(cause, err)
		}
		if _, stopped := run.OwnerStop(); stopped {
			return cause
		}
	}

	if w.deferPlanUpstream(ctx, step, cause) {
		return nil
	}
	if w.policy.Mode == agent.ModePlan && w.definition != nil {
		if node, ok := w.definition.Profile.Node(step.Node); ok && (node.Action == "lf_comment" || node.Action == "lf_ask") {
			// The retained reply is the recovery debt. A second failure notice
			// for the same input would make its pending delivery ambiguous.
			return cause
		}
	}
	if w.policy.Handlers != nil {
		if w.prep == nil {
			// No run was allocated, so no owner comment is spent: the trigger
			// stays unhandled and is retried whole.
			return cause
		}
		return errors.Join(cause, w.reportStepFailure(ctx, step, cause))
	}
	if dispatched && !w.agentStep(step) && !w.failureRouted(step) {
		return cause
	}
	return errors.Join(cause, w.publishFailure(ctx, scope, step, cause))
}

// deferPlanUpstream ends a failed Plan agent step quietly when the only reason
// it failed is that an upstream story was publishing its stage result, in
// factory-scheduled Plan work. The step's result is not applied and nothing is
// posted; the run then ends with the existing "waits for an upstream story"
// outcome, and the story is offered again once that publication lands. An
// owner-started run, any other schedule and any other cause still fail and
// tell the owner. The attempt stays failed with the publishing diagnostic and
// StopOnFailure has already ended the instance, so nothing is parked.
func (w *researchWorkflow) deferPlanUpstream(ctx context.Context, step GraphNodeResult, cause error) bool {
	if w.policy.Mode != agent.ModePlan || w.planUpstream == nil || w.prep == nil || !w.agentStep(step) {
		return false
	}
	trigger := w.req.Trigger
	if !store.IsScheduleTrigger(trigger.CommentID) || trigger.Body != PlanWorkPayload || trigger.PlanReply != nil {
		return false
	}
	recorded := w.planUpstream.recorded()
	publishing := errors.Is(cause, planbrief.ErrPlanUpstreamPublishing)
	if !publishing && (recorded == nil || !errors.Is(cause, errNoRetainedResult)) {
		return false
	}
	if !publishing {
		// The submit was refused while the upstream published, so nothing was
		// retained and the attempt's diagnostic names only the missing result.
		// Keep the publishing cause on the attempt, where recovery and the
		// daemon's resume rule read it.
		if _, err := w.opts.DB.RecordGraphOutput(context.WithoutCancel(ctx), store.GraphOutput{NodeAttemptID: step.AttemptID, Kind: store.GraphOutputDiagnostic, Payload: []byte(recorded.Error()), CreatedAt: w.opts.MCP.Now()}); err != nil {
			return false
		}
	}
	w.planUpstream.mu.Lock()
	w.planUpstream.deferred = true
	w.planUpstream.cause = errors.Join(cause, recorded)
	w.planUpstream.mu.Unlock()
	if w.opts.Logger != nil {
		w.opts.Logger.InfoContext(ctx, "Plan step deferred while an upstream story publishes its stage", "node", step.Node, "attempt_id", step.AttemptID, "cause", errors.Join(cause, recorded).Error())
	}
	return true
}

// PlanUpstreamPublishingDiagnostic reports that a failed Plan agent attempt's
// diagnostic is the upstream-publishing refusal.
func PlanUpstreamPublishingDiagnostic(diagnostic string) bool {
	return strings.Contains(diagnostic, planbrief.PlanUpstreamPublishingText)
}

// PlanUpstreamFailureDiagnostic reports that a failed Plan agent attempt's
// diagnostic names an upstream-dependency problem: a dependency inactive or
// unresolved, still publishing, or a stale upstream binding.
func PlanUpstreamFailureDiagnostic(diagnostic string) bool {
	return PlanUpstreamPublishingDiagnostic(diagnostic) || strings.Contains(diagnostic, "is inactive or unresolved") || strings.Contains(diagnostic, "storyrequest.upstream stale_binding")
}

// PlanAttemptDiagnostics joins every diagnostic output an attempt kept.
func PlanAttemptDiagnostics(ctx context.Context, db *store.DB, attemptID int64) (string, error) {
	outputs, err := db.ListGraphOutputs(ctx, attemptID)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, output := range outputs {
		if output.Kind == store.GraphOutputDiagnostic {
			parts = append(parts, string(output.Payload))
		}
	}
	return strings.Join(parts, "\n"), nil
}

// PlanLatestAgentFailedUpstream reports that the latest agent attempt of a
// task's latest Plan instance failed with an upstream-dependency diagnostic.
// Plan agent nodes are named after their stage with an "_agent" suffix.
func PlanLatestAgentFailedUpstream(ctx context.Context, db *store.DB, sessionID int64) (bool, error) {
	instance, err := db.LatestWorkflowInstance(ctx, sessionID, string(agent.ModePlan))
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return false, err
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		if !strings.HasSuffix(attempts[i].Node, "_agent") {
			continue
		}
		if attempts[i].Status != store.GraphFailed {
			return false, nil
		}
		diagnostic, err := PlanAttemptDiagnostics(ctx, db, attempts[i].ID)
		if err != nil {
			return false, err
		}
		return PlanUpstreamFailureDiagnostic(diagnostic), nil
	}
	return false, nil
}

// PlanRunDeferredForUpstream reports that a Plan run was the factory's own
// scheduled Plan work whose latest failed attempt was deferred because an
// upstream story was publishing its stage. Such a run owes no failure comment.
func PlanRunDeferredForUpstream(ctx context.Context, db *store.DB, run store.Run, triggerBody string) (bool, error) {
	if !store.IsScheduleTrigger(run.TriggerCommentID) || triggerBody != PlanWorkPayload {
		return false, nil
	}
	attempts, err := db.ListGraphNodeAttemptsByRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Status != store.GraphFailed {
			continue
		}
		diagnostic, err := PlanAttemptDiagnostics(ctx, db, attempts[i].ID)
		if err != nil {
			return false, err
		}
		return PlanUpstreamPublishingDiagnostic(diagnostic), nil
	}
	return false, nil
}

// agentStep reports whether a failed step was a graph agent (lf_agent) node.
func (w *researchWorkflow) agentStep(step GraphNodeResult) bool {
	if step.Node == "" || step.AttemptID < 1 || w.definition == nil || w.definition.Profile == nil {
		return false
	}
	node, ok := w.definition.Profile.Node(step.Node)
	return ok && node.Action == ResearchAgentAction
}

// failureRouted reports whether the graph itself routes a failed step to the
// shipped failure comment. A step whose failed edge ends elsewhere, such as a
// terminal, keeps its graph routing and publishes nothing.
func (w *researchWorkflow) failureRouted(step GraphNodeResult) bool {
	if step.Node == "" || w.definition == nil || w.definition.Profile == nil {
		return false
	}
	target, declared := graphexec.Decide(w.definition.Profile, step.Node, "failed")
	if !declared {
		return false
	}
	node, ok := w.definition.Profile.Node(target)
	return ok && node.Action == ResearchCommentAction && node.Attrs["lf_kind"] == "failure"
}

// failureComment publishes the shipped failure node when the graph itself
// reaches it: the story effect finished with a failed, conflict or uncertain
// outcome and its edge leads here. The reported source is that effect's attempt.
func (r *planGraphRuntime) failureComment(ctx context.Context, a ActivityRequest) (ActivityResult, error) {
	attempts, err := r.opts.DB.ListGraphNodeAttempts(ctx, a.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	var source store.GraphNodeAttempt
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].ID < a.NodeAttemptID {
			source = attempts[i]
			break
		}
	}
	if source.ID == 0 {
		return ActivityResult{}, errors.New("failure comment has no preceding step to report")
	}
	reason, err := r.stepCause(ctx, source)
	if err != nil {
		return ActivityResult{}, err
	}
	posted, err := r.opts.ResearchReporter.ReportGraphFailure(ctx, r.req, PlanWorkflowPolicy(), source, a.Node, errors.New(reason))
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	payload, err := json.Marshal(struct {
		CommentID       int64 `json:"comment_id"`
		SourceAttemptID int64 `json:"source_attempt_id"`
	}{posted.CommentID, source.ID})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, nil
}

// stepCause names why a finished step routed to the failure comment, from the
// story effect's own receipt when it kept one.
func (r *planGraphRuntime) stepCause(ctx context.Context, source store.GraphNodeAttempt) (string, error) {
	outputs, err := r.opts.DB.ListGraphOutputsBounded(ctx, source.ID, 1<<16, 16)
	if err != nil {
		return "", err
	}
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		var receipt PlanStoryReceipt
		if json.Unmarshal(output.Payload, &receipt) != nil || receipt.Outcome == "" {
			continue
		}
		detail := strings.TrimSpace(receipt.Detail)
		if detail == "" {
			detail = "the story effect reported no further detail"
		}
		return fmt.Sprintf("story %s effect on %q ended %s: %s", receipt.Story, source.Node, receipt.Outcome, detail), nil
	}
	return fmt.Sprintf("step %q ended %s", source.Node, source.Outcome), nil
}

// ensurePlanFailureReported completes a failed graph Plan run that is replayed
// before its owner comment is marked handled. The failure comment is published
// here if the run's own attempt never managed it; an error keeps the owner's
// comment unhandled. A comment already delivered is never posted again.
func ensurePlanFailureReported(ctx context.Context, opts Options, req Request, run store.Run) error {
	if _, stopped := run.OwnerStop(); stopped {
		return nil
	}
	if deferred, err := PlanRunDeferredForUpstream(ctx, opts.DB, run, req.Trigger.Body); err != nil || deferred {
		return err
	}
	reported, err := opts.DB.GraphFailureReported(ctx, run.ID, PlanFailureNode)
	if err != nil || reported {
		return err
	}
	if opts.ResearchReporter == nil {
		return errors.New("failed Plan run has no failure comment and no durable reporter to publish one")
	}
	attempts, err := opts.DB.ListGraphNodeAttemptsByRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if len(attempts) == 0 {
		return errors.New("failed Plan run has no graph step to attach a failure comment to")
	}
	source := attempts[len(attempts)-1]
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Status == store.GraphFailed {
			source = attempts[i]
			break
		}
	}
	// A publication failure is handled by recovering the original reply.
	if reported, err := opts.DB.GraphFailureReported(ctx, run.ID, source.Node); err != nil || reported {
		return err
	}
	cause := errors.New(strings.TrimSpace(run.Error))
	if strings.TrimSpace(run.Error) == "" {
		cause = fmt.Errorf("the Plan run ended %s without recording a cause", run.Status)
	}
	_, err = opts.ResearchReporter.ReportGraphFailure(ctx, req, PlanWorkflowPolicy(), source, PlanFailureNode, cause)
	return err
}
