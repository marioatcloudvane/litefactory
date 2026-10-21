package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func driftTestAnalysis(t *testing.T, db *DB) DriftAnalysis {
	t.Helper()
	a, created, err := db.CreateDriftAnalysis(t.Context(), DriftAnalysis{ID: "analysis", RunID: "drift:analysis", Repository: "github:owner/repo", RequestKey: "key", RequestDigest: "request", Scope: json.RawMessage(`{"base":"base","head":"head"}`), Limits: DriftLimits{Paths: 200, Comments: 100, Requests: 20, SourceBytes: 8 << 20, Calls: 40, OutboundBytes: 256 << 10, ResultBytes: 128 << 10, Proposals: 10}})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	return a
}
func TestDriftAtomicBudgetAndFencing(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	ctx := t.Context()
	again, created, err := db.CreateDriftAnalysis(ctx, a)
	if err != nil || created || again.ID != a.ID {
		t.Fatalf("replay: %v %v", created, err)
	}
	changed := a
	changed.RequestDigest = "changed"
	if _, _, err = db.CreateDriftAnalysis(ctx, changed); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("changed request: %v", err)
	}
	if err = db.AppendDriftEvidence(ctx, a.ID, a.Generation, DriftEvidence{ID: "evidence", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	item, err := db.GetDriftEvidence(ctx, a.ID, "evidence")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	fail := make(chan error, 40)
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := db.ChargeDriftBudget(ctx, a.ID, a.Generation, DriftUsage{Calls: 1, OutboundBytes: 2}, []DriftOffer{{EvidenceID: item.ID, Digest: item.Digest, Start: 0, End: 5}}, false)
			fail <- e
		}()
	}
	wg.Wait()
	close(fail)
	accepted := 0
	for e := range fail {
		if e == nil {
			accepted++
		} else if !errors.Is(e, ErrDriftBudget) {
			t.Fatal(e)
		}
	}
	if accepted != 36 {
		t.Fatalf("data calls: %d", accepted)
	}
	current, err := db.GetDriftAnalysis(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Usage.Calls != 36 || len(current.Offers) != 36 {
		t.Fatalf("lost reservation: %#v", current.Usage)
	}
	if _, err = db.ChargeDriftBudget(ctx, a.ID, a.Generation, DriftUsage{Calls: 4}, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.InterruptDriftAnalysis(ctx, a.ID, a.Generation, "interrupted"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChargeDriftBudget(ctx, a.ID, a.Generation, DriftUsage{}, nil, true); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("stale writer: %v", err)
	}
}
func TestDriftFinalizationRollbackReceiptAndExactProposal(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	ctx := t.Context()
	report := json.RawMessage(`{"finding":"bounded evidence"}`)
	candidate := DriftCandidate{Key: "one", Proposal: StandardsProposalVersion{ProposalID: "proposal-one", SchemaVersion: 1, ChangeDigest: "change", ResultDigest: "result", Author: "drift", Operations: []StandardsOperation{}}}
	bad := candidate
	bad.Key = "two" // Duplicate proposal identity fails after first insert; all effects must roll back.
	if _, err := db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, a.Version, "request-one", "digest", report, "completed", "proposals", []DriftCandidate{candidate, bad}); err == nil {
		t.Fatal("duplicate proposal committed")
	}
	if _, err := db.GetStandardsProposal(ctx, candidate.Proposal.ProposalID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial proposal retained: %v", err)
	}
	receipt, err := db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, a.Version, "request-one", "digest", report, "completed", "proposals", []DriftCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, a.Version, "request-one", "digest", report, "completed", "proposals", []DriftCandidate{candidate})
	if err != nil || retry.ProposalLinks[0] != receipt.ProposalLinks[0] {
		t.Fatalf("lost-reply recovery: %#v %v", retry, err)
	}
	if _, err = db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, a.Version, "request-one", "changed", report, "completed", "proposals", []DriftCandidate{candidate}); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	if _, err = db.GetStandardsApproval(ctx, candidate.Proposal.ProposalID, 1); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("generated approval: %v", err)
	}
	retained, err := db.GetDriftReceipt(ctx, a.ID, "request-one")
	if err != nil || retained.Digest != "digest" {
		t.Fatalf("receipt: %#v %v", retained, err)
	}
}

func TestDriftFinalizationRejectsContradictoryOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name        string
		execution   string
		disposition string
		candidate   bool
	}{
		{name: "incomplete no change", execution: "incomplete", disposition: "no_warranted_change"},
		{name: "failed no change", execution: "failed", disposition: "no_warranted_change"},
		{name: "failed proposals", execution: "failed", disposition: "proposals", candidate: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDB(t)
			a := driftTestAnalysis(t, db)
			var candidates []DriftCandidate
			if tt.candidate {
				candidates = []DriftCandidate{{Key: "candidate", Proposal: StandardsProposalVersion{ProposalID: "proposal", SchemaVersion: 1, ChangeDigest: "change", ResultDigest: "result"}}}
			}
			_, err := db.FinalizeDriftAnalysis(t.Context(), a.ID, a.Generation, a.Version, "request", "digest", json.RawMessage(`{}`), tt.execution, tt.disposition, candidates)
			if err == nil {
				t.Fatal("contradictory outcome accepted")
			}
			if _, err := db.GetDriftReceipt(t.Context(), a.ID, "request"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("rejected outcome retained receipt: %v", err)
			}
			current, err := db.GetDriftAnalysis(t.Context(), a.ID)
			if err != nil || current.Execution != "running" || current.Version != a.Version {
				t.Fatalf("rejected outcome changed execution: %#v %v", current, err)
			}
		})
	}
}

func TestDriftRecoverySpendsRetainedAllowanceWithoutChangingResult(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	ctx := t.Context()
	if _, err := db.ChargeDriftRecovery(ctx, a.ID, DriftUsage{Calls: 1}); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("recovery before acceptance: %v", err)
	}
	receipt, err := db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, a.Version, "request", "digest", json.RawMessage(`{}`), "completed", "insufficient_evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := db.ChargeDriftRecovery(ctx, a.ID, DriftUsage{Calls: 40, OutboundBytes: 256 << 10})
	if err != nil || current.Execution != "completed" || current.Usage.Calls != 40 {
		t.Fatalf("recovery charge: %#v %v", current, err)
	}
	if _, err := db.ChargeDriftRecovery(ctx, a.ID, DriftUsage{Calls: 1}); !errors.Is(err, ErrDriftBudget) {
		t.Fatalf("recovery exceeded allowance: %v", err)
	}
	retained, err := db.GetDriftReceipt(ctx, a.ID, "request")
	if err != nil || string(retained.Report) != string(receipt.Report) || retained.Digest != receipt.Digest {
		t.Fatalf("recovery changed accepted result: %#v %v", retained, err)
	}
}

