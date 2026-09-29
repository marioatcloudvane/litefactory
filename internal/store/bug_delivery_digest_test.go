package store

import "testing"

// TestBugDeliveryReviewDigest_ChangesWithFactsAndSelfChecks pins the delivery
// review digest's staleness contract: it binds the complete fact list and the
// repair attempt's self-check summary, so either one changing after a review
// was computed changes the digest, sending the review stale through the
// invalidate-and-re-present path instead of silently omitting new evidence.
func TestBugDeliveryReviewDigest_ChangesWithFactsAndSelfChecks(t *testing.T) {
	t.Parallel()
	evidence := BugDeliveryEvidence{Identity: BugIdentity{SessionID: 1, Repository: "acme/api", IssueID: 1, IssueNumber: 1, OwnerID: 501}}
	pr := BugPullRequest{ID: 900, Number: 42, Head: "lf/1-fix", Branch: "lf/1-fix", Target: "main", State: "open"}

	base, err := BugDeliveryReviewDigest(evidence, pr, nil, nil)
	if err != nil {
		t.Fatalf("base digest: %v", err)
	}

	withFact, err := BugDeliveryReviewDigest(evidence, pr, []BugFact{{Stage: BugFactStageAnalysis, Reference: "fact-1", Text: "a late finding"}}, nil)
	if err != nil {
		t.Fatalf("digest with fact: %v", err)
	}
	if withFact == base {
		t.Fatal("recording a fact after the review was computed did not change the digest")
	}

	withSelfCheck, err := BugDeliveryReviewDigest(evidence, pr, nil, []BugSelfCheckSummary{{RunID: "selfcheck:repair-1:1", Ordinal: 1, Phases: []BugSelfCheckPhaseSummary{{Phase: "regression", Outcome: "passed"}}}})
	if err != nil {
		t.Fatalf("digest with self-check: %v", err)
	}
	if withSelfCheck == base {
		t.Fatal("recording a self-check run after the review was computed did not change the digest")
	}

	// Presentation (owner discussion) also participates: appending a
	// clarification changes the digest too.
	withPresentation, err := BugDeliveryReviewDigest(evidence, pr, nil, nil, BugDeliveryClarification{ReviewDigest: base, CommentID: 5, Body: "why not X?"})
	if err != nil {
		t.Fatalf("digest with presentation: %v", err)
	}
	if withPresentation == base {
		t.Fatal("appending owner discussion did not change the digest")
	}

	// Deterministic: recomputing with identical input reproduces the same digest.
	again, err := BugDeliveryReviewDigest(evidence, pr, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again != base {
		t.Fatal("recomputing the same evidence gave a different digest")
	}
}
