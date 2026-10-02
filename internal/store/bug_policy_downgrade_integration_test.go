package store

// Integration coverage for story-002 (issue #426): a policy-class refusal is
// downgraded to a recorded fact instead of stopping the workflow, that fact
// is visible to the delivery review's own digest computation, a fact
// recorded after a review's digest was computed makes a fresh computation
// disagree with it (the staleness the real invalidate-and-re-present path
// relies on), a downgraded policy notice never spends repair or analysis
// budget, and the Bug graph's DOT source is untouched by this feature.

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// TestBugPolicyDowngrade_RecordsFactWithoutSpendingBudget drives a real
// analyst pass whose proposed remedy boundaries repeat an allowed path — a
// policy-class defect [buganalysis.BugRepairBoundaries.Validate] reports as a
// [buganalysis.Notice] rather than a hard error. [DB.ApplyBugAnalysis] records
// that notice as a real bug_events fact in the very same transaction that
// accepts the analysis, instead of refusing the analyst's revision.
func TestBugPolicyDowngrade_RecordsFactWithoutSpendingBudget(t *testing.T) {
	t.Parallel()
	f := newBugFixture(t, 4002)
	ctx := f.t.Context()

	instance := f.newInstance("record")
	f.ensureLifecycle(instance)

	beforeRevision := f.revision()
	var beforeUsed int
	if err := f.db.sql.QueryRowContext(ctx, `SELECT analysis_used FROM bug_tasks WHERE session_id=?`, f.session.ID).Scan(&beforeUsed); err != nil {
		t.Fatalf("read analysis_used: %v", err)
	}

	approved := f.buildApprovedAnalysisWith(instance, func(a *buganalysis.BugAnalysis) {
		a.Boundaries = &buganalysis.BugRepairBoundaries{AllowedPaths: []string{"internal/example/handler.go", "internal/example/handler.go"}}
	})

	facts, err := f.db.BugFacts(ctx, f.session.ID, "")
	if err != nil {
		t.Fatalf("bug facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("got %d recorded facts, want exactly 1 (the downgraded duplicate-path notice): %+v", len(facts), facts)
	}
	if facts[0].Stage != BugFactStageAnalysis {
		t.Fatalf("fact stage = %q, want %q", facts[0].Stage, BugFactStageAnalysis)
	}
	if facts[0].Reference == "" || facts[0].Text == "" {
		t.Fatalf("fact is missing its reference or text: %+v", facts[0])
	}

	// The downgraded refusal must never have consumed the lifetime analysis or
	// repair budget: the workflow continued past it, it did not spend anything.
	var afterUsed int
	if err := f.db.sql.QueryRowContext(ctx, `SELECT analysis_used FROM bug_tasks WHERE session_id=?`, f.session.ID).Scan(&afterUsed); err != nil {
		t.Fatalf("read analysis_used: %v", err)
	}
	if afterUsed != beforeUsed+1 {
		t.Fatalf("analysis_used = %d, want exactly %d (one spent pass for the one analyst invocation, nothing extra for the downgraded notice)", afterUsed, beforeUsed+1)
	}
	var repairCount int
	if err := f.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM bug_repairs WHERE session_id=?`, f.session.ID).Scan(&repairCount); err != nil {
		t.Fatalf("count bug_repairs: %v", err)
	}
	if repairCount != 0 {
		t.Fatalf("a policy notice consumed repair budget: %d bug_repairs rows exist", repairCount)
	}
	if got := f.revision(); got <= beforeRevision {
		t.Fatalf("revision did not advance past the applied analysis: before=%d after=%d", beforeRevision, got)
	}
	_ = approved

	// The fact is exactly what [BugDeliveryReviewDigest] binds into a delivery
	// review's digest (AC-007): a review computed now, and one computed after a
	// further fact lands, must disagree.
	evidence := BugDeliveryEvidence{}
	pr := BugPullRequest{}
	digestBefore, err := BugDeliveryReviewDigest(evidence, pr, facts, nil)
	if err != nil {
		t.Fatalf("digest before: %v", err)
	}

	if err := f.db.RecordBugFact(ctx, f.session.ID, 0, BugFactStageDelivery, "delivery-notice-1", "a later policy-class observation surfaced during delivery review"); err != nil {
		t.Fatalf("record later fact: %v", err)
	}
	factsAfter, err := f.db.BugFacts(ctx, f.session.ID, "")
	if err != nil {
		t.Fatalf("bug facts after: %v", err)
	}
	if len(factsAfter) != 2 {
		t.Fatalf("got %d facts after recording a second one, want 2: %+v", len(factsAfter), factsAfter)
	}
	digestAfter, err := BugDeliveryReviewDigest(evidence, pr, factsAfter, nil)
	if err != nil {
		t.Fatalf("digest after: %v", err)
	}
	if digestBefore == digestAfter {
		t.Fatal("a fact recorded after the review digest was computed did not change the digest: the review would not go stale")
	}

	// Recomputing with the exact same facts is stable: only new evidence moves
	// the digest, nothing else does.
	digestBeforeAgain, err := BugDeliveryReviewDigest(evidence, pr, facts, nil)
	if err != nil {
		t.Fatalf("digest before, recomputed: %v", err)
	}
	if digestBeforeAgain != digestBefore {
		t.Fatal("BugDeliveryReviewDigest is not stable over the same inputs")
	}

	requireBugDotUnchangedSinceMainline(t)
}
