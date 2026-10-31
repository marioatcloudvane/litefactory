package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// NewGraphBugRuntime activates only the complete supported simplified package.
func NewGraphBugRuntime(ctx context.Context, opts Options, cfg BugRuntimeConfig) (*BugRuntime, error) {
	if cfg.Code == nil {
		cfg.Code = cfg.Forge
	}
	if cfg.Tickets == nil {
		cfg.Tickets = cfg.Forge
	}
	if opts.DB == nil || opts.Git == nil || opts.CloneGate == nil || opts.MCP == nil || opts.MCP.Redactor == nil || opts.MCP.BotID < 1 || opts.ResearchReporter == nil || (cfg.Code == nil || cfg.Tickets == nil) || cfg.OwnerID < 1 || cfg.Permissions == nil {
		return nil, errors.New("simplified Bug requires concrete source, result, owner, reporter and delivery bindings")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := SimplifiedBugSpecs(); err != nil {
		return nil, err
	}
	// Source selection belongs to each repository admission or retained capture.
	return &BugRuntime{opts: opts, cfg: cfg}, nil
}

// bugReplyNotApplied marks a refusal of an owner comment so the daemon answers
// and consumes it; a refused comment must never stall the issue's queue.
// Shutdown and the existing between-questions refusal keep their own paths.
func bugReplyNotApplied(e error) error {
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) || errors.Is(e, store.ErrGraphReplyPredatesQuestion) {
		return e
	}
	return fmt.Errorf("%w: %w", ErrBugReplyNotApplied, e)
}

func (r *BugRuntime) graphAdmission(ctx context.Context, req Request) (store.WorkflowAdmission, error) {
	// Reject retired contracts before attempting to reconstruct their files.
	if existing, err := r.opts.DB.LatestStandaloneBugInstance(ctx, req.SessionID); err == nil {
		pin, err := r.opts.DB.GetGraphDefinition(ctx, existing.DefinitionID)
		if err != nil {
			return store.WorkflowAdmission{}, err
		}
		if pin.ProfileVersion != 2 {
			passage, contextErr := legacyRecoveryContext(ctx, r.opts.DB, req)
			if contextErr != nil {
				return store.WorkflowAdmission{}, contextErr
			}
			err := errors.New("unsupported pinned legacy Bug; existing work is preserved and dispatch is refused. Open a new issue for the same bug using existing Bug intake." + passage)
			if req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) {
				err = bugReplyNotApplied(err)
			}
			return store.WorkflowAdmission{}, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.WorkflowAdmission{}, err
	}
	specs, e := SimplifiedBugSpecs()
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	r.definition, e = capturedWorkflow(ctx, r.opts, req, agent.WorkflowBug, specs)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	instance, e := r.opts.DB.LatestStandaloneBugInstance(ctx, req.SessionID)
	if e == nil {
		pin, e := r.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
		if e != nil {
			return store.WorkflowAdmission{}, e
		}
		if pin.Digest != r.definition.Profile.Digest || pin.ProfileVersion != 2 {
			passage, contextErr := legacyRecoveryContext(ctx, r.opts.DB, req)
			if contextErr != nil {
				return store.WorkflowAdmission{}, contextErr
			}
			e := errors.New("unsupported pinned legacy Bug; existing work is preserved and dispatch is refused. Open a new issue for the same bug using existing Bug intake." + passage)
			if req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) {
				e = bugReplyNotApplied(e)
			}
			return store.WorkflowAdmission{}, e
		}
	}
	// Execute holds the task lock. Only a fresh authenticated owner comment may
	// restart a launch that failed before any execution or publication began.
	untouched := false
	if e == nil && instance.Status == store.GraphRunning && req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) && req.Trigger.AuthorID == r.cfg.OwnerID {
		untouched, e = r.untouchedGraphLaunch(ctx, instance)
		if e != nil {
			return store.WorkflowAdmission{}, e
		}
	}
	if e == nil && instance.Status == store.GraphRunning && !untouched {
		if req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) {
			wait, e := r.opts.DB.GetGraphOwnerWait(ctx, instance.ID)
			if e != nil {
				return store.WorkflowAdmission{}, bugReplyNotApplied(e)
			}
			delivery, e := r.opts.DB.GetReportDelivery(ctx, wait.DeliveryKey)
			if errors.Is(e, store.ErrNotFound) {
				intent, readErr := r.opts.DB.GetGraphComment(ctx, wait.DeliveryKey)
				var question GraphStopQuestion
				if readErr != nil || json.Unmarshal(intent.Candidate, &question) != nil || question.OwnerStop == nil {
					// The question was parked but its report never stored: post it
					// now, or answer this reply visibly instead of failing raw.
					if _, e = r.opts.ResearchReporter.RecoverGraphStopQuestion(ctx, wait.DeliveryKey); e != nil {
						return store.WorkflowAdmission{}, bugReplyNotApplied(e)
					}
					if delivery, e = r.opts.DB.GetReportDelivery(ctx, wait.DeliveryKey); e != nil {
						return store.WorkflowAdmission{}, bugReplyNotApplied(e)
					}
				}
			} else if e != nil {
				return store.WorkflowAdmission{}, e
			}
			// The store validates the owner stop's retained run and real reply source
			// inside the claim. Ordinary questions still require their report journal.

			session, e := r.opts.DB.GetSession(ctx, req.SessionID)
			if e != nil {
				return store.WorkflowAdmission{}, e
			}
			if wait.State == "claimed" && wait.ReplyID == req.Trigger.CommentID && wait.ReplyAuthorID == r.cfg.OwnerID && req.Trigger.AuthorID == r.cfg.OwnerID && wait.ReplyBody == req.Trigger.Body {
				return retainedAdmission(ctx, r.opts.DB, req, instance, "bug"), nil
			}
			_, e = AdmitGraphBugOwnerReply(ctx, r.opts.DB, store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: r.cfg.OwnerID, AuthorID: req.Trigger.AuthorID, CommentID: req.Trigger.CommentID, QuestionCommentID: delivery.CommentID, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: req.Trigger.Body, At: r.opts.MCP.Now()})
			if e != nil {
				return store.WorkflowAdmission{}, bugReplyNotApplied(e)
			}
		}
		return retainedAdmission(ctx, r.opts.DB, req, instance, "bug"), nil
	}
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return store.WorkflowAdmission{}, e
	}
	if store.IsGraphTrigger(req.Trigger.CommentID) {
		return store.WorkflowAdmission{}, errors.New("bug internal dispatch has no admitted graph")
	}
	if _, e = r.opts.DB.AdmitWorkflowPath(ctx, req.SessionID, "graph"); e != nil {
		return store.WorkflowAdmission{}, e
	}
	pin, e := r.pin(ctx, r.definition, workflowGraphPath(r.definition))
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	topology, e := graphexec.SemanticDigest(r.definition.Profile)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	raw, e := json.Marshal(r.definition.Files)
	if e != nil {
		return store.WorkflowAdmission{}, e
	}
	return r.opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: pin.ID, Start: r.definition.Profile.Start, ContentDigest: r.definition.Digest, TopologyDigest: topology, Files: raw, Provenance: store.WorkflowProvenance(r.definition.Origin), Reply: req.Trigger.Body})
}

