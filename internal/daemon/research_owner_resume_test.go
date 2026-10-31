package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// publicationRecoveringExecutor is the fake turn with the production
// publication-recovery seam bound to real turn recovery over the fixture store.
type publicationRecoveringExecutor struct {
	*fakeExecutor
	opts turn.Options
}

func (e publicationRecoveringExecutor) RecoverGraphPublications(ctx context.Context, sessionID int64, at time.Time) error {
	return turn.RecoverGraphPublications(ctx, e.opts, sessionID, at)
}

type failedPublication struct {
	session  store.Session
	instance store.GraphInstance
	key      string
	question int64
}

// failedPublicationFixture leaves a Research instance failed at node because the
// node's comment POST got a 500, the way the graph engine records it: the
// attempt carries the publication identity and the delivery is uncertain. The
// forge then shows the marker comment, as GitHub does after a 500 that created it.
func failedPublicationFixture(t *testing.T, f *fixture, node, kind string) failedPublication {
	t.Helper()
	ctx := t.Context()
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1246, Number: testIssue, Title: "Recovered report", User: stranger(), Labels: toFakeLabels("Mode: Research")})
	f.settle()
	s := f.sessionOf(testIssue)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.db.AdmitWorkflowPath(ctx, s.ID, "graph")
	must(err)
	specs, err := turn.ResearchSpecs()
	must(err)
	capture, err := teamsourcetest.CaptureResearch(ctx, specs)
	must(err)
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: capture.Profile.Team, Mode: "research", ProfileVersion: capture.Profile.ProfileVersion, Path: "research/research.dot", Digest: capture.Profile.Digest})
	must(err)
	topology, err := graphexec.SemanticDigest(capture.Profile)
	must(err)
	files, err := json.Marshal(capture.Files)
	must(err)
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "research", SessionID: s.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "investigate", ContentDigest: capture.Digest, TopologyDigest: topology, Files: files})
	must(err)
	at := f.clock.Now()
	segment, err := f.db.GetOrCreateSegment(ctx, s.ID, 1, "research", store.AgentClaude)
	must(err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: s.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 22, LogPath: func(int) string { return "research.log" }})
	must(err)
	source, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: "investigate", RunID: run.ID, Limit: 8, StartedAt: at})
	must(err)
	must(f.db.CompleteGraphNodeAttempt(ctx, source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"done"}`)}}, Transition: &store.GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "investigate", ToNode: node, OnOutcome: "success", At: at}}))
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: node, RunID: run.ID, Limit: 8, StartedAt: at, FactoryStep: true})
	must(err)
	key := "research-" + node + "-operation"
	must(f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: key, InstanceID: admitted.Instance.ID, SourceAttemptID: source.ID, Node: node, Kind: kind, Candidate: []byte(`{"outcome":"completed"}`)}))
	marker := "<!-- lf:delivery v=1 key=" + key + " -->"
	_, err = f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Version: 1, Key: key, SessionID: s.ID, RunID: run.ID, TriggerID: 22, Task: s.TaskRef, Destination: s.TaskRef, TriggerNamespace: "comment", Purpose: "research-comment:" + node, BotUserID: botID, Body: "Research " + kind + ".\n\n" + marker, Marker: marker})
	must(err)
	f.forge.mu.Lock()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, `{"message":"server error"}`, http.StatusInternalServerError)
		return true
	}
	f.forge.mu.Unlock()
	_, postErr := f.reporter.RecoverWorkflowComment(ctx, key, agentresult.Context{}, turn.ResearchWorkflowPolicy())
	f.forge.mu.Lock()
	f.forge.onPostComment = nil
	f.forge.mu.Unlock()
	if postErr == nil {
		t.Fatal("the report POST did not fail")
	}
	delivery, err := f.db.GetReportDelivery(ctx, key)
	must(err)
	if !delivery.PossiblySent || delivery.Completed {
		t.Fatalf("a 500 must leave the delivery uncertain: %+v", delivery)
	}
	identity, err := json.Marshal(map[string]any{"delivery_key": key, "comment_id": 0, "source_attempt_id": source.ID})
	must(err)
	must(f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: at, InstanceStatus: store.GraphFailed, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: identity}, {Kind: store.GraphOutputDiagnostic, Payload: []byte(postErr.Error())}}}))
	_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: postErr.Error(), FinishedAt: at})
	must(err)
	// GitHub created the comment despite the 500.
	f.forge.addComment(testIssue, 45, bot(), delivery.Body)
	instance, err := f.db.GetGraphInstance(ctx, admitted.Instance.ID)
	must(err)
	if instance.Status != store.GraphFailed || instance.CurrentNode != node {
		t.Fatalf("fixture instance: %+v", instance)
	}
	f.turns = &fakeExecutor{fn: f.ranSuccessfully, forge: f.forge}
	f.daemon = f.newDaemon(daemon.Options{Turn: publicationRecoveringExecutor{fakeExecutor: f.turns, opts: turn.Options{DB: f.db, ResearchReporter: f.reporter}}})
	// The bot's own comment is consumed on its own tick, as it was long ago in production.
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatalf("the bot comment started a turn: %+v", f.turns.calls())
	}
	return failedPublication{session: s, instance: instance, key: key, question: 45}
}

func pendingDispatches(t *testing.T, db *store.DB, instanceID int64) []store.GraphDispatch {
	t.Helper()
	due, err := db.ListDueGraphDispatches(t.Context(), time.Now().Add(1000*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.GraphDispatch
	for _, d := range due {
		if d.InstanceID == instanceID {
			out = append(out, d)
		}
	}
	return out
}

func publishedTransitions(t *testing.T, db *store.DB, instanceID int64, from string) int {
	t.Helper()
	transitions, err := db.ListGraphTransitions(t.Context(), instanceID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tr := range transitions {
		if tr.FromNode == from && tr.OnOutcome == "published" {
			n++
		}
	}
	return n
}

// #1246 C1: one owner comment processed by the daemon settles the uncertain
// report and carries the graph on along the report's published edge.
func TestRecoveredReportDeliveryResumesGraph(t *testing.T) {
	f := newFixture(t)
	p := failedPublicationFixture(t, f, "report", "final")
	f.forge.addComment(testIssue, 50, owner(), "Please carry on")
	f.settle()
	delivery, err := f.db.GetReportDelivery(t.Context(), p.key)
	if err != nil || !delivery.Completed || delivery.CommentID != p.question {
		t.Fatalf("delivery not confirmed on the existing comment: %+v %v\n%s", delivery, err, f.logs)
	}
	posted, _, _ := f.forge.snapshot()
	for _, body := range posted {
		if strings.Contains(body, p.key) {
			t.Fatalf("recovery reposted the report: %q", body)
		}
	}
	instance, err := f.db.GetGraphInstance(t.Context(), p.instance.ID)
	if err != nil || instance.Status != store.GraphRunning || instance.CurrentNode != "shapeloop" {
		t.Fatalf("graph did not resume at the published edge: %+v %v\n%s", instance, err, f.logs)
	}
	// The resume owes one dispatch at shapeloop, and the daemon carries it on
	// exactly once: the owner comment's own turn is not a second continuation.
	dispatch, err := f.db.LatestGraphDispatch(t.Context(), p.instance.ID)
	if err != nil || dispatch.Node != "shapeloop" || len(pendingDispatches(t, f.db, p.instance.ID)) > 1 {
		t.Fatalf("no single shapeloop dispatch: %+v %v", dispatch, err)
	}
	continuations := 0
	for _, trigger := range f.turns.triggers() {
		if store.IsGraphTrigger(trigger) {
			continuations++
			if trigger != dispatch.TriggerID() {
				t.Fatalf("continuation ran under another dispatch: %d", trigger)
			}
		}
	}
	if continuations > 1 {
		t.Fatalf("continuations = %d, want at most 1", continuations)
	}
	if n := publishedTransitions(t, f.db, p.instance.ID, "report"); n != 1 {
		t.Fatalf("report transitions = %d, want 1", n)
	}
	latest, err := f.db.LatestWorkflowInstance(t.Context(), p.session.ID, "research")
	if err != nil || latest.ID != p.instance.ID {
		t.Fatalf("a successor replaced the resumed instance: %+v %v", latest, err)
	}
}

// #1246 C2: the one owner reply that triggers recovery of an uncertain question
// is claimed as that question's answer, with no successor and no dispatch.
func TestRecoveredQuestionDeliveryClaimsTriggeringReply(t *testing.T) {
	f := newFixture(t)
	p := failedPublicationFixture(t, f, "ask", "question")
	f.forge.addComment(testIssue, 50, owner(), "Use the HTTP boundary")
	f.settle()
	delivery, err := f.db.GetReportDelivery(t.Context(), p.key)
	if err != nil || !delivery.Completed {
		t.Fatalf("question delivery not confirmed: %+v %v\n%s", delivery, err, f.logs)
	}
	latest, err := f.db.LatestWorkflowInstance(t.Context(), p.session.ID, "research")
	if err != nil || latest.ID != p.instance.ID {
		t.Fatalf("a successor replaced the question's instance: %+v %v", latest, err)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), p.instance.ID)
	if err != nil || (wait.State != "claimed" && wait.State != "allocated") || wait.ReplyID != 50 || wait.DeliveryKey != p.key {
		t.Fatalf("the triggering reply did not answer the question: %+v %v\n%s", wait, err, f.logs)
	}
	if _, err := f.db.LatestGraphDispatch(t.Context(), p.instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a question resume must not schedule a dispatch: %v", err)
	}
}

// #1246 C5: a restart after the committed resume yields exactly one
// continuation, and a further recovery adds nothing.
func TestRecoveredReportResumeSurvivesRestartOnce(t *testing.T) {
	f := newFixture(t)
	p := failedPublicationFixture(t, f, "report", "final")
	// Commit the resume without letting this daemon process its dispatch.
	if err := turn.RecoverGraphPublications(t.Context(), turn.Options{DB: f.db, ResearchReporter: f.reporter}, p.session.ID, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	due := pendingDispatches(t, f.db, p.instance.ID)
	if len(due) != 1 {
		t.Fatalf("resume did not commit one dispatch: %+v\n%s", due, f.logs)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.db = reopened
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Logger: f.logger, Environ: []string{}}), Limit: 65536, Logger: f.logger})
	if err != nil {
		t.Fatal(err)
	}
	f.reporter, err = turn.NewReporter(turn.ReporterOptions{BotUserID: botID, StoryLockDir: f.paths.StoryProgressLocksDir(), DB: reopened, Forge: f.client, Renderer: renderer, Logger: f.logger})
	if err != nil {
		t.Fatal(err)
	}
	identities, err := turn.LoadIdentities(t.Context(), reopened, store.ForgeGitHub)
	if err != nil {
		t.Fatal(err)
	}
	if f.filter, err = turn.NewFilter(reopened, identities, f.logger); err != nil {
		t.Fatal(err)
	}
	f.turns = &fakeExecutor{fn: f.ranSuccessfully, forge: f.forge}
	opts := turn.Options{DB: reopened, ResearchReporter: f.reporter}
	f.daemon = f.newDaemon(daemon.Options{DB: reopened, Filter: f.filter, Reporter: f.reporter, InstanceID: "restarted", Turn: publicationRecoveringExecutor{fakeExecutor: f.turns, opts: opts}})
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	f.settle()
	trigger := due[0].TriggerID()
	calls := 0
	for _, req := range f.turns.calls() {
		if req.Trigger.CommentID == trigger {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("dispatch continuations = %d, want 1\n%s", calls, f.logs)
	}
	if left := pendingDispatches(t, reopened, p.instance.ID); len(left) != 0 {
		t.Fatalf("dispatch still pending after processing: %+v", left)
	}
	if _, found, err := reopened.GetRunByTrigger(t.Context(), p.session.ID, trigger); err != nil || !found {
		t.Fatalf("no run allocated for the dispatch: %v %v", found, err)
	}
	if err := turn.RecoverGraphPublications(t.Context(), opts, p.session.ID, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n := publishedTransitions(t, reopened, p.instance.ID, "report"); n != 1 {
		t.Fatalf("report transitions = %d, want 1", n)
	}
	if left := pendingDispatches(t, reopened, p.instance.ID); len(left) != 0 {
		t.Fatalf("further recovery added a dispatch: %+v", left)
	}
}

// toggleRenderer fails report rendering while fail is set, as the display
// redaction did for an analysis that mentioned a literal-text tag (#1283).
type toggleRenderer struct {
	turn.Renderer
	fail *atomic.Bool
}

func (r toggleRenderer) RenderReportDelivery(ctx context.Context, body, marker string) (redact.Body, error) {
	if r.fail.Load() {
		return redact.Body{}, errors.New("review display has an incomplete literal-text span")
	}
	return r.Renderer.RenderReportDelivery(ctx, body, marker)
}

// parkedBugWithoutDelivery parks a Bug conversation review whose report could
// not be rendered: the owner wait exists, but no report delivery was stored.
// Rendering keeps failing until the returned flag is cleared.
func parkedBugWithoutDelivery(t *testing.T) (*fixture, store.GraphInstance, *atomic.Bool) {
	t.Helper()
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := newFixture(t)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Logger: f.logger, Token: secrets.NewToken(testToken), Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: f.logger})
	must(err)
	fail := new(atomic.Bool)
	reporter, err := turn.NewReporter(turn.ReporterOptions{BotUserID: botID, DB: f.db, Forge: f.client, Renderer: toggleRenderer{Renderer: renderer, fail: fail}, Logger: f.logger})
	must(err)
	binary, err := git.Resolve(ctx)
	must(err)
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "undeliverable-review", Logger: f.logger, CloneGate: unusedRecoveryCloneGate{}, Runners: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, ResearchReporter: reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID, Now: f.clock.Now}}
	runtime, err := turn.NewGraphBugRuntime(ctx, opts, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) {
		return turn.BugRolePermissions{}, errors.New("test must not allocate a role")
	}})
	must(err)
	executorOpts := opts
	executorOpts.Bug = runtime
	f.daemon = f.newDaemon(daemon.Options{Bug: runtime, Turn: daemon.NewExecutor(executorOpts)})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1283, Number: testIssue, Title: "Undeliverable review", User: stranger(), Labels: toFakeLabels("bug")})
	f.settle()
	s := f.sessionOf(testIssue)
	_, err = f.db.AdmitWorkflowPath(ctx, s.ID, "graph")
	must(err)
	_, err = f.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 22})
	must(err)
	specs, err := turn.SimplifiedBugSpecs()
	must(err)
	capture, err := teamsourcetest.CaptureBug(ctx, specs)
	must(err)
	profile := capture.Profile
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: profile.Digest})
	must(err)
	topology, err := graphexec.SemanticDigest(profile)
	must(err)
	files, err := json.Marshal(capture.Files)
	must(err)
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: s.ID, TriggerID: 22, DefinitionID: pin.ID, Start: profile.Start, ContentDigest: capture.Digest, TopologyDigest: topology, Files: files})
	must(err)
	inst := admitted.Instance
	activities := turn.ActivityRegistry{}
	must(activities.Register(graphexec.ResearchLoopAction, turn.GraphBugConversation{Profile: profile, Reporter: reporter}))
	step := func() (turn.GraphNodeResult, error) {
		return turn.ExecuteNodeAction(ctx, opts, turn.GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: inst.ID, At: f.clock.Now()})
	}
	first, err := step()
	must(err)
	if first.NextNode != "analyst" {
		t.Fatalf("initial entry=%+v", first)
	}
	// The analyst's accepted result, retained as production retains it.
	at := f.clock.Now()
	segment, err := f.db.GetOrCreateSegment(ctx, s.ID, 1, "conversation-analyst", store.AgentClaude)
	must(err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: s.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 22, StartedAt: at, LogPath: func(int) string { return "analyst.log" }})
	must(err)
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: inst.ID, Node: "analyst", RunID: run.ID, Limit: 8, StartedAt: at})
	must(err)
	analysis := buganalysis.BugAnalysis{Expected: "Safe empty input", Actual: "Panic on empty input", Reproduction: buganalysis.BugReproduction{Environment: "Go fixture", Observation: "not_observed"}, Cause: "Unchecked index", Evidence: []string{"search.go:12 indexes empty bytes"}, Remedy: "Guard input length", Exclusions: []string{"Nonempty behavior"}, Criteria: []buganalysis.BugCriterion{{ID: "C1", Requirement: "No panic", Measurement: "Call empty input"}}}
	result := agentresult.Result{Outcome: "completed", Comment: "Complete candidate", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "done", Invocation: fmt.Sprintf("analysis:%d:%d", inst.ID, attempt.ID), Analysis: &analysis}}}
	binding, err := f.db.BindResultAttempt(ctx, store.ResultAttempt{Workflow: "bug", Task: s.TaskRef, RunID: run.ID, WorkContext: fmt.Sprintf("undeliverable-%d", attempt.ID), Attempt: fmt.Sprintf("analyst-%d", attempt.ID), SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	must(err)
	snapshot, err := json.Marshal(agentresult.Snapshot{Result: result})
	must(err)
	_, err = f.db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "analyst-result", Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"complete":true}`), Retired: []byte(`[]`), Accepted: true})
	must(err)
	retained, err := turn.RetainBugNode(binding, 1, agentresult.ResearchNode{Version: 1, Response: "done", Result: result})
	must(err)
	_, err = f.db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: []byte(`{"record":"source_preparation","sync_outcome":"up_to_date"}`), CreatedAt: at})
	must(err)
	_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded})
	must(err)
	must(f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "done", FinishedAt: at, InstanceStatus: store.GraphRunning, NextDueAt: at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: retained}}, Transition: &store.GraphTransition{InstanceID: inst.ID, FromNode: "analyst", ToNode: "conversation", OnOutcome: "done", At: at}}))
	// The review parks, then its report cannot be rendered.
	fail.Store(true)
	if _, err = step(); err == nil {
		t.Fatal("review publication did not fail")
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, inst.ID)
	must(err)
	if wait.State != "waiting" {
		t.Fatalf("review not parked: %+v", wait)
	}
	if _, err = f.db.GetReportDelivery(ctx, wait.DeliveryKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delivery exists after render failure: %v", err)
	}
	if posted, _, _ := f.forge.snapshot(); len(posted) != 0 {
		t.Fatalf("undeliverable review posted: %v", posted)
	}
	return f, inst, fail
}

