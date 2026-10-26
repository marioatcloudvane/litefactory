package store

import (
	"errors"
	"testing"
)

func TestRepairWorktreeGuardHoldsOnlyUnsettledOperations(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	assertHeld := func(want bool) {
		t.Helper()
		err := db.CheckRepairWorktree(t.Context(), instance.SessionID, 0)
		if errors.Is(err, ErrRepairWorktreeHeld) != want || err != nil && !errors.Is(err, ErrRepairWorktreeHeld) {
			t.Fatalf("held=%v: %v", want, err)
		}
		if err := db.CheckRepairWorktree(t.Context(), instance.SessionID, instance.ID); err != nil {
			t.Fatalf("own reconciliation refused: %v", err)
		}
		if err := db.CheckRepairWorktree(t.Context(), instance.SessionID+1000, 0); err != nil {
			t.Fatalf("unrelated session held: %v", err)
		}
	}
	assertHeld(false)
	candidate := retainedCandidate(t, db, instance.ID)
	assertHeld(true) // running repair, before capture intent
	if err := db.PutRepairCandidate(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	journal := func(phase, state, result string) {
		t.Helper()
		in := RepairCandidateJournal{InstanceID: instance.ID, RoundID: candidate.RoundID, Phase: phase, State: state, ExpectedBase: candidate.BaseCommit, At: graphTestTime()}
		if phase == "apply" {
			in.ExpectedResult = repairPost
			in.ResultCommit = result
		}
		if err := db.JournalRepairCandidate(t.Context(), candidate.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	journal("capture", "intent", "")
	if err := db.FinishRepairRound(t.Context(), candidate.RoundID, RepairFinish{State: RepairTooBig, At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	assertHeld(true) // capture interruption holds even after round settlement
	journal("capture", "complete", "")
	assertHeld(true) // a saved snapshot does not prove reset completed
	journal("reset", "intent", "")
	journal("reset", "blocked", "")
	assertHeld(true)
	journal("reset", "complete", "")
	assertHeld(false) // a pending retained candidate does not hold other work
	if err := db.SetRepairCandidateReviewComplete(t.Context(), candidate.ID, candidate.ReviewDigest, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveRepairCandidate(t.Context(), candidate.ID, RepairCandidateDecision{Action: "accept", ReviewDigest: candidate.ReviewDigest, ReportRevision: "current-report", Principal: "github:123", SourceID: "comment:guard", At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	assertHeld(true) // authorization alone is not verified application
	journal("apply", "intent", "")
	assertHeld(true)
	journal("apply", "complete", repairPost)
	assertHeld(false)
}