// untouchedGraphLaunch is deliberately limited to the failed bootstrap shape.
// The caller has already checked the supported pin; absence of a wait alone
// never authorizes restarting work, and failed reads never count as absence.
func (r *BugRuntime) untouchedGraphLaunch(ctx context.Context, instance store.GraphInstance) (bool, error) {
	if instance.Status != store.GraphRunning || instance.CurrentNode != r.definition.Profile.Start || !instance.NextDueAt.IsZero() || !instance.FinishedAt.IsZero() {
		return false, nil
	}
	if _, err := r.opts.DB.GetGraphOwnerWait(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if _, err := r.opts.DB.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	attempts, err := r.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil || len(attempts) != 0 {
		return false, err
	}
	runs, err := r.opts.DB.ListRuns(ctx, instance.SessionID)
	if err != nil || len(runs) != 0 {
		return false, err
	}
	transitions, err := r.opts.DB.ListGraphTransitions(ctx, instance.ID)
	if err != nil || len(transitions) != 0 {
		return false, err
	}
	comments, err := r.opts.DB.ListGraphComments(ctx, instance.ID, "", 1)
	if err != nil || len(comments) != 0 {
		return false, err
	}
	return true, nil
}

func graphBugExhaustionReason(ctx context.Context, db *store.DB, node graphexec.Node, attempts []store.GraphNodeAttempt) (string, error) {
	reason := fmt.Sprintf("node %q dispatch limit %d exhausted", node.Name, node.Limit)
	if node.Name != "fixer" || len(attempts) == 0 {
		return reason, nil
	}
	last := attempts[len(attempts)-1]
	// A declared sync traversal can sit between delivery and its fresh check.
	// Stop at any other activity or unsuccessful sync; evidence never grants permission.
	for i := len(attempts) - 1; i >= 0; i-- {
		last = attempts[i]
		if last.Status != store.GraphSucceeded {
			return reason, nil
		}
		if last.Node == "finalization" && last.Outcome == "revalidate" {
			break
		}
		outputs, err := db.ListGraphOutputs(ctx, last.ID)
		if err != nil {
			return "", err
		}
		sync := false
		for _, output := range outputs {
			var evidence syncBaseOutput
			if output.Kind == store.GraphOutputJSON && json.Unmarshal(output.Payload, &evidence) == nil && evidence.Working != "" && evidence.Base != "" && evidence.Result == last.Outcome && (last.Outcome == "synced" || last.Outcome == "current" || last.Outcome == "retry") {
				sync = true
			}
		}
		if !sync || i == 0 {
			return reason, nil
		}
	}
	if last.Node != "finalization" || last.Outcome != "revalidate" {
		return reason, nil
	}
	outputs, err := db.ListGraphOutputs(ctx, last.ID)
	if err != nil {
		return "", err
	}
	for _, output := range outputs {
		var progress graphDeliveryProgress
		if output.Kind == store.GraphOutputJSON && json.Unmarshal(output.Payload, &progress) == nil && progress.Record == "delivery_progress" && progress.Outcome == "revalidate" {
			return reason + ". The repair passed its prior checks. " + progress.Reason + " Reply to rerun the checks on the new head with one extra bounded fixer run.", nil
		}
	}
	return reason, nil
}

func (r *BugRuntime) executeGraph(ctx context.Context, req Request, result Result) (Result, error) {
	if capacity := stepCapacity(ctx); capacity != nil {
		defer capacity.Release()
	}
	local := *r
	r = &local
	if r.opts.ProjectApprovers {
		var err error
		r.opts, err = bindProjectApprover(ctx, r.opts, req.SessionID)
		if err != nil {
			return result, err
		}
		r.cfg.OwnerID, err = ticketOwnerID(ctx, r.opts)
		if err != nil {
			return result, err
		}
	}
	result.ResearchGraph = true
	admission, e := r.graphAdmission(ctx, req)
	if e != nil {
		return result, e
	}
	if req.Trigger.CommentID > 0 && !store.IsGraphTrigger(req.Trigger.CommentID) {
		if _, e = r.opts.ResearchReporter.Consume(ctx, req.SessionID, req.Trigger.CommentID); e != nil {
			return result, e
		}
	}
	w := &researchWorkflow{opts: r.opts, req: req, result: result, definition: r.definition, admission: admission, policy: BugWorkflowPolicy()}
	finish := func(cause error) (Result, error) {
		if id, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok && !w.result.GraphDeferred {
			cause = errors.Join(cause, r.opts.DB.FinishGraphDispatch(context.WithoutCancel(ctx), id, r.opts.MCP.Now()))
		}
		if w.prep != nil {
			return w.finish(ctx, cause)
		}
		return w.result, cause
	}
	sourceNote := ""
	prepareWith := func(synchronize bool) func(context.Context, Options, ActivityRequest) error {
		return func(ctx context.Context, _ Options, a ActivityRequest) error {
			if w.prep == nil {
				return errors.New("source callback missing prepared factory run")
			}
			note, e := PrepareGraphBugSource(ctx, r.opts, w.req, a, w.prep.worktree, w.prep.repo, w.prep.author, r.opts.Logger, r.opts.MCP.Now(), synchronize)
			sourceNote = note
			return e
		}
	}
	prepareSource := prepareWith(true)
	prepareRole := func(ctx context.Context, _ Options, a ActivityRequest, scope agentresult.Context, retained string) (ResearchInvocation, error) {
		if w.prep == nil || w.prep.run.ID != a.RunID {
			return ResearchInvocation{}, errors.New("role callback lacks exact active run")
		}
		request := w.prep.request
		request.Mode, request.Workflow = "", agent.WorkflowBug
		request.Invocation = agent.InvocationResearchNode
		request.Graph = &agent.GraphInvocation{InstanceID: a.InstanceID, AttemptID: a.NodeAttemptID, RunID: a.RunID, Node: a.Node}
		expected := *scope.Bug
		route, e := w.openResultRoute(ctx, &request, &resultOverride{Resolve: func(check context.Context) (agentresult.Context, error) {
			if expected.Role == "bug-analyst" {
				current, _, e := GraphBugAnalystContext(check, r.opts.DB, a.InstanceID, a.NodeAttemptID)
				if e == nil && (current.Bug == nil || *current.Bug != expected) {
					e = errors.New("analyst scope changed")
				}
				return current, e
			}
			approved, e := GraphBugApprovedHandoff(check, r.opts.DB, a.InstanceID)
			if e != nil {
				return scope, e
			}
			if approved.Candidate.Revision != expected.ApprovedRevision {
				return scope, store.ErrBugRepairUnauthorized
			}
			return scope, nil
		}, Targets: agentresult.EditTargets("bug"), Pass: fmt.Sprintf("bug:%s:%s", expected.Role, expected.Invocation)})
		if e != nil {
			return ResearchInvocation{}, e
		}
		suffix := retained + "\n<note>Factory source preparation.\n" + sourceNote + "</note>\n"
		return w.finishRoute(ctx, route, &request, suffix, w.prep.promptInput.SessionArtifactDir)
	}
	finalization := GraphBugFinalization{Code: r.cfg.Code, Tickets: r.cfg.Tickets, Redactor: r.opts.MCP.Redactor, Resolve: r.resolveGraphDelivery, Save: r.saveGraphDelivery, PublishSaved: r.pushGraphDelivery, SyncBase: r.syncGraphDelivery}
	policy := SimplifiedBugPolicy{Profile: r.definition.Profile, Reporter: r.opts.ResearchReporter, Analyst: GraphBugAnalystActivity{Prepare: func(ctx context.Context, o Options, a ActivityRequest, c agentresult.Context, s string) (ResearchInvocation, error) {
		if e := prepareWith(false)(ctx, o, a); e != nil {
			return ResearchInvocation{}, e
		}
		return prepareRole(ctx, o, a, c, s)
	}}, SyncBase: r.graphSyncBaseBinding(req.SessionID), Fixer: GraphBugFixerActivity{PrepareSource: prepareSource, Prepare: prepareRole}, Finalization: finalization}
	var specs graphexec.Registry
	var activities ActivityRegistry
	if e = policy.Register(&specs, &activities); e != nil {
		return finish(e)
	}
	var prerequisite *store.BugSyncPrerequisite
	if gate, ok := graphexec.BugFixerSyncGate(r.definition.Profile); ok {
		fixer, _ := graphexec.Decide(r.definition.Profile, gate.Name, "synced")
		prerequisite = &store.BugSyncPrerequisite{SyncNode: gate.Name, FixerNode: fixer, AllowDeferred: slices.Contains(gate.Outcomes, "deferred")}
	}
	for {
		if e = ctx.Err(); e != nil {
			return finish(e)
		}
		instance, e := r.opts.DB.GetGraphInstance(ctx, admission.Instance.ID)
		if e != nil {
			return finish(e)
		}
		if instance.Status != store.GraphRunning {
			if instance.Status == store.GraphSucceeded {
				if _, e = r.opts.ResearchReporter.ReportGraphDeliveryCompletion(ctx, instance.ID); e != nil {
					return finish(e)
				}
			}
			return finish(nil)
		}
		wait, we := r.opts.DB.GetGraphOwnerWait(ctx, instance.ID)
		if we == nil && wait.State == "waiting" {
			_, e = r.opts.ResearchReporter.RecoverGraphStopQuestion(ctx, wait.DeliveryKey)
			return finish(e)
		}
		if we != nil && !errors.Is(we, store.ErrNotFound) {
			return finish(we)
		}
		node, ok := r.definition.Profile.Node(instance.CurrentNode)
		if !ok {
			return finish(errors.New("pinned Bug node absent"))
		}
		if prerequisite != nil && node.Name == prerequisite.SyncNode {
			if _, e = GraphBugApprovedHandoff(ctx, r.opts.DB, instance.ID); e != nil {
				return finish(e)
			}
			attempts, err := r.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
			if err != nil {
				return finish(err)
			}
			fixer, _ := r.definition.Profile.Node(prerequisite.FixerNode)
			spent := 0
			for _, attempt := range attempts {
				if attempt.Node == fixer.Name && attempt.Attempt > spent {
					spent = attempt.Attempt
				}
			}
			if fixer.Limit > 0 && spent >= fixer.Limit {
				allowed, err := r.opts.DB.GraphBugSyncEntryAllowance(ctx, instance.ID, *prerequisite)
				if err != nil {
					return finish(err)
				}
				if !allowed {
					reason, err := graphBugExhaustionReason(ctx, r.opts.DB, fixer, attempts)
					if err != nil {
						return finish(err)
					}
					if err = r.opts.DB.RedirectGraphBugSyncToFixerStop(ctx, instance.ID, *prerequisite, r.opts.MCP.Now()); err != nil {
						return finish(err)
					}
					_, err = parkGraphAction(ctx, r.opts, GraphNodeRequest{LaunchID: admission.LaunchID, Profile: r.definition.Profile, InstanceID: instance.ID, At: r.opts.MCP.Now()}, GraphNodeResult{InstanceID: instance.ID, Node: fixer.Name, NextNode: fixer.Name, Activity: ActivityResult{Diagnostic: reason}}, "exhausted", false)
					return finish(err)
				}
			}
		}
		runID := int64(0)
		allowanceSource := int64(0)
		if node.Action == ResearchAgentAction {
			if prerequisite != nil && node.Name == prerequisite.FixerNode {
				if _, e = GraphBugApprovedHandoff(ctx, r.opts.DB, instance.ID); e != nil {
					return finish(e)
				}
				freshErr := graphBugFreshSync(ctx, r.opts.DB, r.definition.Profile, instance.ID, 0)
				if we == nil && wait.State == "claimed" && wait.OriginNode == node.Name {
					freshErr = errors.New("owner restart requires fresh base synchronization")
				}
				if freshErr == nil {
					freshErr = verifySyncHandoffBeforeAgent(ctx, r.opts, r.definition.Profile, instance, r.graphSyncBaseBinding(req.SessionID))
				}
				if freshErr != nil {
					if e = r.opts.DB.RedirectGraphBugFixerToSync(ctx, instance.ID, *prerequisite, r.opts.MCP.Now()); e != nil {
						return finish(e)
					}
					continue
				}
			} else if e = verifySyncHandoffBeforeAgent(ctx, r.opts, r.definition.Profile, instance, r.graphSyncBaseBinding(req.SessionID)); e != nil {
				return finish(e)
			}
			allowanceSource, e = r.opts.DB.GraphBugMechanicalAllowance(ctx, instance.ID, node.Name, prerequisite)
			if e != nil {
				return finish(e)
			}
			attempts, readErr := r.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
			if readErr != nil {
				return finish(readErr)
			}
			maximum := 0
			for _, attempt := range attempts {
				if attempt.Node == node.Name && attempt.Attempt > maximum {
					maximum = attempt.Attempt
				}
			}
			if node.Limit > 0 && maximum >= node.Limit && allowanceSource == 0 && (we != nil || wait.State != "claimed" || wait.OriginNode != node.Name) {
				diagnostic, err := graphBugExhaustionReason(ctx, r.opts.DB, node, attempts)
				if err != nil {
					return finish(err)
				}
				_, parkErr := parkGraphAction(ctx, r.opts, GraphNodeRequest{LaunchID: admission.LaunchID, Profile: r.definition.Profile, InstanceID: instance.ID, At: r.opts.MCP.Now()}, GraphNodeResult{InstanceID: instance.ID, Node: node.Name, NextNode: node.Name, Activity: ActivityResult{Diagnostic: diagnostic}}, "exhausted", false)
				return finish(parkErr)
			}
			admitted, diagnostic, capacityErr := admitGraphCapacity(ctx, r.opts, instance.ID, admission.LaunchID, node.Name, "bug")
			if capacityErr != nil {
				return finish(capacityErr)
			}
			if !admitted {
				r.opts.Logger.InfoContext(ctx, diagnostic)
				w.result.GraphDeferred = true
				return finish(parkCapacityDeferred(ctx, r.opts, req, instance.ID, node.Name, admission.LaunchID))
			}
			permissions, e := r.cfg.Permissions(req.Agent, node.Attrs["lf_agent_markdown"])
			if e != nil {
				return finish(e)
			}
			nr := req
			nr.Tools, nr.AllowedTools, nr.CodexSandbox, nr.ToolProfile = permissions.Tools, permissions.AllowedTools, permissions.Sandbox, node.Attrs["lf_agent_markdown"]
			nr.researchDefinition, nr.researchNode, nr.researchInstance, nr.workflowPolicy = r.definition, node.Name, instance.ID, &w.policy
			// A Bug node may ask only when its DOT routes an ask back to the owner.
			nr.noQuestions = !slices.Contains(node.Outcomes, "ask")
			nr.graphBugPreparation = true
			w.req = nr
			w.prep, e = prepare(ctx, r.opts, nr, result.Session, result.Ref, r.opts.Logger)
			if e != nil {
				return finish(e)
			}
			w.prep.mcpRuntime = r.opts.MCP
			w.prep.run, e = r.opts.DB.StartRun(ctx, w.prep.run.ID)
			if e != nil {
				return finish(e)
			}
			runID = w.prep.run.ID
			w.result.Run, w.result.Session = w.prep.run, w.prep.session
		}
		step, e := ExecuteNodeAction(ctx, r.opts, GraphNodeRequest{RetainCapacity: true, LaunchID: admission.LaunchID, StopOnFailure: true, Profile: r.definition.Profile, Activities: activities, InstanceID: instance.ID, RunID: runID, OwnerAllowanceSource: allowanceSource, At: r.opts.MCP.Now()})
		if step.Activity.ProcessResult != nil {
			w.process = step.Activity.ProcessResult
		}
		if e != nil {
			return finish(e)
		}
		if step.Deferred {
			if step.Activity.Diagnostic != "" && r.opts.Logger != nil {
				r.opts.Logger.InfoContext(ctx, step.Activity.Diagnostic)
			}
			w.result.GraphDeferred = true
			return finish(parkCapacityDeferred(ctx, r.opts, req, instance.ID, step.NextNode, admission.LaunchID))
		}
		if node.Action == ResearchAgentAction {
			// Settle real execution before any runless candidate or delivery consumer.
			settled, se := w.finish(ctx, nil)
			if se != nil {
				return settled, se
			}
			w.result = settled
			w.prep = nil
			w.process = nil
			next, se := r.opts.DB.GetGraphInstance(ctx, instance.ID)
			if se != nil {
				return settled, se
			}
			if step.Status == "waiting_owner" {
				return finish(nil)
			}
			if next.Status == store.GraphRunning {
				spent, _ := store.GraphTriggerDispatch(req.Trigger.CommentID)
				_, se = r.opts.DB.ParkCapturedGraphInstanceAfter(ctx, next.ID, next.CurrentNode, r.opts.MCP.Now(), r.opts.MCP.Now(), spent, admission.LaunchID)
			}
			return finish(se)
		}
		if step.Status == "waiting_owner" || step.Activity.Suspended || step.Status == store.GraphSucceeded {
			if step.Status == store.GraphSucceeded {
				if _, e = r.opts.ResearchReporter.ReportGraphDeliveryCompletion(ctx, instance.ID); e != nil {
					return finish(e)
				}
			}
			return finish(nil)
		}
	}
}

func (r *BugRuntime) resolveGraphDelivery(ctx context.Context, _ Options, a ActivityRequest, eligibility GraphBugRepairEligibility) (GraphDeliveryBinding, error) {
	instance, e := r.opts.DB.GetGraphInstance(ctx, a.InstanceID)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	s, e := r.opts.DB.GetSession(ctx, instance.SessionID)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	identity, e := r.identity(ctx, s.ID)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	repo, e := executionRepository(ctx, r.opts.DB, s)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	display, e := lookupTicketDisplay(ctx, r.opts.SubIssueCreator, ref, s.IssueNumber)
	if e != nil {
		return GraphDeliveryBinding{}, e
	}
	ticketRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
	if ref.Owner != "" {
		ticketRepo = github.RepoFromRef(ref)
	}
	return GraphDeliveryBinding{IssueReference: display.Reference(), Record: "delivery_binding", InstanceID: a.InstanceID, SessionID: s.ID, IssueID: identity.IssueID, IssueNumber: s.IssueNumber, BotID: r.opts.MCP.BotID, TicketRepository: ticketRepo, Repository: github.Repo{Owner: repo.Owner, Name: repo.Name}, Branch: s.Branch, Target: repo.BaseBranch, Marker: fmt.Sprintf("<!-- lf:bug-delivery instance=%d issue=%d -->", a.InstanceID, identity.IssueID), Eligibility: eligibility}, nil
}
func (r *BugRuntime) saveGraphDelivery(ctx context.Context, _ Options, a ActivityRequest, b GraphDeliveryBinding) (string, error) {
	s, e := r.opts.DB.GetSession(ctx, b.SessionID)
	if e != nil {
		return "", e
	}
	repo, e := executionRepository(ctx, r.opts.DB, s)
	if e != nil {
		return "", e
	}
	release, e := r.opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	if e := verifyExecutionClone(ctx, r.opts, s, clone); e != nil {
		return "", e
	}
	result, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: s.WorktreePath, Branch: s.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		return "", e
	}
	author, e := resolveAuthor(ctx, r.opts, clone)
	if e != nil {
		return "", e
	}
	run, e := r.opts.DB.GetRun(ctx, b.Eligibility.RunID)
	if e != nil {
		return "", e
	}
	commit, e := result.Worktree.Commit(ctx, git.CommitOptions{Summary: b.Eligibility.Repair.Summary, Task: s.TaskRef, Workflow: "bug", Run: run.Ordinal, Author: author})
	return commit.Head, e
}
func (r *BugRuntime) pushGraphDelivery(ctx context.Context, _ Options, a ActivityRequest, head string) error {
	instance, e := r.opts.DB.GetGraphInstance(ctx, a.InstanceID)
	if e != nil {
		return e
	}
	s, e := r.opts.DB.GetSession(ctx, instance.SessionID)
	if e != nil {
		return e
	}
	repo, e := executionRepository(ctx, r.opts.DB, s)
	if e != nil {
		return e
	}
	release, e := r.opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return e
	}
	if e := verifyExecutionClone(ctx, r.opts, s, clone); e != nil {
		return e
	}
	result, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: s.WorktreePath, Branch: s.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		return e
	}
	actual, e := result.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		return e
	}
	if actual != head {
		return errors.New("task head changed after saving the repair")
	}
	return result.Worktree.Push(ctx, r.opts.remote())
}

