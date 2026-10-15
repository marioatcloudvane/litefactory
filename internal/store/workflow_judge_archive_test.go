package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWorkflowJudgeArchivePreservesFinishedBindingOnClosedIssue(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := archiveTestSession(t, db, "acme", 810)
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "judging", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "source", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	source, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "source", Limit: 1, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, source.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: instance.ID, FromNode: "source", ToNode: "judge", OnOutcome: "success", At: graphTestTime()}}); err != nil {
		t.Fatal(err)
	}
	judge, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "judge", Limit: 1, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.AdmitJudgeRequest(ctx, JudgeRequest{SessionID: session.ID, Task: session.TaskRef, TicketKey: "qualified", CommentID: "workflow", CallerKind: "workflow", GraphInstanceID: instance.ID, JudgeNodeAttemptID: judge.ID, SourceNode: "source", SourceNodeAttemptID: source.ID, Source: JudgeSource{Task: session.TaskRef, Reference: "saved-exact-source"}, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	report := []byte(`{"retained":"private advisory report"}`)
	if err = db.SaveJudgeReport(ctx, request.ID, report); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, judge.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef, IssueClosed: true}
	now := time.Now().UTC()
	reason, err := archiveCheckReason(ctx, db.sql, family, now, true)
	if err != nil || reason != "workflow_judge_reference" {
		t.Fatalf("closed archive guard: %q %v", reason, err)
	}
	trailer := archiveTestTrailer(t, db, family)
	if _, err = db.DeleteArchivedFamily(ctx, family, trailer, now); !errors.Is(err, ErrArchiveNotSettled) || !strings.Contains(err.Error(), "workflow_judge_reference") {
		t.Fatalf("closed judged family was archived: %v", err)
	}
	retained, err := db.FindWorkflowJudgeRequest(ctx, instance.ID, judge.ID)
	if err != nil || retained.ID != request.ID || retained.SourceNodeAttemptID != source.ID || retained.GraphInstanceID != instance.ID || !bytes.Equal(retained.Report, report) {
		t.Fatalf("private binding changed: %+v %v", retained, err)
	}
	for _, id := range []int64{source.ID, judge.ID} {
		if _, err = db.GetGraphNodeAttempt(ctx, id); err != nil {
			t.Fatalf("retained graph reference removed: %d %v", id, err)
		}
	}
	problems, err := db.ArchiveRegistryProblems(ctx)
	if err != nil || len(problems) != 0 {
		t.Fatalf("unguarded archive foreign keys: %v %v", problems, err)
	}
}
