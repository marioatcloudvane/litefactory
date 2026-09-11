package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func meterFixture(t *testing.T) (*DB, MeterAttempt) {
	t.Helper()
	db := newTestDB(t)
	s := testSession(t, db, 73)
	r := testRun(t, db, s.ID, 123)
	a, err := db.StartMeterAttempt(t.Context(), r.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(t.Context(), MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	return db, a
}
func meterObservation(a MeterAttempt, key string) MeterObservation {
	model := "test-model"
	return MeterObservation{AttemptID: a.ID, SourceID: "s", EventKey: key, InvocationID: "root", Semantics: "incremental/exclusive", ModelKey: `["test-model"]`, ActualModel: &model, ModelProvenance: "source", Digest: strings.Repeat("a", 64), Status: "complete"}
}
func addMeterFixture(ctx context.Context, db *DB, o MeterObservation, n int64) error {
	return db.ApplyMeterObservation(ctx, o, func(state MeterLedgerState) (MeterMutation, error) {
		if state.AcceptedDigest != "" {
			return MeterMutation{Duplicate: true}, nil
		}
		if state.Measurement != nil && state.Measurement.Output != nil {
			n += *state.Measurement.Output
		}
		return MeterMutation{Measurement: &MeterTokens{Output: &n}}, nil
	})
}
func TestMeterReplayAndSnapshot(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	o := meterObservation(a, "one")
	if err := addMeterFixture(ctx, db, o, 7); err != nil {
		t.Fatal(err)
	}
	if err := addMeterFixture(ctx, db, o, 7); err != nil {
		t.Fatal(err)
	}
	snap, err := db.NewMeterSnapshot(ctx, MeterFilter{}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snap.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	o.EventKey = "two"
	if err := addMeterFixture(ctx, db, o, 3); err != nil {
		t.Fatal(err)
	}
	rows, err := snap.Next(ctx, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || *rows[0].Tokens.Output != 7 {
		t.Fatalf("snapshot changed: %+v", rows)
	}
	if rows[0].Tokens.InputUncached != nil {
		t.Fatal("unknown fabricated")
	}
	var n int64
	if err := db.sql.QueryRowContext(ctx, `SELECT output_tokens FROM meter_measurements WHERE attempt_id=?`, a.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("want10 got%d", n)
	}
}
func TestMeterAtomicRollbackAndConstraints(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	o := meterObservation(a, "one")
	boom := errors.New("reject")
	if err := db.ApplyMeterObservation(ctx, o, func(MeterLedgerState) (MeterMutation, error) { return MeterMutation{}, boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM meter_observations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rollback retained evidence")
	}
	o.InvocationID = "missing"
	if err := addMeterFixture(ctx, db, o, 1); err == nil {
		t.Fatal("missing invocation accepted")
	}
	o.InvocationID = "root"
	if err := addMeterFixture(ctx, db, o, -1); err == nil {
		t.Fatal("negative counter accepted")
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "child", ParentID: "root", SourceID: "s", SourceInvocationID: "child", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "root", ParentID: "child", SourceID: "s", SourceInvocationID: "root"}); err == nil {
		t.Fatal("cycle accepted")
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "root2", SourceID: "s", SourceInvocationID: "root2"}); err == nil {
		t.Fatal("second root accepted")
	}
}
func TestMeterAttemptAndFinalization(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	b, err := db.StartMeterAttempt(ctx, a.RunID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || b.Ordinal != 2 || a.PriceRevisionID != 1 {
		t.Fatalf("attempts %+v %+v", a, b)
	}
	if err := db.FinishMeterAttempt(ctx, a.ID, "failure", "partial", "v1", "test", []string{"missing_evidence"}); err != nil {
		t.Fatal(err)
	}
	if err := addMeterFixture(ctx, db, meterObservation(a, "late"), 1); err == nil {
		t.Fatal("late measurement rewrote finalized history")
	}
	if _, err := db.ReconcileOrphans(ctx, "new-daemon"); err != nil {
		t.Fatal(err)
	}
	var outcome string
	if err := db.sql.QueryRowContext(ctx, `SELECT outcome FROM meter_attempts WHERE id=?`, b.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "abandoned" {
		t.Fatal(outcome)
	}
}
func TestMeterSnapshotLimitsAndCancellation(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	if err := addMeterFixture(ctx, db, meterObservation(a, "one"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewMeterSnapshot(ctx, MeterFilter{}, MeterSnapshotOptions{RowBytes: func(MeterRow) (int, error) { return 256 << 20, nil }, HeaderBytes: 1}); err == nil {
		t.Fatal("oversize accepted")
	}
	snap, err := db.NewMeterSnapshot(ctx, MeterFilter{}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := snap.Close(cancelCtx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_temp_master WHERE name LIKE 'meter_snapshot_%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("temp leak %d", count)
	}
}
func TestMeterSnapshotExactTimeBounds(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	start := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET started_at=? WHERE id=?`, FormatTime(start), a.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		from, to *time.Time
		want     int
	}{{&start, nil, 1}, {nil, &start, 0}, {newTime(start.Add(time.Nanosecond)), nil, 0}} {
		snap, err := db.NewMeterSnapshot(ctx, MeterFilter{TimeFrom: tc.from, TimeTo: tc.to}, MeterSnapshotOptions{})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := snap.Next(ctx, 256)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != tc.want {
			t.Fatalf("rows %d want %d", len(rows), tc.want)
		}
		if err := snap.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
func newTime(t time.Time) *time.Time { return &t }

func TestMeterOutOfOrderAndCrossAttempt(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	nodes := []MeterInvocation{{AttemptID: a.ID, ID: "grandchild", ParentID: "child", SourceID: "s", SourceInvocationID: "grandchild"}, {AttemptID: a.ID, ID: "child", ParentID: "root", SourceID: "s", SourceInvocationID: "child"}}
	if err := db.PutMeterInvocations(ctx, nodes); err != nil {
		t.Fatal(err)
	}
	b, err := db.StartMeterAttempt(ctx, a.RunID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: b.ID, ID: "child", ParentID: "root", SourceID: "s", SourceInvocationID: "child"}); err == nil {
		t.Fatal("cross-attempt parent accepted")
	}
	nodes[0].SourceID = "changed"
	if err := db.PutMeterInvocation(ctx, nodes[0]); err == nil {
		t.Fatal("source reassignment accepted")
	}
}
func TestMeterCheckpointAndRollback(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	o := meterObservation(a, "one")
	o.Semantics = "cumulative/exclusive"
	o.CounterKey = "scope"
	o.CounterEpoch = "epoch"
	n := int64(10)
	if err := db.SeedMeterCheckpoint(ctx, o, MeterTokens{Output: &n}); err != nil {
		t.Fatal(err)
	}
	n = 20
	if err := db.SeedMeterCheckpoint(ctx, o, MeterTokens{Output: &n}); err == nil {
		t.Fatal("baseline replaced")
	}
	err := db.ApplyMeterObservation(ctx, o, func(s MeterLedgerState) (MeterMutation, error) {
		if s.Checkpoint == nil || *s.Checkpoint.Output != 10 {
			t.Fatal("baseline missing")
		}
		negative := int64(-1)
		return MeterMutation{Checkpoint: &MeterTokens{Output: &n}, Measurement: &MeterTokens{Output: &negative}}, nil
	})
	if err == nil {
		t.Fatal("negative aggregate accepted")
	}
	var last string
	if err := db.sql.QueryRowContext(ctx, `SELECT last_dimensions_json FROM meter_checkpoints WHERE attempt_id=?`, a.ID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(last, `"output":10`) {
		t.Fatalf("rollback lost baseline: %s", last)
	}
}
func TestMeterConcurrentAttempts(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, err := db.StartMeterAttempt(ctx, a.RunID, "codex"); results <- err }()
	}
	for i := 0; i < 4; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count, maximum int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*),MAX(ordinal) FROM meter_attempts WHERE run_id=?`, a.RunID).Scan(&count, &maximum); err != nil {
		t.Fatal(err)
	}
	if count != 5 || maximum != 5 {
		t.Fatal(count, maximum)
	}
}
func TestMeterQueryIndexes(t *testing.T) {
	db, _ := meterFixture(t)
	ctx := t.Context()
	rows, err := db.sql.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id FROM meter_attempts WHERE coding_agent=? AND started_at>=? ORDER BY started_at,id`, "codex", "2026")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plans, " "), "meter_attempt_agent_time") {
		t.Fatal(plans)
	}
}

func TestMeterDiagnosticUnionAndProvenance(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	if err := db.UpdateMeterCoverage(ctx, a.ID, "partial", "v", "s", []string{"first_gap"}); err != nil {
		t.Fatal(err)
	}
	o := meterObservation(a, "one")
	if err := addMeterFixture(ctx, db, o, 1); err != nil {
		t.Fatal(err)
	}
	o.EventKey = "two"
	o.ModelProvenance = "alternate"
	if err := addMeterFixture(ctx, db, o, 1); err != nil {
		t.Fatal(err)
	}
	o.EventKey = "three"
	if err := db.ApplyMeterObservation(ctx, o, func(MeterLedgerState) (MeterMutation, error) { return MeterMutation{Rejection: "invalid_counter"}, nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(ctx, a.ID, "failure", "complete", "v", "s", []string{"final_gap"}); err != nil {
		t.Fatal(err)
	}
	var codes, provenance, status string
	if err := db.sql.QueryRowContext(ctx, `SELECT diagnostic_codes_json,collection_status FROM meter_attempts WHERE id=?`, a.ID).Scan(&codes, &status); err != nil {
		t.Fatal(err)
	}
	if codes != `["final_gap","first_gap","invalid_counter"]` || status != "partial" {
		t.Fatal(codes, status)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT model_provenance FROM meter_measurements WHERE attempt_id=?`, a.ID).Scan(&provenance); err != nil {
		t.Fatal(err)
	}
	if provenance != `["alternate","source"]` {
		t.Fatal(provenance)
	}
}
func TestMeterPopulatedMigrationAndReopenReplay(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "meter.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	applyMigrationsThrough(t, db, 18)
	session := testSession(t, db, 73)
	// Seed the actual version-18 schema without calling a latest-schema run
	// accessor: newer additive run columns must not be required by this fixture.
	segment, err := db.GetOrCreateSegment(ctx, session.ID, FirstSegmentOrdinal, "migration-session", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := db.sql.ExecContext(ctx, `INSERT INTO runs
		(session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status, started_at, log_path)
		VALUES (?, ?, 1, 'research', 'claude', 123, 'running', ?, 'migration.log')`, session.ID, segment.ID, FormatTime(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	runID, err := inserted.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: runID}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(ctx, MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root"}); err != nil {
		t.Fatal(err)
	}
	o := meterObservation(a, "one")
	if err := addMeterFixture(ctx, db, o, 7); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := addMeterFixture(ctx, db, o, 7); err != nil {
		t.Fatal(err)
	}
	var count, n int64
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*),SUM(output_tokens) FROM meter_measurements`).Scan(&count, &n); err != nil {
		t.Fatal(err)
	}
	if count != 1 || n != 7 {
		t.Fatal(count, n)
	}
	persisted, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SessionID != session.ID {
		t.Fatal("migration lost populated run")
	}
}
func TestMeterJoinedQueryWithRealisticCardinality(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	if _, err := db.sql.ExecContext(ctx, `WITH RECURSIVE counter(n) AS (SELECT 2 UNION ALL SELECT n+1 FROM counter WHERE n<3000) INSERT INTO meter_attempts(id,run_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) SELECT 'fixture-'||n,?,n,CASE WHEN n%2=0 THEN 'codex' ELSE 'claude' END,'2026-09-09T00:00:00.000Z','running','unknown','[]',1 FROM counter`, a.RunID); err != nil {
		t.Fatal(err)
	}
	rows, err := db.sql.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT a.id FROM meter_attempts a JOIN runs r ON r.id=a.run_id JOIN sessions s ON s.id=r.session_id JOIN repos p ON p.id=s.repo_id WHERE s.repo_id=(SELECT repo_id FROM sessions LIMIT 1) AND a.coding_agent='codex' AND a.started_at>='2026-09-01T00:00:00.000Z' ORDER BY a.started_at,a.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "meter_attempt_agent_time") {
		t.Fatal(plan)
	}
}

func TestMeterFilteredIdentityCollisionAndCancelledCollection(t *testing.T) {
	db, a := meterFixture(t)
	ctx := t.Context()
	for n, identity := range [][2]string{{"a", "b/c"}, {"a/b", "c"}} {
		if _, err := db.sql.ExecContext(ctx, `INSERT INTO meter_attempts(id,run_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,?,'codex','2026-09-09T00:00:00.000Z','running','partial','[]',1)`, identity[0], a.RunID, n+2); err != nil {
			t.Fatal(err)
		}
		v := MeterInvocation{AttemptID: identity[0], ID: identity[1], SourceID: "s", SourceInvocationID: identity[1], Status: "partial"}
		if err := db.PutMeterInvocation(ctx, v); err != nil {
			t.Fatal(err)
		}
		o := meterObservation(MeterAttempt{ID: identity[0]}, "one")
		o.InvocationID = identity[1]
		if err := addMeterFixture(ctx, db, o, 7); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := db.NewMeterSnapshot(ctx, MeterFilter{Model: "different"}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(ctx, 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.AttemptID == "a" || row.AttemptID == "a/b" {
			count++
		}
	}
	if count != 2 {
		t.Fatal("filtered tuple collision", count)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := addMeterFixture(cancelled, db, meterObservation(MeterAttempt{ID: "a"}, "cancelled"), 9); err == nil {
		t.Fatal("cancelled write accepted")
	}
	if err := db.FinishMeterAttempt(context.WithoutCancel(cancelled), "a", "cancelled", "partial", "v", "s", []string{"cancelled"}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.sql.QueryRowContext(ctx, `SELECT output_tokens FROM meter_measurements WHERE attempt_id='a'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatal("cancel lost subtotal")
	}
}
