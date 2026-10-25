package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const (
	// implementPollInterval is how long a coordinator rests before it looks at
	// the ledger again on its own. A story that changes state wakes it sooner;
	// this only bounds a lost wake. Every rest costs a visit of the coordinator
	// loop's bounded nodes, so it is long: at five minutes a story building for
	// over five hours used up the merge node's limit and failed the coordinator.
	implementPollInterval = time.Hour
	// implementRetryDelay is how long a coordinator waits before it tries a merge
	// again that found the integration branch busy or moved.
	implementRetryDelay = 30 * time.Second
)

// implementRuntime is one admitted Implement run: the parent's coordinator
// (worker nil) or one story's worker. It holds the factory-owned facts the
// graph's activities and hooks read; none of it comes from agent text.
type implementRuntime struct {
	opts   Options
	req    Request
	result Result
	// parentRef is the parent task: the run's own task for the coordinator, the
	// story's parent for a worker.
	parentRef  naming.Ref
	definition *teamsource.WorkflowDefinition
	// generation is the managed feature generation the ledger is scoped to.
	generation int64
	// worker is the story this run builds, nil for the coordinator.
	worker *store.ImplementStory
	// save is the run's save step, set when the graph is executed.
	save func(context.Context) (Result, error)
	// worktree is the run's admitted worktree, set when the graph is executed.
	worktree func() (*git.Worktree, error)
}

func (rt *implementRuntime) now() time.Time { return spikeNow(rt.opts) }

// admitImplementExecution takes every Implement launch on a managed parent, and
// every story worker, onto the graph. Anything else, such as an Implement task
// with no managed stories, keeps the legacy path, as does automatic recovery of a
// run that began there.
func admitImplementExecution(ctx context.Context, opts Options, req Request, result Result) (Result, bool, error) {
	if req.mode() != string(agent.ModeImplement) || req.Workflow == agent.WorkflowBug {
		return result, false, nil
	}
	rt := &implementRuntime{opts: opts, req: req, result: result, parentRef: result.Ref}
	row, err := opts.DB.ImplementStoryByWorkerSession(ctx, req.SessionID)
	switch {
	case err == nil:
		rt.parentRef, err = naming.ParseRef(row.Parent)
		if err != nil {
			return result, true, fmt.Errorf("implement story parent: %w", err)
		}
		rt.generation = row.Generation
		rt.worker = &row
	case errors.Is(err, store.ErrNotFound):
		feature, featureErr := opts.DB.GetManagedFeature(ctx, result.Ref.String())
		if errors.Is(featureErr, store.ErrNotFound) {
			return result, false, nil
		}
		if featureErr != nil {
			return result, true, featureErr
		}
		if feature.State == "planning" {
			if err := opts.DB.CheckManagedReadiness(ctx, result.Ref.String()); errors.Is(err, store.ErrPlanNotReady) {
				return result, false, nil
			} else if err != nil {
				return result, true, err
			}
		}
		rt.generation = int64(feature.Generation)
	default:
		return result, true, err
	}
	graph, err := opts.DB.HasWorkflowGraphSession(ctx, req.SessionID, string(agent.ModeImplement))
	if err != nil {
		return result, true, err
	}
	if req.Continuation != nil {
		if !graph {
			return result, false, nil
		}
		return result, true, errors.New("implement graphs require explicit owner continuation")
	}
	if opts.MCP == nil || opts.ResearchReporter == nil {
		if rt.worker != nil {
			return result, true, errors.New("graph Implement requires durable reporting and redacted effect dependencies")
		}
		// A launch with none of the graph's runtime (a bare harness, never the
		// daemon) keeps the legacy path.
		return result, false, nil
	}
	out, err := rt.admit(ctx)
	if errors.Is(err, errManagedPreparationAssessment) {
		return result, false, nil
	}
	return out, true, err
}