func TestDriftEvidenceHonorsLowerItemLimit(t *testing.T) {
	db := newTestDB(t)
	a, _, err := db.CreateDriftAnalysis(t.Context(), DriftAnalysis{
		ID: "small", Repository: "github:owner/repo", RequestDigest: "request",
		Scope: json.RawMessage(`{}`), Limits: DriftLimits{ItemBytes: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.RunID != a.ID {
		t.Fatalf("standalone run identity not allocated: %#v", a)
	}
	if err := db.AppendDriftEvidence(t.Context(), a.ID, a.Generation, DriftEvidence{ID: "large", Text: "abc"}); !errors.Is(err, ErrDriftBudget) {
		t.Fatalf("item ceiling bypassed: %v", err)
	}
	if _, err := db.GetDriftEvidence(t.Context(), a.ID, "large"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oversized item retained: %v", err)
	}
	if err := db.AppendDriftEvidence(t.Context(), a.ID, a.Generation, DriftEvidence{ID: "exact", Text: "ab"}); err != nil {
		t.Fatalf("exact limit refused: %v", err)
	}
}

func TestDriftAnalystRunIdentityIsAtomicAndImmutable(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	ctx := t.Context()
	var analysis string
	if err := db.sql.QueryRowContext(ctx, `SELECT analysis_id FROM drift_analyst_runs WHERE id=?`, a.RunID).Scan(&analysis); err != nil || analysis != a.ID {
		t.Fatalf("persisted analyst run: %q %v", analysis, err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE drift_analyst_runs SET id='replacement' WHERE id=?`, a.RunID); err == nil {
		t.Fatal("analyst run identity changed")
	}
	duplicate := a
	duplicate.ID = "second-analysis"
	duplicate.RequestKey = "second-key"
	if _, _, err := db.CreateDriftAnalysis(ctx, duplicate); err == nil {
		t.Fatal("analyst run attached to a second analysis")
	}
	if _, err := db.GetDriftAnalysis(ctx, duplicate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed run allocation left analysis: %v", err)
	}
}

func TestDriftFinalizationRequiresReportAndCoverageObjects(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	for _, report := range []string{`null`, `[]`, `{"coverage":null}`, `{"coverage":[]}`} {
		if _, err := db.FinalizeDriftAnalysis(t.Context(), a.ID, a.Generation, a.Version, "request", "digest", json.RawMessage(report), "completed", "insufficient_evidence", nil); err == nil {
			t.Errorf("accepted malformed report %s", report)
		}
	}
	if _, err := db.GetDriftReceipt(t.Context(), a.ID, "request"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed report retained receipt: %v", err)
	}
}

func TestDriftEvidenceExpiryPreservesProvenanceAndAcceptedResult(t *testing.T) {
	db := newTestDB(t)
	a := driftTestAnalysis(t, db)
	ctx := t.Context()
	for _, item := range []DriftEvidence{
		{ID: "text", Kind: "code", Locator: "commit:path", Text: "bounded café", Omission: "source omission", Metadata: json.RawMessage(`{"blob":"object"}`)},
		{ID: "empty", Kind: "code", Locator: "commit:empty", Text: ""},
	} {
		if err := db.AppendDriftEvidence(ctx, a.ID, a.Generation, item); err != nil {
			t.Fatal(err)
		}
	}
	before, err := db.GetDriftEvidence(ctx, a.ID, "text")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExpireDriftEvidence(ctx, a.ID); !errors.Is(err, ErrDriftConflict) {
		t.Fatalf("expired active analysis: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE drift_evidence SET evidence_json=json_set(evidence_json,'$.locator','forged') WHERE analysis_id=?`, a.ID); err == nil {
		t.Fatal("source metadata update allowed")
	}
	charged, err := db.ChargeDriftBudget(ctx, a.ID, a.Generation, DriftUsage{Calls: 1}, []DriftOffer{{EvidenceID: before.ID, Digest: before.Digest, Start: 0, End: len(before.Text)}}, false)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.FinalizeDriftAnalysis(ctx, a.ID, a.Generation, charged.Version, "request", "digest", json.RawMessage(`{"coverage":{"retained":2}}`), "completed", "insufficient_evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.ExpireDriftEvidence(ctx, a.ID); err != nil || n != 2 {
		t.Fatalf("expire excerpts: %d %v", n, err)
	}
	if n, err := db.ExpireDriftEvidence(ctx, a.ID); err != nil || n != 0 {
		t.Fatalf("repeat expiry: %d %v", n, err)
	}
	after, err := db.GetDriftEvidence(ctx, a.ID, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.ExcerptBytes = len(before.Text)
	want.ExcerptExpired = true
	want.Text = ""
	want.Omission += "; retained excerpt expired"
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("expiry changed provenance: got %#v want %#v", after, want)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE drift_evidence SET evidence_json=json_set(evidence_json,'$.text','restored') WHERE analysis_id=? AND id=?`, a.ID, before.ID); err == nil {
		t.Fatal("expired excerpt restoration allowed")
	}
	retained, err := db.GetDriftReceipt(ctx, a.ID, receipt.RequestID)
	if err != nil || !reflect.DeepEqual(retained, receipt) {
		t.Fatalf("expiry changed accepted receipt: %#v %v", retained, err)
	}
	current, err := db.GetDriftAnalysis(ctx, a.ID)
	if err != nil || len(current.Offers) != 1 || current.Offers[0].End != len(before.Text) || string(current.Report) != string(receipt.Report) {
		t.Fatalf("expiry changed report or offered provenance: %#v %v", current, err)
	}
}
