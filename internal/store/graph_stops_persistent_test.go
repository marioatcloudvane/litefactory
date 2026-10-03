package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func persistentBugStopFixture(t *testing.T) (*DB, string, GraphInstance, Session) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stops.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 457)
	if _, err = db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug.dot", Digest: strings.Repeat("b", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "repair", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	return db, path, instance, session
}

func reopenBugStopDB(t *testing.T, db *DB, path string) *DB {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func TestGraphStopPersistentSourceLessClaimAndBound(t *testing.T) {
	db, path, instance, session := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	first, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", Limit: 1, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishGraphNodeAttempt(ctx, first.ID, GraphFailed, "", at); err != nil {
		t.Fatal(err)
	}
	stop := GraphStop{Key: "persistent-stop", InstanceID: instance.ID, Origin: "repair", Cause: "exhausted", Reason: "The finite invocation budget was spent", Candidate: []byte(`{"reason":"budget spent","evidence":"saved-log"}`), At: at, AllowRunless: true}
	if err = db.ParkGraphNode(ctx, stop); err != nil {
		t.Fatal(err)
	}
	db = reopenBugStopDB(t, db, path)
	intent, err := db.GetGraphComment(ctx, stop.Key)
	if err != nil || intent.SourceAttemptID != 0 || string(intent.Candidate) != string(stop.Candidate) {
		t.Fatalf("intent=%+v err=%v", intent, err)
	}
	wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.OriginNode != "repair" || wait.State != "waiting" {
		t.Fatalf("wait=%+v err=%v", wait, err)
	}
	if _, err = db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", Limit: 1, StartedAt: at}); err == nil {
		t.Fatal("waiting budget dispatched")
	}
	_, err = db.EnsureReportDelivery(ctx, ReportDelivery{Key: stop.Key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#457", TriggerNamespace: "issue-comment", Purpose: "bug-question", BotUserID: 99, Body: "Reply with guidance", Marker: stop.Key})
	if err != nil {
		t.Fatal(err)
	}
	reply := GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, QuestionCommentID: 101, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Workflow: "bug", DeliveryKey: stop.Key, Body: "Continue with the saved work", At: at.Add(time.Minute)}
	if _, err = db.ClaimGraphOwnerReply(ctx, reply); err == nil {
		t.Fatal("unpublished question admitted")
	}
	if _, err = db.MarkReportDeliverySent(ctx, stop.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmReportDelivery(ctx, stop.Key, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteReportDelivery(ctx, stop.Key); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*GraphOwnerReply){
		"foreign issue":    func(r *GraphOwnerReply) { r.SourceRef = "github:acme/repo#999" },
		"unauthorized":     func(r *GraphOwnerReply) { r.AuthorID = 8 },
		"foreign workflow": func(r *GraphOwnerReply) { r.Workflow = "plan" },
		"stale question":   func(r *GraphOwnerReply) { r.QuestionCommentID = 100 },
		"predating":        func(r *GraphOwnerReply) { r.CommentID = 100 },
	} {
		bad := reply
		mutate(&bad)
		if _, err = db.ClaimGraphOwnerReply(ctx, bad); err == nil {
			t.Fatalf("%s claimed continuation", name)
		}
	}
	if _, err = db.ClaimGraphOwnerReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	db = reopenBugStopDB(t, db, path)
	if _, err = db.ClaimGraphOwnerReply(ctx, reply); !errors.Is(err, ErrGraphOwnerWait) {
		t.Fatalf("duplicate claim: %v", err)
	}
	next, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", Limit: 1, StartedAt: reply.At})
	if err != nil || next.Attempt != 2 {
		t.Fatalf("continuation=%+v err=%v", next, err)
	}
	wait, err = db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.State != "allocated" || wait.ReplyBody != reply.Body || wait.ContinuationAttemptID != next.ID {
		t.Fatalf("allocated=%+v err=%v", wait, err)
	}
	if err = db.FinishGraphNodeAttempt(ctx, next.ID, GraphFailed, "", reply.At); err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", Limit: 1, StartedAt: reply.At}); !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("claim released more than one invocation: %v", err)
	}
}

func TestGraphStopPersistentCompletionRollback(t *testing.T) {
	db, path, instance, _ := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	stop := GraphStop{Key: "rollback-stop", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Origin: "repair", Cause: "failed", Reason: "execution interrupted", Candidate: []byte(`{"reason":"execution interrupted"}`), At: at, AllowRunless: false, Completion: &GraphCompletion{Status: GraphFailed, Outcome: "", FinishedAt: at, InstanceStatus: GraphRunning}}
	if err = db.ParkGraphNode(ctx, stop); !errors.Is(err, ErrGraphInvariant) {
		t.Fatalf("invalid source err=%v", err)
	}
	db = reopenBugStopDB(t, db, path)
	got, err := db.GetGraphNodeAttempt(ctx, attempt.ID)
	if err != nil || got.Status != GraphRunning {
		t.Fatalf("completion escaped rollback: %+v %v", got, err)
	}
	if _, err = db.GetGraphComment(ctx, stop.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back question exists: %v", err)
	}
	stop.AllowRunless = true
	if err = db.ParkGraphNode(ctx, stop); err != nil {
		t.Fatal(err)
	}
	db = reopenBugStopDB(t, db, path)
	got, err = db.GetGraphNodeAttempt(ctx, attempt.ID)
	if err != nil || got.Status != GraphFailed || got.Outcome != "" {
		t.Fatalf("failed history=%+v %v", got, err)
	}
	wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil || wait.SourceAttemptID != attempt.ID || wait.OriginNode != "repair" {
		t.Fatalf("wait=%+v %v", wait, err)
	}
}