func (rt *implementRuntime) admit(ctx context.Context) (Result, error) {
	if recovered, err := rt.recoverWorkerSyncParking(ctx); recovered || err != nil {
		return rt.idle("settled the retained worker base-sync blockage"), err
	}
	if store.IsGraphTrigger(rt.req.Trigger.CommentID) {
		return rt.resumeDispatch(ctx)
	}
	if rt.worker != nil {
		if row, err := rt.opts.DB.GetImplementStory(ctx, rt.worker.Parent, rt.worker.Generation, rt.worker.Key); err != nil {
			return rt.result, err
		} else if row.State != store.ImplementRunning {
			return rt.idle("this story is no longer running"), nil
		}
	} else {
		if err := rt.opts.DB.CheckManagedReadiness(ctx, rt.parentRef.String()); err != nil {
			if errors.Is(err, store.ErrPlanNotReady) {
				return rt.result, errManagedPreparationAssessment
			}
			return rt.result, err
		}
		if err := prepareManagedScope(ctx, rt.opts, rt.req, rt.parentRef); err != nil {
			if errors.Is(err, store.ErrPlanNotReady) {
				return rt.result, errManagedPreparationAssessment
			}
			return rt.result, err
		}
		feature, err := rt.opts.DB.GetManagedFeature(ctx, rt.parentRef.String())
		if err != nil {
			return rt.result, err
		}
		if feature.State != "implementing" {
			return rt.result, errors.New("managed stories are not approved for implementation")
		}
		rt.generation = int64(feature.Generation)
	}
	if _, err := rt.opts.DB.AdmitWorkflowPath(ctx, rt.req.SessionID, "graph"); err != nil {
		return rt.result, err
	}
	definition, err := capturedImplement(ctx, rt.opts, rt.req)
	if err != nil {
		return rt.result, err
	}
	rt.definition = definition
	topology, err := graphexec.SemanticDigest(definition.Profile)
	if err != nil {
		return rt.result, err
	}
	files, err := json.Marshal(definition.Files)
	if err != nil {
		return rt.result, err
	}
	pin, err := rt.opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Team: definition.Profile.Team, Mode: string(agent.ModeImplement), ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest})
	if err != nil {
		return rt.result, err
	}
	if rt.worker == nil {
		if err := rt.checkEarlyReply(ctx); err != nil {
			return rt.result, err
		}
	}
	admitted, err := rt.opts.DB.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: string(agent.ModeImplement), SessionID: rt.req.SessionID, TriggerID: rt.req.Trigger.CommentID, DefinitionID: pin.ID, Start: definition.Profile.Start, ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin), Reply: rt.req.Trigger.Body})
	if err != nil {
		return rt.result, err
	}
	actual, err := rt.opts.DB.GetGraphDefinition(ctx, admitted.Instance.DefinitionID)
	if err != nil {
		return rt.result, err
	}
	definition.Profile.Digest = actual.Digest
	if rt.worker == nil {
		if err := rt.applyReply(ctx, admitted.Instance); err != nil {
			return rt.result, err
		}
	}
	return rt.execute(ctx, definition, admitted)
}

// resumeDispatch enters the graph at an instance's current node with the
// definition it was admitted under. Nothing here is a fresh admission.
func (rt *implementRuntime) resumeDispatch(ctx context.Context) (Result, error) {
	instance, err := rt.opts.DB.LatestWorkflowInstance(ctx, rt.req.SessionID, string(agent.ModeImplement))
	if err != nil {
		return rt.result, fmt.Errorf("resume implement dispatch: %w", err)
	}
	if instance.Status != store.GraphRunning {
		return rt.result, errors.New("implement graph dispatch targets an instance that is no longer running")
	}
	pin, err := rt.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return rt.result, err
	}
	definition, err := capturedImplement(ctx, rt.opts, rt.req)
	if err != nil {
		return rt.result, err
	}
	if definition.Profile.Digest != pin.Digest {
		return rt.result, errors.New("retained implement graph does not match the running instance; the resting instance cannot resume")
	}
	rt.definition = definition
	return rt.execute(ctx, definition, retainedAdmission(ctx, rt.opts.DB, rt.req, instance, "implement"))
}

// idle is the result of a run that found nothing left to do.
func (rt *implementRuntime) idle(reason string) Result {
	out := rt.result
	out.ResearchGraph = true
	out.Reason = reason
	return out
}

