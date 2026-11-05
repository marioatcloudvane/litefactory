package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Brainstorm task item states. plan_started and uncertain_exhausted are final;
// not_created and plan_start_failed are final unless an open continuation names them.
const (
	BrainstormItemPending            = "pending"
	BrainstormItemCreating           = "creating"
	BrainstormItemUncertain          = "uncertain"
	BrainstormItemUncertainExhausted = "uncertain_exhausted"
	BrainstormItemNotCreated         = "not_created"
	BrainstormItemCreated            = "created"
	BrainstormItemPlanStarting       = "plan_starting"
	BrainstormItemPlanStarted        = "plan_started"
	BrainstormItemPlanStartFailed    = "plan_start_failed"
)

// BrainstormLaunchFailureLimit is the failed launch count that makes an item
// plan_start_failed. A repeated confirmation resets the count to one below it,
// so each repeated confirmation allows exactly one more launch attempt.
const BrainstormLaunchFailureLimit = 3

// ErrBrainstormFrozenChanged reports that an item frozen in a proposal moved.
var ErrBrainstormFrozenChanged = errors.New("store: brainstorm frozen items changed; request a fresh proposal")

// ErrBrainstormTaskInvalid reports malformed brainstorm task admission input.
var ErrBrainstormTaskInvalid = errors.New("store: invalid brainstorm task input")

// BrainstormTaskBatch is one confirmed draft set turned into issues and Plan runs.
type BrainstormTaskBatch struct {
	ID, OwnerActionID, ExecutionID                 string
	OwnerID                                        string
	OwnerGeneration                                int64
	RepositoryID                                   int64
	RepositoryRef, BrainstormID, DraftSetID, State string
	CreatedAt, SettledAt                           time.Time
}

// BrainstormTaskItem is the per-draft ledger of one batch.
type BrainstormTaskItem struct {
	ID, BatchID, DraftID                string
	Ordinal, Generation                 int
	Title, Body, Marker                 string
	Ledger                              json.RawMessage
	LedgerDigest                        string
	State                               string
	IssueID                             int64
	IssueNumber                         int
	IssueLink                           string
	SessionID                           int64
	AttemptAt                           time.Time
	ChecksDone                          int
	NextCheckAt                         time.Time
	CreateRetries, FailedLaunchAttempts int
	Reason                              string
	UpdatedAt                           time.Time
}

// BrainstormFrozenItem is one item exactly as the owner reviewed it.
type BrainstormFrozenItem struct {
	ItemID     string `json:"item_id"`
	State      string `json:"state"`
	Generation int    `json:"generation"`
}

// BrainstormTaskContinuation is one confirmed pass over frozen batch items.
type BrainstormTaskContinuation struct {
	ID, BatchID, ExecutionID, Kind    string
	Items                             []BrainstormFrozenItem
	FrozenDigest, State, SummaryState string
	CreatedAt, SettledAt              time.Time
}

