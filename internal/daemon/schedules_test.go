package daemon_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDueScheduleDispatchesStoredPayloadAndCountsPendingWork(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Scheduled work")
	f.settle() // create the session and watermark

	session := f.session()
	// The fixture clock advances on every daemon clock read. Keep the due time
	// well beyond the setup ticks, then advance deterministically past only the
	// first of the two rows.
	due := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	first, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 701, OwnerHandle: ownerLogin,
		Payload: "/spike first scheduled payload", DueAt: due,
	})
	if err != nil {
		t.Fatalf("create first schedule: %v", err)
	}
	if _, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{
		SessionID: session.ID, SourceCommentID: 702, OwnerHandle: ownerLogin,
		Payload: "/spike later scheduled payload", DueAt: due.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("create later schedule: %v", err)
	}

	before := f.settle()
	if before.Scheduled != 2 {
		t.Fatalf("before due scheduled=%d, want 2; calls=%+v\nlogs:\n%s", before.Scheduled, f.turns.calls(), f.logs)
	}
	if got := len(f.turns.calls()); got != 0 {
		t.Fatalf("ran %d turns before due", got)
	}

	f.clock.advance(25 * time.Hour)
	after := f.settle()
	if after.Scheduled != 1 {
		t.Fatalf("after due scheduled=%d, want 1\nlogs:\n%s", after.Scheduled, f.logs)
	}
	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("ran %d scheduled turns, want 1", len(calls))
	}
	if got := calls[0].Command.Text; got != "first scheduled payload" {
		t.Errorf("scheduled instruction = %q, want stored payload only", got)
	}
	if strings.Contains(calls[0].Trigger.Body, "/schedule") {
		t.Errorf("scheduled trigger carried scheduling metadata: %q", calls[0].Trigger.Body)
	}
	stored, err := f.db.GetSchedule(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("read completed schedule: %v", err)
	}
	if stored.Status != store.ScheduleSucceeded || stored.RunID == 0 {
		t.Errorf("completed schedule = %+v, want succeeded with run id", stored)
	}
}
