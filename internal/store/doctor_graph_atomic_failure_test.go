package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// These triggers fail after earlier dependent transitions have executed. A
// status-only assertion would miss leaked guidance, backups or journal entries.
func TestDoctorGraphWholeRepairAbortsEveryEffect(t *testing.T) {
	cases := []struct {
		name, trigger, want string
	}{
		{"late-related-precondition", `CREATE TRIGGER fail_graph_repair AFTER UPDATE OF status ON graph_node_attempts WHEN NEW.status='cancelled' BEGIN INSERT INTO graph_node_attempts(instance_id,node,attempt,status,started_at) VALUES(NEW.instance_id,NEW.node,99,'running',NEW.started_at); END`, "related work is active"},
		{"late-row-write", `CREATE TRIGGER fail_graph_repair BEFORE UPDATE OF status ON graph_instances WHEN NEW.status='failed' BEGIN SELECT RAISE(ABORT,'injected late graph write'); END`, "injected late graph write"},
		{"reply-handoff", `CREATE TRIGGER fail_graph_repair BEFORE INSERT ON owner_instruction_inputs BEGIN SELECT RAISE(ABORT,'injected guidance handoff'); END`, "injected guidance handoff"},
		{"postimage-verification", `CREATE TRIGGER fail_graph_repair AFTER UPDATE OF status ON graph_instances WHEN NEW.status='failed' BEGIN UPDATE graph_dispatches SET cancellation_reason='injected postimage drift' WHERE instance_id=NEW.id; END`, "postimage verification failed"},
		{"last-numbered-journal", `CREATE TRIGGER fail_graph_repair BEFORE UPDATE OF state ON doctor_repair_steps WHEN NEW.state='verified' AND NEW.number=4 BEGIN SELECT RAISE(ABORT,'injected last journal'); END`, "injected last journal"},
		{"last-operation-write", `CREATE TRIGGER fail_graph_repair BEFORE UPDATE OF state ON doctor_repairs WHEN NEW.state='applying' BEGIN SELECT RAISE(ABORT,'injected commit boundary'); END`, "injected commit boundary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _, session, steps := graphAtomicFixture(t)
			op := graphApproveSteps(t, db, session, "forward-failure", steps)
			if _, err := db.sql.ExecContext(t.Context(), tc.trigger); err != nil {
				t.Fatal(err)
			}
			before := graphFailureSnapshot(t, db)
			err := db.ApplyDoctorGraphRepair(t.Context(), op.ID)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected injected failure %q, got %v", tc.want, err)
			}
			if after := graphFailureSnapshot(t, db); after != before {
				t.Fatalf("aborted repair leaked rows, guidance, revisions or journals\nbefore=%s\nafter=%s", before, after)
			}
			// A proven abort can be retried without rebuilding its approved manifest.
			if _, err = db.sql.ExecContext(t.Context(), `DROP TRIGGER fail_graph_repair`); err != nil {
				t.Fatal(err)
			}
			if err = db.ApplyDoctorGraphRepair(t.Context(), op.ID); err != nil {
				t.Fatalf("unchanged approved repair could not retry after full abort: %v", err)
			}
			if err = db.VerifyDoctorGraphRepair(t.Context(), op.ID); err != nil {
				t.Fatal(err)
			}
			committed := graphFailureSnapshot(t, db)
			if err = db.ApplyDoctorGraphRepair(t.Context(), op.ID); err != nil {
				t.Fatalf("lost receipt reconciliation failed: %v", err)
			}
			if after := graphFailureSnapshot(t, db); after != committed {
				t.Fatal("reconciled repair changed its committed rows or numbered journals")
			}
		})
	}
}

