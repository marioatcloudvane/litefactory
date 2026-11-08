package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// saveBugResult records one Bug result the way the Bug graph's agent node
// does: mode "bug", its own work context.
func saveBugResult(t *testing.T, db *DB, session Session, trigger int64, snapshot string) {
	t.Helper()
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "bug-results", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "bug", Agent: AgentClaude, TriggerCommentID: trigger, LogPath: func(n int) string { return fmt.Sprintf("/logs/bug-%d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Mode: "bug", Task: session.TaskRef, WorkContext: fmt.Sprintf("graph#bug:%d", trigger), Attempt: fmt.Sprintf("bug-%d", trigger), RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	record := resultRecord(binding, fmt.Sprintf("bug-%d", trigger), 0)
	record.Snapshot = []byte(snapshot)
	if _, err = db.RecordResult(t.Context(), record); err != nil {
		t.Fatal(err)
	}
}

func bugSnapshot(finding string) string {
	return fmt.Sprintf(`{"meta":{"mode":"bug"},"result":{"outcome":"completed","payload":{"bug":{"request":"saving crashes","findings":[%q]}}}}`, finding)
}

// A saved Bug result with findings is a judge source next to research and
// Plan; a failed one and one without findings are not.
func TestJudgeSourcesIncludeSavedBugResults(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 1101)
	saveBugResult(t, db, session, 1, bugSnapshot("first cause"))
	saveBugResult(t, db, session, 2, `{"result":{"outcome":"completed","payload":{"bug":{"request":"saving crashes"}}}}`)
	saveBugResult(t, db, session, 3, `{"result":{"outcome":"failed","payload":{"bug":{"request":"saving crashes","findings":["failed run"]}}}}`)
	saveBugResult(t, db, session, 4, bugSnapshot("second cause"))

	sources, err := db.ListJudgeSources(ctx, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("want the two results with findings, got %+v", sources)
	}
	if !strings.Contains(string(sources[0].Snapshot), "first cause") || !strings.Contains(string(sources[1].Snapshot), "second cause") {
		t.Fatal("results listed out of save order")
	}
	newest, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil || newest.Reference != sources[1].Reference {
		t.Fatalf("default=%+v %v", newest, err)
	}
	older, err := db.ResolveJudgeSource(ctx, session.TaskRef, sources[0].Reference)
	if err != nil || older.Reference != sources[0].Reference {
		t.Fatalf("explicit older=%+v %v", older, err)
	}
}

// A ticket with only results without findings has nothing to judge.
func TestJudgeSourcesIgnoreBugResultsWithoutFindings(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 1102)
	saveBugResult(t, db, session, 1, `{"result":{"outcome":"completed","payload":{"bug":{"request":"saving crashes"}}}}`)
	if _, err := db.ResolveJudgeSource(t.Context(), session.TaskRef, ""); !errors.Is(err, ErrJudgeSourceUnavailable) {
		t.Fatalf("a result without findings became a judge source: %v", err)
	}
}