// execute advances the captured graph under the task lock through the shared
// driver, with this run's policy.
func (rt *implementRuntime) execute(ctx context.Context, definition *teamsource.WorkflowDefinition, admission store.WorkflowAdmission) (Result, error) {
	policy, err := rt.policy()
	if err != nil {
		return rt.result, err
	}
	out, err := ExecuteWorkflow(ctx, rt.opts, rt.req, rt.result, definition, admission, policy)
	if limit := (*GraphLimitError)(nil); rt.worker == nil && errors.As(err, &limit) {
		err = errors.Join(err, rt.reportExhausted(context.WithoutCancel(ctx), admission.Instance.ID, limit))
	}
	if err != nil && rt.worker != nil && ctx.Err() == nil {
		reason, readErr := rt.workerSyncBlockage(ctx, admission.Instance.ID, true)
		if readErr != nil {
			return out, errors.Join(err, readErr)
		}
		if reason != "" {
			_, parkErr := rt.parkSyncStory(ctx, reason)
			out.Reason = reason
			return out, parkErr
		}
	}
	if err == nil || rt.worker == nil || out.Harness != nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return out, err
	}
	// Before the process exists there is no agent repair attempt. Settle the
	// admitted instance so daemon discovery can apply its infrastructure limit.
	reason := clipText(strings.Join(strings.Fields(err.Error()), " "), maxImplementReason)
	bookkeeping := context.WithoutCancel(ctx)
	settleErr := rt.opts.DB.FailImplementWorkerInstance(bookkeeping, admission.Instance.ID, reason, rt.now())
	// prepare can allocate a run and then fail without returning a preparation.
	run, exists, lookupErr := rt.opts.DB.GetRunByTrigger(bookkeeping, rt.req.SessionID, rt.req.Trigger.CommentID)
	if lookupErr == nil && exists && (run.Status == store.RunPrelaunch || run.Status == store.RunRunning) {
		run, lookupErr = rt.opts.DB.FinishRun(bookkeeping, run.ID, store.RunOutcome{Status: store.RunFailed, Error: reason, FinishedAt: rt.now()})
		out.Run = run
	}
	return out, errors.Join(err, settleErr, lookupErr)
}

// reportExhausted tells the owner on the parent that the coordinator stopped
// because a step used up its dispatch limit, and how to resume. The comment is
// derived from the stopped instance, the step's last attempt and its text, so a
// repeat delivers the one comment.
func (rt *implementRuntime) reportExhausted(ctx context.Context, instanceID int64, limit *GraphLimitError) error {
	if rt.opts.ResearchReporter == nil {
		return nil
	}
	source, err := rt.opts.DB.LatestGraphNodeAttempt(ctx, instanceID, limit.Node)
	if err != nil {
		return fmt.Errorf("find the last attempt of the exhausted step %q: %w", limit.Node, err)
	}
	candidate := agentresult.Result{
		Outcome: "incomplete",
		Comment: fmt.Sprintf("Implement stopped at step `%s`: the step reached its limit of attempts, so the coordinator ended instead of running it again. Work already saved on the feature branch is kept.", limit.Node),
		Reason:  "Reply on this issue to resume. Your reply starts Implement again from its base sync, with every step's limit fresh, and reaches any parked story as guidance.",
		Payload: agentresult.Payload{Implement: &agentresult.Implement{}},
		Questions: []agentresult.Question{{
			ID: "q1", Audience: "technical",
			Text: fmt.Sprintf("**Stopped at `%s`**: %s", limit.Node, sentence(limit.Diagnostic)),
		}},
	}
	policy := ImplementWorkflowPolicy()
	_, _, err = rt.opts.ResearchReporter.ReportWorkflowCandidate(ctx, source, limit.Node, "question", candidate, policy.baseContext(true), "", policy)
	return err
}

