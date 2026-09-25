package store

import (
	"strings"
	"testing"
)

// The failure report is a retained graph comment at the named failure node,
// whose source attempt belongs to the run and whose delivery completed. Until
// then the owner's triggering comment may not be marked handled.
func TestGraphFailureReportedRequiresACompletedDeliveryForTheRun(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 380)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	run, other := testRun(t, db, session.ID, 10), testRun(t, db, session.ID, 11)
	definition, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitResearchLaunch(t.Context(), ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "shape", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.md":"c2hhcGU="}`)})
	if err != nil {
		t.Fatal(err)
	}
	instance := admitted.Instance.ID
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance, Node: "shape", RunID: run.ID, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphFailed, FinishedAt: graphTestTime(), InstanceStatus: GraphFailed}); err != nil {
		t.Fatal(err)
	}
	byRun, err := db.ListGraphNodeAttemptsByRun(t.Context(), run.ID)
	if err != nil || len(byRun) != 1 || byRun[0].ID != attempt.ID {
		t.Fatalf("attempts by run: %+v %v", byRun, err)
	}
	if none, err := db.ListGraphNodeAttemptsByRun(t.Context(), other.ID); err != nil || len(none) != 0 {
		t.Fatalf("another run's attempts leaked: %+v %v", none, err)
	}
	if _, err = db.ListGraphNodeAttemptsByRun(t.Context(), 0); err == nil {
		t.Fatal("a missing run was listed")
	}
	reported := func(runID int64, node string) bool {
		t.Helper()
		got, err := db.GraphFailureReported(t.Context(), runID, node)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if reported(run.ID, "failed") {
		t.Fatal("a failure was reported before any comment existed")
	}
	key := "failure-comment"
	if err = db.RetainGraphComment(t.Context(), GraphCommentIntent{Key: key, InstanceID: instance, SourceAttemptID: attempt.ID, Node: "failed", Kind: "failure", Candidate: []byte(`{"text":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	if reported(run.ID, "failed") {
		t.Fatal("a retained but undelivered comment counted as reported")
	}
	if _, err = db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: key, SessionID: session.ID, TriggerID: 10, RunID: run.ID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "failure", BotUserID: 77, Body: "body", Marker: "marker"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConfirmReportDelivery(t.Context(), key, 5001); err != nil {
		t.Fatal(err)
	}
	if reported(run.ID, "failed") {
		t.Fatal("a confirmed but uncompleted delivery counted as reported")
	}
	if _, err = db.CompleteReportDelivery(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if !reported(run.ID, "failed") {
		t.Fatal("a completed failure comment was not reported")
	}
	if reported(other.ID, "failed") || reported(run.ID, "waiting") {
		t.Fatal("the report leaked to another run or node")
	}
	if _, err = db.GraphFailureReported(t.Context(), run.ID, ""); err == nil {
		t.Fatal("an unnamed node was queried")
	}
}
