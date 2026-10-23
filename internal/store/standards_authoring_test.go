package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func authoringTestSession(t *testing.T, db *DB) StandardsAuthoringSession {
	t.Helper()
	s, e := db.SaveStandardsAuthoring(t.Context(), StandardsAuthoringSession{ID: "session", Mode: "create", TargetID: "standard", Status: "draft", Data: json.RawMessage(`{"draft":"saved","answers":["settled"]}`)}, 0)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestStandardsAuthoringIndependentFencingAndHistory(t *testing.T) {
	first := newTestDB(t)
	second, e := Open(t.Context(), first.path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := second.Close(); e != nil {
			t.Error(e)
		}
	})
	s := authoringTestSession(t, first)
	original := s
	attempt, e := first.BeginStandardsAuthoringAttempt(t.Context(), StandardsAuthoringAttempt{ID: "attempt", SessionID: s.ID, InputVersion: s.Version, Token: "fence", LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = second.BeginStandardsAuthoringAttempt(t.Context(), StandardsAuthoringAttempt{ID: "other", SessionID: s.ID, InputVersion: s.Version, Token: "other", LeaseUntil: time.Now().UTC().Add(time.Minute)}); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("concurrent attempt: %v", e)
	}
	s.Data = json.RawMessage(`{"draft":"operator edit","answers":["settled"]}`)
	s, e = second.SaveStandardsAuthoring(t.Context(), s, s.Version)
	if e != nil {
		t.Fatal(e)
	}
	late := original
	late.Data = json.RawMessage(`{"draft":"late output"}`)
	if _, e = first.FinishStandardsAuthoringAttempt(t.Context(), attempt, &late); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("late output: %v", e)
	}
	if _, e = first.SaveStandardsAuthoring(t.Context(), original, original.Version); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("stale update: %v", e)
	}
	current, e := first.GetStandardsAuthoring(t.Context(), s.ID, 0)
	if e != nil || !reflect.DeepEqual(current.Data, s.Data) {
		t.Fatalf("lost edited work: %s %v", current.Data, e)
	}
	old, e := first.GetStandardsAuthoring(t.Context(), s.ID, 1)
	if e != nil || !reflect.DeepEqual(old, original) {
		t.Fatalf("changed history: %#v %v", old, e)
	}
	if _, e = first.sql.ExecContext(t.Context(), `UPDATE standards_authoring_versions SET data_json='{}' WHERE session_id=?`, s.ID); e == nil {
		t.Fatal("mutable history")
	}
	attempts, e := first.ListStandardsAuthoringAttempts(t.Context(), s.ID)
	if e != nil || len(attempts) != 1 || attempts[0].Outcome != "interrupted" {
		t.Fatalf("missing interruption: %#v %v", attempts, e)
	}
}

