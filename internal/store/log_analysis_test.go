package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func createLogAnalysisFixture(t *testing.T, db *DB, id string) LogAnalysis {
	t.Helper()
	a, err := db.CreateLogAnalysis(t.Context(), LogAnalysis{ID: id, StartKey: "start-" + id, RequestDigest: "request-" + id, GrantDigest: "grant", Scope: json.RawMessage(`{"runs":["run-1"]}`), Limits: json.RawMessage(`{"calls":6,"bytes":1000}`)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func logAnalysisCandidateFixture(key, id string) LogAnalysisCandidate {
	return LogAnalysisCandidate{Key: key, Proposal: StandardsProposalVersion{ProposalID: id, SchemaVersion: 1, Author: "log analyst", ChangeDigest: "change-" + id, ResultDigest: "result-" + id, Operations: []StandardsOperation{{Kind: "standard_add", Standard: &StandardsStandard{ID: "new-standard", Title: "Practice", Body: "Guidance"}}}}}
}

func TestLogAnalysisAtomicBatchAndReceiptRecovery(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "atomic")
	f := LogAnalysisFinalization{AnalysisID: a.ID, Generation: a.Generation, RequestID: "submit-1", RequestDigest: "report-digest", Report: json.RawMessage(`{"findings":["supported practice"]}`), State: "completed", Disposition: "proposals", Candidates: []LogAnalysisCandidate{logAnalysisCandidateFixture("first", "proposal-1"), logAnalysisCandidateFixture("second", "proposal-1")}}
	// The second conflicting upstream identity fails after the first insertion; nothing may survive.
	if r, err := db.FinalizeLogAnalysis(t.Context(), f); err == nil || r.AnalysisID != "" {
		t.Fatalf("partial receipt: %#v %v", r, err)
	}
	if _, err := db.GetStandardsProposal(t.Context(), "proposal-1", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proposal leaked after rollback: %v", err)
	}
	if _, err := db.GetLogAnalysisReport(t.Context(), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report leaked after rollback: %v", err)
	}
	current, err := db.GetLogAnalysis(t.Context(), a.ID)
	if err != nil || current.State != "running" || current.Generation != a.Generation {
		t.Fatalf("rollback changed writer: %#v %v", current, err)
	}
	f.Candidates[1] = logAnalysisCandidateFixture("second", "proposal-2")
	f.Candidates[1].Proposal.Version = -1
	if _, err = db.FinalizeLogAnalysis(t.Context(), f); err == nil {
		t.Fatal("negative prepared proposal version accepted")
	}
	if _, err = db.GetStandardsProposal(t.Context(), "proposal-1", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("proposal leaked after invalid prepared version: %v", err)
	}
	f.Candidates[1].Proposal.Version = 0
	receipt, err := db.FinalizeLogAnalysis(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Proposals) != 2 {
		t.Fatalf("missing exact proposal links: %#v", receipt)
	}
	retry, err := db.FinalizeLogAnalysis(t.Context(), f)
	if err != nil || !reflect.DeepEqual(receipt, retry) {
		t.Fatalf("identical retry: %#v %v", retry, err)
	}
	recovered, err := db.GetLogAnalysisReceipt(t.Context(), a.ID, f.RequestID)
	if err != nil || !reflect.DeepEqual(receipt, recovered) {
		t.Fatalf("receipt recovery: %#v %v", recovered, err)
	}
	f.RequestDigest = "changed-report"
	if _, err = db.FinalizeLogAnalysis(t.Context(), f); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
	for _, id := range []string{"proposal-1", "proposal-2"} {
		if _, err = db.GetStandardsApproval(t.Context(), id, 1); !errors.Is(err, ErrStandardsApprovalRequired) {
			t.Fatalf("generated approval: %s %v", id, err)
		}
	}
	links, err := db.ListLogAnalysisProposalLinks(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(links, receipt.Proposals) {
		t.Fatalf("stored links differ: %#v %v", links, err)
	}
}

func TestLogAnalysisFencingAndPermanentBudget(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "bounded")
	// Supplying global ceilings cannot expand the saved lower allowance.
	if err := db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 5, 900, 40, 256<<10); err != nil {
		t.Fatal(err)
	}
	if err := db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 1, 101, 40, 256<<10); !errors.Is(err, ErrLogAnalysisBudget) {
		t.Fatalf("lower byte bound expanded: %v", err)
	}
	if err := db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 2, 0, 40, 256<<10); !errors.Is(err, ErrLogAnalysisBudget) {
		t.Fatalf("lower call bound expanded: %v", err)
	}
	current, err := db.GetLogAnalysis(t.Context(), a.ID)
	if err != nil || current.Calls != 5 || current.OutboundBytes != 900 {
		t.Fatalf("reservation refunded or partial: %#v %v", current, err)
	}
	if err = db.CheckLogAnalysisWriter(t.Context(), a.ID, a.Generation+1); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("competing writer accepted: %v", err)
	}
	if err = db.InterruptLogAnalysis(t.Context(), a.ID, a.Generation+1, "incomplete", "interrupted"); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("competing interruption accepted: %v", err)
	}
	if err = db.InterruptLogAnalysis(t.Context(), a.ID, a.Generation, "incomplete", "interrupted"); err != nil {
		t.Fatal(err)
	}
	if err = db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 0, 0, 40, 256<<10); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("orphan writer accepted: %v", err)
	}
	if _, err = db.RecordLogAnalysisEvidence(t.Context(), LogAnalysisEvidence{AnalysisID: a.ID, Generation: a.Generation, CallKey: "late", Metadata: json.RawMessage(`{}`)}); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("orphan evidence accepted: %v", err)
	}
	retry, err := db.CreateLogAnalysis(t.Context(), LogAnalysis{ID: "replacement", StartKey: a.StartKey, RequestDigest: a.RequestDigest, GrantDigest: a.GrantDigest, Scope: a.Scope, Limits: a.Limits})
	if err != nil || retry.ID != a.ID || retry.State != "incomplete" || retry.Calls != 5 || retry.OutboundBytes != 900 {
		t.Fatalf("restart reset invocation: %#v %v", retry, err)
	}
}

