package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrStandardsHandoffPersistence isolates advisory provenance persistence failure from the story transition.
var ErrStandardsHandoffPersistence = errors.New("standards: work handoff persistence failure")

// StandardsRunBinding retains the exact canonical book for one owner invocation.
type StandardsRunBinding struct {
	RootRunID     string    `json:"root_run_id"`
	RevisionID    int64     `json:"revision_id"`
	Source        string    `json:"source"`
	SchemaVersion int       `json:"schema_version"`
	Digest        string    `json:"digest"`
	SnapshotBytes []byte    `json:"-"`
	Canonical     string    `json:"canonical,omitempty"`
	Outcome       string    `json:"outcome"`
	Reason        string    `json:"reason,omitempty"`
	CapturedAt    time.Time `json:"captured_at"`
}

// StandardsRunSelection is an immutable additive choice by one logical consumer.
type StandardsRunSelection struct {
	RootRunID   string    `json:"root_run_id"`
	ConsumerKey string    `json:"consumer_key"`
	Version     int64     `json:"version"`
	Digest      string    `json:"digest"`
	CategoryIDs []string  `json:"category_ids"`
	StandardIDs []string  `json:"standard_ids"`
	Reason      string    `json:"reason"`
	AttemptID   string    `json:"attempt_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// StandardsRunObservation records verification, offered ranges and agent reports.
type StandardsRunObservation struct {
	RootRunID        string    `json:"root_run_id"`
	ConsumerKey      string    `json:"consumer_key"`
	AttemptID        string    `json:"attempt_id"`
	Kind             string    `json:"kind"`
	Outcome          string    `json:"outcome"`
	Reason           string    `json:"reason,omitempty"`
	SelectionVersion int64     `json:"selection_version"`
	StandardID       string    `json:"standard_id,omitempty"`
	Start            int       `json:"start"`
	End              int       `json:"end"`
	Total            int       `json:"total"`
	CreatedAt        time.Time `json:"created_at"`
}

func readRunBinding(ctx context.Context, tx *sql.Tx, root string) (StandardsRunBinding, error) {
	var b StandardsRunBinding
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT binding_json FROM standards_run_bindings WHERE root_run_id=?`, root).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(raw), &b)
	if err == nil && b.RootRunID != root {
		return b, ErrStandardsCorrupt
	}
	b.SnapshotBytes = []byte(b.Canonical)
	if err != nil {
		return b, fmt.Errorf("%w: malformed run binding", ErrStandardsCorrupt)
	}
	return b, nil
}

// GetStandardsRunBinding reads without initializing or repairing a binding.
func (db *DB) GetStandardsRunBinding(ctx context.Context, root string) (StandardsRunBinding, error) {
	var b StandardsRunBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; b, e = readRunBinding(ctx, tx, root); return e })
	return b, err
}

