package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	repairPre  = strings.Repeat("a", 40)
	repairPost = strings.Repeat("b", 40)
)

func TestRepairRoundsAreCountedAndBounded(t *testing.T) { // AC-003, AC-004
	db := newTestDB(t)
	first, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err != nil || first.Round != 1 || first.State != RepairRunning {
		t.Fatalf("first %+v %v", first, err)
	}
	if got, err := db.RunningRepairRound(t.Context(), 1); err != nil || got.ID != first.ID {
		t.Fatalf("running %+v %v", got, err)
	}
	// An unfinished round still counts as used.
	second, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err != nil || second.Round != 2 {
		t.Fatalf("second %+v %v", second, err)
	}
	if _, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()}); !errors.Is(err, ErrRepairRoundLimit) {
		t.Fatalf("third round: %v", err)
	}
	if other, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 2, PreCommit: repairPre, StartedAt: graphTestTime()}); err != nil || other.Round != 1 {
		t.Fatalf("another instance %+v %v", other, err)
	}
}

func TestFinishedRepairRoundIsImmutable(t *testing.T) {
	db := newTestDB(t)
	round, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishRepairRound(t.Context(), round.ID, RepairFinish{State: RepairKept, At: graphTestTime()}); err == nil {
		t.Fatal("a kept round without a commit was accepted")
	}
	if err := db.FinishRepairRound(t.Context(), round.ID, RepairFinish{State: RepairTooBig, ResultCommit: repairPost, At: graphTestTime()}); err == nil {
		t.Fatal("a rejected round with a commit was accepted")
	}
	done := RepairFinish{Response: "repaired", State: RepairKept, Files: []string{"a.go"}, Kinds: []string{}, DiffDigest: "d", Diff: "diff", ResultCommit: repairPost, At: graphTestTime()}
	if err := db.FinishRepairRound(t.Context(), round.ID, done); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishRepairRound(t.Context(), round.ID, done); err != nil {
		t.Fatalf("same content again: %v", err)
	}
	other := RepairFinish{State: RepairTooBig, DiffDigest: "x", At: graphTestTime()}
	if err := db.FinishRepairRound(t.Context(), round.ID, other); !errors.Is(err, ErrRepairFinished) {
		t.Fatalf("different content: %v", err)
	}
	got, err := db.GetRepairRound(t.Context(), round.ID)
	if err != nil || got.State != RepairKept || got.ResultCommit != repairPost || len(got.Files) != 1 || got.Diff != "diff" {
		t.Fatalf("stored %+v %v", got, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validate_repair_rounds SET reason='x' WHERE id=?`, round.ID); err == nil {
		t.Fatal("a finished round was updated in place")
	}
	if _, err := db.RunningRepairRound(t.Context(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("running after finish: %v", err)
	}
}

func TestAttemptTimeoutIsRecordedOnce(t *testing.T) { // AC-004
	db := newTestDB(t)
	attempt := graphTestAttempt(t, db, graphTestInstance(t, db), 3)
	if err := db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: 0, TimedOutAt: graphTestTime()}); err == nil {
		t.Fatal("a zero budget was accepted")
	}
	if _, err := db.GetAttemptTimeout(t.Context(), attempt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before: %v", err)
	}
	if err := db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: 15 * time.Minute, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAttemptTimeout(t.Context(), AttemptTimeout{NodeAttemptID: attempt.ID, Budget: time.Hour, TimedOutAt: graphTestTime()}); err != nil {
		t.Fatalf("second record: %v", err)
	}
	got, err := db.GetAttemptTimeout(t.Context(), attempt.ID)
	if err != nil || got.Budget != 15*time.Minute {
		t.Fatalf("timeout %+v %v", got, err)
	}
}

func TestReviewRowsAreUniquePerRoleAndCommit(t *testing.T) { // AC-005, AC-009
	db := newTestDB(t)
	in := ValidateReview{InstanceID: 1, Role: "code", Commit: repairPre, TreeDigest: "t", Attempt: "a1", StartedAt: graphTestTime()}
	row, created, err := db.BeginValidateReview(t.Context(), in)
	if err != nil || !created || row.State != ReviewPending {
		t.Fatalf("begin %+v %v %v", row, created, err)
	}
	again, created, err := db.BeginValidateReview(t.Context(), in)
	if err != nil || created || again.ID != row.ID {
		t.Fatalf("second begin %+v %v %v", again, created, err)
	}
	bad := in
	bad.Role = "judge"
	if _, _, err := db.BeginValidateReview(t.Context(), bad); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	findings := json.RawMessage(`[{"story":"s"}]`)
	if err := db.FinishValidateReview(t.Context(), row.ID, ReviewAccepted, "", findings, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishValidateReview(t.Context(), row.ID, ReviewAccepted, "", findings, graphTestTime()); err != nil {
		t.Fatalf("same finish again: %v", err)
	}
	if err := db.FinishValidateReview(t.Context(), row.ID, ReviewFailed, "late", nil, graphTestTime()); !errors.Is(err, ErrReviewFinished) {
		t.Fatalf("different finish: %v", err)
	}
	list, err := db.ListValidateReviews(t.Context(), 1, repairPre)
	if err != nil || len(list) != 1 || list[0].State != ReviewAccepted || string(list[0].Findings) != string(findings) {
		t.Fatalf("list %+v %v", list, err)
	}
	if other, err := db.ListValidateReviews(t.Context(), 1, repairPost); err != nil || len(other) != 0 {
		t.Fatalf("other commit %+v %v", other, err)
	}
	failed, _, err := db.BeginValidateReview(t.Context(), ValidateReview{InstanceID: 1, Role: "acceptance", Commit: repairPre, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishValidateReview(t.Context(), failed.ID, ReviewFailed, "boom", findings, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	list, _ = db.ListValidateReviews(t.Context(), 1, repairPre)
	if string(list[1].Findings) != "[]" {
		t.Fatalf("a failed reviewer kept findings: %s", list[1].Findings)
	}
}

func TestValidateOutcomeIsRecordedOncePerCommit(t *testing.T) { // AC-007
	db := newTestDB(t)
	in := ValidateOutcome{InstanceID: 1, Commit: repairPre, Outcome: ValidateAssessed, Assessment: json.RawMessage(`[{"story":"s"}]`), CreatedAt: graphTestTime()}
	first, err := db.PutValidateOutcome(t.Context(), in)
	if err != nil || first.ID == 0 {
		t.Fatalf("put %+v %v", first, err)
	}
	again, err := db.PutValidateOutcome(t.Context(), in)
	if err != nil || again.ID != first.ID {
		t.Fatalf("same again %+v %v", again, err)
	}
	other := in
	other.Outcome = ValidateBlocked
	if _, err := db.PutValidateOutcome(t.Context(), other); err == nil {
		t.Fatal("a different outcome replaced the recorded one")
	}
	for _, word := range []string{"accepted", "approved", ""} {
		bad := in
		bad.Commit, bad.Outcome = repairPost, word
		if _, err := db.PutValidateOutcome(t.Context(), bad); err == nil {
			t.Fatalf("outcome %q was accepted", word)
		}
	}
	next := in
	next.Commit, next.Outcome, next.Handback = repairPost, ValidateHandedBack, json.RawMessage(`{"failures":[]}`)
	if _, err := db.PutValidateOutcome(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	latest, err := db.LatestValidateOutcome(t.Context(), 1)
	if err != nil || latest.Commit != repairPost || string(latest.Handback) != `{"failures":[]}` {
		t.Fatalf("latest %+v %v", latest, err)
	}
	if got, err := db.GetValidateOutcome(t.Context(), 1, repairPre); err != nil || got.Handback != nil {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validate_outcomes SET reason='x'`); err == nil {
		t.Fatal("an outcome was updated")
	}
}

func TestLatestKeptRepairCommitIsTheCommitTheSuiteRunsOnAfterARepair(t *testing.T) { // AC-003
	db := newTestDB(t)
	if _, err := db.LatestKeptRepairCommit(t.Context(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no round: %v", err)
	}
	first, _ := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err := db.FinishRepairRound(t.Context(), first.ID, RepairFinish{State: RepairTooBig, At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LatestKeptRepairCommit(t.Context(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a rejected round is not a commit to test: %v", err)
	}
	second, _ := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: 1, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err := db.FinishRepairRound(t.Context(), second.ID, RepairFinish{State: RepairKept, ResultCommit: repairPost, At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.LatestKeptRepairCommit(t.Context(), 1); err != nil || got != repairPost {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := db.LatestKeptRepairCommit(t.Context(), 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another instance: %v", err)
	}
}
