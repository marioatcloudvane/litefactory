package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func persistentStopReporterFixture(t *testing.T) (*fixture, *fakeForge, *graphexec.Profile, store.GraphInstance) {
	t.Helper()
	f := newFixture(t, nil)
	_, err := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	profile := &graphexec.Profile{Team: "builtin", Workflow: agent.WorkflowBug, ProfileVersion: 1, Digest: strings.Repeat("d", 64), Nodes: []graphexec.Node{{Name: "analyst", Action: ResearchAgentAction, Attrs: map[string]string{"lf_park": "owner", "lf_stop": "blocked,failed,invalid,exhausted", "lf_response": "result"}}}}
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: 1, Path: "bug.dot", Digest: profile.Digest, CreatedAt: time.Now().UTC()})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "analyst", StartedAt: time.Now().UTC(), NextDueAt: time.Now().UTC()})
	mustProgress(t, err)
	return f, newFakeForge(t), profile, instance
}

func retainPersistentStopQuestion(t *testing.T, f *fixture, instance store.GraphInstance) string {
	t.Helper()
	question := GraphStopQuestion{Version: 1, InstanceID: instance.ID, Origin: "analyst", Cause: "exhausted", Reason: "Finite budget spent; retain the prior findings."}
	raw, err := json.Marshal(question)
	mustProgress(t, err)
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:])
	mustProgress(t, f.db.ParkGraphNode(t.Context(), store.GraphStop{Key: key, InstanceID: instance.ID, Origin: question.Origin, Cause: question.Cause, Reason: question.Reason, Candidate: raw, At: time.Now().UTC()}))
	return key
}

func reopenPersistentStopFixture(t *testing.T, f *fixture) {
	t.Helper()
	// Preserve the fixture-owned DB pointer so its cleanup and new reporter use
	// the reopened real database rather than an abandoned connection.
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	*f.db = *reopened
}

func TestGraphStopPersistentReporterLostAcknowledgment(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	reporter := newReporter(t, f, forge, nil)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	posted, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	if err == nil || posted.CommentID == 0 {
		t.Fatalf("lost persistence acknowledgment hidden: posted=%+v err=%v", posted, err)
	}
	original := forge.only(t).body
	reopenPersistentStopFixture(t, f)
	forge.afterPost = nil
	before, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if !before.PossiblySent || before.Completed || before.Body != original {
		t.Fatalf("publication debt lost: %+v", before)
	}
	reporter = newReporter(t, f, forge, nil)
	recovered, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	if recovered.CommentID != posted.CommentID || forge.attemptCount() != 1 {
		t.Fatalf("recovery repeated uncertain post: %+v posts=%d", recovered, forge.attemptCount())
	}
	after, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if !after.Completed || after.Body != original || after.Key != key {
		t.Fatalf("immutable delivery changed: %+v", after)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || wait.SourceAttemptID != 0 || wait.OriginNode != "analyst" {
		t.Fatalf("publication released source-less work: %+v", wait)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(attempts) != 0 {
		t.Fatalf("publication invented agent dispatch: %+v", attempts)
	}
	if _, err = reporter.RecoverWorkflowComment(t.Context(), key, agentresult.Context{}, BugWorkflowPolicy()); err != nil {
		t.Fatal(err)
	}
	if forge.attemptCount() != 1 {
		t.Fatal("completed recovery reposted")
	}
}

func TestGraphStopPersistentReporterRefusalRetainsDebt(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	forge.failWith(503)
	_, err := newReporter(t, f, forge, nil).RecoverGraphStopQuestion(t.Context(), key)
	if err == nil {
		t.Fatal("publication failure hidden")
	}
	before, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if before.Completed || !before.PossiblySent || before.CommentID != 0 {
		t.Fatalf("failure manufactured publication: %+v", before)
	}
	mustProgress(t, f.db.Close())
	reopenPersistentStopFixture(t, f)
	forge.recover()
	// A complete lookup with no marker-bearing comment reposts the retained
	// question exactly once.
	if _, err = newReporter(t, f, forge, nil).RecoverGraphStopQuestion(t.Context(), key); err != nil {
		t.Fatalf("empty lookup did not repost: %v", err)
	}
	if forge.attemptCount() != 2 || forge.only(t).body != before.Body {
		t.Fatalf("repost count or bytes wrong: attempts=%d", forge.attemptCount())
	}
	after, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if !after.Completed || after.Body != before.Body {
		t.Fatalf("repost not recorded: %+v", after)
	}
}

func TestGraphStopPersistentOrphanRecoveryParksWithoutDispatch(t *testing.T) {
	f, forge, profile, instance := persistentStopReporterFixture(t)
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "bug-results", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 5, LogPath: func(int) string { return "orphan.log" }})
	mustProgress(t, err)
	at := time.Now().UTC()
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: run.ID, StartedAt: at})
	mustProgress(t, err)
	req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at}
	if _, err = RecoverGraphStoppedExecution(t.Context(), f.opts, req, attempt.ID); err == nil {
		t.Fatal("unreconciled active run parked")
	}
	_, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, Error: "provider interrupted after allocation"})
	mustProgress(t, err)
	mustProgress(t, f.db.Close())
	reopenPersistentStopFixture(t, f)
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	recovered, err := RecoverGraphStoppedExecution(t.Context(), f.opts, req, attempt.ID)
	mustProgress(t, err)
	if recovered.Status != "waiting_owner" || recovered.NextNode != "analyst" {
		t.Fatalf("orphan progressed: %+v", recovered)
	}
	history, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	if len(history) != 1 || history[0].ID != attempt.ID || history[0].Status != store.GraphFailed {
		t.Fatalf("recovery allocated/reclassified execution: %+v", history)
	}
	if !strings.Contains(forge.only(t).body, "provider interrupted after allocation") {
		t.Fatal("interruption reason lost")
	}
}

