package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreventionAtomicFinalizationAndRecovery(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := PreventionAnalysis{ID: "a", AttemptID: "writer", RequestKey: "request", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{"issues":[1]}`), Limits: json.RawMessage(`{}`)}
	saved, created, err := db.CreatePreventionAnalysis(ctx, a)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	same, created, err := db.CreatePreventionAnalysis(ctx, a)
	if err != nil || created || !reflect.DeepEqual(saved, same) {
		t.Fatalf("recover invocation: %v %v", created, err)
	}
	a.ScopeDigest = "changed"
	if _, _, err = db.CreatePreventionAnalysis(ctx, a); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	if err = db.FreezePreventionAnalysis(ctx, "a", "stale", json.RawMessage(`{}`)); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	if err = db.FreezePreventionAnalysis(ctx, "a", "writer", json.RawMessage(`{"evidence":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err = db.FreezePreventionAnalysis(ctx, "a", "writer", json.RawMessage(`{}`)); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	p := standardsTestProposal(t, db, "base", 0)
	revision, err := db.PublishStandardsProposal(ctx, p, StandardsSnapshot{Categories: []StandardsCategory{}, Standards: []StandardsStandard{}}, "42")
	if err != nil {
		t.Fatal(err)
	}
	pair := PreventionProposal{Proposal: StandardsProposalVersion{ProposalID: "new", BaseRevision: revision.ID, SchemaVersion: 1, ChangeDigest: "change", ResultDigest: "result", Operations: []StandardsOperation{}, Author: "analyst"}, Provenance: json.RawMessage(`{"cause":"evidence-1"}`)}
	broken := pair
	broken.Proposal.ProposalID = "bad"
	broken.Proposal.BaseRevision = 9999
	if _, err = db.FinalizePreventionAnalysis(ctx, "a", "writer", "digest", json.RawMessage(`{"outcome":"proposed"}`), []PreventionProposal{pair, broken}); err == nil {
		t.Fatal("broken batch committed")
	}
	if _, err = db.GetStandardsProposal(ctx, "new", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("partial proposal", err)
	}
	got, err := db.FinalizePreventionAnalysis(ctx, "a", "writer", "digest", json.RawMessage(`{"outcome":"proposed"}`), []PreventionProposal{pair})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := db.FinalizePreventionAnalysis(ctx, "a", "stale", "digest", json.RawMessage(`{"outcome":"proposed"}`), nil)
	if err != nil || !reflect.DeepEqual(got, retry) {
		t.Fatalf("receipt recovery: %v %v", retry, err)
	}
	if _, err = db.FinalizePreventionAnalysis(ctx, "a", "writer", "different", json.RawMessage(`{}`), nil); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	if _, err = db.GetStandardsApproval(ctx, "new", 1); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatal("analysis approved proposal", err)
	}
	if err = db.UpdatePreventionAnalysis(ctx, "a", "writer", "failed", nil); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("final result mutable", err)
	}
}