// BrainstormFrozenDigest is the digest bound into a proposal for frozen items.
func BrainstormFrozenDigest(items []BrainstormFrozenItem) string {
	raw, _ := json.Marshal(items)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func brainstormTaskID(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(newUUIDLike(), "-", "")[:20]
}

func newUUIDLike() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func taskTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTaskTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// AdmitBrainstormTaskBatch writes the batch, its items and its create
// continuation in one transaction keyed by execution id, and marks the draft
// set used. A replay with the same execution id returns the stored rows.
func (db *DB) AdmitBrainstormTaskBatch(ctx context.Context, b BrainstormTaskBatch, items []BrainstormTaskItem, c BrainstormTaskContinuation, at time.Time) (BrainstormTaskBatch, BrainstormTaskContinuation, bool, error) {
	if b.ExecutionID == "" || b.DraftSetID == "" || len(items) == 0 {
		return BrainstormTaskBatch{}, BrainstormTaskContinuation{}, false, ErrBrainstormTaskInvalid
	}
	var outB BrainstormTaskBatch
	var outC BrainstormTaskContinuation
	replayed := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanTaskBatch(tx.QueryRowContext(ctx, batchSelect+` WHERE execution_id=?`, b.ExecutionID))
		if err == nil {
			outB, replayed = existing, true
			outC, err = scanTaskContinuation(tx.QueryRowContext(ctx, continuationSelect+` WHERE execution_id=?`, b.ExecutionID))
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if b.ID == "" {
			b.ID = brainstormTaskID("btb")
		}
		b.State, b.CreatedAt = "running", at
		if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_task_batches(id,owner_action_id,execution_id,owner_id,owner_generation,repository_id,repository_ref,brainstorm_id,draft_set_id,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			b.ID, b.OwnerActionID, b.ExecutionID, b.OwnerID, b.OwnerGeneration, b.RepositoryID, b.RepositoryRef, b.BrainstormID, b.DraftSetID, b.State, taskTime(at)); err != nil {
			return fmt.Errorf("brainstorm batch: %w", err)
		}
		frozen := make([]BrainstormFrozenItem, 0, len(items))
		for i, it := range items {
			if it.ID == "" {
				it.ID = brainstormTaskID("bti")
			}
			if it.Generation < 1 {
				it.Generation = 1
			}
			it.BatchID, it.State = b.ID, BrainstormItemPending
			if it.Ordinal == 0 {
				it.Ordinal = i + 1
			}
			if len(it.Ledger) == 0 {
				it.Ledger = json.RawMessage(`{}`)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_task_items(id,batch_id,draft_id,ordinal,generation,title,body,marker,ledger,ledger_digest,state,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				it.ID, it.BatchID, it.DraftID, it.Ordinal, it.Generation, it.Title, it.Body, it.Marker, string(it.Ledger), it.LedgerDigest, it.State, taskTime(at)); err != nil {
				return fmt.Errorf("brainstorm item: %w", err)
			}
			frozen = append(frozen, BrainstormFrozenItem{ItemID: it.ID, State: it.State, Generation: it.Generation})
		}
		c.BatchID, c.ExecutionID, c.Kind, c.Items = b.ID, b.ExecutionID, "create", frozen
		c.FrozenDigest = BrainstormFrozenDigest(frozen)
		if outC, err = insertTaskContinuation(ctx, tx, c, at); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE brainstorm_draft_sets SET state='used' WHERE id=?`, b.DraftSetID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrNotFound
		}
		outB = b
		return nil
	})
	return outB, outC, replayed, err
}

func insertTaskContinuation(ctx context.Context, tx *sql.Tx, c BrainstormTaskContinuation, at time.Time) (BrainstormTaskContinuation, error) {
	if c.ID == "" {
		c.ID = brainstormTaskID("btc")
	}
	c.State, c.SummaryState, c.CreatedAt = "running", "pending", at
	raw, err := json.Marshal(c.Items)
	if err != nil {
		return c, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_task_continuations(id,batch_id,execution_id,kind,items,frozen_digest,state,summary_state,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		c.ID, c.BatchID, c.ExecutionID, c.Kind, string(raw), c.FrozenDigest, c.State, c.SummaryState, taskTime(at))
	return c, err
}

// AdmitBrainstormTaskContinuation admits a continue or recreate pass over the
// frozen items of an existing batch, keyed by execution id. It refuses with
// ErrBrainstormFrozenChanged when any frozen item's state or generation moved.
// Frozen not_created items return to pending and plan_start_failed items to
// created with one more allowed launch attempt; a recreate bumps the named
// uncertain_exhausted item's generation with its new title, body and marker.
func (db *DB) AdmitBrainstormTaskContinuation(ctx context.Context, c BrainstormTaskContinuation, recreate *BrainstormTaskItem, at time.Time) (BrainstormTaskContinuation, bool, error) {
	if c.ExecutionID == "" || c.BatchID == "" || (c.Kind != "continue" && c.Kind != "recreate") || len(c.Items) == 0 || (c.Kind == "recreate") != (recreate != nil) {
		return BrainstormTaskContinuation{}, false, ErrBrainstormTaskInvalid
	}
	var out BrainstormTaskContinuation
	replayed := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanTaskContinuation(tx.QueryRowContext(ctx, continuationSelect+` WHERE execution_id=?`, c.ExecutionID))
		if err == nil {
			out, replayed = existing, true
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if c.FrozenDigest != BrainstormFrozenDigest(c.Items) {
			return ErrBrainstormFrozenChanged
		}
		var batchState string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM brainstorm_task_batches WHERE id=?`, c.BatchID).Scan(&batchState); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		now := taskTime(at)
		for _, f := range c.Items {
			it, err := scanTaskItem(tx.QueryRowContext(ctx, itemSelect+` WHERE id=?`, f.ItemID))
			if errors.Is(err, ErrNotFound) {
				return ErrBrainstormFrozenChanged
			} else if err != nil {
				return err
			}
			if it.BatchID != c.BatchID || it.State != f.State || it.Generation != f.Generation {
				return ErrBrainstormFrozenChanged
			}
			var q string
			var args []any
			switch {
			case recreate != nil && recreate.ID == it.ID:
				if it.State != BrainstormItemUncertainExhausted || recreate.Generation <= it.Generation {
					return ErrBrainstormFrozenChanged
				}
				q = `UPDATE brainstorm_task_items SET generation=?,title=?,body=?,marker=?,state='pending',attempt_at='',checks_done=0,next_check_at='',create_retries=0,reason='',updated_at=? WHERE id=?`
				args = []any{recreate.Generation, recreate.Title, recreate.Body, recreate.Marker, now, it.ID}
			case it.State == BrainstormItemNotCreated:
				q = `UPDATE brainstorm_task_items SET state='pending',create_retries=0,reason='',updated_at=? WHERE id=?`
				args = []any{now, it.ID}
			case it.State == BrainstormItemPlanStartFailed:
				q = `UPDATE brainstorm_task_items SET state='created',failed_launch_attempts=?,reason='',updated_at=? WHERE id=?`
				args = []any{min(it.FailedLaunchAttempts, BrainstormLaunchFailureLimit-1), now, it.ID}
			}
			if q != "" {
				if _, err = tx.ExecContext(ctx, q, args...); err != nil {
					return err
				}
			}
		}
		if batchState != "running" {
			if _, err = tx.ExecContext(ctx, `UPDATE brainstorm_task_batches SET state='running',settled_at='' WHERE id=?`, c.BatchID); err != nil {
				return err
			}
		}
		out, err = insertTaskContinuation(ctx, tx, c, at)
		return err
	})
	return out, replayed, err
}

