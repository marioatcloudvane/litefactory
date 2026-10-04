package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

// ErrArchiveNotSettled reports that the family changed or is no longer settled
// when the delete transaction rechecked it.
var ErrArchiveNotSettled = errors.New("archive family is not settled")

// ErrArchiveDigestMismatch reports that the live rows no longer match the
// digests of the verified archive file.
var ErrArchiveDigestMismatch = errors.New("archive digest does not match live rows")

// ArchiveFamily identifies one archivable task family: the canonical owner
// session and, through managed_stories, every child story session.
type ArchiveFamily struct {
	SessionID int64
	RepoID    int64
	TaskRef   string
	// IssueClosed is set once the forge confirmed the issue closed. Closing
	// withdraws every pending wait, report and effect, so only active work then
	// blocks the delete; the archive file still keeps every row.
	IssueClosed bool
}

func archiveNamed(f ArchiveFamily) []any {
	return []any{sql.Named("sid", f.SessionID), sql.Named("repo", f.RepoID), sql.Named("task", f.TaskRef)}
}

type archiveQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ArchiveOwner is a retirement owner observation plus the archive-only checks.
type ArchiveOwner struct {
	RetirementOwner
	Family ArchiveFamily
	// ArchivePending is true when anything is unfinished or unknown, including
	// a failure to read; the owner is then kept.
	ArchivePending       bool
	ArchivePendingReason string
	// ArchivableRows counts rows of archived tables; 0 means nothing to do.
	ArchivableRows int64
}

type archivePendingCheck struct {
	reason string
	query  string
}

var archivePendingChecks = []archivePendingCheck{
	{"graph_instance_unfinished", `SELECT EXISTS(SELECT 1 FROM graph_instances WHERE id IN ` + subInst + ` AND status='running') OR EXISTS(SELECT 1 FROM graph_node_attempts WHERE id IN ` + subNodeAt + ` AND status IN ('pending','running'))`},
	{"graph_owner_wait_open", `SELECT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id IN ` + subInst + ` AND state IN ('waiting','claimed','allocated'))`},
	{"graph_dispatch_open", `SELECT EXISTS(SELECT 1 FROM graph_dispatches WHERE (session_id IN ` + inFS + ` OR instance_id IN ` + subInst + `) AND status IN ('pending','reserved','running'))`},
	{"retirement_intent_unknown", `SELECT EXISTS(SELECT 1 FROM retirement_intents WHERE session_id IN ` + inFS + ` AND outcome='unknown')`},
	{"story_progress_effect_undelivered", `SELECT EXISTS(SELECT 1 FROM story_progress_effects WHERE (run_id IN ` + inFR + ` OR parent_task IN ` + inFT + `) AND delivered=0 AND superseded=0)`},
	{"comment_operation_pending", `SELECT EXISTS(SELECT 1 FROM comment_operations WHERE run_id IN ` + inFR + ` AND state='uncertain') OR EXISTS(SELECT 1 FROM family_requests WHERE run_id IN ` + inFR + ` AND disposition IN ('pending','uncertain'))`},
	{"application_result_attempt_unfinished", `SELECT EXISTS(SELECT 1 FROM application_result_attempts WHERE run_id IN ` + inFR + ` AND active=1) OR EXISTS(SELECT 1 FROM mcp_attempt_leases WHERE run_id IN ` + inFR + ` AND state='active')`},
	{"bug_history_retained", `SELECT EXISTS(SELECT 1 FROM bug_tasks WHERE session_id IN ` + inFS + `)`},
	{"mcp_creation_open", `SELECT EXISTS(SELECT 1 FROM mcp_creation_intents WHERE id IN ` + subIntents + ` AND expires_ns>:now) OR EXISTS(SELECT 1 FROM mcp_creation_operations WHERE id IN ` + subOps + ` AND state IN ('reserved','dispatched','uncertain')) OR EXISTS(SELECT 1 FROM creation_owner_publications WHERE intent_id IN ` + subIntents + ` AND state IN ('dispatched','uncertain')) OR EXISTS(SELECT 1 FROM mcp_creation_presentations WHERE (session_id IN ` + inFS + ` OR run_id IN ` + inFR + `) AND presented=0) OR EXISTS(SELECT 1 FROM mcp_creation_current c JOIN mcp_creation_proposals p ON p.hash=c.proposal_hash WHERE c.source_task IN ` + inFT + ` AND p.retained_ns>:proposal_floor)`},
	{"plan_effect_unsettled", `SELECT EXISTS(SELECT 1 FROM plan_story_candidates c WHERE c.parent IN ` + inFT + ` AND COALESCE((SELECT outcome FROM plan_story_effects e WHERE e.parent=c.parent AND e.request_id=c.request_id ORDER BY e.id DESC LIMIT 1),'pending') IN ('pending','uncertain'))`},
	{"doctor_reference", `SELECT EXISTS(SELECT 1 FROM doctor_conversations WHERE task_ref IN ` + inFT + `) OR EXISTS(SELECT 1 FROM doctor_inbox WHERE task_ref IN ` + inFT + `) OR EXISTS(SELECT 1 FROM doctor_repairs WHERE task_ref IN ` + inFT + `) OR EXISTS(SELECT 1 FROM doctor_repair_holds WHERE task_ref IN ` + inFT + `) OR EXISTS(SELECT 1 FROM doctor_local_presentations WHERE task_ref IN ` + inFT + `)`},
}