// SaveStandardsRunBinding returns the immutable winner of racing initializations.
func (db *DB) SaveStandardsRunBinding(ctx context.Context, b StandardsRunBinding) (StandardsRunBinding, error) {
	if b.RootRunID == "" {
		return b, errors.New("standards: root required")
	}
	b.Canonical = string(b.SnapshotBytes)
	b.CapturedAt = nowUTC()
	raw, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	err = db.standardsWrite(ctx, func(tx *sql.Tx) error {
		if lease, ok := ctx.Value(preventionWriterKey{}).(preventionWriterLease); ok && b.RootRunID != "analysis:"+lease.id {
			return ErrPreventionConflict
		}
		if e := checkPreventionLease(ctx, tx); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO standards_run_bindings(root_run_id,binding_json) VALUES(?,?) ON CONFLICT(root_run_id) DO NOTHING`, b.RootRunID, string(raw)); e != nil {
			return e
		}
		var e error
		b, e = readRunBinding(ctx, tx, b.RootRunID)
		return e
	})
	return b, err
}
func readRunSelections(ctx context.Context, tx *sql.Tx, root, consumer string) ([]StandardsRunSelection, error) {
	out := []StandardsRunSelection{}
	rows, err := tx.QueryContext(ctx, `SELECT consumer_key,version,selection_json FROM standards_run_selections WHERE root_run_id=? AND (?='' OR consumer_key=?) ORDER BY consumer_key,version`, root, consumer, consumer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var s StandardsRunSelection
		var rowConsumer string
		var rowVersion int64
		if err = rows.Scan(&rowConsumer, &rowVersion, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, fmt.Errorf("%w: malformed retained selection", ErrStandardsCorrupt)
		}
		if s.RootRunID != root || s.ConsumerKey != rowConsumer || s.Version != rowVersion {
			return nil, fmt.Errorf("%w: selection row identity mismatch", ErrStandardsCorrupt)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListStandardsRunSelections returns retained versions in stable order.
func (db *DB) ListStandardsRunSelections(ctx context.Context, root, consumer string) ([]StandardsRunSelection, error) {
	var out []StandardsRunSelection
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = readRunSelections(ctx, tx, root, consumer); return e })
	return out, err
}

// SaveStandardsRunSelection atomically checks expected version and additive categories.
func (db *DB) SaveStandardsRunSelection(ctx context.Context, s StandardsRunSelection, expected int64) (StandardsRunSelection, error) {
	s.CreatedAt = nowUTC()
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		if lease, ok := ctx.Value(preventionWriterKey{}).(preventionWriterLease); ok && s.RootRunID != "analysis:"+lease.id {
			return ErrPreventionConflict
		}
		if e := checkStandardsRunLease(ctx, tx, s.AttemptID); e != nil {
			return e
		}
		all, e := readRunSelections(ctx, tx, s.RootRunID, s.ConsumerKey)
		if e != nil {
			return e
		}
		var current StandardsRunSelection
		if len(all) > 0 {
			current = all[len(all)-1]
		}
		if current.Version > 0 && current.Digest == s.Digest {
			s = current
			return nil
		}
		if current.Version != expected {
			return ErrStandardsConflict
		}
		ids := map[string]bool{}
		for _, id := range s.CategoryIDs {
			ids[id] = true
		}
		for _, id := range current.CategoryIDs {
			if !ids[id] {
				return ErrStandardsConflict
			}
		}
		s.Version = expected + 1
		raw, e := json.Marshal(s)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO standards_run_selections(root_run_id,consumer_key,version,selection_json) VALUES(?,?,?,?)`, s.RootRunID, s.ConsumerKey, s.Version, string(raw))
		return e
	})
	return s, err
}