func countPosts(f *fixture, text string) int {
	posted, _, _ := f.forge.snapshot()
	n := 0
	for _, body := range posted {
		if strings.Contains(body, text) {
			n++
		}
	}
	return n
}

const (
	analysisHeading      = "Bug analysis, revision 1"
	betweenQuestionsText = "Your instruction is retained and undelivered"
	notAppliedText       = "could not be applied to the bug work at its current step"
	missingDeliveryError = "read report delivery"
)

// #1283 C3, C6(a), C6(c): the next owner comment posts the parked review once,
// is answered with the between-questions notice only after that notice is
// delivered, and a later reply claims the wait normally.
func TestBugReplyRecoversUndeliveredReview(t *testing.T) {
	f, inst, fail := parkedBugWithoutDelivery(t)
	fail.Store(false)
	// The notice POST (the second post: after the recovered review) fails once.
	f.forge.mu.Lock()
	base, failed := len(f.forge.postedBodies), false
	f.forge.onPostComment = func(w http.ResponseWriter, call int) bool {
		if failed || call != base+2 {
			return false
		}
		failed = true
		http.Error(w, `{"message":"validation failed"}`, http.StatusUnprocessableEntity)
		return true
	}
	f.forge.mu.Unlock()
	f.forge.addComment(testIssue, 42, owner(), "Any progress?")
	f.settle()
	if got := countPosts(f, analysisHeading); got != 1 {
		t.Fatalf("recovered review posted %d times\n%s", got, f.logs)
	}
	if f.watermark() >= 42 && countPosts(f, betweenQuestionsText) == 0 {
		t.Fatalf("comment consumed before its notice was delivered\n%s", f.logs)
	}
	if !settleUntilHandled(f, 42, 30) {
		t.Fatalf("triggering comment never consumed\n%s", f.logs)
	}
	// Replays after consumption post neither the review nor the notice again.
	for range 2 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if review, notice := countPosts(f, analysisHeading), countPosts(f, betweenQuestionsText); review != 1 || notice != 1 || countPosts(f, notAppliedText) != 0 {
		t.Fatalf("review=%d notice=%d, want one each\n%s", review, notice, f.logs)
	}
	if strings.Contains(f.logs.String(), missingDeliveryError) {
		t.Fatalf("missing delivery still reported\n%s", f.logs)
	}
	// A reply newer than the recovered review.
	f.forge.mu.Lock()
	later := f.forge.nextCommentID + 100
	f.forge.mu.Unlock()
	f.forge.addComment(testIssue, later, owner(), "Please make the guard explicit.")
	f.settle()
	wait, err := f.db.GetGraphOwnerWait(t.Context(), inst.ID)
	if err != nil || wait.ReplyID != later {
		t.Fatalf("later reply not claimed: %+v %v\n%s", wait, err, f.logs)
	}
}

