package analystschedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// What the analyst read is a fact shown with its result; a truncated excerpt
// does not leave an accepted report's source unresolved.
func TestReconcileAcceptedLogReportCoversTruncatedSource(t *testing.T) {
	root := t.TempDir()
	svc, occurrence, now := typedScheduleFixture(t, Config{Name: "Truncated evidence", Kind: "logs", RunLimit: 1}, root, nil)
	repo, err := svc.DB.GetRepo(t.Context(), occurrence.RepositoryID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.DB.GetOrCreateSession(t.Context(), repo, 71)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := svc.DB.GetOrCreateSegment(t.Context(), session.ID, 1, "truncated-evidence-session", "claude")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "truncated.jsonl")
	if err = os.WriteFile(logPath, append(raw(map[string]string{"type": "result", "result": strings.Repeat("x", 900)}), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	run, err := svc.DB.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "claude", StartedAt: now.Add(-time.Hour), LogPath: func(int) string { return logPath }})
	if err != nil {
		t.Fatal(err)
	}
	run, err = svc.DB.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err = svc.DB.SetAnalystSources(t.Context(), occurrence.ID, occurrence.Fence, []store.AnalystSourceWork{{Kind: "run", SourceKey: fmt.Sprintf("run:%d", run.ID), RunID: run.ID, State: "selected"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	analysis := allocateScheduledLog(t, svc, occurrence, now)
	budget, err := logevidence.NewBudget(logevidence.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := logevidence.New(t.Context(), logevidence.Options{Store: svc.DB, RunsRoot: root, Scope: logevidence.Scope{RepositoryIDs: []int64{repo.ID}, RunIDs: []int64{run.ID}}, Budget: budget, Redactor: redact.New(redact.Options{})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	selector := raw(logevidence.ReadRequest{RunID: run.ID})
	body, err := reader.Handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "truncated-event-eof", Payload: raw(submission.ToolCall{Name: "run_logs_read", Arguments: selector})})
	if err != nil {
		t.Fatal(err)
	}
	var result logevidence.Result
	if err = json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || !result.Coverage.EndOfSource || result.NextCursor != "" || len(result.Records) != 1 || !result.Records[0].Truncated {
		t.Fatalf("expected a truncated event in a successful EOF read: %+v", result)
	}
	if len(result.Coverage.Omissions) != 0 || (result.Coverage.Incomplete && !result.Coverage.RetentionOnly) {
		t.Fatalf("fixture has a coverage gap independent of event truncation: %+v", result.Coverage)
	}
	// Retain only the delivery metadata, exactly as the upstream analyst does.
	evidence := loganalysis.Evidence{Tool: "run_logs_read", Selector: selector, Status: result.Status, Runs: result.Runs, Coverage: result.Coverage, Next: result.NextCursor != "", Budget: result.Budget}
	for _, record := range result.Records {
		evidence.Citations = append(evidence.Citations, record.Citation)
		evidence.Events = append(evidence.Events, loganalysis.OfferedEvent{Citation: record.Citation, Redactions: record.Redactions, Truncated: record.Truncated})
	}
	if _, err = svc.DB.RecordLogAnalysisEvidence(t.Context(), store.LogAnalysisEvidence{AnalysisID: analysis.ID, Generation: analysis.Generation, CallKey: "truncated-eof", Metadata: raw(evidence)}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DB.FinalizeLogAnalysis(t.Context(), store.LogAnalysisFinalization{AnalysisID: analysis.ID, Generation: analysis.Generation, RequestID: "accepted", RequestDigest: "truncated-report", Report: raw(loganalysis.Report{Summary: "One run, no pattern.", NoChangeReason: "A single occurrence."}), State: "completed", Disposition: "no_warranted_change"}); err != nil {
		t.Fatal(err)
	}
	assertCovered := func() {
		t.Helper()
		current, err := svc.DB.GetAnalystOccurrence(t.Context(), occurrence.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State != "completed" || current.Disposition != "no_warranted_change" || len(current.Works) != 1 {
			t.Fatalf("accepted analysis was downgraded: %+v", current)
		}
		work := current.Works[0]
		if !work.Covered || work.State != "covered" || !work.Offered {
			t.Fatalf("accepted report did not cover its offered source: %+v", work)
		}
	}
	if err = svc.Recover(t.Context(), now.Add(leaseDuration+time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertCovered()
	path := svc.DB.Path()
	if err = svc.DB.Close(); err != nil {
		t.Fatal(err)
	}
	svc.DB, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.DB.Close() })
	assertCovered()
	if err = svc.Recover(t.Context(), now.Add(2*leaseDuration)); err != nil {
		t.Fatal(err)
	}
	assertCovered()
}
