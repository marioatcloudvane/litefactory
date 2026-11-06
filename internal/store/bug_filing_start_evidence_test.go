package store

import (
	"strings"
	"testing"
)

func TestBugStartEvidenceReadsAdmissionAndRuntimeRows(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 10)
	ev, err := db.BugStartEvidence(ctx, session.ID)
	if err != nil || ev.Admitted || len(ev.Runs) != 0 {
		t.Fatalf("no Bug yet: %+v %v", ev, err)
	}
	if _, err = db.AdmitWorkflow(ctx, TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	at := graphTestTime()
	def, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: strings.Repeat("c", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	trigger := BugStartTrigger(1)
	admitted, err := db.AdmitWorkflowLaunch(ctx, WorkflowLaunch{Workflow: "bug", SessionID: session.ID, TriggerID: trigger, DefinitionID: def.ID, Start: "conversation", ContentDigest: def.Digest, TopologyDigest: def.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	ev, err = db.BugStartEvidence(ctx, session.ID)
	if err != nil || !ev.Admitted || ev.Instance.ID != admitted.Instance.ID || ev.AdmissionTrigger != trigger {
		t.Fatalf("admission: %+v %v", ev, err)
	}
	if len(ev.Runs) != 0 || len(ev.Attempts) != 0 || len(ev.Transitions) != 0 || ev.HasDispatch || ev.HasWait || ev.HasComment {
		t.Fatalf("untouched launch shape: %+v", ev)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, FirstSegmentOrdinal, "conv", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: trigger, StartedAt: at, LogPath: func(int) string { return "/logs/run.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "conversation", RunID: run.ID, Limit: 64, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, a.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "analyse", FinishedAt: at, InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "conversation", ToNode: "analyst", OnOutcome: "analyse", At: at}}); err != nil {
		t.Fatal(err)
	}
	ev, err = db.BugStartEvidence(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Runs) != 1 || len(ev.Attempts) != 1 || ev.Attempts[0].Status != GraphSucceeded || len(ev.Transitions) != 1 || ev.Transitions[0].ToNode != "analyst" || ev.Instance.CurrentNode != "analyst" {
		t.Fatalf("run before analyst: %+v", ev)
	}
}