func (r *BugRuntime) reconcileGraphBug(ctx context.Context) error {
	instances, e := r.opts.DB.ListStandaloneBugInstances(ctx)
	if e != nil {
		return e
	}
	// One instance that cannot be reconciled must not hold up the others.
	var all error
	for _, instance := range instances {
		all = errors.Join(all, r.reconcileGraphBugInstance(ctx, instance))
	}
	return all
}

func (r *BugRuntime) reconcileGraphBugInstance(ctx context.Context, instance store.GraphInstance) error {
	local := *r
	r = &local
	if instance.Status == store.GraphSucceeded {
		// A finished instance only owes its completion report; its retained
		// capture (which may predate captures or the current Bug graph) is not
		// needed for that. One finished before delivery progress was recorded
		// owes nothing.
		_, e := r.opts.ResearchReporter.ReportGraphDeliveryCompletion(ctx, instance.ID)
		if errors.Is(e, store.ErrNotFound) {
			return nil
		}
		return e
	}
	specs, e := SimplifiedBugSpecs()
	if e != nil {
		return e
	}
	saved, e := r.opts.DB.WorkflowCaptureForAdmission(ctx, instance.SessionID, 0, "bug")
	if e != nil {
		return e
	}
	if saved.InstanceID != instance.ID {
		return errors.New("bug recovery capture belongs to another instance")
	}
	_, e = restoreWorkflow(ctx, r.opts.DB, saved, agent.WorkflowBug, specs)
	if e != nil {
		return e
	}

	if instance.Status == store.GraphSucceeded {
		_, e := r.opts.ResearchReporter.ReportGraphDeliveryCompletion(ctx, instance.ID)
		return e
	}
	attempts, e := r.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
	if e != nil {
		return e
	}
	for _, a := range attempts {
		if a.Status != store.GraphRunning && a.Status != store.GraphPending {
			continue
		}
		attemptCapture, err := r.opts.DB.WorkflowCaptureForAttempt(ctx, a.ID)
		if err != nil {
			return err
		}
		definition, err := restoreWorkflow(ctx, r.opts.DB, attemptCapture, agent.WorkflowBug, specs)
		if err != nil {
			return err
		}
		request := GraphNodeRequest{LaunchID: a.LaunchID, Profile: definition.Profile, InstanceID: instance.ID, At: r.opts.MCP.Now()}
		if a.RunID > 0 {
			if _, e = RecoverGraphStoppedExecution(ctx, r.opts, request, a.ID); e != nil {
				return e
			}
			continue
		}
		// Factory steps are safe to repeat; the owner's reply runs this one again.
		_, e = parkGraphAction(ctx, r.opts, request, GraphNodeResult{InstanceID: instance.ID, AttemptID: a.ID, Node: a.Node, NextNode: a.Node, Activity: ActivityResult{Diagnostic: "The factory restarted while this step was running. Reply to run it again; it is safe to repeat."}}, "failed", false)
		if e != nil {
			return e
		}
	}
	wait, e := r.opts.DB.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(e, store.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if wait.State == "claimed" {
		if _, e = r.opts.DB.ParkCapturedGraphInstanceAfter(ctx, instance.ID, instance.CurrentNode, r.opts.MCP.Now(), r.opts.MCP.Now(), 0, saved.ID); e != nil {
			return e
		}
	}
	if wait.State == "waiting" {
		_, e = r.opts.ResearchReporter.RecoverGraphStopQuestion(ctx, wait.DeliveryKey)
	}
	return e
}

// syncGraphDelivery runs inside Execute's task exclusion. The generic sync
// owns scratch publication; advice is tool-free and runs outside the clone gate.
func (r *BugRuntime) syncGraphDelivery(ctx context.Context, opts Options, a ActivityRequest, b GraphDeliveryBinding, head string) (syncBaseOutput, error) {
	s, err := r.opts.DB.GetSession(ctx, b.SessionID)
	if err != nil {
		return syncBaseOutput{}, err
	}
	run, err := r.opts.DB.GetRun(ctx, b.Eligibility.RunID)
	if err != nil {
		return syncBaseOutput{}, err
	}
	advisor, _ := conflictAdvisorFor(agent.Kind(run.Agent), r.opts.Runners)
	guidance, err := r.opts.DB.GraphDiagnosisGuidance(ctx, a.InstanceID)
	if err != nil {
		return syncBaseOutput{}, err
	}
	var replies []string
	for _, reply := range guidance {
		replies = append(replies, reply.ReplyBody)
	}
	retained, err := json.Marshal(replies)
	if err != nil {
		return syncBaseOutput{}, err
	}
	binding := SyncBaseBinding{Repo: bugDeliveryRepo{runtime: r, session: s}, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		return SyncBaseTarget{Working: b.Branch, Base: b.Target, ExpectedHead: head, Lock: func(ctx context.Context) (func() error, bool, error) {
			current, err := r.opts.DB.GetSession(ctx, s.ID)
			if err != nil {
				return nil, false, err
			}
			if current.LockOwner == "" || current.LockOwner != r.opts.InstanceID {
				return nil, false, errors.New("bug delivery requires the current task writer exclusion")
			}
			return func() error { return nil }, true, nil
		}}, nil
	}, ResolveConflict: func(ctx context.Context, scratch ImplementScratch, target SyncBaseTarget) (string, error) {
		return resolveBugDeliveryConflict(ctx, scratch, target, advisor, s.TaskRef, a.NodeAttemptID, run.Agent, shortenForPrompt(string(retained)), opts.ConflictContentMaxBytes)
	}}
	result, err := (SyncBaseActivity{binding}).Execute(ctx, opts, a)
	var out syncBaseOutput
	if err == nil {
		err = json.Unmarshal(result.Output, &out)
	}
	return out, err
}