func TestStandardsAuthoringExpiredLeaseAndAtomicCompletion(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	s := authoringTestSession(t, db)
	a, e := db.BeginStandardsAuthoringAttempt(ctx, StandardsAuthoringAttempt{ID: "old", SessionID: s.ID, InputVersion: 1, Token: "old", LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	// Simulate an abandoned process without sleeping. Both authoritative lease fields agree.
	a.LeaseUntil = time.Now().UTC().Add(-time.Minute)
	raw, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.sql.ExecContext(ctx, `UPDATE standards_authoring_attempts SET lease_until=?,attempt_json=? WHERE id=?`, FormatTime(a.LeaseUntil), string(raw), a.ID); e != nil {
		t.Fatal(e)
	}
	b, e := db.BeginStandardsAuthoringAttempt(ctx, StandardsAuthoringAttempt{ID: "new", SessionID: s.ID, InputVersion: 1, Token: "new", LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.FinishStandardsAuthoringAttempt(ctx, a, &s); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("expired accepted: %v", e)
	}
	b.Outcome = "succeeded"
	if _, e = db.FinishStandardsAuthoringAttempt(ctx, b, &s); e != nil {
		t.Fatal(e)
	}
	// A proposal insert with a missing base must roll back all partial work.
	p := StandardsProposalVersion{ProposalID: "proposal", BaseRevision: 123, SchemaVersion: 1, Operations: []StandardsOperation{}, ChangeDigest: "change", ResultDigest: "result", Author: "owner"}
	if _, e = db.CompleteStandardsAuthoring(ctx, s.ID, s.Version, p); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("stale base: %v", e)
	}
	proposals, e := db.ListStandardsProposals(ctx)
	if e != nil || len(proposals) != 0 {
		t.Fatalf("partial proposal: %#v %v", proposals, e)
	}
	p.BaseRevision = 0
	if _, e = db.sql.ExecContext(ctx, `CREATE TRIGGER authoring_test_completion_failure BEFORE INSERT ON standards_authoring_completions BEGIN SELECT RAISE(ABORT, 'simulated interruption'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.CompleteStandardsAuthoring(ctx, s.ID, s.Version, p); e == nil {
		t.Fatal("completion should fail")
	}
	proposals, e = db.ListStandardsProposals(ctx)
	if e != nil || len(proposals) != 0 {
		t.Fatalf("proposal survived failed link: %#v %v", proposals, e)
	}
	if _, e = db.sql.ExecContext(ctx, `DROP TRIGGER authoring_test_completion_failure`); e != nil {
		t.Fatal(e)
	}
	linked, e := db.CompleteStandardsAuthoring(ctx, s.ID, s.Version, p)
	if e != nil {
		t.Fatal(e)
	}
	// Moving the head cannot prevent recovery of a committed completion reply.
	published := standardsTestProposal(t, db, "published", 0)
	if _, e = db.PublishStandardsProposal(ctx, published, StandardsSnapshot{Categories: []StandardsCategory{}, Standards: []StandardsStandard{}}, "42"); e != nil {
		t.Fatal(e)
	}
	retry, e := db.CompleteStandardsAuthoring(ctx, s.ID, s.Version, p)
	if e != nil || !reflect.DeepEqual(linked, retry) {
		t.Fatalf("completion retry: %#v %v", retry, e)
	}
	if _, e = db.GetStandardsApproval(ctx, linked.ProposalID, linked.Version); !errors.Is(e, ErrStandardsApprovalRequired) {
		t.Fatalf("authoring approved: %v", e)
	}
	current, e := db.GetStandardsAuthoring(ctx, s.ID, 0)
	if e != nil || current.Status != "completed" {
		t.Fatalf("completion status: %#v %v", current, e)
	}
	if _, e = db.SaveStandardsAuthoring(ctx, s, s.Version); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("completed edit: %v", e)
	}
}

func TestStandardsAuthoringMetadataAndCompletionFence(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	s := authoringTestSession(t, db)
	invalid := s
	invalid.Status = "completed"
	if _, e := db.SaveStandardsAuthoring(ctx, invalid, s.Version); e == nil {
		t.Fatal("completed state accepted without proposal")
	}
	a, e := db.BeginStandardsAuthoringAttempt(ctx, StandardsAuthoringAttempt{ID: "working", SessionID: s.ID, InputVersion: s.Version, Token: "fence", LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	p := StandardsProposalVersion{ProposalID: "handoff", SchemaVersion: 1, Operations: []StandardsOperation{}, ChangeDigest: "change", ResultDigest: "result", Author: "owner"}
	if _, e = db.CompleteStandardsAuthoring(ctx, s.ID, s.Version, p); e != nil {
		t.Fatal(e)
	}
	if _, e = db.FinishStandardsAuthoringAttempt(ctx, a, &s); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("completed session accepted output: %v", e)
	}
	attempts, e := db.ListStandardsAuthoringAttempts(ctx, s.ID)
	if e != nil || len(attempts) != 1 || attempts[0].Outcome != "interrupted" || attempts[0].Diagnostic == "" {
		t.Fatalf("completion left active attempt: %#v %v", attempts, e)
	}
}