// #1283 C3, C6(b): while rendering still fails, the owner comment gets one
// visible not-applied notice and is consumed; once rendering works, the next
// comment posts the review.
func TestBugReplyAnswersPersistentRenderFailure(t *testing.T) {
	f, inst, fail := parkedBugWithoutDelivery(t)
	f.forge.addComment(testIssue, 42, owner(), "Any progress?")
	if !settleUntilHandled(f, 42, 4) {
		t.Fatalf("comment never consumed\n%s", f.logs)
	}
	if row, err := f.db.GetReportRecovery(t.Context(), fmt.Sprintf("source:%d:42", inst.SessionID)); err == nil && row.State != "resolved" {
		t.Fatalf("retry left for a consumed comment: %+v", row)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	for range 2 {
		f.clock.advance(time.Minute)
		f.settle()
	}
	if review, notice := countPosts(f, analysisHeading), countPosts(f, notAppliedText); review != 0 || notice != 1 {
		t.Fatalf("review=%d not-applied=%d, want 0 and 1\n%s", review, notice, f.logs)
	}
	if strings.Contains(f.logs.String(), missingDeliveryError) {
		t.Fatalf("missing delivery still reported\n%s", f.logs)
	}
	fail.Store(false)
	f.forge.addComment(testIssue, 43, owner(), "Try again.")
	if !settleUntilHandled(f, 43, 4) {
		t.Fatalf("second comment never consumed\n%s", f.logs)
	}
	if review := countPosts(f, analysisHeading); review != 1 {
		t.Fatalf("review posted %d times after rendering recovered\n%s", review, f.logs)
	}
}
