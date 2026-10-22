package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// saveBugResult records one standalone Bug result the way the Bug graph's
// analyst or fixer node does: workflow "bug", empty mode, its own work context.
func saveBugResult(t *testing.T, db *DB, session Session, node string, trigger int64, snapshot string) {
	t.Helper()
	if _, err := db.GetTaskWorkflowAdmission(t.Context(), session.ID); errors.Is(err, ErrNotFound) {
		if _, err = db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
			t.Fatal(err)
		}
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "bug-results", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: AgentClaude, TriggerCommentID: trigger, LogPath: func(n int) string { return fmt.Sprintf("/logs/bug-%d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Workflow: "bug", Task: session.TaskRef, WorkContext: fmt.Sprintf("graph#bug:%s:%d", node, trigger), Attempt: fmt.Sprintf("%s-%d", node, trigger), RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, fmt.Sprintf("%s-%d", node, trigger), 0)
	record.Snapshot = []byte(snapshot)
	if _, err = db.RecordResult(t.Context(), record); err != nil {
		t.Fatal(err)
	}
}

func bugAnalysisSnapshot(cause string) string {
	return fmt.Sprintf(`{"meta":{"workflow":"bug","mode":""},"result":{"outcome":"completed","payload":{"bug":{"response":"done","analysis":{"expected":"works","actual":"fails","cause":%q,"remedy":"fix it","criteria":[{"id":"c1","requirement":"test fails without fix"}]}}}}}`, cause)
}

// A saved Bug analysis is a judge source next to research and Plan; the
// fixer's repair result and an analysis without a cause are not.
func TestJudgeSourcesIncludeSavedBugAnalyses(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 1101)
	saveBugResult(t, db, session, "bug-analyst:analysis", 1, bugAnalysisSnapshot("first cause"))
	saveBugResult(t, db, session, "bug-analyst:analysis", 2, `{"result":{"outcome":"blocked","payload":{"bug":{"response":"blocked","analysis":{"cause":"  "}}}}}`)
	saveBugResult(t, db, session, "bug-analyst:analysis", 3, `{"result":{"outcome":"failed","payload":{"bug":{"response":"done","analysis":{"cause":"failed run"}}}}}`)
	saveBugResult(t, db, session, "bug-fixer:repair", 4, `{"result":{"outcome":"completed","payload":{"bug":{"response":"succeeded","repair":{"summary":"patched"}}}}}`)
	saveBugResult(t, db, session, "bug-analyst:analysis", 5, bugAnalysisSnapshot("second cause"))
	saveBugResult(t, db, session, "bug-fixer:repair", 6, `{"result":{"outcome":"completed","payload":{"bug":{"response":"succeeded","repair":{"summary":"patched again"}}}}}`)

	sources, err := db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("want the two substantive analyses, got %+v", sources)
	}
	for i, s := range sources {
		if want := fmt.Sprintf("Bug analysis rev %d", i+1); s.Label != want || s.Kind != "result" || s.Provider != AgentClaude {
			t.Fatalf("source %d = %+v, want label %q", i, s, want)
		}
	}
	if !strings.Contains(string(sources[0].Snapshot), "first cause") || !strings.Contains(string(sources[1].Snapshot), "second cause") {
		t.Fatal("analyses listed out of save order")
	}

	// Newest by default, older selectable by reference.
	newest, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil || newest.Reference != sources[1].Reference || newest.NewestLegacy {
		t.Fatalf("default=%+v %v", newest, err)
	}
	older, err := db.ResolveJudgeSource(ctx, session.TaskRef, sources[0].Reference)
	if err != nil || older.Label != "Bug analysis rev 1" {
		t.Fatalf("explicit older=%+v %v", older, err)
	}
	_, err = db.ResolveJudgeSource(ctx, session.TaskRef, "result:0:1:0:missing")
	if !errors.Is(err, ErrJudgeSourceUnavailable) || !strings.Contains(err.Error(), sources[0].Reference) || !strings.Contains(err.Error(), "Bug analysis rev 1") {
		t.Fatalf("unknown target should list the analyses by name: %v", err)
	}

	// The label survives the pinned request, so the report names the same revision later.
	request, err := db.AdmitJudgeRequest(ctx, JudgeRequest{SessionID: session.ID, TriggerCommentID: 9, Task: session.TaskRef, TicketKey: "qualified", CommentID: "comment-9", Source: older, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.FindJudgeRequest(ctx, "qualified", "comment-9")
	if err != nil || again.ID != request.ID || again.Source.Label != "Bug analysis rev 1" || again.Source.Reference != older.Reference {
		t.Fatalf("pinned source=%+v %v", again.Source, err)
	}
}

// A ticket with only fixer results has nothing to judge.
func TestJudgeSourcesIgnoreBugRepairResults(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 1102)
	saveBugResult(t, db, session, "bug-fixer:repair", 1, `{"result":{"outcome":"completed","payload":{"bug":{"response":"succeeded","repair":{"summary":"patched"}}}}}`)
	if _, err := db.ResolveJudgeSource(t.Context(), session.TaskRef, ""); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("repair result became a judge source: %v", err)
	}
}
