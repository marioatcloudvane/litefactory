package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const graphInstanceColumns = `id,session_id,definition_id,status,current_node,next_due_at,started_at,finished_at`

// CreateGraphInstance creates running progress against a pinned definition.
func (db *DB) CreateGraphInstance(ctx context.Context, in GraphInstance) (out GraphInstance, err error) {
	if in.Status == "" {
		in.Status = GraphRunning
	}
	if in.SessionID <= 0 || in.DefinitionID <= 0 || in.CurrentNode == "" || in.StartedAt.IsZero() || in.Status != GraphRunning || !in.FinishedAt.IsZero() {
		return out, graphInvariant("new instance needs a session, definition, current node and start time, and must be running")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,next_due_at,started_at) VALUES(?,?,?,?,?,?) RETURNING `+graphInstanceColumns, in.SessionID, in.DefinitionID, in.Status, in.CurrentNode, nullTime(in.NextDueAt), FormatTime(in.StartedAt)))
		return e
	})
	if err != nil {
		return GraphInstance{}, fmt.Errorf("store: create graph instance: %w", err)
	}
	return out, nil
}

// GetGraphInstance loads durable current-node progress.
func (db *DB) GetGraphInstance(ctx context.Context, id int64) (GraphInstance, error) {
	out, err := scanGraphInstance(db.sql.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, id))
	if err != nil {
		return GraphInstance{}, fmt.Errorf("store: get graph instance: %w", err)
	}
	return out, nil
}

// SetGraphInstanceStatus changes running progress or terminalizes it. Terminal
// instances cannot be revived. Cancellation with an in-flight attempt requires
// explicit attempt finalization; automatic reconciliation is future wiring.
func (db *DB) SetGraphInstanceStatus(ctx context.Context, id int64, status string, nextDue, finished time.Time) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, id))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning {
			return ErrGraphInstanceTerminal
		}
		if status == GraphSucceeded {
			var waiting int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')`, id).Scan(&waiting); e != nil {
				return e
			}
			if waiting > 0 {
				return ErrGraphOwnerWait
			}
		}
		return setGraphInstanceTx(ctx, tx, id, instance.CurrentNode, status, nextDue, finished)
	})
	if err != nil {
		return fmt.Errorf("store: set graph instance status: %w", err)
	}
	return nil
}

func setGraphInstanceTx(ctx context.Context, tx *sql.Tx, id int64, node, status string, nextDue, finished time.Time) error {
	if node == "" || (status != GraphRunning && !graphTerminal(status)) || (status == GraphRunning && !finished.IsZero()) || (graphTerminal(status) && (finished.IsZero() || !nextDue.IsZero())) {
		return graphInvariant("invalid instance status or time combination")
	}
	_, err := tx.ExecContext(ctx, `UPDATE graph_instances SET current_node=?,status=?,next_due_at=?,finished_at=? WHERE id=?`, node, status, nullTime(nextDue), nullTime(finished), id)
	return err
}

// ListDueGraphInstances selects bounded running instances in due-time/ID order.
// No clock is read; limit must be positive.
func (db *DB) ListDueGraphInstances(ctx context.Context, now time.Time, limit int) ([]GraphInstance, error) {
	if limit <= 0 {
		return nil, graphInvariant("due selection limit must be positive")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE status='running' AND next_due_at IS NOT NULL AND next_due_at<=? ORDER BY next_due_at,id LIMIT ?`, FormatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due graph instances: %w", err)
	}
	defer rows.Close()
	out := make([]GraphInstance, 0)
	for rows.Next() {
		v, e := scanGraphInstance(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan due graph instance: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list due graph instances: %w", err)
	}
	return out, nil
}

func scanGraphInstance(row scanner) (GraphInstance, error) {
	var v GraphInstance
	var started string
	var due, finished sql.NullString
	if err := row.Scan(&v.ID, &v.SessionID, &v.DefinitionID, &v.Status, &v.CurrentNode, &due, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, ErrNotFound
		}
		return v, err
	}
	var err error
	if v.StartedAt, err = ParseTime(started); err != nil {
		return v, err
	}
	if v.NextDueAt, err = scanTime(due); err != nil {
		return v, err
	}
	v.FinishedAt, err = scanTime(finished)
	return v, err
}

// ListPinnedGraphInstances restricts restart recovery to the supported exact
// profile; old executions remain untouched even when they owe work.
func (db *DB) ListPinnedGraphInstances(ctx context.Context, digest string) ([]GraphInstance, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE definition_id IN (SELECT id FROM graph_definitions WHERE workflow='bug' AND digest=?) AND status IN ('running','succeeded') ORDER BY id`, digest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphInstance
	for rows.Next() {
		v, e := scanGraphInstance(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListStandaloneBugInstances selects retained Bug work independently of current package bytes.
func (db *DB) ListStandaloneBugInstances(ctx context.Context) ([]GraphInstance, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE definition_id IN (SELECT id FROM graph_definitions WHERE workflow='bug') AND status IN ('running','succeeded') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphInstance
	for rows.Next() {
		v, e := scanGraphInstance(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListGraphOwnerWaitSessions discovers live waits even without a due time or
// polling backlog. Selection is workflow independent; closure is checked later.
func (db *DB) ListGraphOwnerWaitSessions(ctx context.Context) ([]Session, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id IN (
SELECT i.session_id FROM graph_instances i JOIN graph_owner_waits w ON w.instance_id=i.id
WHERE i.status='running' AND w.state IN ('waiting','claimed')) ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list graph owner wait sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}
