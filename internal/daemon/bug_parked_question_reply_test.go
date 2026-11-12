package daemon_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// #1514: a Bug parked at deliver whose question was never posted (the POST was
// refused, so the delivery stays pending with nothing sent) must not refuse the
// owner's next comment with "reply does not match published active graph
// question". Publication recovery runs for Bug before the reply is matched, so
// the question is posted first and the comment answers it.
func TestBugUnpublishedParkedQuestionTakesOwnersReply(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 1514, Number: testIssue, Title: "Bug delivery", User: stranger(), Labels: toFakeLabels("bug")})
	f.settle()
	s := f.sessionOf(testIssue)
	_, err := f.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: s.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: 22})
	must(err)
	_, err = f.db.AdmitWorkflowPath(ctx, s.ID, "graph")
	must(err)
	specs, err := turn.BugSpecs()
	must(err)
	capture, err := teamsourcetest.CaptureBug(ctx, specs)
	must(err)
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: capture.Profile.Team, Mode: "bug", ProfileVersion: capture.Profile.ProfileVersion, Path: "bug/bug.dot", Digest: capture.Profile.Digest})
	must(err)
	topology, err := graphexec.SemanticDigest(capture.Profile)
	must(err)
	files, err := json.Marshal(capture.Files)
	must(err)
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "bug", SessionID: s.ID, TriggerID: 22, DefinitionID: pin.ID, Start: "bug", ContentDigest: capture.Digest, TopologyDigest: topology, Files: files})
	must(err)
	instance := admitted.Instance
	at := f.clock.Now()
	segment, err := f.db.GetOrCreateSegment(ctx, s.ID, 1, "bug", store.AgentClaude)
	must(err)
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: s.ID, SegmentID: segment.ID, Mode: "bug", Agent: store.AgentClaude, TriggerCommentID: 22, LogPath: func(int) string { return "bug.log" }})
	must(err)
	source, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, LaunchID: admitted.LaunchID, Node: "bug", RunID: run.ID, StartedAt: at})
	must(err)
	must(f.db.CompleteGraphNodeAttempt(ctx, source.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "done", FinishedAt: at, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "bug", ToNode: "deliver", OnOutcome: "done", At: at}}))
	_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: at})
	must(err)
	deliver, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, LaunchID: admitted.LaunchID, Node: "deliver", StartedAt: at, FactoryStep: true})
	must(err)
	question := turn.GraphStopQuestion{Version: 1, InstanceID: instance.ID, SourceAttemptID: deliver.ID, Origin: "deliver", Cause: "blocked", Reason: "The merge was refused."}
	raw, err := json.Marshal(question)
	must(err)
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])
	completion := store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "blocked", FinishedAt: at, InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputDiagnostic, Payload: []byte(question.Reason)}}}
	must(f.db.ParkGraphNode(ctx, store.GraphStop{Key: key, InstanceID: instance.ID, SourceAttemptID: deliver.ID, Origin: "deliver", Cause: "blocked", Reason: question.Reason, Candidate: raw, At: at, AllowRunless: true, Completion: &completion}))
	// The question's POST is refused outright: the delivery row exists but
	// nothing was sent, as on #1507.
	f.forge.mu.Lock()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, `{"message":"validation failed"}`, http.StatusUnprocessableEntity)
		return true
	}
	f.forge.mu.Unlock()
	if _, err = f.reporter.RecoverGraphStopQuestion(ctx, key); err == nil {
		t.Fatal("the parked question POST did not fail")
	}
	f.forge.mu.Lock()
	f.forge.onPostComment = nil
	f.forge.mu.Unlock()
	if err = f.db.UnmarkReportDeliverySent(ctx, key); err != nil {
		t.Fatal(err)
	}
	pending, err := f.db.GetReportDelivery(ctx, key)
	must(err)
	if pending.Completed || pending.PossiblySent || pending.CommentID != 0 {
		t.Fatalf("fixture delivery: %+v", pending)
	}
	f.turns = &fakeExecutor{fn: f.ranSuccessfully, forge: f.forge}
	f.daemon = f.newDaemon(daemon.Options{Turn: publicationRecoveringExecutor{fakeExecutor: f.turns, opts: turn.Options{DB: f.db, ResearchReporter: f.reporter}}})
	f.settle()

	f.forge.addComment(testIssue, 50, owner(), "ship")
	f.settle()
	delivery, err := f.db.GetReportDelivery(ctx, key)
	if err != nil || !delivery.Completed || delivery.CommentID == 0 {
		t.Fatalf("the parked question was not posted before the reply was matched: %+v %v\n%s", delivery, err, f.logs)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || (wait.State != "claimed" && wait.State != "allocated") || wait.ReplyID != 50 || wait.DeliveryKey != key {
		t.Fatalf("the owner's reply did not answer the parked question: %+v %v\n%s", wait, err, f.logs)
	}
}