func TestLogAnalysisStandardsBindingOwnsRealInvocation(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "standalone")
	if _, err := db.SaveAnalystStandardsBinding(t.Context(), "missing", StandardsRunBinding{Outcome: "unavailable"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fabricated invocation binding: %v", err)
	}
	original, err := db.SaveAnalystStandardsBinding(t.Context(), a.ID, StandardsRunBinding{RootRunID: "workflow-root", Outcome: "unavailable", Reason: "No published book"})
	if err != nil {
		t.Fatal(err)
	}
	if original.RootRunID != "analysis:"+a.ID {
		t.Fatalf("wrong namespace: %#v", original)
	}
	next, err := db.SaveAnalystStandardsBinding(t.Context(), a.ID, StandardsRunBinding{Outcome: "verified", SnapshotBytes: []byte(`{"changed":true}`)})
	if err != nil || !reflect.DeepEqual(original, next) {
		t.Fatalf("standalone binding repinned: %#v %v", next, err)
	}
	if _, err = db.GetStandardsRunBinding(t.Context(), "workflow-root"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("workflow identity fabricated: %v", err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM standards_run_bindings WHERE root_run_id=?`, original.RootRunID); err == nil {
		t.Fatal("retained standalone book deleted")
	}
	fetched, err := db.GetAnalystStandardsBinding(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(original, fetched) {
		t.Fatalf("association lost: %#v %v", fetched, err)
	}
}

func TestLogAnalysisFencedRecoveryKeepsOriginalAllowance(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "recovery")
	if err := db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 4, 700, 40, 256<<10); err != nil {
		t.Fatal(err)
	}
	f := LogAnalysisFinalization{AnalysisID: a.ID, Generation: a.Generation, RequestID: "accepted", RequestDigest: "accepted-digest", Report: json.RawMessage(`{"reason":"weak evidence"}`), State: "completed", Disposition: "insufficient_evidence"}
	receipt, err := db.FinalizeLogAnalysis(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"provider":"fixture","completed":true}`)
	if err = db.RecordLogAnalysisProviderResult(t.Context(), a.ID, a.Generation, result); err != nil {
		t.Fatal(err)
	}
	if err = db.ReserveLogAnalysisRecoveryBudget(t.Context(), a.ID, a.Generation, 1, 200); err != nil {
		t.Fatal(err)
	}
	if err = db.ReserveLogAnalysisRecoveryBudget(t.Context(), a.ID, a.Generation, 1, 101); !errors.Is(err, ErrLogAnalysisBudget) {
		t.Fatalf("recovery expanded byte ceiling: %v", err)
	}
	if err = db.ReserveLogAnalysisRecoveryBudget(t.Context(), a.ID, a.Generation, 2, 0); !errors.Is(err, ErrLogAnalysisBudget) {
		t.Fatalf("recovery expanded call ceiling: %v", err)
	}
	if err = db.ReserveLogAnalysisRecoveryBudget(t.Context(), a.ID, a.Generation, 1, 100); err != nil {
		t.Fatal(err)
	}
	current, err := db.GetLogAnalysis(t.Context(), a.ID)
	if err != nil || current.Calls != 6 || current.OutboundBytes != 1000 || string(current.Provenance) != string(result) {
		t.Fatalf("recovery reset state: %#v %v", current, err)
	}
	if err = db.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 0, 0, 40, 256<<10); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("recovery relaunched live work: %v", err)
	}
	for _, generation := range []int64{0, a.Generation + 1, a.Generation + 2} {
		if err = db.RecordLogAnalysisProviderResult(t.Context(), a.ID, generation, result); !errors.Is(err, ErrLogAnalysisConflict) {
			t.Fatalf("provider provenance accepted wrong origin %d: %v", generation, err)
		}
		if err = db.ReserveLogAnalysisRecoveryBudget(t.Context(), a.ID, generation, 0, 0); !errors.Is(err, ErrLogAnalysisConflict) {
			t.Fatalf("receipt accepted wrong origin %d: %v", generation, err)
		}
	}
	recovered, err := db.GetLogAnalysisReceipt(t.Context(), a.ID, f.RequestID)
	if err != nil || !reflect.DeepEqual(receipt, recovered) {
		t.Fatalf("host provider result changed receipt: %#v %v", recovered, err)
	}
	report, err := db.GetLogAnalysisReport(t.Context(), a.ID)
	if err != nil || string(report) != string(f.Report) {
		t.Fatalf("host provider result changed report: %s %v", report, err)
	}
}

