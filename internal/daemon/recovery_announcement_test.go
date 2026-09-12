package daemon_test

import (
	"github.com/cloudvane/litefactory/internal/store"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecoveryAnnouncementCannotCreateScheduleOrRun(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Recovery announcement")
	f.settle()
	f.forge.addComment(testIssue, 50, bot(), "Automatic action 1 is persisted.\n\n/schedule -to 09/09/2026 15:02:00 UTC Continue the interrupted request in the original conversation.")
	f.settle()
	if got := len(f.turns.calls()); got != 0 {
		t.Fatalf("bot announcement caused %d runs", got)
	}
	pending, err := f.db.CountPendingSchedules(t.Context())
	if err != nil || pending != 0 {
		t.Fatalf("bot announcement caused schedules: %d %v", pending, err)
	}
}

func TestRecoveryAnnouncementReplayIgnoresAdvancedWatermark(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Recovery replay")
	f.settle()
	session := f.session()
	if err := f.db.SetSessionWorktree(t.Context(), session.ID, "/worktree"); err != nil {
		t.Fatal(err)
	}
	session = f.session()
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "saved-conversation", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 42, LogPath: func(int) string { return "/logs/interrupted.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	observed := run.StartedAt.Add(time.Second)
	zero := int64(0)
	_, action, err := f.db.FinishInterruptedRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "interrupted", ExitCode: &zero, FinishedAt: observed}, &store.Continuation{ResetBound: observed.Add(time.Hour), DueAt: observed.Add(time.Hour + time.Minute), Precision: "exact", Context: store.ContinuationContext{Task: session.TaskRef, Worktree: "/worktree", Agent: store.AgentClaude, Mode: store.ModeSpike, Request: "original", ResumeIdentity: "saved-conversation"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdvanceWatermark(t.Context(), session.ID, 999); err != nil {
		t.Fatal(err)
	}
	f.daemon = recoveryDaemon(t, f, true)
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "temporary post loss"})
		return true
	}
	if _, err := f.tickErr(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.logs.String(), "report recovery pending") {
		t.Fatal("missing bounded failure diagnostic")
	}
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err := f.db.ListPendingInterruptionReports(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("failed announcement lost: %+v %v", pending, err)
	}
	f.forge.onPostComment = nil
	retained := restoreTrustedRecoveryComment(t, f, session.ID, 42, 900)
	f.settle()
	got, err := f.db.GetRun(t.Context(), run.ID)
	if err != nil || got.Status != store.RunReported || got.SemanticOutcome != "interrupted" {
		t.Fatalf("durable report did not recover after watermark: %+v %v logs=%s", got, err, f.logs)
	}
	saved, err := f.db.GetContinuation(t.Context(), action.ID)
	if err != nil || saved.AnnouncementCommentID == 0 || saved.AnnouncementCommentID != got.ResultCommentID {
		t.Fatalf("announcement bookkeeping: %+v %v", saved, err)
	}
	posts, _, _ := f.forge.snapshot()
	if len(posts) != 0 || !strings.Contains(retained, "/schedule -to") {
		t.Fatalf("replayed announcement: %v", posts)
	}
	if f.watermark() != 999 || len(f.turns.calls()) != 0 {
		t.Fatalf("replay changed owner progress or invoked agent: watermark=%d calls=%d", f.watermark(), len(f.turns.calls()))
	}
	f.settle()
	posts, _, _ = f.forge.snapshot()
	if len(posts) != 0 {
		t.Fatalf("confirmed announcement repeated: %v", posts)
	}
}

// A lost response does not authorize reposting. Supply the exact retained intent
// as trusted remote evidence, then advance to the persisted retry deadline.
func restoreTrustedRecoveryComment(t *testing.T, f *fixture, sessionID, triggerID, commentID int64) string {
	t.Helper()
	delivery, found, err := f.db.FindPendingReportDelivery(t.Context(), sessionID, triggerID)
	if err != nil || !found {
		t.Fatalf("missing uncertain delivery: %+v %v", delivery, err)
	}
	f.forge.addComment(testIssue, commentID, bot(), delivery.Body)
	rows, err := f.db.ListReportRecoveries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.SessionID == sessionID && row.TriggerID == triggerID {
			if delta := row.NextAt.Sub(f.clock.Now()); delta > 0 {
				f.clock.advance(delta)
			}
		}
	}
	return delivery.Body
}

func TestRecoveryCleanupResolvesAfterSourceAlreadyConsumed(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Completed delivery cleanup")
	f.settle()
	session := f.session()
	if _, err := f.db.AdvanceWatermark(t.Context(), session.ID, 999); err != nil {
		t.Fatal(err)
	}
	due := f.clock.Now().Add(testInterval)
	debt := store.ReportRecovery{Key: "source:cleanup-test", SessionID: session.ID, TriggerID: 42, Task: session.TaskRef, State: "cleanup", Reason: "local cleanup unavailable", NextAt: due, Attempts: 3, Escalated: true}
	if err := f.db.PutReportRecovery(t.Context(), debt); err != nil {
		t.Fatal(err)
	}
	f.daemon = recoveryDaemon(t, f, true)
	f.clock.advance(testInterval)
	f.settle()
	saved, err := f.db.GetReportRecovery(t.Context(), debt.Key)
	if err != nil || saved.State != "resolved" || !saved.Escalated {
		t.Fatalf("completed cleanup debt=%+v %v", saved, err)
	}
	posts, _, _ := f.forge.snapshot()
	if len(posts) != 0 || len(f.turns.calls()) != 0 || f.watermark() != 999 {
		t.Fatalf("cleanup repeated delivery/execution: posts=%v calls=%d watermark=%d", posts, len(f.turns.calls()), f.watermark())
	}
}
