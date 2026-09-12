package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReportRecoveryMigrationRestartAndResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, 30)
	session := testSession(t, db, 123)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 9, 10, 19, 33, 47, 123000000, time.UTC)
	initial := ReportRecovery{Key: "source:123:55", SessionID: session.ID, TriggerID: 55, Task: session.TaskRef, Attempts: 0, NextAt: due, State: "pending", Reason: "intent storage unavailable"}
	if err := db.PutReportRecovery(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetReportRecovery(t.Context(), initial.Key)
	if err != nil || !reflect.DeepEqual(got, initial) {
		t.Fatalf("initial=%+v %v", got, err)
	}
	escalated := initial
	escalated.Attempts = 3
	escalated.NextAt = due.Add(time.Hour)
	escalated.State = "uncertain"
	escalated.Reason = "lookup unavailable [REDACTED]"
	escalated.Escalated = true
	escalated.DeliveryKey = "future-intent-key"
	if err := db.PutReportRecovery(t.Context(), escalated); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	*db = *reopened
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetReportRecovery(t.Context(), initial.Key)
	if err != nil || !reflect.DeepEqual(got, escalated) {
		t.Fatalf("restart=%+v want=%+v err=%v", got, escalated, err)
	}
	all, err := db.ListReportRecoveries(t.Context())
	if err != nil || len(all) != 1 || !reflect.DeepEqual(all[0], escalated) {
		t.Fatalf("restored=%+v %v", all, err)
	}
	if err := db.DeleteReportRecovery(t.Context(), initial.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetReportRecovery(t.Context(), initial.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolved row retained: %v", err)
	}
	if err := db.DeleteReportRecovery(t.Context(), initial.Key); err != nil {
		t.Fatal(err)
	}
	all, err = db.ListReportRecoveries(t.Context())
	if err != nil || len(all) != 0 {
		t.Fatalf("resolved backlog=%+v %v", all, err)
	}
}

func TestReportRecoverySourceIsolationAndFailedWrites(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 123)
	initial := ReportRecovery{Key: "source", SessionID: session.ID, TriggerID: 55, NextAt: time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC), State: "uncertain", Reason: "safe diagnostic"}
	if err := db.PutReportRecovery(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	collision := initial
	collision.TriggerID++
	if err := db.PutReportRecovery(t.Context(), collision); err == nil {
		t.Fatal("key changed original source")
	}
	if _, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER recovery_fault BEFORE UPDATE ON report_recoveries BEGIN SELECT RAISE(ABORT,'recovery save failure'); END`); err != nil {
		t.Fatal(err)
	}
	changed := initial
	changed.Attempts = 3
	changed.Escalated = true
	changed.NextAt = initial.NextAt.Add(time.Hour)
	if err := db.PutReportRecovery(t.Context(), changed); err == nil {
		t.Fatal("fault did not prevent save")
	}
	current, err := db.GetReportRecovery(t.Context(), initial.Key)
	if err != nil || !reflect.DeepEqual(current, initial) {
		t.Fatalf("failed write changed metadata=%+v %v", current, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER recovery_fault`); err != nil {
		t.Fatal(err)
	}
	if err := db.PutReportRecovery(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.DeleteReportRecovery(cancelled, initial.Key); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delete=%v", err)
	}
	current, err = db.GetReportRecovery(t.Context(), initial.Key)
	if err != nil || !reflect.DeepEqual(current, changed) {
		t.Fatalf("failed delete lost debt=%+v %v", current, err)
	}
}

func TestReportRecoveryRetainsStoppedRunAndSourceOnlyDebt(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 123)
	run := testRun(t, db, session.ID, 1)
	due := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	rows := []ReportRecovery{
		{Key: "stopped:123:1", SessionID: session.ID, TriggerID: 1, Task: session.TaskRef, RunID: run.ID, NextAt: due, State: "stopped", Reason: "outcome persistence pending"},
		{Key: "source:123:2", SessionID: session.ID, TriggerID: 2, NextAt: due, State: "pending", Reason: "initial intent unavailable"},
	}
	for _, r := range rows {
		if err := db.PutReportRecovery(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	all, err := db.ListReportRecoveries(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("debts=%+v %v", all, err)
	}
	for _, r := range rows {
		got, err := db.GetReportRecovery(t.Context(), r.Key)
		if err != nil || !reflect.DeepEqual(got, r) {
			t.Fatalf("debt=%+v %v", got, err)
		}
	}
	deliveries, err := db.ListPendingReportDeliveries(t.Context())
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("scheduling synthesized delivery intent=%+v %v", deliveries, err)
	}
	stored, err := db.GetRun(t.Context(), run.ID)
	if err != nil || stored.Status != RunRunning {
		t.Fatalf("scheduling changed execution=%+v %v", stored, err)
	}
	wm, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || wm.LastCommentID != 0 {
		t.Fatalf("scheduling consumed source=%+v %v", wm, err)
	}
}
