package store

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkflowJudgePinnedBindingAndPrivatePublication(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 810)
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
	sourceRun := resultRun(t, db, session, "research", "source", 1)
	sourceAttempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "source", RunID: sourceRun.RunID, Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"result":{"outcome":"completed","payload":{"research":{"findings":[{"id":"f1","text":"saved"}]}}}}`)
	binding, err := db.BindResultAttempt(ctx, sourceRun)
	if err != nil {
		t.Fatal(err)
	}
	accepted := resultRecord(binding, "research-accepted", 0)
	accepted.Snapshot = raw
	if _, err = db.RecordResult(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, sourceAttempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "source", ToNode: "judge", OnOutcome: "success", At: graphTestTime()}, Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: raw, CreatedAt: graphTestTime()}}}); err != nil {
		t.Fatal(err)
	}
	source, pinned, err := db.ResolveWorkflowJudgeSource(ctx, admitted.Instance.ID, "source")
	if err != nil || pinned.ID != sourceAttempt.ID || source.AcceptedResultID != binding.ResultID || source.AcceptedRevision != 1 {
		t.Fatalf("source: %+v %v", pinned, err)
	}
	judge, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "judge", Limit: 1, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	request := JudgeRequest{SessionID: session.ID, Task: session.TaskRef, TicketKey: "qualified", CommentID: "workflow-judge", CallerKind: "workflow", GraphInstanceID: admitted.Instance.ID, JudgeNodeAttemptID: judge.ID, SourceNode: "source", SourceNodeAttemptID: pinned.ID, Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)}
	retained, err := db.AdmitJudgeRequest(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	found, err := db.FindWorkflowJudgeRequest(ctx, admitted.Instance.ID, judge.ID)
	if err != nil || found.ID != retained.ID {
		t.Fatalf("binding: %+v %v", found, err)
	}
	request.SourceNodeAttemptID = judge.ID
	if _, err = db.AdmitJudgeRequest(ctx, request); !errors.Is(err, ErrResultCorrelation) {
		t.Fatalf("retarget accepted: %v", err)
	}
	pending, err := db.PendingJudgeRequests(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("workflow request exposed for manual publication: %v %v", pending, err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, judge.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "failed", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: admitted.Instance.ID, FromNode: "judge", ToNode: "source", OnOutcome: "failed", At: graphTestTime()}}); err != nil {
		t.Fatal(err)
	}
	intent := GraphCommentIntent{Key: "advice", InstanceID: admitted.Instance.ID, SourceAttemptID: judge.ID, Node: "publish", Kind: "advisory", Candidate: []byte("retained failure explanation")}
	if err = db.RetainGraphComment(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetGraphOwnerWait(ctx, admitted.Instance.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("advice opened owner wait: %v", err)
	}
	intent.Candidate = []byte("changed explanation")
	if err = db.RetainGraphComment(ctx, intent); err == nil {
		t.Fatal("publication bytes changed")
	}
	newer, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "source", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(ctx, newer.ID, GraphCompletion{Status: GraphFailed, FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.ResolveWorkflowJudgeSource(ctx, admitted.Instance.ID, "source"); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("older attempt substituted after latest failure: %v", err)
	}
}

func TestWorkflowJudgePlanContextKeepsExactAcceptedDraftAndGaps(t *testing.T) {
	db := newTestDB(t)
	source := JudgeSource{Task: "ticket", Snapshot: []byte(`{"result":{"payload":{"plan":{"storyrequest":{"shared_revision":"shared-exact","draft":{"key":"one","name":"First story","user_story":{"want":"Exact saved intent"}}}}}}}`)}
	if err := db.pinWorkflowPlanContext(t.Context(), &source); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source.PlanContext), `"shared_revision":"shared-exact"`) || !strings.Contains(string(source.PlanContext), `"draft_revision":`) || source.PlanContextGap != "" {
		t.Fatalf("missing exact draft context: %s %s", source.PlanContext, source.PlanContextGap)
	}
	source = JudgeSource{Task: "ticket", Snapshot: []byte(`{"result":{"payload":{"plan":{"storyset":{"entries":[{"name":"First story","intent":"Saved decomposition intent"}]}}}}}`)}
	if err := db.pinWorkflowPlanContext(t.Context(), &source); err != nil {
		t.Fatal(err)
	}
	if len(source.PlanContext) != 0 || source.PlanContextGap == "" {
		t.Fatalf("unbound parent drafts were invented: %s %s", source.PlanContext, source.PlanContextGap)
	}
}

func TestWorkflowResearchSourceProvesLatestAcceptedRevision(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 811)
	run := resultRun(t, db, session, "research", "source", 1)
	binding, err := db.BindResultAttempt(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	projected := []byte(`{"version":1,"result":{"outcome":"completed","payload":{"research":{"findings":[{"text":"first saved finding"}]}}}}`)
	if _, err = db.workflowResearchAcceptedResult(ctx, run.RunID, session.TaskRef, projected); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("unsaved projection accepted: %v", err)
	}
	record := resultRecord(binding, "first-save", 0)
	record.Snapshot = []byte(`{"meta":{"private":"metadata"},"result":{"payload":{"research":{"findings":[{"text":"first saved finding"}]}},"outcome":"completed"}}`)
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	accepted, err := db.workflowResearchAcceptedResult(ctx, run.RunID, session.TaskRef, projected)
	if err != nil || accepted.ResultID != binding.ResultID || accepted.Revision != 1 || string(accepted.Snapshot) != string(record.Snapshot) {
		t.Fatalf("accepted proof: %+v %v", accepted, err)
	}
	record = resultRecord(binding, "newer-save", 1)
	record.Snapshot = []byte(`{"result":{"outcome":"completed","payload":{"research":{"findings":[{"text":"newer saved finding"}]}}}}`)
	if _, err = db.RecordResult(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err = db.workflowResearchAcceptedResult(ctx, run.RunID, session.TaskRef, projected); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("older matching revision substituted: %v", err)
	}
	if _, err = db.workflowResearchAcceptedResult(ctx, run.RunID, "other-ticket", record.Snapshot); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("cross-ticket source accepted: %v", err)
	}
}