const batchSelect = `SELECT id,owner_action_id,execution_id,owner_id,owner_generation,repository_id,repository_ref,brainstorm_id,draft_set_id,state,created_at,settled_at FROM brainstorm_task_batches`

func scanTaskBatch(row interface{ Scan(...any) error }) (BrainstormTaskBatch, error) {
	var b BrainstormTaskBatch
	var created, settled string
	err := row.Scan(&b.ID, &b.OwnerActionID, &b.ExecutionID, &b.OwnerID, &b.OwnerGeneration, &b.RepositoryID, &b.RepositoryRef, &b.BrainstormID, &b.DraftSetID, &b.State, &created, &settled)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	if b.CreatedAt, err = parseTaskTime(created); err != nil {
		return b, err
	}
	b.SettledAt, err = parseTaskTime(settled)
	return b, err
}

const continuationSelect = `SELECT id,batch_id,execution_id,kind,items,frozen_digest,state,summary_state,created_at,settled_at FROM brainstorm_task_continuations`

func scanTaskContinuation(row interface{ Scan(...any) error }) (BrainstormTaskContinuation, error) {
	var c BrainstormTaskContinuation
	var items, created, settled string
	err := row.Scan(&c.ID, &c.BatchID, &c.ExecutionID, &c.Kind, &items, &c.FrozenDigest, &c.State, &c.SummaryState, &created, &settled)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(items), &c.Items); err != nil {
		return c, err
	}
	if c.CreatedAt, err = parseTaskTime(created); err != nil {
		return c, err
	}
	c.SettledAt, err = parseTaskTime(settled)
	return c, err
}

const itemSelect = `SELECT id,batch_id,draft_id,ordinal,generation,title,body,marker,ledger,ledger_digest,state,issue_id,issue_number,issue_link,session_id,attempt_at,checks_done,next_check_at,create_retries,failed_launch_attempts,reason,updated_at FROM brainstorm_task_items`