type bugDeliveryRepo struct {
	runtime *BugRuntime
	session store.Session
}

func (r bugDeliveryRepo) Begin(ctx context.Context, branch string) (ImplementScratch, error) {
	opts := r.runtime.opts
	repo, err := executionRepository(ctx, opts.DB, r.session)
	if err != nil {
		return nil, err
	}
	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return nil, err
	}
	if err = verifyExecutionClone(ctx, opts, r.session, clone); err != nil {
		return nil, err
	}
	author, err := resolveAuthor(ctx, opts, clone)
	if err != nil {
		return nil, err
	}
	scratch, err := beginImplementScratch(ctx, opts.CloneGate, repo.Path, GitImplementRepo{Clone: clone, Remote: opts.remote(), ScratchRoot: implementScratchDir(opts), Author: author}, branch)
	if err != nil {
		return nil, err
	}
	return gatedScratch{ImplementScratch: scratch, gate: opts.CloneGate, clone: repo.Path, verify: func(ctx context.Context) error {
		return verifyExecutionClone(ctx, opts, r.session, clone)
	}}, nil
}

func resolveBugDeliveryConflict(ctx context.Context, scratch ImplementScratch, target SyncBaseTarget, advisor ConflictAdvisor, task string, attempt int64, model, guidance string, contentLimit int) (string, error) {
	gated, ok := scratch.(gatedScratch)
	if !ok {
		return "", errors.New("conflict resolution requires a gated scratch worktree")
	}
	s, ok := gated.ImplementScratch.(*gitScratch)
	if !ok {
		return "", errors.New("conflict advisor or scratch worktree unavailable")
	}
	base, snapshots, err := prepareBugDeliveryConflict(ctx, gated, s, target.Base, contentLimit)
	if err != nil {
		return "", err
	}
	var validated []git.ValidatedConflict
	for _, snapshot := range snapshots {
		regions, proven := git.ResolveConflictAdditions(snapshot)
		if !proven {
			validated = nil
			break
		}
		v, err := git.ValidateConflict(snapshot, regions)
		if err != nil {
			return "", err
		}
		validated = append(validated, v)
	}
	if len(validated) != len(snapshots) {
		if advisor == nil {
			return "", errors.New("conflict advisor unavailable for conflicts not proven insertion-only")
		}
		req := agent.ConflictResolutionRequest{Guidance: guidance, Branch: target.Working, Base: target.Base, LocalSHA: s.head, TargetSHA: base, TaskInputSHA: s.head, BaseInputSHA: base, Files: conflictAdvisorFiles(snapshots)}
		resolution, err := advisor.ProposeConflictResolution(ctx, req)
		if err != nil {
			return "", fmt.Errorf("conflict advisor: %w", err)
		}
		if err = agent.ValidateRegionConflictResolution(resolution, req); err != nil {
			return "", err
		}
		validated, err = validateCollectedConflictResolution(snapshots, resolution)
		if err != nil {
			return "", err
		}
	}
	var files []git.ResolvedFile
	for _, v := range validated {
		files = append(files, git.ResolvedFile{Path: v.Path, Format: git.RegionProposalVersion, BranchSHA: s.head, BaseSHA: base, ContentLimit: contentLimit, Validated: v})
	}
	return applyBugDeliveryConflict(ctx, gated, s, base, git.CompleteMergeOptions{Task: task, Mode: "bug", Author: s.repo.Author, Files: files, ProposalID: attempt, ConfirmedBy: "factory-approved Bug remedy", Model: model})
}