func TestPreventionLeaseRecoveryFencesExpiredWriter(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a := PreventionAnalysis{ID: "lease", AttemptID: "owner", RequestKey: "lease-request", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)}
	a, _, err := db.CreatePreventionAnalysis(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	live, err := db.RecoverPreventionAnalysis(ctx, a.ID)
	if err != nil || live.Status != "collecting" {
		t.Fatal(live.Status, err)
	}
	// Simulate a crash and elapsed host deadline without waiting ten minutes.
	a.DeadlineAt = a.CreatedAt.Add(-1)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE prevention_analyses SET record_json=? WHERE id=?`, string(raw), a.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := db.RecoverPreventionAnalysis(ctx, a.ID)
	if err != nil || expired.Status != "interrupted" {
		t.Fatal(expired.Status, err)
	}
	if err = db.FreezePreventionAnalysis(ctx, a.ID, a.AttemptID, json.RawMessage(`{}`)); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
}

func TestPreventionExpiredCollectionRetainsEvidenceWithoutLaunching(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a, _, err := db.CreatePreventionAnalysis(ctx, PreventionAnalysis{ID: "expired-collection", AttemptID: "owner", RequestKey: "expired-collection", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	a.DeadlineAt = a.CreatedAt.Add(-1)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE prevention_analyses SET record_json=? WHERE id=?`, string(raw), a.ID); err != nil {
		t.Fatal(err)
	}
	manifest := json.RawMessage(`{"evidence":[{"id":"retained","excerpt":"completed before cancellation"}]}`)
	if err = db.FreezePreventionAnalysis(ctx, a.ID, "stale", manifest); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	if err = db.FreezePreventionAnalysis(ctx, a.ID, a.AttemptID, manifest); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetPreventionAnalysis(ctx, a.ID)
	if err != nil || got.Status != "interrupted" || string(got.Manifest) != string(manifest) {
		t.Fatal(got, err)
	}
	if err = db.UpdatePreventionAnalysis(ctx, a.ID, a.AttemptID, "interrupted", json.RawMessage(`{"reason":"deadline"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinalizePreventionAnalysis(ctx, a.ID, a.AttemptID, "digest", json.RawMessage(`{}`), nil); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
}

func TestPreventionStandardsWritesUseAtomicLease(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a, _, err := db.CreatePreventionAnalysis(ctx, PreventionAnalysis{ID: "binding-lease", AttemptID: "owner", RequestKey: "binding-lease", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	leased := WithPreventionWriter(ctx, a.ID, a.AttemptID)
	binding := StandardsRunBinding{RootRunID: "analysis:" + a.ID, Outcome: "unavailable"}
	if _, err = db.SaveStandardsRunBinding(leased, binding); err != nil {
		t.Fatal(err)
	}
	selection := StandardsRunSelection{RootRunID: binding.RootRunID, ConsumerKey: "analyst", AttemptID: a.AttemptID, Digest: "selected", CategoryIDs: []string{}, StandardIDs: []string{}}
	if _, err = db.SaveStandardsRunSelection(leased, selection, 0); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdatePreventionAnalysis(ctx, a.ID, a.AttemptID, "interrupted", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SaveStandardsRunBinding(leased, binding); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("stale binding writer", err)
	}
	if _, err = db.SaveStandardsRunSelection(leased, selection, 1); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("stale selection writer", err)
	}
	if err = db.AppendStandardsRunObservation(leased, StandardsRunObservation{RootRunID: binding.RootRunID, AttemptID: a.AttemptID, Kind: "offered"}); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("stale delivery writer", err)
	}
	// Ordinary workflow callers without a standalone lease retain existing semantics.
	if err = db.AppendStandardsRunObservation(ctx, StandardsRunObservation{RootRunID: binding.RootRunID, AttemptID: a.AttemptID, Kind: "offered"}); err != nil {
		t.Fatal(err)
	}
}

func TestPreventionOutputReservationIsDurableAndMonotonic(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	a, _, err := db.CreatePreventionAnalysis(ctx, PreventionAnalysis{ID: "budget", AttemptID: "owner", RequestKey: "budget", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetPreventionOutput(ctx, a.ID, a.AttemptID, 4096, true); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdatePreventionAnalysis(ctx, a.ID, a.AttemptID, "failed", nil); err != nil {
		t.Fatal(err)
	}
	if err = db.SetPreventionOutput(ctx, a.ID, a.AttemptID, 8192, false); err != nil {
		t.Fatal(err)
	}
	saved, err := db.GetPreventionAnalysis(ctx, a.ID)
	if err != nil || saved.OutputBytes != 8192 || !saved.OutputLimited {
		t.Fatal(saved, err)
	}
	if err = db.SetPreventionOutput(ctx, a.ID, a.AttemptID, 1, false); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("budget reset", err)
	}
	if err = db.SetPreventionOutput(ctx, a.ID, "stale", 8192, false); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal("stale reservation", err)
	}
	if err = db.SetPreventionOutput(ctx, a.ID, a.AttemptID, (128<<10)+1, false); err == nil {
		t.Fatal("allowance exceeded")
	}
}

func TestPreventionRepositoryFilterPrecedesLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	for i, repo := range []string{"wanted/repo", "other/repo", "other/repo"} {
		id := string(rune('a' + i))
		_, _, err := db.CreatePreventionAnalysis(ctx, PreventionAnalysis{ID: id, AttemptID: id, RequestKey: id, ScopeDigest: id, Repository: repo, Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.ListPreventionAnalysesForRepository(ctx, "wanted/repo", 1)
	if err != nil || len(rows) != 1 || rows[0].ID != "a" {
		t.Fatal(rows, err)
	}
}

func TestPreventionCompetingConnectionsFinalizeOnce(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "competing.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if err = first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	a, _, err := first.CreatePreventionAnalysis(ctx, PreventionAnalysis{ID: "concurrent", AttemptID: "writer", RequestKey: "concurrent", ScopeDigest: "scope", Repository: "owner/repo", Scope: json.RawMessage(`{}`), Limits: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = first.FreezePreventionAnalysis(ctx, a.ID, a.AttemptID, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	base := standardsTestProposal(t, first, "concurrent-base", 0)
	revision, err := first.PublishStandardsProposal(ctx, base, StandardsSnapshot{Categories: []StandardsCategory{}, Standards: []StandardsStandard{}}, "42")
	if err != nil {
		t.Fatal(err)
	}
	batch := []PreventionProposal{{Proposal: StandardsProposalVersion{ProposalID: "concurrent-proposal", BaseRevision: revision.ID, SchemaVersion: 1, ChangeDigest: "change", ResultDigest: "book", Operations: []StandardsOperation{}, Author: "analyst"}, Provenance: json.RawMessage(`{}`)}}
	result := json.RawMessage(`{"result":"same"}`)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []*DB{first, second} {
		go func() {
			<-start
			_, e := db.FinalizePreventionAnalysis(ctx, a.ID, a.AttemptID, "digest", result, batch)
			results <- e
		}()
	}
	close(start)
	succeeded := 0
	for range 2 {
		e := <-results
		if e == nil {
			succeeded++
		} else if !errors.Is(e, ErrStandardsConflict) {
			t.Fatal(e)
		}
	}
	if succeeded == 0 {
		t.Fatal("both competing writes failed")
	}
	left, err := first.FinalizePreventionAnalysis(ctx, a.ID, a.AttemptID, "digest", result, batch)
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.FinalizePreventionAnalysis(ctx, a.ID, "stale", "digest", result, nil)
	if err != nil || !reflect.DeepEqual(left, right) {
		t.Fatal(right, err)
	}
	if _, err = second.FinalizePreventionAnalysis(ctx, a.ID, a.AttemptID, "changed", json.RawMessage(`{}`), batch); !errors.Is(err, ErrPreventionConflict) {
		t.Fatal(err)
	}
	var count int
	if err = second.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM prevention_analysis_proposals WHERE analysis_id=?`, a.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = second.GetStandardsApproval(ctx, "concurrent-proposal", 1); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatal(err)
	}
}