func scanTaskItem(row interface{ Scan(...any) error }) (BrainstormTaskItem, error) {
	var it BrainstormTaskItem
	var ledger, attempt, next, updated string
	err := row.Scan(&it.ID, &it.BatchID, &it.DraftID, &it.Ordinal, &it.Generation, &it.Title, &it.Body, &it.Marker, &ledger, &it.LedgerDigest, &it.State, &it.IssueID, &it.IssueNumber, &it.IssueLink, &it.SessionID, &attempt, &it.ChecksDone, &next, &it.CreateRetries, &it.FailedLaunchAttempts, &it.Reason, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	it.Ledger = json.RawMessage(ledger)
	if it.AttemptAt, err = parseTaskTime(attempt); err != nil {
		return it, err
	}
	if it.NextCheckAt, err = parseTaskTime(next); err != nil {
		return it, err
	}
	it.UpdatedAt, err = parseTaskTime(updated)
	return it, err
}

// ReadBrainstormTaskBatch reads one batch.
func (db *DB) ReadBrainstormTaskBatch(ctx context.Context, id string) (BrainstormTaskBatch, error) {
	return scanTaskBatch(db.sql.QueryRowContext(ctx, batchSelect+` WHERE id=?`, id))
}

// RunningBrainstormTaskBatches lists running batches oldest first, so settlement
// and pending summaries resume after a restart even when no item is active.
func (db *DB) RunningBrainstormTaskBatches(ctx context.Context, limit int) ([]BrainstormTaskBatch, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := db.sql.QueryContext(ctx, batchSelect+` WHERE state='running' ORDER BY created_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrainstormTaskBatch
	for rows.Next() {
		b, err := scanTaskBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BrainstormTaskBatchForDraftSet returns the batch of a draft set; ErrNotFound when none.
func (db *DB) BrainstormTaskBatchForDraftSet(ctx context.Context, draftSetID string) (BrainstormTaskBatch, error) {
	return scanTaskBatch(db.sql.QueryRowContext(ctx, batchSelect+` WHERE draft_set_id=?`, draftSetID))
}

// BrainstormTaskContinuationByExecution finds the continuation of one confirmed proposal.
func (db *DB) BrainstormTaskContinuationByExecution(ctx context.Context, executionID string) (BrainstormTaskContinuation, error) {
	return scanTaskContinuation(db.sql.QueryRowContext(ctx, continuationSelect+` WHERE execution_id=?`, executionID))
}

// BrainstormTaskContinuations lists a batch's continuations oldest first.
func (db *DB) BrainstormTaskContinuations(ctx context.Context, batchID string) ([]BrainstormTaskContinuation, error) {
	rows, err := db.sql.QueryContext(ctx, continuationSelect+` WHERE batch_id=? ORDER BY created_at, id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrainstormTaskContinuation
	for rows.Next() {
		c, err := scanTaskContinuation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) queryTaskItems(ctx context.Context, q string, args ...any) ([]BrainstormTaskItem, error) {
	rows, err := db.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrainstormTaskItem
	for rows.Next() {
		it, err := scanTaskItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// BrainstormTaskItems lists a batch's items by ordinal.
func (db *DB) BrainstormTaskItems(ctx context.Context, batchID string) ([]BrainstormTaskItem, error) {
	return db.queryTaskItems(ctx, itemSelect+` WHERE batch_id=? ORDER BY ordinal`, batchID)
}

// ReadBrainstormTaskItem reads one item.
func (db *DB) ReadBrainstormTaskItem(ctx context.Context, id string) (BrainstormTaskItem, error) {
	return scanTaskItem(db.sql.QueryRowContext(ctx, itemSelect+` WHERE id=?`, id))
}

// ActiveBrainstormTaskItems lists items of running batches that still need work.
func (db *DB) ActiveBrainstormTaskItems(ctx context.Context, limit int) ([]BrainstormTaskItem, error) {
	if limit < 1 {
		limit = 100
	}
	return db.queryTaskItems(ctx, itemSelect+` WHERE batch_id IN (SELECT id FROM brainstorm_task_batches WHERE state='running') AND state IN ('pending','creating','uncertain','created','plan_starting') ORDER BY batch_id, ordinal LIMIT ?`, limit)
}

// SaveBrainstormTaskItem writes every mutable field when the item is still in
// fromState at the same generation. The commit is the durable boundary.
func (db *DB) SaveBrainstormTaskItem(ctx context.Context, it BrainstormTaskItem, fromState string, at time.Time) (bool, error) {
	ledger := string(it.Ledger)
	if ledger == "" {
		ledger = "{}"
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_task_items SET title=?,body=?,marker=?,ledger=?,ledger_digest=?,state=?,issue_id=?,issue_number=?,issue_link=?,session_id=?,attempt_at=?,checks_done=?,next_check_at=?,create_retries=?,failed_launch_attempts=?,reason=?,updated_at=? WHERE id=? AND state=? AND generation=?`,
		it.Title, it.Body, it.Marker, ledger, it.LedgerDigest, it.State, it.IssueID, it.IssueNumber, it.IssueLink, it.SessionID, taskTime(it.AttemptAt), it.ChecksDone, taskTime(it.NextCheckAt), it.CreateRetries, it.FailedLaunchAttempts, it.Reason, taskTime(at), it.ID, fromState, it.Generation)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SettleBrainstormTaskContinuation marks one continuation settled.
func (db *DB) SettleBrainstormTaskContinuation(ctx context.Context, id string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_task_continuations SET state='settled',settled_at=? WHERE id=? AND state='running'`, taskTime(at), id)
	return err
}

// SetBrainstormTaskContinuationSummary records the summary delivery state.
func (db *DB) SetBrainstormTaskContinuationSummary(ctx context.Context, id, summaryState string) error {
	if summaryState != "pending" && summaryState != "sent" && summaryState != "no_channel" {
		return ErrBrainstormTaskInvalid
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_task_continuations SET summary_state=? WHERE id=?`, summaryState, id)
	return err
}

// SettleBrainstormTaskBatch marks a batch settled when no continuation is running.
func (db *DB) SettleBrainstormTaskBatch(ctx context.Context, id string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_task_batches SET state='settled',settled_at=? WHERE id=? AND state='running' AND NOT EXISTS(SELECT 1 FROM brainstorm_task_continuations WHERE batch_id=? AND state='running')`, taskTime(at), id, id)
	return err
}
