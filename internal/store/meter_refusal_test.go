package store

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestMeterRefusalDurableSeparateFallback(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "refusal.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 261)
	run := testRun(t, db, session.ID, 261)
	refused, err := db.StartMeterAttempt(ctx, run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	var started string
	if err := db.sql.QueryRowContext(ctx, `SELECT started_at FROM meter_attempts WHERE id=?`, refused.ID).Scan(&started); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateMeterCoverage(ctx, refused.ID, "partial", "", "stream-json", []string{"source_unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttemptWithRefusal(ctx, refused.ID, "failure", "unknown", "", "stream-json", []string{"resume_fallback"}, true); err != nil {
		t.Fatal(err)
	}
	fallback, err := db.StartMeterAttempt(ctx, run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(ctx, fallback.ID, "success", "partial", "2.1.267", "stream-json", []string{"descendant_coverage_unknown"}); err != nil {
		t.Fatal(err)
	}
	var finished string
	if err := db.sql.QueryRowContext(ctx, `SELECT finished_at FROM meter_attempts WHERE id=?`, refused.ID).Scan(&finished); err != nil {
		t.Fatal(err)
	}
	if finished == "" {
		t.Fatal("refused attempt was not finalized")
	}
	if err := db.FinishMeterAttempt(ctx, refused.ID, "success", "complete", "invented", "other", nil); err == nil {
		t.Fatal("finalized refusal overwritten")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeterAttempt(ctx, refused.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := refused
	want.Outcome = "failure"
	want.ProviderReportedZeroCostRefusal = true
	if got != want {
		t.Fatalf("refused attempt changed: got %+v want %+v", got, want)
	}
	gotFallback, err := db.GetMeterAttempt(ctx, fallback.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFallback.ID == got.ID || gotFallback.RunID != got.RunID || gotFallback.Ordinal != got.Ordinal+1 || gotFallback.Outcome != "success" || gotFallback.ProviderReportedZeroCostRefusal {
		t.Fatalf("fallback relationship lost: %+v %+v", got, gotFallback)
	}
	snap, err := db.NewMeterSnapshot(ctx, MeterFilter{RunID: run.ID}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snap.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snap.Next(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want two independent coverage rows: %+v", rows)
	}
	for _, row := range rows {
		if row.ActualModel != nil || row.EstimatedUSD != nil || row.PricedSubtotalUSD != nil || row.PricingStatus != "unknown" || row.RowKind != "coverage" || row.Tokens != (MeterTokens{}) {
			t.Errorf("unknown evidence invented: %+v", row)
		}
		if row.AttemptID == refused.ID {
			if !row.ProviderReportedZeroCostRefusal || row.AttemptStartedAt != started || row.AttemptFinishedAt != finished || row.CollectionStatus == "complete" || !slices.Contains(row.Diagnostics, "source_unavailable") || !slices.Contains(row.Diagnostics, "resume_fallback") {
				t.Errorf("refusal facts or diagnostics changed: %+v", row)
			}
		} else if row.ProviderReportedZeroCostRefusal || !slices.Contains(row.Diagnostics, "descendant_coverage_unknown") {
			t.Errorf("fallback diagnostics lost: %+v", row)
		}
	}
}

func TestMeterRefusalMigrationPreservesUnknownHistory(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "meter_refusal"))
	session := testSession(t, db, 261)
	run := historicalRun(t, db, session.ID, 262, ModeSpike)
	attempt, err := db.StartMeterAttempt(ctx, run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	const start = "2026-09-09T18:00:00.000Z"
	const end = "2026-09-09T18:00:01.000Z"
	if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET started_at=?,finished_at=?,outcome='failure',collection_status='unknown',diagnostic_codes_json='["legacy_unknown"]' WHERE id=?`, start, end, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeterAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt.Outcome = "failure"
	if got != attempt {
		t.Fatalf("migration changed attempt: %+v want %+v", got, attempt)
	}
	snap, err := db.NewMeterSnapshot(ctx, MeterFilter{RunID: run.ID}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snap.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snap.Next(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	row := rows[0]
	if row.ProviderReportedZeroCostRefusal || row.AttemptStartedAt != start || row.AttemptFinishedAt != end || row.ActualModel != nil || row.PricingStatus != "unknown" || row.EstimatedUSD != nil || row.PricedSubtotalUSD != nil || !reflect.DeepEqual(row.Diagnostics, []string{"legacy_unknown"}) {
		t.Fatalf("legacy unknown history altered: %+v", row)
	}
}
