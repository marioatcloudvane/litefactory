package store

// Integration coverage for story-001 (issue #425)'s crash-during-report-delivery
// retry: the delivery-key dedup on report_deliveries, combined with the
// bug_events row of kind "report" [DB.RetainBugLifecycleReport] journals, is
// what a retry after a mid-delivery crash relies on to post the report
// exactly once. This drives the real store end to end against a real sqlite
// database; nothing here is mocked.

import "testing"

func TestBugReportDelivery_CrashMidDeliveryRetryProducesExactlyOneComment(t *testing.T) {
	t.Parallel()
	f := newBugFixture(t, 4001)
	ctx := f.t.Context()

	const deliveryKey = "bug-notice:1"
	const event = "notice:setup-blocked"

	// The factory journals its intent to deliver a report before attempting the
	// post, exactly as production does ahead of any external effect.
	if err := f.db.RetainBugLifecycleReport(ctx, BugLifecycleReport{
		SessionID: f.session.ID, Event: event, Kind: "pre-dispatch", DeliveryKey: deliveryKey,
	}); err != nil {
		t.Fatalf("retain lifecycle report: %v", err)
	}

	pending, err := f.db.PendingBugLifecycleReports(ctx)
	if err != nil {
		t.Fatalf("pending reports: %v", err)
	}
	if len(pending) != 1 || pending[0].SessionID != f.session.ID || pending[0].Event != event || pending[0].DeliveryKey != deliveryKey {
		t.Fatalf("pending reports = %+v, want exactly one matching the retained journal", pending)
	}

	intent := ReportDelivery{
		Key: deliveryKey, SessionID: f.session.ID, TriggerID: 0, Task: f.session.TaskRef,
		Destination: "issue", TriggerNamespace: "bug-notice", Purpose: "notice",
		BotUserID: 42, Body: "the bug workflow could not start: setup failed", Marker: "<!-- lf:bug-notice -->",
	}

	// First attempt: the intent is retained, then the process crashes before
	// the post is ever confirmed (no MarkReportDeliverySent/Confirm/Complete).
	first, err := f.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		t.Fatalf("first EnsureReportDelivery: %v", err)
	}
	if first.Completed || first.CommentID != 0 {
		t.Fatalf("a freshly retained delivery is already confirmed: %+v", first)
	}

	// The retry: a restarted daemon rediscovers the same pending report and
	// replays the exact same intent. Delivery-key dedup must hand back the
	// original row rather than create a second one.
	retry, err := f.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		t.Fatalf("retry EnsureReportDelivery: %v", err)
	}
	if retry.Key != first.Key || retry.Body != first.Body {
		t.Fatalf("retry returned a different delivery: first=%+v retry=%+v", first, retry)
	}

	var rowCount int
	if err := f.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM report_deliveries WHERE delivery_key=?`, deliveryKey).Scan(&rowCount); err != nil {
		t.Fatalf("count report_deliveries: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("report_deliveries has %d rows for delivery key %q after a retry, want exactly 1", rowCount, deliveryKey)
	}

	// The retry is the one that actually reaches the forge: it claims the post,
	// and the post is then confirmed with the comment id the forge assigned.
	claimed, err := f.db.MarkReportDeliverySent(ctx, deliveryKey)
	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if !claimed {
		t.Fatal("retry did not claim the only POST opportunity")
	}
	// A second, concurrent claim attempt (e.g. two daemons racing the same
	// retry) must not also believe it may post.
	claimedAgain, err := f.db.MarkReportDeliverySent(ctx, deliveryKey)
	if err != nil {
		t.Fatalf("second mark sent: %v", err)
	}
	if claimedAgain {
		t.Fatal("a second concurrent claim also won the POST opportunity")
	}

	const postedCommentID = int64(777)
	if _, err := f.db.ConfirmReportDelivery(ctx, deliveryKey, postedCommentID); err != nil {
		t.Fatalf("confirm delivery: %v", err)
	}
	if _, err := f.db.CompleteReportDelivery(ctx, deliveryKey); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}
	if err := f.db.ConfirmBugLifecycleReport(ctx, f.session.ID, event); err != nil {
		t.Fatalf("confirm bug lifecycle report: %v", err)
	}

	// The lifecycle report is no longer pending, and a later re-observation
	// (another restart) must not treat it as owed work again.
	pending, err = f.db.PendingBugLifecycleReports(ctx)
	if err != nil {
		t.Fatalf("pending reports after confirmation: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending reports after confirmation = %+v, want none", pending)
	}

	// One further replay of the exact same intent, after the delivery already
	// completed (a third crash-and-restart) must still resolve to the single
	// original, now-completed row rather than minting a new one.
	final, err := f.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		t.Fatalf("post-completion EnsureReportDelivery: %v", err)
	}
	if !final.Completed || final.CommentID != postedCommentID {
		t.Fatalf("post-completion replay lost confirmation: %+v", final)
	}
	if err := f.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM report_deliveries WHERE delivery_key=?`, deliveryKey).Scan(&rowCount); err != nil {
		t.Fatalf("count report_deliveries after completion: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("report_deliveries has %d rows after completion and a further replay, want exactly 1: a duplicate comment would have been produced", rowCount)
	}

	// Re-confirming an already-reported lifecycle report is a safe no-op, not
	// a second publication: ConfirmBugLifecycleReport is idempotent.
	if err := f.db.ConfirmBugLifecycleReport(ctx, f.session.ID, event); err != nil {
		t.Fatalf("re-confirm bug lifecycle report: %v", err)
	}
}