func TestLogAnalysisInterruptionRetainsHostOnlyReport(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "host-interrupted")
	if err := db.InterruptLogAnalysis(t.Context(), a.ID, a.Generation, "incomplete", "provider interrupted before submission"); err != nil {
		t.Fatal(err)
	}
	raw, err := db.GetLogAnalysisReport(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Disposition string            `json:"disposition"`
		Reason      string            `json:"reason"`
		Findings    []json.RawMessage `json:"findings"`
	}
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Disposition != "not_evaluated" || report.Reason != "provider interrupted before submission" || len(report.Findings) != 0 {
		t.Fatalf("unexpected host report: %s", raw)
	}
	receipt, err := db.GetLogAnalysisReceipt(t.Context(), a.ID, "host-interrupted")
	if err != nil || receipt.State != "incomplete" || receipt.Disposition != "not_evaluated" || len(receipt.Proposals) != 0 {
		t.Fatalf("host receipt: %#v %v", receipt, err)
	}
	if err = db.InterruptLogAnalysis(t.Context(), a.ID, a.Generation, "failed", "second callback"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := db.GetLogAnalysisReport(t.Context(), a.ID)
	if err != nil || string(unchanged) != string(raw) {
		t.Fatalf("host report replaced: %s %v", unchanged, err)
	}
	links, err := db.ListLogAnalysisProposalLinks(t.Context(), a.ID)
	if err != nil || len(links) != 0 {
		t.Fatalf("interruption produced proposals: %#v %v", links, err)
	}
}

