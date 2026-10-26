package store

import (
	"path/filepath"
	"testing"
)

func reportIntent(session Session, run Run) ReportDelivery {
	return ReportDelivery{Key: "v1-key", SessionID: session.ID, TriggerID: run.TriggerCommentID, RunID: run.ID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "run", BotUserID: 9, Body: "redacted body marker", Marker: "marker"}
}

func TestReportDeliveryJournalTransitionsAndAtomicCompletion(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 55)
	intent := reportIntent(session, run)
	if _, err := db.EnsureReportDelivery(t.Context(), intent); err == nil {
		t.Fatal("nonterminal intent accepted")
	}
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunFailed, Error: "finished"}); err != nil {
		t.Fatal(err)
	}
	saved, err := db.EnsureReportDelivery(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	changed := intent
	changed.Body = "changed output"
	changed.Marker = "changed marker"
	changed.BotUserID = 99
	same, err := db.EnsureReportDelivery(t.Context(), changed)
	if err != nil || same.Body != saved.Body || same.Marker != saved.Marker || same.BotUserID != 9 {
		t.Fatalf("retained intent drifted=%+v %v", same, err)
	}
	changed.Destination = "github:acme/api#999"
	if _, err := db.EnsureReportDelivery(t.Context(), changed); err == nil {
		t.Fatal("identity collision accepted")
	}
	if _, err := db.CompleteReportDelivery(t.Context(), saved.Key); err == nil {
		t.Fatal("unconfirmed delivery completed")
	}
	for idx := range 2 {
		claimed, err := db.MarkReportDeliverySent(t.Context(), saved.Key)
		if err != nil || claimed != (idx == 0) {
			t.Fatalf("dispatch %d = %v %v", idx, claimed, err)
		}
	}
	if _, err := db.ConfirmReportDelivery(t.Context(), saved.Key, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(t.Context(), saved.Key, 501); err == nil {
		t.Fatal("trusted receipt replaced")
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_complete BEFORE UPDATE OF completed ON report_deliveries WHEN NEW.completed=1 BEGIN SELECT RAISE(ABORT,'completion fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), saved.Key); err == nil {
		t.Fatal("fault not exercised")
	}
	current, err := db.GetRun(t.Context(), run.ID)
	if err != nil || current.Status != RunFailed || current.ResultCommentID != 0 {
		t.Fatalf("run escaped transaction=%+v %v", current, err)
	}
	wm, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || wm.LastCommentID != 0 {
		t.Fatalf("source escaped transaction=%+v %v", wm, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER reject_complete`); err != nil {
		t.Fatal(err)
	}
	done, err := db.CompleteReportDelivery(t.Context(), saved.Key)
	if err != nil || !done.Completed {
		t.Fatalf("complete=%+v %v", done, err)
	}
	current, err = db.GetRun(t.Context(), run.ID)
	if err != nil || current.Status != RunReported || current.ResultCommentID != 500 {
		t.Fatalf("run=%+v %v", current, err)
	}
	wm, _, err = db.GetWatermark(t.Context(), session.ID)
	if err != nil || wm.LastCommentID != 55 {
		t.Fatalf("source=%+v %v", wm, err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), saved.Key); err != nil {
		t.Fatal(err)
	}
}

func TestReportDeliveryMigrationPreservesLegacyProvenance(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "before.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "report_deliveries"))
	session := testSession(t, db, 123)
	old := historicalRun(t, db, session.ID, 1, ModeSpike)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	fresh := testRun(t, db, session.ID, 2)
	for _, tc := range []struct {
		id   int64
		want bool
	}{{old.ID, true}, {fresh.ID, false}} {
		legacy, err := db.IsLegacyReportRun(t.Context(), tc.id)
		if err != nil || legacy != tc.want {
			t.Fatalf("legacy %d=%v %v", tc.id, legacy, err)
		}
	}
	if _, err := db.FinishRun(t.Context(), old.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, old)
	intent.Legacy = true
	saved, err := db.EnsureReportDelivery(t.Context(), intent)
	if err != nil || !saved.PossiblySent {
		t.Fatalf("legacy intent=%+v %v", saved, err)
	}
	claimed, err := db.MarkReportDeliverySent(t.Context(), saved.Key)
	if err != nil || claimed {
		t.Fatalf("legacy repost eligible=%v %v", claimed, err)
	}
}

func TestReportDeliveryRunlessSyntheticAndRefinementAccounting(t *testing.T) {
	for _, kind := range []string{"comment", "schedule", "automatic", "refinement"} {
		t.Run(kind, func(t *testing.T) {
			db, session, _ := approvedFixture(t)
			trigger := int64(555)
			switch kind {
			case "schedule":
				trigger = scheduleTriggerBase + 1
			case "automatic":
				trigger = automaticTriggerBase + 1
			case "refinement":
				if err := db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: trigger, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "change"}); err != nil {
					t.Fatal(err)
				}
			}
			d := reportIntent(session, Run{TriggerCommentID: trigger})
			d.Purpose = "reply"
			if _, err := db.EnsureReportDelivery(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ConfirmReportDelivery(t.Context(), d.Key, 99); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CompleteReportDelivery(t.Context(), d.Key); err != nil {
				t.Fatal(err)
			}
			wm, _, err := db.GetWatermark(t.Context(), session.ID)
			want := int64(0)
			if kind == "comment" {
				want = trigger
			}
			if err != nil || wm.LastCommentID != want {
				t.Fatalf("watermark=%+v %v", wm, err)
			}
			if kind == "refinement" {
				r, err := db.GetStoryRefinement(t.Context(), session.ID, trigger)
				if err != nil || !r.Consumed {
					t.Fatalf("child=%+v %v", r, err)
				}
			}
		})
	}
}

func TestBodyRefusalCompletionIsAtomic(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 124)
	if _, _, err := db.BootstrapTicketWatermark(t.Context(), session.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetOrCreateModeSelection(t.Context(), ModeSelection{SessionID: session.ID, Mode: "research", Label: "Mode: Research"}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, Run{})
	intent.TriggerNamespace, intent.Purpose = "issue-body", "reply:missing_mode_mapping"
	if _, err := db.EnsureReportDelivery(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	assertPending := func(want bool) {
		t.Helper()
		pending, err := db.TicketBodyPending(t.Context(), session.ID)
		if err != nil || pending != want {
			t.Fatalf("pending=%v want %v: %v", pending, want, err)
		}
	}
	if _, err := db.CompleteReportDelivery(t.Context(), intent.Key); err == nil {
		t.Fatal("unconfirmed completion")
	}
	assertPending(true)
	if _, err := db.ConfirmReportDelivery(t.Context(), intent.Key, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_body_completion BEFORE UPDATE OF completed ON report_deliveries BEGIN SELECT RAISE(ABORT,'completion fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), intent.Key); err == nil {
		t.Fatal("fault not exercised")
	}
	assertPending(true)
	selection, err := db.GetModeSelection(t.Context(), session.ID, 0)
	if err != nil || selection.CleanupPending {
		t.Fatalf("cleanup escaped transaction: %+v %v", selection, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER reject_body_completion`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), intent.Key); err != nil {
		t.Fatal(err)
	}
	assertPending(false)
	if _, found, err := db.PendingModeCleanup(t.Context(), session.ID); err != nil || !found {
		t.Fatalf("missing cleanup: %v %v", found, err)
	}
}

func TestReportDeliveryFirstConfirmationMetadata(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 55)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	intent := reportIntent(session, run)
	if _, err := db.EnsureReportDelivery(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	first, err := db.ConfirmReportDelivery(t.Context(), intent.Key, 500, "https://github.com/acme/api/issues/123#issuecomment-500")
	if err != nil || first.ConfirmedAt.IsZero() || first.ConfirmedURL == "" {
		t.Fatalf("first confirmation: %+v %v", first, err)
	}
	retry, err := db.ConfirmReportDelivery(t.Context(), intent.Key, 500, "https://example.com/replacement")
	if err != nil || retry.ConfirmedAt.IsZero() || !retry.ConfirmedAt.Equal(first.ConfirmedAt) || retry.ConfirmedURL != first.ConfirmedURL {
		t.Fatalf("confirmation drift: %+v %v", retry, err)
	}
	if _, err := db.ConfirmReportDelivery(t.Context(), intent.Key, 501); err == nil {
		t.Fatal("replacement receipt accepted")
	}
	// A pre-migration receipt remains undated even when recovery observes it again.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE report_deliveries SET confirmed_at=NULL,confirmed_url=NULL WHERE delivery_key=?`, intent.Key); err != nil {
		t.Fatal(err)
	}
	historical, err := db.ConfirmReportDelivery(t.Context(), intent.Key, 500, first.ConfirmedURL)
	if err != nil || !historical.ConfirmedAt.IsZero() || historical.ConfirmedURL != first.ConfirmedURL {
		t.Fatalf("historical receipt was dated: %+v %v", historical, err)
	}
}