func prepareBugDeliveryConflict(ctx context.Context, gated gatedScratch, s *gitScratch, base string, contentLimit int) (string, []git.ConflictSnapshot, error) {
	release, err := gated.gate.Acquire(ctx, gated.clone)
	if err != nil {
		return "", nil, err
	}
	defer release()
	sha, err := s.wt.RevParse(ctx, "refs/remotes/"+s.repo.Remote+"/"+base, git.RevParseOptions{})
	if err != nil {
		return "", nil, err
	}
	preview, err := s.wt.MergePreview(ctx, sha)
	if err != nil {
		return "", nil, err
	}
	snapshots, err := s.wt.CollectConflictSnapshots(ctx, preview.TreeSHA, s.head, sha, preview.Conflicts, contentLimit)
	return sha, snapshots, err
}

func applyBugDeliveryConflict(ctx context.Context, gated gatedScratch, s *gitScratch, base string, options git.CompleteMergeOptions) (string, error) {
	release, err := gated.gate.Acquire(ctx, gated.clone)
	if err != nil {
		return "", err
	}
	defer release()
	if err = s.wt.WriteResolvedFiles(ctx, base, options.Files); err != nil {
		return "", err
	}
	if err = s.wt.StageResolvedFiles(ctx, options.Files); err != nil {
		return "", err
	}
	result, err := s.wt.CompleteMerge(ctx, options)
	return result.Head, err
}