func TestLogAnalysisLinksPreserveProducedVersionAfterHumanEdit(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "human-edit")
	candidate := logAnalysisCandidateFixture("practice", "analysis-produced")
	f := LogAnalysisFinalization{AnalysisID: a.ID, Generation: a.Generation, RequestID: "produce", RequestDigest: "original-bytes", Report: json.RawMessage(`{"reason":"supported change"}`), State: "completed", Disposition: "proposals", Candidates: []LogAnalysisCandidate{candidate}}
	original, err := db.FinalizeLogAnalysis(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := db.GetStandardsProposal(t.Context(), candidate.Proposal.ProposalID, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Operations[0].Standard.Body = "Human review changed the guidance"
	proposal.ChangeDigest = "human-changed"
	proposal.ResultDigest = "human-result"
	proposal.Author = "human-reviewer"
	edited, err := db.SaveStandardsProposal(t.Context(), proposal, 1)
	if err != nil || edited.Version != 2 {
		t.Fatalf("human edit: %#v %v", edited, err)
	}
	links, err := db.ListLogAnalysisProposalLinks(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(original.Proposals, links) {
		t.Fatalf("analysis links followed mutable head: %#v %v", links, err)
	}
	produced, err := db.GetStandardsProposal(t.Context(), links[0].ProposalID, links[0].Version)
	if err != nil || produced.Version != 1 || produced.ChangeDigest != candidate.Proposal.ChangeDigest || produced.Operations[0].Standard.Body == edited.Operations[0].Standard.Body {
		t.Fatalf("analysis-produced version changed: %#v %v", produced, err)
	}
	recovered, err := db.GetLogAnalysisReceipt(t.Context(), a.ID, f.RequestID)
	if err != nil || !reflect.DeepEqual(original, recovered) {
		t.Fatalf("human edit changed original receipt: %#v %v", recovered, err)
	}
}

func TestLogAnalysisFailedFenceRollsBackReportAndCandidates(t *testing.T) {
	db := newTestDB(t)
	a := createLogAnalysisFixture(t, db, "failed-fence")
	// Simulate a transition that lost its conditional write after validation.
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TEMP TRIGGER refuse_analysis_fence BEFORE UPDATE OF state ON log_analyses BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	f := LogAnalysisFinalization{AnalysisID: a.ID, Generation: a.Generation, RequestID: "fenced", RequestDigest: "fenced-bytes", Report: json.RawMessage(`{"reason":"supported change"}`), State: "completed", Disposition: "proposals", Candidates: []LogAnalysisCandidate{logAnalysisCandidateFixture("practice", "rolled-back")}}
	if _, err := db.FinalizeLogAnalysis(t.Context(), f); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("missing fence committed report: %v", err)
	}
	if _, err := db.GetStandardsProposal(t.Context(), "rolled-back", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing fence leaked proposal: %v", err)
	}
	if _, err := db.GetLogAnalysisReport(t.Context(), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing fence leaked report: %v", err)
	}
	if err := db.InterruptLogAnalysis(t.Context(), a.ID, a.Generation, "incomplete", "interrupted"); !errors.Is(err, ErrLogAnalysisConflict) {
		t.Fatalf("missing interruption fence committed report: %v", err)
	}
	if _, err := db.GetLogAnalysisReport(t.Context(), a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing interruption fence leaked report: %v", err)
	}
}
