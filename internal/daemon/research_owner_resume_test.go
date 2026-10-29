package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
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