func TestGraphRunlessStopProgressSurvivesShorteningAndLostAcknowledgment(t *testing.T) {
	f, forge, profile, instance := persistentStopReporterFixture(t)
	ctx, at := t.Context(), time.Now().UTC()
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
	req := GraphNodeRequest{Profile: profile, InstanceID: instance.ID, At: at}
	out := GraphNodeResult{InstanceID: instance.ID, Node: "analyst", Activity: ActivityResult{Diagnostic: strings.Repeat("long explanation ", 2000)}}
	// Parking remains durable even when no reporter is available.
	f.opts.ResearchReporter = nil
	_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
	if err == nil {
		t.Fatal("missing reporter hidden")
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	intent, err := f.db.GetGraphComment(ctx, wait.DeliveryKey)
	mustProgress(t, err)
	var question GraphStopQuestion
	mustProgress(t, json.Unmarshal(intent.Candidate, &question))
	if !strings.Contains(question.Reason, "[explanation shortened]") || !strings.HasSuffix(question.Reason, fmt.Sprintf("Latest completed attempt: %d.", attempt.ID)) || question.SourceAttemptID != 0 {
		t.Fatalf("progress lost: %+v", question)
	}
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	forge.afterPost = func() { mustProgress(t, f.db.Close()) }
	_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
	if err == nil {
		t.Fatal("lost acknowledgment hidden")
	}
	reopenPersistentStopFixture(t, f)
	forge.afterPost = nil
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	req.At = at.Add(time.Hour)
	for range 2 {
		_, err = parkGraphAction(ctx, f.opts, req, out, "exhausted", false)
		mustProgress(t, err)
	}
	recovered, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	if recovered.DeliveryKey != wait.DeliveryKey || recovered.State != "waiting" || recovered.ContinuationAttemptID != 0 || forge.attemptCount() != 1 {
		t.Fatalf("retry changed stop or allowance: %+v", recovered)
	}
}

func TestGraphStopMaterialUpdateKeepsOriginalWaitAndNotice(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	reporter := newReporter(t, f, forge, nil)
	original, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	retained, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 42, AuthorID: 42, CommentID: original.CommentID + 1, QuestionCommentID: original.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: key, Body: "Keep the findings and continue this step", At: time.Now().UTC()})
	mustProgress(t, err)
	update, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	if update.CommentID == original.CommentID || len(forge.all()) != 2 {
		t.Fatalf("material update missing: %+v %+v", original, update)
	}
	again, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	if again.CommentID != update.CommentID || len(forge.all()) != 2 {
		t.Fatal("unchanged disposition duplicated notice")
	}
	originalAfter, err := f.db.GetReportDelivery(t.Context(), key)
	mustProgress(t, err)
	if originalAfter != retained {
		t.Fatal("material update rewrote original immutable notice")
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.DeliveryKey != key || wait.State != "claimed" || watermark(t, f) != 0 {
		t.Fatalf("informational update consumed input or changed original wait: %+v watermark=%d", wait, watermark(t, f))
	}
	posts := forge.all()
	if !strings.Contains(posts[1].body, key) || !strings.Contains(posts[1].body, "continuation admitted") || strings.Contains(posts[1].body, "confirmed completion") {
		t.Fatalf("update overclaims recovery or lost linkage: %s", posts[1].body)
	}
}