// policy is the Implement policy with this run's activities and hooks.
func (rt *implementRuntime) policy() (WorkflowPolicy, error) {
	// Only the coordinator merges, starts stories and reports; a story worker needs
	// none of the forge, repository or authority they bind.
	var bindings ImplementBindings
	if rt.worker == nil {
		var err error
		if bindings, err = rt.bindings(); err != nil {
			return WorkflowPolicy{}, err
		}
	}
	p := ImplementWorkflowPolicy()
	p.RegisterEffects = func(specs *graphexec.Registry, activities *ActivityRegistry, _ *graphexec.Profile, _ func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), _ func() (string, string, error), env WorkflowEnv) error {
		rt.save, rt.worktree = env.Save, env.Worktree
		return rt.registerEffects(specs, activities, bindings)
	}
	p.Park = rt.parkHook
	p.BeforeAgent = rt.verifyWorkerSyncHandoff
	p.AfterAgentFailure = implementAfterAgentFailure
	p.ResultOverride = implementResultOverride
	return p, nil
}

// resumeStories applies an owner's reply to the parked stories: it resumes the
// ones the reply names by key, or all of them when it names none. A resumed story
// keeps its branch and work, gets its attempts reset and takes the reply as
// guidance; it restarts through ready within the cap. It is safe to repeat: only a
// row that is still parked is resumed.
func (rt *implementRuntime) resumeStories(ctx context.Context, reply string) (int, error) {
	defer rt.refreshStatus(ctx, rt.opts)
	rows, err := rt.parkedReplyTargets(ctx, reply)
	if err != nil {
		return 0, err
	}
	resumed := 0
	for _, row := range rows {
		_, err := rt.opts.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: rt.parentRef.String(), Generation: rt.generation, Key: row.Key,
			From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: row.Revision,
			Guidance: strings.TrimSpace(reply), At: rt.now(),
		})
		if err != nil {
			return resumed, err
		}
		resumed++
	}
	return resumed, nil
}

// parkedReplyTargets shares the existing key selection between the fresh-input
// check and application, so refusing an unused input needs no launch record.
func (rt *implementRuntime) parkedReplyTargets(ctx context.Context, reply string) ([]store.ImplementStory, error) {
	rows, err := rt.opts.DB.ListImplementStories(ctx, rt.parentRef.String(), rt.generation)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, row := range rows {
		if namesStory(reply, row.Key) {
			named[row.Key] = true
		}
	}
	var targets []store.ImplementStory
	for _, row := range rows {
		if row.State == store.ImplementParked && (len(named) == 0 || named[row.Key]) {
			targets = append(targets, row)
		}
	}
	return targets, nil
}

// namesStory reports whether text names a story by its key, as a whole word.
func namesStory(text, key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	return regexp.MustCompile(`(?i)(^|[^a-z0-9_-])` + regexp.QuoteMeta(key) + `($|[^a-z0-9_-])`).MatchString(text)
}

// ErrImplementReplyNotApplied identifies ordinary parent guidance with no
// parked target. The daemon answers it through durable report-and-consume.
var ErrImplementReplyNotApplied = errors.New("implement reply selected no parked stories")

// applyReply retains the claimed-question path and also accepts ordinary owner
// guidance while the coordinator rests between stories. Owner authentication and
// session serialization belong to daemon admission; private inputs and commands
// never become early guidance here.
func (rt *implementRuntime) applyReply(ctx context.Context, instance store.GraphInstance) error {
	wait, err := rt.opts.DB.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && wait.State == "claimed" && wait.ReplyID == rt.req.Trigger.CommentID {
		_, err = rt.resumeStories(ctx, wait.ReplyBody)
		return err
	}
	if !rt.earlyReply(instance) || (err == nil && (wait.State == "waiting" || wait.State == "claimed")) {
		return nil
	}
	wm, _, err := rt.opts.DB.GetWatermark(ctx, rt.req.SessionID)
	if err != nil || wm.LastCommentID >= rt.req.Trigger.CommentID {
		return err
	}
	_, err = rt.resumeStories(ctx, rt.req.Trigger.Body)
	return err
}

func (rt *implementRuntime) earlyReply(instance store.GraphInstance) bool {
	command := ParseCommand(rt.req.Trigger.Body)
	return instance.Status == store.GraphRunning && instance.CurrentNode == graphexec.ImplementAwaitNode &&
		rt.req.Trigger.CommentID > 0 && !store.IsPrivateTrigger(rt.req.Trigger.CommentID) &&
		command.Kind == KindFreeText && command.Name == ""
}

