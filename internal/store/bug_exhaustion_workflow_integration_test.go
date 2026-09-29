package store

// Integration coverage for story-004 (issue #428): three failed lifetime
// repair attempts exhaust the base budget, the owner grants a fourth attempt
// with instructions, that attempt fails too, a second exhaustion is raised
// whose history includes the grant, and the owner stops it — leaving the
// task owner-stopped, the issue untouched (no branch/PR action ever taken)
// and the Bug graph's DOT source exactly as it was before this feature.

import "testing"

func TestBugExhaustion_ThreeFailuresGrantFourthFailureSecondExhaustionOwnerStops(t *testing.T) {
	t.Parallel()
	f := newBugFixture(t, 4005)
	ctx := f.t.Context()

	instance := f.newInstance("record")
	f.ensureLifecycle(instance)
	approved := f.buildApprovedAnalysis(instance)

	reserveAndFail := func(ordinal int, cause, grantReference string) GraphNodeAttempt {
		t.Helper()
		request := "repair-" + string(rune('0'+ordinal))
		attempt := f.startAttempt(instance, instance.CurrentNode)
		if _, err := f.db.ReserveBugRepair(ctx, BugRepairRequest{
			SessionID: f.session.ID, Request: request, AnalysisRevision: approved.Revision, AnalysisDigest: approved.Digest,
			ApprovalReview: approved.Review, Cause: cause, Evidence: "attempt fell short",
			SourceCommit: bugSourceSHA(byte(ordinal)), SourceTree: bugSourceSHA(byte(ordinal + 16)),
			GraphAttemptID: attempt.ID, GrantReference: grantReference,
		}, f.revision()); err != nil {
			t.Fatalf("reserve repair %d: %v", ordinal, err)
		}
		if err := f.db.StopBugRepair(ctx, f.session.ID, request, "failed", "project checks still fail", true); err != nil {
			t.Fatalf("stop repair %d: %v", ordinal, err)
		}
		return attempt
	}

	reserveAndFail(1, "initial", "")
	f.finishAttemptFailed(mustAttempt(t, f, "repair-1"), false)
	reserveAndFail(2, "test_failure", "")
	f.finishAttemptFailed(mustAttempt(t, f, "repair-2"), false)
	third := reserveAndFail(3, "test_failure", "")
	// The third failure exhausts the base budget: terminalize the parent
	// instance in the same call, the shape [DecideBugExhaustion]'s grant path
	// requires before it may create a successor envelope.
	f.finishAttemptFailed(third, true)

	if _, err := f.db.RecordBugExhaustion(ctx, f.session.ID, third.ID); err != nil {
		t.Fatalf("record first exhaustion: %v", err)
	}

	const grantComment = int64(101)
	const grantInstructions = "focus the fix on the boundary check in handler.go, nowhere else"
	grantOutcome, err := f.db.DecideBugExhaustion(ctx, BugExhaustionDecisionRequest{
		SessionID: f.session.ID, N: 1, Decision: "grant", CommentID: grantComment, AuthorID: f.ownerID,
		Instructions: grantInstructions, ExpectedRevision: f.revision(), At: f.now(),
	})
	if err != nil {
		t.Fatalf("decide first exhaustion (grant): %v", err)
	}
	if grantOutcome.Decision != "grant" || grantOutcome.Successor.ID == 0 {
		t.Fatalf("grant outcome = %+v, want a successor graph instance", grantOutcome)
	}
	successor := grantOutcome.Successor
	if successor.CurrentNode != "attempt" {
		t.Fatalf("successor current node = %q, want %q (the granted attempt's entry)", successor.CurrentNode, "attempt")
	}

	// The granted fourth attempt, reserved against the successor envelope and
	// spending the grant reference.
	const grantReference = "exhaustion:1"
	fourthAttempt := f.startAttempt(successor, successor.CurrentNode)
	if _, err := f.db.ReserveBugRepair(ctx, BugRepairRequest{
		SessionID: f.session.ID, Request: "repair-4", AnalysisRevision: approved.Revision, AnalysisDigest: approved.Digest,
		ApprovalReview: approved.Review, Cause: "test_failure", Evidence: "granted attempt fell short too",
		SourceCommit: bugSourceSHA(20), SourceTree: bugSourceSHA(21), GraphAttemptID: fourthAttempt.ID, GrantReference: grantReference,
	}, f.revision()); err != nil {
		t.Fatalf("reserve granted repair: %v", err)
	}
	if err := f.db.StopBugRepair(ctx, f.session.ID, "repair-4", "failed", "still failing after the grant", true); err != nil {
		t.Fatalf("stop granted repair: %v", err)
	}
	f.finishAttemptFailed(fourthAttempt, true)

	secondN, err := f.db.RecordBugExhaustion(ctx, f.session.ID, fourthAttempt.ID)
	if err != nil {
		t.Fatalf("record second exhaustion: %v", err)
	}
	if secondN != 2 {
		t.Fatalf("second exhaustion number = %d, want 2", secondN)
	}

	// The second exhaustion's history must include the earlier grant.
	history, err := f.db.BugRepairHistory(ctx, f.session.ID)
	if err != nil {
		t.Fatalf("bug repair history: %v", err)
	}
	if len(history.Attempts) != 4 {
		t.Fatalf("got %d repair attempts in history, want 4", len(history.Attempts))
	}
	if history.Attempts[3].GrantReference != grantReference || history.Attempts[3].GrantInstructions != grantInstructions {
		t.Fatalf("fourth attempt history = %+v, want grant reference %q and instructions %q", history.Attempts[3], grantReference, grantInstructions)
	}
	if len(history.Exhaustions) != 2 {
		t.Fatalf("got %d exhaustions in history, want 2", len(history.Exhaustions))
	}
	if history.Exhaustions[0].Decision != "grant" || history.Exhaustions[0].Instructions != grantInstructions {
		t.Fatalf("first exhaustion history = %+v, want the recorded grant", history.Exhaustions[0])
	}
	if history.Exhaustions[1].Decision != "" {
		t.Fatalf("second exhaustion already shows a decision before one was made: %+v", history.Exhaustions[1])
	}

	// The owner stops it.
	stopOutcome, err := f.db.DecideBugExhaustion(ctx, BugExhaustionDecisionRequest{
		SessionID: f.session.ID, N: 2, Decision: "stop", CommentID: 102, AuthorID: f.ownerID,
		ExpectedRevision: f.revision(), At: f.now(),
	})
	if err != nil {
		t.Fatalf("decide second exhaustion (stop): %v", err)
	}
	if stopOutcome.Decision != "stop" || stopOutcome.Successor.ID != 0 {
		t.Fatalf("stop outcome = %+v, want no successor", stopOutcome)
	}

	lifecycle, err := f.db.GetBugLifecycle(ctx, f.session.ID)
	if err != nil {
		t.Fatalf("get bug lifecycle: %v", err)
	}
	if lifecycle.Disposition != "owner-stopped" {
		t.Fatalf("disposition = %q, want owner-stopped", lifecycle.Disposition)
	}
	if lifecycle.Reference != "exhaustion:2" {
		t.Fatalf("terminal reference = %q, want it to name the second exhaustion", lifecycle.Reference)
	}

	// No branch/PR action was ever taken: no bug_delivery row exists for this
	// task at all.
	var deliveryRows int
	if err := f.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM bug_delivery WHERE session_id=?`, f.session.ID).Scan(&deliveryRows); err != nil {
		t.Fatalf("count bug_delivery: %v", err)
	}
	if deliveryRows != 0 {
		t.Fatalf("bug_delivery has %d rows, want 0: no branch or PR action should have been taken", deliveryRows)
	}

	// The final history read confirms the second exhaustion now shows the
	// owner's stop decision too.
	history, err = f.db.BugRepairHistory(ctx, f.session.ID)
	if err != nil {
		t.Fatalf("bug repair history after stop: %v", err)
	}
	if history.Exhaustions[1].Decision != "stop" {
		t.Fatalf("second exhaustion decision = %q, want stop", history.Exhaustions[1].Decision)
	}

	requireBugDotUnchangedSinceMainline(t)
}

// mustAttempt reads the graph attempt id a reserved repair is bound to, so
// the test can finish the same attempt [ReserveBugRepair] was given.
func mustAttempt(t *testing.T, f *bugFixture, request string) GraphNodeAttempt {
	t.Helper()
	var attemptID int64
	if err := f.db.sql.QueryRowContext(f.t.Context(), `SELECT graph_attempt_id FROM bug_repairs WHERE session_id=? AND request=?`, f.session.ID, request).Scan(&attemptID); err != nil {
		t.Fatalf("read repair graph attempt: %v", err)
	}
	attempt, err := f.db.GetGraphNodeAttempt(f.t.Context(), attemptID)
	if err != nil {
		t.Fatalf("get graph node attempt: %v", err)
	}
	return attempt
}