func archivePendingArgs(f ArchiveFamily, now time.Time) []any {
	return append(archiveNamed(f), sql.Named("now", now.UnixNano()), sql.Named("proposal_floor", now.Add(-MCPCreationApprovalTTL).UnixNano()))
}

// archivePendingReason returns the first unfinished condition, "" when the
// family is settled, and an error when any check cannot be read.
func archivePendingReason(ctx context.Context, q archiveQuerier, f ArchiveFamily, now time.Time) (string, error) {
	args := archivePendingArgs(f, now)
	for _, c := range archivePendingChecks {
		var open bool
		if err := q.QueryRowContext(ctx, archiveCTE+c.query, args...).Scan(&open); err != nil {
			return "read_error:" + c.reason, fmt.Errorf("check %s: %w", c.reason, err)
		}
		if open {
			return c.reason, nil
		}
	}
	return "", nil
}

func countArchivable(ctx context.Context, q archiveQuerier, f ArchiveFamily) (int64, error) {
	var total int64
	for _, t := range archiveRegistry {
		var n int64
		if err := q.QueryRowContext(ctx, archiveCTE+`SELECT COUNT(*) FROM `+t.Name+` WHERE (`+t.Where+`)`, archiveNamed(f)...).Scan(&n); err != nil {
			return 0, fmt.Errorf("count %s: %w", t.Name, err)
		}
		total += n
	}
	return total, nil
}

// GetArchiveOwner is GetRetirementOwner plus the archive-only pending checks.
// A failure of the archive-only reads never returns an error for the owner: it
// is reported as ArchivePending so the owner is kept. Context cancellation and
// failure of the retirement observation itself are returned.
func (db *DB) GetArchiveOwner(ctx context.Context, id int64, now time.Time) (ArchiveOwner, error) {
	var out ArchiveOwner
	owner, err := db.GetRetirementOwner(ctx, id)
	out.RetirementOwner = owner
	if err != nil {
		return out, err
	}
	out.Family = ArchiveFamily{SessionID: id, RepoID: owner.Session.RepoID, TaskRef: owner.Session.TaskRef}
	reason, err := archivePendingReason(ctx, db.sql, out.Family, now)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.ArchivePending, out.ArchivePendingReason = true, reason
		return out, nil
	}
	if reason != "" {
		out.ArchivePending, out.ArchivePendingReason = true, reason
	}
	// Counted even when something is pending: a closed issue is archived
	// despite pending bookkeeping, and zero rows would wrongly mean "nothing".
	out.ArchivableRows, err = countArchivable(ctx, db.sql, out.Family)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.ArchivePending, out.ArchivePendingReason = true, "read_error:count"
	}
	return out, nil
}