// AppendStandardsRunObservation adds one durable observation without storing bodies.
func (db *DB) AppendStandardsRunObservation(ctx context.Context, o StandardsRunObservation) error {
	o.CreatedAt = nowUTC()
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		if lease, ok := ctx.Value(preventionWriterKey{}).(preventionWriterLease); ok && o.RootRunID != "analysis:"+lease.id {
			return ErrPreventionConflict
		}
		if e := checkStandardsRunLease(ctx, tx, o.AttemptID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO standards_run_observations(root_run_id,consumer_key,observation_json) VALUES(?,?,?)`, o.RootRunID, o.ConsumerKey, string(raw))
		return e
	})
}

// ListStandardsRunObservations returns retained observations for inspection.
func (db *DB) ListStandardsRunObservations(ctx context.Context, root, consumer string) ([]StandardsRunObservation, error) {
	out := []StandardsRunObservation{}
	rows, err := db.sql.QueryContext(ctx, `SELECT observation_json FROM standards_run_observations WHERE root_run_id=? AND (?='' OR consumer_key=?) ORDER BY id`, root, consumer, consumer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var o StandardsRunObservation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ResolveStandardsRoot follows recovery ownership and graph owner-request boundaries.
func (db *DB) ResolveStandardsRoot(ctx context.Context, runID int64) (int64, error) {
	root := runID
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var alias int64
		aliasErr := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_run_aliases WHERE run_id=?`, root).Scan(&alias)
		if aliasErr == nil {
			root = alias
			return nil
		} else if !errors.Is(aliasErr, sql.ErrNoRows) {
			return aliasErr
		}
		var recovery int64
		e := tx.QueryRowContext(ctx, `SELECT c.root_run_id FROM automatic_continuations c JOIN runs r ON r.id=? AND r.session_id=c.session_id WHERE c.allocated_run_id=r.id OR r.trigger_comment_id=?+c.id`, root, automaticTriggerBase).Scan(&recovery)
		if e == nil {
			root = recovery
			var mapped int64
			lookup := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_run_aliases WHERE run_id=?`, root).Scan(&mapped)
			if lookup == nil {
				root = mapped
				return nil
			}
			if !errors.Is(lookup, sql.ErrNoRows) {
				return lookup
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var session, trigger int64
		if e = tx.QueryRowContext(ctx, `SELECT session_id,trigger_comment_id FROM runs WHERE id=?`, root).Scan(&session, &trigger); e != nil {
			return e
		}
		var launch int64
		e = tx.QueryRowContext(ctx, `SELECT id FROM research_launch_definitions WHERE session_id=? AND trigger_id=?`, session, trigger).Scan(&launch)
		if errors.Is(e, sql.ErrNoRows) {
			if dispatch, ok := GraphTriggerDispatch(trigger); ok {
				e = tx.QueryRowContext(ctx, `SELECT launch_id FROM graph_dispatches WHERE id=? AND session_id=?`, dispatch, session).Scan(&launch)
			} else {
				e = tx.QueryRowContext(ctx, `SELECT launch_id FROM graph_node_attempts WHERE run_id=? ORDER BY id LIMIT 1`, root).Scan(&launch)
			}
		}
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		graphRoot, graphErr := readStandardsGraphInvocation(ctx, tx, launch)
		if graphErr == nil {
			root = graphRoot
			return nil
		}
		if !errors.Is(graphErr, ErrNotFound) {
			return graphErr
		}
		var first sql.NullInt64
		e = tx.QueryRowContext(ctx, `SELECT MIN(r.id) FROM runs r WHERE r.session_id=? AND (r.trigger_comment_id=(SELECT trigger_id FROM research_launch_definitions WHERE id=?) OR r.trigger_comment_id IN (SELECT ?+id FROM graph_dispatches WHERE launch_id=?) OR r.id IN (SELECT allocated_run_id FROM graph_dispatches WHERE launch_id=?) OR r.id IN (SELECT run_id FROM graph_node_attempts WHERE launch_id=?) OR r.id IN (SELECT run_id FROM standards_graph_run_aliases WHERE launch_id=?))`, session, launch, graphTriggerBase, launch, launch, launch, launch).Scan(&first)
		if e == nil && first.Valid {
			root = first.Int64
		}
		return e
	})
	return root, err
}

// LinkStandardsRun retains a trusted launch's invocation identity without changing its book.
func (db *DB) LinkStandardsRun(ctx context.Context, child, root int64) error {
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO standards_run_aliases(run_id,root_run_id) VALUES(?,?) ON CONFLICT(run_id) DO NOTHING`, child, root); e != nil {
			return e
		}
		var retained int64
		if e := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_run_aliases WHERE run_id=?`, child).Scan(&retained); e != nil {
			return e
		}
		if retained != root {
			return ErrStandardsConflict
		}
		return nil
	})
}

// LinkStandardsGraphRun binds an auxiliary agent to its trusted graph attempt's launch.
func (db *DB) LinkStandardsGraphRun(ctx context.Context, runID, graphAttemptID int64) error {
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var launch int64
		if e := tx.QueryRowContext(ctx, `SELECT launch_id FROM graph_node_attempts WHERE id=?`, graphAttemptID).Scan(&launch); e != nil {
			return e
		}
		var session, trigger int64
		if e := tx.QueryRowContext(ctx, `SELECT session_id,trigger_id FROM research_launch_definitions WHERE id=?`, launch).Scan(&session, &trigger); e != nil {
			return e
		}
		var root sql.NullInt64
		if e := tx.QueryRowContext(ctx, `SELECT MIN(id) FROM runs WHERE session_id=? AND (trigger_comment_id=? OR trigger_comment_id IN (SELECT ?+id FROM graph_dispatches WHERE launch_id=?) OR id IN (SELECT allocated_run_id FROM graph_dispatches WHERE launch_id=?) OR id IN (SELECT run_id FROM graph_node_attempts WHERE launch_id=?) OR id IN (SELECT run_id FROM standards_graph_run_aliases WHERE launch_id=?))`, session, trigger, graphTriggerBase, launch, launch, launch, launch).Scan(&root); e != nil {
			return e
		}
		graphRoot, graphErr := readStandardsGraphInvocation(ctx, tx, launch)
		if graphErr == nil {
			root.Int64 = graphRoot
			root.Valid = true
		} else if !errors.Is(graphErr, ErrNotFound) {
			return graphErr
		}
		if !root.Valid {
			root.Int64 = runID
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO standards_graph_run_aliases(run_id,launch_id) VALUES(?,?) ON CONFLICT(run_id) DO NOTHING`, runID, launch); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO standards_run_aliases(run_id,root_run_id) VALUES(?,?) ON CONFLICT(run_id) DO NOTHING`, runID, root.Int64); e != nil {
			return e
		}
		var existing int64
		if e := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_run_aliases WHERE run_id=?`, runID).Scan(&existing); e != nil {
			return e
		}
		if existing != root.Int64 {
			return ErrStandardsConflict
		}
		return nil
	})
}

