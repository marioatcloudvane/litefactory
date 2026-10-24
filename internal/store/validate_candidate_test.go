package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func retainedCandidate(t *testing.T, db *DB, instanceID int64) RepairCandidate {
	t.Helper()
	round, err := db.BeginRepairRound(t.Context(), RepairRound{InstanceID: instanceID, PreCommit: repairPre, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	patch := strings.Repeat("+complete bytes\n", 10000) + string([]byte{0xff, 0xfe})
	sum := sha256.Sum256([]byte(patch))
	return RepairCandidate{ID: "candidate-" + hex.EncodeToString([]byte{byte(instanceID)}), TaskID: "github:example/repo#1", InstanceID: instanceID, RoundID: round.ID, BaseCommit: repairPre, Tree: strings.Repeat("c", 40), Commit: repairPost, Ref: "refs/litefactory/repair/test", Patch: patch, PatchDigest: hex.EncodeToString(sum[:]), Manifest: json.RawMessage(`[{"path":"a.go","mode":"100644"}]`), Evidence: json.RawMessage(`{"reason":"too big","uncertainty":"unknown binary"}`), ScopeRevision: "scope", ReportRevision: "original-report", ReviewDigest: "review", Eligible: true, CreatedAt: graphTestTime()}
}

func TestRetainedCandidateImmutableAndUnclipped(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	in := retainedCandidate(t, db, 1)
	if err := db.PutRepairCandidate(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if err := db.PutRepairCandidate(t.Context(), in); err != nil {
		t.Fatalf("replay: %v", err)
	}
	changed := in
	changed.Evidence = json.RawMessage(`{"reason":"changed"}`)
	if err := db.PutRepairCandidate(t.Context(), changed); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("changed evidence: %v", err)
	}
	changed = in
	changed.ID = "other"
	if err := db.PutRepairCandidate(t.Context(), changed); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("same round new identity: %v", err)
	}
	changed = in
	changed.Patch += "changed"
	if err := db.PutRepairCandidate(t.Context(), changed); err == nil {
		t.Fatal("bad patch digest accepted")
	}
	got, err := db.GetRepairCandidate(t.Context(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Patch != in.Patch || len(got.Patch) <= MaxRepairDiffBytes || got.Resolution != "pending" || got.ReviewComplete || got.ResultCommit != "" {
		t.Fatalf("retained bytes/state: %d %+v", len(got.Patch), got.Resolution)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE validate_repair_candidates SET content='{}' WHERE id=?`, in.ID); err == nil {
		t.Fatal("immutable content updated")
	}
	if _, err := db.LatestKeptRepairCommit(t.Context(), in.InstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending selected: %v", err)
	}
}

func TestRetainedCandidateResolutionCAS(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	in := retainedCandidate(t, db, 2)
	if err := db.PutRepairCandidate(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	decision := RepairCandidateDecision{Action: "accept", ReviewDigest: in.ReviewDigest, ReportRevision: "current-report", Principal: "github:123", SourceID: "comment:1", At: graphTestTime()}
	if _, err := db.ResolveRepairCandidate(t.Context(), in.ID, decision); !errors.Is(err, ErrRepairCandidateIneligible) {
		t.Fatalf("incomplete acceptance: %v", err)
	}
	if err := db.SetRepairCandidateReviewComplete(t.Context(), in.ID, "wrong", graphTestTime()); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("changed review: %v", err)
	}
	if err := db.SetRepairCandidateReviewComplete(t.Context(), in.ID, in.ReviewDigest, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.ResolveRepairCandidate(t.Context(), in.ID, decision); err != nil || got.Resolution != "accept" {
		t.Fatalf("accept %+v %v", got, err)
	}
	if _, err := db.ResolveRepairCandidate(t.Context(), in.ID, decision); err != nil {
		t.Fatalf("replay: %v", err)
	}
	conflict := decision
	conflict.Action = "discard"
	if _, err := db.ResolveRepairCandidate(t.Context(), in.ID, conflict); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("conflict: %v", err)
	}
	conflict = decision
	conflict.ReportRevision = "later-report"
	if _, err := db.ResolveRepairCandidate(t.Context(), in.ID, conflict); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("changed report replay: %v", err)
	}
	other := retainedCandidate(t, db, 3)
	other.Eligible = false
	if err := db.PutRepairCandidate(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRepairCandidateReviewComplete(t.Context(), other.ID, other.ReviewDigest, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveRepairCandidate(t.Context(), other.ID, decision); !errors.Is(err, ErrRepairCandidateIneligible) {
		t.Fatalf("frozen candidate: %v", err)
	}
	decision.Action = "discard"
	decision.SourceID = "comment:2"
	if _, err := db.ResolveRepairCandidate(t.Context(), other.ID, decision); err != nil {
		t.Fatalf("discard incomplete/ineligible: %v", err)
	}
	if _, err := db.LatestKeptRepairCommit(t.Context(), other.InstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("discard selected: %v", err)
	}
	third := retainedCandidate(t, db, 4)
	if err := db.PutRepairCandidate(t.Context(), third); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveRepairCandidate(t.Context(), third.ID, decision); !errors.Is(err, ErrRepairCandidateConflict) {
		t.Fatalf("source reused: %v", err)
	}
}

func TestRetainedCandidateJournalRequiresVerifiedBoundaries(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	in := retainedCandidate(t, db, 5)
	j := RepairCandidateJournal{InstanceID: in.InstanceID, RoundID: in.RoundID, Phase: "capture", State: "intent", ExpectedBase: in.BaseCommit, At: graphTestTime()}
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	journals, err := db.ListRepairCandidateJournals(t.Context(), in.InstanceID)
	if err != nil || len(journals) != 1 {
		t.Fatalf("interrupted capture visible %+v %v", journals, err)
	}
	j.Phase = "reset"
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err == nil {
		t.Fatal("reset allowed before complete capture")
	}
	j.Phase = "capture"
	j.State = "complete"
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); !errors.Is(err, ErrNotFound) {
		t.Fatalf("capture marked complete without artifacts: %v", err)
	}
	if err := db.PutRepairCandidate(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "reset"
	j.State = "intent"
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	j.State = "complete"
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "apply"
	j.State = "intent"
	j.ExpectedResult = in.Commit
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); !errors.Is(err, ErrRepairCandidateIneligible) {
		t.Fatalf("apply before approval: %v", err)
	}
	if err := db.SetRepairCandidateReviewComplete(t.Context(), in.ID, in.ReviewDigest, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveRepairCandidate(t.Context(), in.ID, RepairCandidateDecision{Action: "accept", ReviewDigest: in.ReviewDigest, ReportRevision: "current-report", Principal: "github:123", SourceID: "comment:1", At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LatestKeptRepairCommit(t.Context(), in.InstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("authorization/intent selected as result: %v", err)
	}
	j.State = "complete"
	j.ResultCommit = repairPre
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err == nil {
		t.Fatal("wrong resulting commit accepted")
	}
	j.ResultCommit = in.Commit
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatal(err)
	}
	if err := db.JournalRepairCandidate(t.Context(), in.ID, j); err != nil {
		t.Fatalf("completion replay: %v", err)
	}
	if got, err := db.LatestKeptRepairCommit(t.Context(), in.InstanceID); err != nil || got != in.Commit {
		t.Fatalf("verified result %q %v", got, err)
	}
	if got, err := db.LatestAdoptedRepairCommit(t.Context(), in.InstanceID); err != nil || got != in.Commit {
		t.Fatalf("adopted result %q %v", got, err)
	}
	if err := db.FinishRepairRound(t.Context(), in.RoundID, RepairFinish{State: RepairTooBig, At: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	round, err := db.GetRepairRound(t.Context(), in.RoundID)
	if err != nil || round.State != RepairTooBig || round.ResultCommit != "" {
		t.Fatalf("historical outcome rewritten %+v %v", round, err)
	}
}
