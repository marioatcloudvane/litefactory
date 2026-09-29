package store

import (
	"fmt"
	"strings"
	"testing"
)

// TestRecordBugFact_CapSizeAndOverflow is AC-004/AC-010: at most 32 facts are
// individually kept per bug, each bounded to 1 KiB, and everything recorded
// past the cap folds into exactly one overflow fact instead of erroring.
func TestRecordBugFact_CapSizeAndOverflow(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 450)

	for i := 0; i < BugFactCap; i++ {
		ref := fmt.Sprintf("fact-%d", i)
		if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, ref, "a fact worth recording"); err != nil {
			t.Fatalf("record fact %d: %v", i, err)
		}
	}
	facts, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != BugFactCap {
		t.Fatalf("facts = %d, want the cap %d", len(facts), BugFactCap)
	}

	// The 33rd and 34th facts fold into one overflow entry, not a rejection and
	// not two separate overflow rows.
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, "fact-over-1", "past the cap"); err != nil {
		t.Fatalf("record over-cap fact: %v", err)
	}
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageChecks, "fact-over-2", "also past the cap"); err != nil {
		t.Fatalf("record second over-cap fact: %v", err)
	}
	after, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != BugFactCap+1 {
		t.Fatalf("facts after overflow = %d, want %d (cap plus one overflow marker)", len(after), BugFactCap+1)
	}
	overflow := after[len(after)-1]
	if overflow.Stage != BugFactStageOverflow || !strings.Contains(overflow.Text, "2") {
		t.Fatalf("overflow fact = %+v, want it to report 2 dropped findings", overflow)
	}

	// A fact longer than 1 KiB is truncated, never rejected.
	long := strings.Repeat("x", 4096)
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageDelivery, "fact-long", long); err != nil {
		t.Fatalf("record over-cap oversized fact: %v", err)
	}
	// This is itself past the cap, so it also folds into the overflow marker
	// rather than adding a new row; confirm the marker advanced to 3.
	final, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(final) != BugFactCap+1 {
		t.Fatalf("facts after a third overflow = %d, want %d", len(final), BugFactCap+1)
	}
	if !strings.Contains(final[len(final)-1].Text, "3") {
		t.Fatalf("overflow fact = %+v, want it to report 3 dropped findings", final[len(final)-1])
	}
}

// TestRecordBugFact_SizeLimitUnderTheCap pins the 1 KiB per-fact bound for a
// fact recorded while under the cap (truncated, not rejected, not folded into
// the overflow marker).
func TestRecordBugFact_SizeLimitUnderTheCap(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 451)

	long := strings.Repeat("y", 4096)
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageRepair, "fact-1", long); err != nil {
		t.Fatalf("record oversized fact: %v", err)
	}
	facts, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(facts))
	}
	if len(facts[0].Text) != 1024 {
		t.Fatalf("stored fact length = %d, want the 1024-byte bound", len(facts[0].Text))
	}
}

// TestRecordBugFact_ReplayIsIdempotent pins that recording the same reference
// twice with the same text does not consume a second cap slot.
func TestRecordBugFact_ReplayIsIdempotent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 452)

	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, "fact-1", "first observation"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, "fact-1", "first observation"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	facts, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1 (replay must not duplicate)", len(facts))
	}
}

// TestBugFacts_FiltersByStage pins that a stage filter returns only that
// stage's facts, exercised by the delivery review (all stages) versus the
// analysis review (BugFactStageAnalysis only).
func TestBugFacts_FiltersByStage(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 453)

	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, "a-1", "analysis notice"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageRepair, "r-1", "repair notice"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageDelivery, "d-1", "delivery notice"); err != nil {
		t.Fatal(err)
	}

	analysisOnly, err := db.BugFacts(t.Context(), session.ID, BugFactStageAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysisOnly) != 1 || analysisOnly[0].Reference != "a-1" {
		t.Fatalf("analysis-only facts = %+v", analysisOnly)
	}

	all, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("all facts = %d, want 3", len(all))
	}
}

// TestRecordBugFact_RejectsInvalidInput pins the hard boundary checks that
// remain real errors: an unknown stage and a blank fact text.
func TestRecordBugFact_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 454)

	if err := db.RecordBugFact(t.Context(), session.ID, 0, "not-a-real-stage", "ref-1", "text"); err == nil {
		t.Fatal("an unknown stage was accepted")
	}
	if err := db.RecordBugFact(t.Context(), session.ID, 0, BugFactStageAnalysis, "ref-1", "   "); err == nil {
		t.Fatal("blank fact text was accepted")
	}
}