// Inverse transitions have the same one-transaction boundary, including removal
// of newly retained pending guidance and all numbered reversal journal entries.
func TestDoctorGraphWholeInverseAbortsEveryEffect(t *testing.T) {
	for _, tc := range []struct{ name, trigger, want string }{
		{"late-write", `CREATE TRIGGER fail_graph_inverse BEFORE UPDATE OF status ON graph_node_attempts WHEN NEW.status='running' BEGIN SELECT RAISE(ABORT,'injected inverse write'); END`, "injected inverse write"},
		{"guidance-reversal", `CREATE TRIGGER fail_graph_inverse BEFORE DELETE ON owner_instruction_inputs BEGIN SELECT RAISE(ABORT,'injected inverse guidance'); END`, "injected inverse guidance"},
		{"last-journal", `CREATE TRIGGER fail_graph_inverse BEFORE UPDATE OF state ON doctor_repair_steps WHEN NEW.state='verified' AND NEW.number=4 BEGIN SELECT RAISE(ABORT,'injected inverse journal'); END`, "injected inverse journal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _, session, steps := graphAtomicFixture(t)
			forward := graphApproveSteps(t, db, session, "before-inverse", steps)
			if err := db.ApplyDoctorGraphRepair(t.Context(), forward.ID); err != nil {
				t.Fatal(err)
			}
			if err := db.SettleDoctorTypedRepair(t.Context(), forward.ID, true, ""); err != nil {
				t.Fatal(err)
			}
			inverse, err := db.PrepareDoctorGraphRollback(t.Context(), session.TaskRef, forward.ID, "Restore only the exact approved repair")
			if err != nil {
				t.Fatal(err)
			}
			op := graphApproveSteps(t, db, session, "inverse-failure", inverse)
			if _, err = db.sql.ExecContext(t.Context(), tc.trigger); err != nil {
				t.Fatal(err)
			}
			before := graphFailureSnapshot(t, db)
			err = db.ApplyDoctorGraphRepair(t.Context(), op.ID)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected inverse failure %q, got %v", tc.want, err)
			}
			if after := graphFailureSnapshot(t, db); after != before {
				t.Fatalf("failed inverse left partial reversal\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

func TestDoctorGraphInverseRefusesProgressEvenWhenRowsReturn(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	forward := graphApproveSteps(t, db, session, "progress-forward", steps)
	if err := db.ApplyDoctorGraphRepair(t.Context(), forward.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), forward.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	// The target's exact values return to their postimage. Its monotonic scoped
	// revision must still prove that related work advanced between observations.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_instances SET current_node='intervening' WHERE id=?`, instance.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_instances SET current_node=? WHERE id=?`, instance.CurrentNode, instance.ID); err != nil {
		t.Fatal(err)
	}
	before := graphFailureSnapshot(t, db)
	if _, err := db.PrepareDoctorGraphRollback(t.Context(), session.TaskRef, forward.ID, "Undo after progress"); err == nil {
		t.Fatal("inverse admitted after related work advanced and returned")
	}
	if after := graphFailureSnapshot(t, db); after != before {
		t.Fatal("refused inverse changed state")
	}
}

func TestDoctorGraphUncertainJournalRetainsHold(t *testing.T) {
	db, _, session, steps := graphAtomicFixture(t)
	op := graphApproveSteps(t, db, session, "uncertain-journal", steps)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE doctor_repair_steps SET state='uncertain' WHERE repair_id=? AND number=2`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), op.ID, false, "The repair journal does not establish a complete commit or abort"); err != nil {
		t.Fatal(err)
	}
	held, err := db.DoctorRepairHeld(t.Context(), session.TaskRef)
	if err != nil || !held {
		t.Fatalf("uncertain repair released exclusion: held=%v %v", held, err)
	}
	operation, err := db.ReadDoctorRepair(t.Context(), session.TaskRef)
	if err != nil || operation.State != "needs_review" || !operation.Fenced {
		t.Fatalf("uncertain outcome was not fenced for inspection: %+v %v", operation, err)
	}
	before := graphFailureSnapshot(t, db)
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err == nil {
		t.Fatal("uncertain repair was replayed")
	}
	if after := graphFailureSnapshot(t, db); after != before {
		t.Fatal("uncertain replay changed retained effects")
	}
}

func TestDoctorGraphInverseApprovalDoesNotSurviveRelatedProgress(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	forward := graphApproveSteps(t, db, session, "inverse-stale-forward", steps)
	if err := db.ApplyDoctorGraphRepair(t.Context(), forward.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), forward.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	inverse, err := db.PrepareDoctorGraphRollback(t.Context(), session.TaskRef, forward.ID, "Restore only while no related work advances")
	if err != nil {
		t.Fatal(err)
	}
	op := graphApproveSteps(t, db, session, "inverse-stale", inverse)
	// Simulate stale trusted observations after the exact inverse was approved.
	// Even values returning to the approved image cannot reuse that authority.
	for _, change := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, []any{op.ID}},
		{`UPDATE graph_instances SET current_node='intervening' WHERE id=?`, []any{instance.ID}},
		{`UPDATE graph_instances SET current_node=? WHERE id=?`, []any{instance.CurrentNode, instance.ID}},
		{`DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, []any{op.ID}},
	} {
		if _, err := db.sql.ExecContext(t.Context(), change.query, change.args...); err != nil {
			t.Fatal(err)
		}
	}
	before := graphFailureSnapshot(t, db)
	if err := db.ApplyDoctorGraphRepair(t.Context(), op.ID); err == nil {
		t.Fatal("stale exact inverse approval survived related progress")
	}
	if after := graphFailureSnapshot(t, db); after != before {
		t.Fatal("stale inverse partially restored repair rows")
	}
}

func TestDoctorGraphInverseRefusesCorruptBackup(t *testing.T) {
	db, _, session, steps := graphAtomicFixture(t)
	forward := graphApproveSteps(t, db, session, "corrupt-backup", steps)
	if err := db.ApplyDoctorGraphRepair(t.Context(), forward.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.SettleDoctorTypedRepair(t.Context(), forward.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE doctor_repair_steps SET backup=X'7B7D' WHERE repair_id=? AND number=4`, forward.ID); err != nil {
		t.Fatal(err)
	}
	before := graphFailureSnapshot(t, db)
	if _, err := db.PrepareDoctorGraphRollback(t.Context(), session.TaskRef, forward.ID, "Restore from verified backup"); err == nil || !strings.Contains(err.Error(), "backup integrity") {
		t.Fatalf("corrupt backup offered an inverse: %v", err)
	}
	if after := graphFailureSnapshot(t, db); after != before {
		t.Fatal("corrupt backup inspection changed retained state")
	}
}

func TestDoctorGraphHoldCoversRunlessRelations(t *testing.T) {
	db, instance, session, steps := graphAtomicFixture(t)
	var attemptID int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT id FROM graph_node_attempts WHERE instance_id=? AND run_id IS NULL`, instance.ID).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	// Seed a related output before obtaining the hold, then test every mutation
	// through the owning graph instance rather than a nonexistent run.
	result, err := db.sql.ExecContext(t.Context(), `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'text','retained',?)`, attemptID, FormatTime(graphTestTime()))
	if err != nil {
		t.Fatal(err)
	}
	outputID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	// Refresh manifests because the added output is part of related evidence.
	for index := range steps {
		action, err := db.PrepareDoctorGraphAction(t.Context(), session.TaskRef, steps[index].Kind, steps[index].Target, "Exclude runless related writers")
		if err != nil {
			t.Fatal(err)
		}
		steps[index].Before, steps[index].After = action.Before, action.After
	}
	graphApproveSteps(t, db, session, "runless-exclusion", steps)
	for _, tc := range []struct {
		name, query string
		args        []any
	}{
		{"attempt-insert", `INSERT INTO graph_node_attempts(instance_id,node,attempt,status,started_at) VALUES(?,'other',1,'running',?)`, []any{instance.ID, FormatTime(graphTestTime())}},
		{"attempt-update", `UPDATE graph_node_attempts SET node='other' WHERE id=?`, []any{attemptID}},
		{"attempt-delete", `DELETE FROM graph_node_attempts WHERE id=?`, []any{attemptID}},
		{"output-insert", `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'text','new',?)`, []any{attemptID, FormatTime(graphTestTime())}},
		{"output-update", `UPDATE graph_outputs SET payload='changed' WHERE id=?`, []any{outputID}},
		{"output-delete", `DELETE FROM graph_outputs WHERE id=?`, []any{outputID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := graphFailureSnapshot(t, db)
			if _, err := db.sql.ExecContext(t.Context(), tc.query, tc.args...); !IsDoctorRepairHold(err) {
				t.Fatalf("runless writer escaped exact repair exclusion: %v", err)
			}
			if after := graphFailureSnapshot(t, db); after != before {
				t.Fatal("refused writer changed retained repair state")
			}
		})
	}
}

// Read every changed and related table, including backup bytes and revision and
// hold state. This is intentionally stronger than checking target statuses.
func graphFailureSnapshot(t *testing.T, db *DB) string {
	t.Helper()
	var snapshot [][]map[string]any
	err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		for _, query := range []string{
			`SELECT * FROM sessions ORDER BY id`,
			`SELECT * FROM graph_instances ORDER BY id`,
			`SELECT * FROM graph_node_attempts ORDER BY id`,
			`SELECT * FROM graph_dispatches ORDER BY id`,
			`SELECT * FROM graph_owner_waits ORDER BY delivery_key`,
			`SELECT * FROM graph_comment_intents ORDER BY delivery_key`,
			`SELECT * FROM graph_outputs ORDER BY id`,
			`SELECT * FROM graph_transitions ORDER BY id`,
			`SELECT * FROM comments ORDER BY id`,
			`SELECT * FROM owner_instruction_inputs ORDER BY comment_id`,
			`SELECT * FROM doctor_repair_steps ORDER BY repair_id,number`,
			`SELECT * FROM doctor_repairs ORDER BY id`,
			`SELECT * FROM doctor_repair_holds ORDER BY task_ref`,
			`SELECT * FROM doctor_repair_write_permits ORDER BY repair_id`,
		} {
			rows, err := graphRows(t.Context(), tx, query)
			if err != nil {
				return fmt.Errorf("snapshot %s: %w", query, err)
			}
			snapshot = append(snapshot, rows)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