// checkEarlyReply refuses a fresh unused input before retaining its launch.
// Existing launches may have applied guidance before an execution failure, so
// their retries must still advance ready even when no parked targets remain.
func (rt *implementRuntime) checkEarlyReply(ctx context.Context) error {
	instance, err := rt.opts.DB.LatestWorkflowInstance(ctx, rt.req.SessionID, string(agent.ModeImplement))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil || !rt.earlyReply(instance) {
		return err
	}
	wait, err := rt.opts.DB.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && (wait.State == "waiting" || wait.State == "claimed") {
		return nil
	}
	wm, _, err := rt.opts.DB.GetWatermark(ctx, rt.req.SessionID)
	if err != nil || wm.LastCommentID >= rt.req.Trigger.CommentID {
		return err
	}
	replayed, err := rt.opts.DB.HasWorkflowLaunch(ctx, rt.req.SessionID, rt.req.Trigger.CommentID)
	if err != nil || replayed {
		return err
	}
	targets, err := rt.parkedReplyTargets(ctx, rt.req.Trigger.Body)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return ErrImplementReplyNotApplied
	}
	return nil
}

// parkHook rests the coordinator at its await node while stories build and
// nothing needs it: stories are running and none is waiting to be merged. A
// merge that found the branch busy rests briefly before it tries again. In every
// other case the coordinator runs on, because the ledger, not the graph position,
// says what is left to do. The coordinator also rests before an agent stage, due
// at once: its bookkeeping may run on the reserved coordinator slot, and an
// agent stage must start on normal run capacity instead.
func (rt *implementRuntime) parkHook(ctx context.Context, opts Options, instance store.GraphInstance, node graphexec.Node) (ParkDecision, error) {
	if rt.worker != nil {
		return ParkDecision{}, nil
	}
	if node.Action == ResearchAgentAction {
		return ParkDecision{Park: true, Due: rt.now()}, nil
	}
	if node.Name != graphexec.ImplementAwaitNode {
		return ParkDecision{}, nil
	}
	rows, err := opts.DB.ListImplementStories(ctx, rt.parentRef.String(), rt.generation)
	if err != nil {
		return ParkDecision{}, err
	}
	running, done := 0, 0
	for _, row := range rows {
		switch row.State {
		case store.ImplementRunning:
			running++
		case store.ImplementDone:
			done++
		}
	}
	now := rt.now()
	if done > 0 {
		retry, err := rt.lastOutcomeWasRetry(ctx, instance.ID)
		if err != nil || !retry {
			return ParkDecision{}, err
		}
		return ParkDecision{Park: true, Due: now.Add(implementRetryDelay)}, nil
	}
	if running == 0 {
		return ParkDecision{}, nil
	}
	return ParkDecision{Park: true, Due: now.Add(implementPollInterval)}, nil
}

// lastOutcomeWasRetry reports whether the latest finished step of the instance
// answered retry.
func (rt *implementRuntime) lastOutcomeWasRetry(ctx context.Context, instanceID int64) (bool, error) {
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return false, err
	}
	for _, a := range slices.Backward(attempts) {
		if a.Status == store.GraphSucceeded {
			return a.Outcome == "retry", nil
		}
	}
	return false, nil
}

// wakeImplementParent makes the parent's resting coordinator owed a run now.
// Several wakes leave exactly one dispatch open. A parent with no resting
// coordinator is left alone: the ledger is read whenever it next runs.
func wakeImplementParent(ctx context.Context, db *store.DB, parent string, at time.Time) error {
	session, err := db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	instance, err := db.LatestWorkflowInstance(ctx, session.ID, string(agent.ModeImplement))
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = db.WakeGraphInstance(ctx, instance.ID, graphexec.ImplementAwaitNode, at)
	return err
}

// implementScratchDir is where merge scratch worktrees are made.
func implementScratchDir(opts Options) string {
	return opts.MCP.Paths.ImplementScratchDir()
}