func checkStandardsRunLease(ctx context.Context, tx *sql.Tx, attempt string) error {
	if lease, ok := ctx.Value(preventionWriterKey{}).(preventionWriterLease); ok {
		if attempt != lease.attempt {
			return ErrPreventionConflict
		}
		if err := checkPreventionLease(ctx, tx); err != nil {
			return err
		}
	}
	if lease, ok := ctx.Value(mcpLeaseKey{}).(MCPLease); ok {
		if attempt != lease.Binding.Attempt {
			return ErrResultAuthority
		}
		return checkResultAccess(ctx, tx, lease.Binding, true)
	}
	return nil
}

func readStandardsGraphInvocation(ctx context.Context, tx *sql.Tx, launch int64) (int64, error) {
	var root int64
	err := tx.QueryRowContext(ctx, `SELECT s.root_run_id FROM standards_graph_invocations s JOIN research_launch_definitions l ON l.session_id=s.session_id AND l.trigger_id=s.source_trigger WHERE l.id=?`, launch).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err == nil && root == 0 {
		return 0, ErrStandardsCorrupt
	}
	return root, err
}

// RecordStandardsGraphInvocation binds a trusted launch to an inherited root, or allocates a stable negative mechanical invocation anchor when root is zero.
func (db *DB) RecordStandardsGraphInvocation(ctx context.Context, launch, root int64) (int64, error) {
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var session, trigger int64
		if err := tx.QueryRowContext(ctx, `SELECT session_id,trigger_id FROM research_launch_definitions WHERE id=?`, launch).Scan(&session, &trigger); err != nil {
			return err
		}
		var err error
		root, err = recordStandardsInvocationKey(ctx, tx, session, trigger, root)
		return err
	})
	return root, err
}
func readStandardsInvocationKey(ctx context.Context, tx *sql.Tx, session, trigger int64) (int64, error) {
	var root int64
	err := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_graph_invocations WHERE session_id=? AND source_trigger=?`, session, trigger).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err == nil && root == 0 {
		return 0, ErrStandardsCorrupt
	}
	return root, err
}
func recordStandardsInvocationKey(ctx context.Context, tx *sql.Tx, session, trigger, root int64) (int64, error) {
	current, err := readStandardsInvocationKey(ctx, tx, session, trigger)
	if err == nil {
		if root != 0 && root != current {
			return 0, ErrStandardsConflict
		}
		return current, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO standards_graph_invocations(session_id,source_trigger,root_run_id) VALUES(?,?,?) ON CONFLICT(session_id,source_trigger) DO NOTHING`, session, trigger, root)
	if err != nil {
		return 0, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		current, err = readStandardsInvocationKey(ctx, tx, session, trigger)
		if err != nil {
			return 0, err
		}
		if root != 0 && root != current {
			return 0, ErrStandardsConflict
		}
		return current, nil
	}
	if root == 0 {
		id, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		root = -id
		if _, err = tx.ExecContext(ctx, `UPDATE standards_graph_invocations SET root_run_id=? WHERE id=? AND root_run_id=0`, root, id); err != nil {
			return 0, err
		}
	}
	return root, nil
}

