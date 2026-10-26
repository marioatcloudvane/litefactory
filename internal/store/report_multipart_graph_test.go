package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRunningGraphMultipartRequiresExactRetainedChild(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 919)
	if _, err := db.AdmitResearchPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := db.AdmitResearchLaunch(ctx, ResearchLaunch{SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "source", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.md":"c2hhcGU="}`)})
	if err != nil {
		t.Fatal(err)
	}
	run := resultRun(t, db, session, "research", "source", 1)
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "source", RunID: run.RunID, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteGraphNodeAttempt(ctx, attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainGraphComment(ctx, GraphCommentIntent{Key: "graph-multipart", InstanceID: admitted.Instance.ID, SourceAttemptID: attempt.ID, Node: "publish", Kind: "final", Candidate: []byte(`{"comment":"complete source"}`)}); err != nil {
		t.Fatal(err)
	}
	parent := ReportDelivery{Key: "graph-multipart", Version: 1, SessionID: session.ID, TriggerID: 1, RunID: run.RunID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "report-multipart", Purpose: "graph-comment", BotUserID: 99, Marker: "<!-- parent -->"}
	child := ReportDelivery{Key: "graph-part-1", Version: 1, SessionID: session.ID, TriggerID: 1, RunID: run.RunID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "report-part", Purpose: "run-report-part:graph-multipart:1/1", BotUserID: 99, Body: "complete part\n\n<!-- child -->", Marker: "<!-- child -->"}
	if _, err := db.EnsureReportDelivery(ctx, child); err == nil {
		t.Fatal("running child retained without a durable parent")
	}
	manifest, err := json.Marshal(ReportMultipart{Summary: "summary", Parts: []ReportDelivery{child}})
	if err != nil {
		t.Fatal(err)
	}
	parent.Body = string(manifest)
	if _, err := db.EnsureReportDelivery(ctx, parent); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ReportDelivery)
	}{
		{"body", func(d *ReportDelivery) { d.Body = "substituted content" }},
		{"marker", func(d *ReportDelivery) { d.Marker = "<!-- substituted -->" }},
		{"publisher", func(d *ReportDelivery) { d.BotUserID++ }},
		{"destination", func(d *ReportDelivery) { d.Destination = "github:other/repo#1" }},
		{"part order", func(d *ReportDelivery) { d.Purpose = "run-report-part:graph-multipart:2/2" }},
		{"namespace", func(d *ReportDelivery) { d.TriggerNamespace = "comment" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := child
			test.mutate(&changed)
			if _, err := db.EnsureReportDelivery(ctx, changed); err == nil {
				t.Fatal("changed running child inherited graph authority")
			}
		})
	}
	if _, err := db.EnsureReportDelivery(ctx, child); err != nil {
		t.Fatalf("exact retained graph child: %v", err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, child.Key, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(ctx, child.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, parent.Key, 102); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(ctx, parent.Key); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetRun(ctx, run.RunID)
	if err != nil || after.Status != RunRunning {
		t.Fatalf("graph publication finalized its running source: %+v %v", after, err)
	}
	if _, exists, err := db.GetWatermark(ctx, session.ID); err != nil || exists {
		t.Fatalf("graph publication consumed owner input: %v %v", exists, err)
	}
}