// ArchiveFamilySessions returns the ids of every session in the family.
func (db *DB) ArchiveFamilySessions(ctx context.Context, f ArchiveFamily) ([]int64, error) {
	rows, err := db.sql.QueryContext(ctx, archiveCTE+`SELECT id FROM fs ORDER BY id`, archiveNamed(f)...)
	if err != nil {
		return nil, fmt.Errorf("read family sessions: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read family session: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func archivePrimaryKey(ctx context.Context, q archiveQuerier, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) WHERE pk>0 ORDER BY pk`, table)
	if err != nil {
		return nil, fmt.Errorf("read primary key of %s: %w", table, err)
	}
	defer rows.Close()
	var pk []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("read primary key of %s: %w", table, err)
		}
		pk = append(pk, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pk) == 0 {
		return nil, fmt.Errorf("table %s has no primary key", table)
	}
	return pk, nil
}

// streamArchiveRows visits the family's rows table by table, parents first
// (reverse delete order), each table in primary-key order.
func streamArchiveRows(ctx context.Context, q archiveQuerier, f ArchiveFamily, fn func(archive.Row) error) error {
	for i := len(archiveRegistry) - 1; i >= 0; i-- {
		if err := streamArchiveTable(ctx, q, archiveRegistry[i], f, fn); err != nil {
			return err
		}
	}
	return nil
}

func streamArchiveTable(ctx context.Context, q archiveQuerier, t ArchiveTable, f ArchiveFamily, fn func(archive.Row) error) error {
	pk, err := archivePrimaryKey(ctx, q, t.Name)
	if err != nil {
		return err
	}
	order := ""
	for i, c := range pk {
		if i > 0 {
			order += ","
		}
		order += `"` + c + `"`
	}
	rows, err := q.QueryContext(ctx, archiveCTE+`SELECT * FROM `+t.Name+` WHERE (`+t.Where+`) ORDER BY `+order, archiveNamed(f)...)
	if err != nil {
		return fmt.Errorf("read %s: %w", t.Name, err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("read %s columns: %w", t.Name, err)
	}
	for rows.Next() {
		values := make([]any, len(names))
		dest := make([]any, len(names))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan %s: %w", t.Name, err)
		}
		row := archive.Row{Table: t.Name, Columns: make([]archive.Column, 0, len(names))}
		for i, n := range names {
			c, err := archive.NewColumn(n, values[i])
			if err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			row.Columns = append(row.Columns, c)
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s: %w", t.Name, err)
	}
	return nil
}

// StreamArchiveRows calls fn for every archivable row of the family: tables in
// foreign-key parent-first order, rows in primary-key order, columns in
// declared order. fn must not use the database.
func (db *DB) StreamArchiveRows(ctx context.Context, f ArchiveFamily, fn func(archive.Row) error) error {
	return streamArchiveRows(ctx, db.sql, f, fn)
}

// DeleteArchivedFamily removes the family's archived-class rows in one
// transaction after rechecking that the family is still settled and that the
// live rows hash to exactly the verified file's trailer. It returns the rows
// deleted per table. Any failure rolls everything back.
func (db *DB) DeleteArchivedFamily(ctx context.Context, f ArchiveFamily, want archive.Trailer, now time.Time) (map[string]int64, error) {
	deleted := make(map[string]int64)
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var fk int
		if err := tx.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			return fmt.Errorf("read foreign_keys pragma: %w", err)
		}
		if fk != 1 {
			return errors.New("foreign key enforcement is off")
		}
		if err := recheckArchiveSettled(ctx, tx, f, now); err != nil {
			return err
		}
		digester := archive.NewDigester()
		if err := streamArchiveRows(ctx, tx, f, func(r archive.Row) error {
			_, err := digester.Add(r)
			return err
		}); err != nil {
			return err
		}
		live := digester.Trailer()
		if live.Digest != want.Digest || !countsEqual(live.Counts, want.Counts) {
			return ErrArchiveDigestMismatch
		}
		granted := false
		for _, t := range archiveRegistry {
			if !granted && isPlanArchiveTable(t.Name) {
				parents, err := planArchiveParents(ctx, tx, f)
				if err != nil {
					return err
				}
				if _, err := writePlanArchiveGrants(ctx, tx, planGrantAdd, parents); err != nil {
					return err
				}
				granted = true
			}
			res, err := tx.ExecContext(ctx, archiveCTE+`DELETE FROM `+t.Name+` WHERE (`+t.Where+`)`, archiveNamed(f)...)
			if err != nil {
				return fmt.Errorf("delete %s: %w", t.Name, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("delete %s: %w", t.Name, err)
			}
			if n != want.Counts[t.Name] {
				return fmt.Errorf("%w: %s deleted %d rows, archive holds %d", ErrArchiveDigestMismatch, t.Name, n, want.Counts[t.Name])
			}
			if n > 0 {
				deleted[t.Name] = n
			}
		}
		if granted {
			parents, err := planArchiveParents(ctx, tx, f)
			if err != nil {
				return err
			}
			if _, err := writePlanArchiveGrants(ctx, tx, planGrantRemove, parents); err != nil {
				return err
			}
			left, err := writePlanArchiveGrants(ctx, tx, planGrantCount, nil)
			if err != nil {
				return err
			}
			if left != 0 {
				return fmt.Errorf("%d Plan archive grants left before commit", left)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

func countsEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func recheckArchiveSettled(ctx context.Context, tx *sql.Tx, f ArchiveFamily, now time.Time) error {
	family := retirementFamilySQL
	args := []any{f.SessionID, f.RepoID, f.TaskRef}
	var runs int
	var finished sql.NullString
	var busy bool
	if err := tx.QueryRowContext(ctx, family+retirementActivitySQL, append(append([]any{}, args...), f.SessionID)...).Scan(&runs, &finished, &busy); err != nil {
		return fmt.Errorf("recheck activity: %w", err)
	}
	if busy {
		return fmt.Errorf("%w: active work", ErrArchiveNotSettled)
	}
	if f.IssueClosed {
		return nil
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, family+retirementPendingSQL, args...).Scan(&pending); err != nil {
		return fmt.Errorf("recheck pending work: %w", err)
	}
	if pending {
		return fmt.Errorf("%w: pending work", ErrArchiveNotSettled)
	}
	reason, err := archivePendingReason(ctx, tx, f, now)
	if err != nil {
		return err
	}
	if reason != "" {
		return fmt.Errorf("%w: %s", ErrArchiveNotSettled, reason)
	}
	return nil
}

// ArchiveSweep is the durable state of the daily archive pass, one singleton
// row independent of retirement and of any per-task bookkeeping.
type ArchiveSweep struct {
	ID         string         `json:"id"`
	StartedAt  time.Time      `json:"started_at"`
	Boundary   int64          `json:"boundary"`
	Cursor     int64          `json:"cursor"`
	Owners     int            `json:"owners"`
	Counts     map[string]int `json:"counts"`
	Deferred   int            `json:"deferred"`
	DeletedAny bool           `json:"deleted_any"`
	Complete   bool           `json:"complete"`
	NextDue    time.Time      `json:"next_due"`
}

// GetArchiveSweep returns zero state before the first pass.
func (db *DB) GetArchiveSweep(ctx context.Context) (ArchiveSweep, error) {
	var s ArchiveSweep
	var data string
	err := db.sql.QueryRowContext(ctx, `SELECT state FROM archive_sweep WHERE singleton=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read archive sweep: %w", err)
	}
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return s, fmt.Errorf("decode archive sweep: %w", err)
	}
	return s, nil
}

// SaveArchiveSweep checkpoints only local state; no transaction spans I/O.
func (db *DB) SaveArchiveSweep(ctx context.Context, s ArchiveSweep) error {
	if s.ID == "" || s.StartedAt.IsZero() || s.Cursor < 0 || s.Boundary < s.Cursor {
		return errors.New("invalid archive sweep")
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode archive sweep: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO archive_sweep(singleton,state) VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET state=excluded.state`, string(data)); err != nil {
		return fmt.Errorf("save archive sweep: %w", err)
	}
	return nil
}

// CompactResult reports database size (main file plus WAL) around compaction.
type CompactResult struct {
	Before int64
	After  int64
}

// Compact runs VACUUM and then a truncating WAL checkpoint, both outside any
// transaction, and reports the on-disk size before and after.
func (db *DB) Compact(ctx context.Context) (CompactResult, error) {
	var r CompactResult
	var err error
	if r.Before, err = db.diskSize(); err != nil {
		return r, err
	}
	if _, err := db.sql.ExecContext(ctx, `VACUUM`); err != nil {
		return r, fmt.Errorf("vacuum: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := db.sql.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return r, fmt.Errorf("wal checkpoint: %w", err)
	}
	if busy != 0 {
		return r, errors.New("wal checkpoint was blocked")
	}
	if r.After, err = db.diskSize(); err != nil {
		return r, err
	}
	return r, nil
}

func (db *DB) diskSize() (int64, error) {
	var total int64
	for _, p := range []string{db.path, db.path + "-wal"} {
		info, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("stat database file: %w", err)
		}
		total += info.Size()
	}
	return total, nil
}