// RecordStandardsScheduledInvocation freezes a trusted factory schedule's source.
// A missing source becomes a durable unavailable binding without reading current.
func (db *DB) RecordStandardsScheduledInvocation(ctx context.Context, session, trigger, root int64) (int64, error) {
	missing := root == 0
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		if !IsScheduleTrigger(trigger) {
			return ErrStandardsConflict
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedules WHERE session_id=? AND id=?)`, session, trigger-scheduleTriggerBase).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var err error
		root, err = recordStandardsInvocationKey(ctx, tx, session, trigger, root)
		if err != nil {
			return err
		}
		if missing {
			binding := StandardsRunBinding{RootRunID: fmt.Sprint(root), Source: "local-published-book", Outcome: "unavailable", Reason: "factory worker schedule has no retained source invocation; current book will not be substituted", CapturedAt: nowUTC()}
			raw, err := json.Marshal(binding)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO standards_run_bindings(root_run_id,binding_json) VALUES(?,?) ON CONFLICT(root_run_id) DO NOTHING`, binding.RootRunID, string(raw))
			return err
		}
		return nil
	})
	return root, err
}

// GetStandardsScheduledInvocation reads only the exact frozen private schedule source.
func (db *DB) GetStandardsScheduledInvocation(ctx context.Context, session, trigger int64) (int64, error) {
	if !IsScheduleTrigger(trigger) {
		return 0, ErrNotFound
	}
	var root int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		root, err = readStandardsInvocationKey(ctx, tx, session, trigger)
		return err
	})
	return root, err
}

// GetStandardsGraphInvocation resolves only a retained trusted launch association.
func (db *DB) GetStandardsGraphInvocation(ctx context.Context, launch int64) (int64, error) {
	var root int64
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; root, e = readStandardsGraphInvocation(ctx, tx, launch); return e })
	return root, err
}
func saveStandardsStoryInvocationTx(ctx context.Context, tx *sql.Tx, parent string, generation int64, key string, stateAt time.Time, root int64) error {
	if root == 0 {
		return errors.New("standards: invocation root is required")
	}
	stamp := FormatTime(stateAt)
	if _, err := tx.ExecContext(ctx, `INSERT INTO standards_story_invocations(parent,generation,story_key,state_at,root_run_id) VALUES(?,?,?,?,?) ON CONFLICT(parent,generation,story_key,state_at) DO NOTHING`, parent, generation, key, stamp, root); err != nil {
		return err
	}
	var retained int64
	if err := tx.QueryRowContext(ctx, `SELECT root_run_id FROM standards_story_invocations WHERE parent=? AND generation=? AND story_key=? AND state_at=?`, parent, generation, key, stamp).Scan(&retained); err != nil {
		return err
	}
	if retained != root {
		return ErrStandardsConflict
	}
	return nil
}

// SaveStandardsStoryInvocation retains immutable factory work-item provenance.
func (db *DB) SaveStandardsStoryInvocation(ctx context.Context, parent string, generation int64, key string, stateAt time.Time, root int64) error {
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		return saveStandardsStoryInvocationTx(ctx, tx, parent, generation, key, stateAt, root)
	})
}

// GetStandardsStoryInvocation reads the exact work item's source invocation, never a latest parent run.
func (db *DB) GetStandardsStoryInvocation(ctx context.Context, parent string, generation int64, key string, stateAt time.Time) (int64, error) {
	var root int64
	err := db.sql.QueryRowContext(ctx, `SELECT root_run_id FROM standards_story_invocations WHERE parent=? AND generation=? AND story_key=? AND state_at=?`, parent, generation, key, FormatTime(stateAt)).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil && root == 0 {
		return 0, ErrStandardsCorrupt
	}
	return root, err
}

// GetStandardsGraphInstanceInvocation resolves the retained source of the latest
// admitted owner launch within one trusted failed worker instance.
func (db *DB) GetStandardsGraphInstanceInvocation(ctx context.Context, instance int64) (int64, error) {
	var root int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var launch int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM research_launch_definitions WHERE instance_id=? ORDER BY id DESC LIMIT 1`, instance).Scan(&launch)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		root, err = readStandardsGraphInvocation(ctx, tx, launch)
		return err
	})
	return root, err
}
