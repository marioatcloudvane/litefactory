package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// brainstormLaunchTriggerBase reserves the factory-private run identities of
// Plan launches a confirmed brainstorm starts. Each identity is the band plus
// one launch attempt id, so a permitted retry gets its own run.
const brainstormLaunchTriggerBase int64 = 1 << 57

// IsBrainstormLaunchTrigger reports whether an identity belongs to a brainstorm
// Plan launch attempt.
func IsBrainstormLaunchTrigger(id int64) bool {
	return id > brainstormLaunchTriggerBase && id < planMemberTriggerBase
}

// BrainstormLaunchTrigger returns the private run identity of one attempt.
func BrainstormLaunchTrigger(attemptID int64) int64 { return brainstormLaunchTriggerBase + attemptID }

// BrainstormLaunchAttemptID inverts BrainstormLaunchTrigger.
func BrainstormLaunchAttemptID(trigger int64) (int64, bool) {
	if !IsBrainstormLaunchTrigger(trigger) {
		return 0, false
	}
	return trigger - brainstormLaunchTriggerBase, true
}

// Launch attempt states and outcomes.
const (
	BrainstormLaunchPending   = "pending"
	BrainstormLaunchReserved  = "reserved"
	BrainstormLaunchRunning   = "running"
	BrainstormLaunchSucceeded = "succeeded"
	BrainstormLaunchFailed    = "failed"

	BrainstormLaunchOutcomeReplayed = "replayed"
	BrainstormLaunchOutcomeDeferred = "deferred"
	BrainstormLaunchOutcomeFailed   = "failed"
	BrainstormLaunchOutcomeStarted  = "started"
)

// Brief seed states.
const (
	BrainstormSeedPending         = "pending"
	BrainstormSeedWriting         = "writing"
	BrainstormSeedComplete        = "complete"
	BrainstormSeedSkippedExisting = "skipped_existing"
)

// BrainstormLaunchAttempt is one permitted start of Plan for a created item.
type BrainstormLaunchAttempt struct {
	ID                   int64
	ItemID               string
	Ordinal              int
	SessionID            int64
	State                string
	RunID                int64
	Outcome, Reason      string
	CreatedAt, UpdatedAt time.Time
}

// TriggerID is the attempt's private run identity.
func (a BrainstormLaunchAttempt) TriggerID() int64 { return BrainstormLaunchTrigger(a.ID) }

// Open reports whether the attempt still counts against the one-open rule.
func (a BrainstormLaunchAttempt) Open() bool {
	return a.State == BrainstormLaunchPending || a.State == BrainstormLaunchReserved || a.State == BrainstormLaunchRunning
}

// BrainstormPlanLaunchSeed binds an item's frozen ledger to an admitted Plan launch.
type BrainstormPlanLaunchSeed struct {
	LaunchID                     int64
	ItemID                       string
	AttemptID                    int64
	LedgerDigest                 string
	BriefDigest, TechnicalDigest string
	SessionDir, State, Reason    string
	CompletedAt, CreatedAt       time.Time
}

const brainstormLaunchAttemptColumns = `id,item_id,ordinal,session_id,state,run_id,outcome,reason,created_at,updated_at`

func scanBrainstormLaunchAttempt(row interface{ Scan(...any) error }) (BrainstormLaunchAttempt, error) {
	var a BrainstormLaunchAttempt
	var created, updated string
	err := row.Scan(&a.ID, &a.ItemID, &a.Ordinal, &a.SessionID, &a.State, &a.RunID, &a.Outcome, &a.Reason, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.CreatedAt, _ = ParseTime(created)
	a.UpdatedAt, _ = ParseTime(updated)
	return a, nil
}

const brainstormSeedColumns = `launch_id,item_id,attempt_id,ledger_digest,brief_digest,technical_digest,session_dir,state,reason,completed_at,created_at`

func scanBrainstormSeed(row interface{ Scan(...any) error }) (BrainstormPlanLaunchSeed, error) {
	var s BrainstormPlanLaunchSeed
	var completed, created string
	err := row.Scan(&s.LaunchID, &s.ItemID, &s.AttemptID, &s.LedgerDigest, &s.BriefDigest, &s.TechnicalDigest, &s.SessionDir, &s.State, &s.Reason, &completed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if completed != "" {
		s.CompletedAt, _ = ParseTime(completed)
	}
	s.CreatedAt, _ = ParseTime(created)
	return s, nil
}

// BrainstormLaunchAttempts lists an item's attempts in ordinal order.
func (db *DB) BrainstormLaunchAttempts(ctx context.Context, itemID string) ([]BrainstormLaunchAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+brainstormLaunchAttemptColumns+` FROM brainstorm_task_launch_attempts WHERE item_id=? ORDER BY ordinal`, itemID)
	if err != nil {
		return nil, fmt.Errorf("store: brainstorm launch attempts: %w", err)
	}
	defer rows.Close()
	var out []BrainstormLaunchAttempt
	for rows.Next() {
		a, err := scanBrainstormLaunchAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReadBrainstormLaunchAttempt reads one attempt.
func (db *DB) ReadBrainstormLaunchAttempt(ctx context.Context, id int64) (BrainstormLaunchAttempt, error) {
	return scanBrainstormLaunchAttempt(db.sql.QueryRowContext(ctx, `SELECT `+brainstormLaunchAttemptColumns+` FROM brainstorm_task_launch_attempts WHERE id=?`, id))
}

// OpenBrainstormLaunchAttempt returns the item's open attempt, or opens a new
// pending one with the next ordinal. created is false when one was open.
func (db *DB) OpenBrainstormLaunchAttempt(ctx context.Context, itemID string, sessionID int64, at time.Time) (a BrainstormLaunchAttempt, created bool, err error) {
	if itemID == "" || sessionID < 1 {
		return a, false, errors.New("store: open brainstorm launch attempt: item and session are required")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		open, e := scanBrainstormLaunchAttempt(tx.QueryRowContext(ctx, `SELECT `+brainstormLaunchAttemptColumns+` FROM brainstorm_task_launch_attempts WHERE item_id=? AND state IN ('pending','reserved','running')`, itemID))
		if e == nil {
			a = open
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var next int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM brainstorm_task_launch_attempts WHERE item_id=?`, itemID).Scan(&next); e != nil {
			return e
		}
		stamp := FormatTime(at.UTC())
		a, e = scanBrainstormLaunchAttempt(tx.QueryRowContext(ctx, `INSERT INTO brainstorm_task_launch_attempts(item_id,ordinal,session_id,state,created_at,updated_at) VALUES(?,?,?,'pending',?,?) RETURNING `+brainstormLaunchAttemptColumns, itemID, next, sessionID, stamp, stamp))
		created = e == nil
		return e
	})
	if err != nil {
		return BrainstormLaunchAttempt{}, false, fmt.Errorf("store: open brainstorm launch attempt: %w", err)
	}
	return a, created, nil
}

// MoveBrainstormLaunchAttempt changes an attempt's state when it is still in
// from. Closing states (succeeded, failed) only leave open states; a closed
// attempt never reopens. It reports whether the row changed.
func (db *DB) MoveBrainstormLaunchAttempt(ctx context.Context, id int64, from, to, outcome string, runID int64, reason string, at time.Time) (bool, error) {
	allowed := map[string][]string{
		BrainstormLaunchPending:  {BrainstormLaunchReserved},
		BrainstormLaunchReserved: {BrainstormLaunchRunning, BrainstormLaunchPending, BrainstormLaunchSucceeded, BrainstormLaunchFailed},
		BrainstormLaunchRunning:  {BrainstormLaunchPending, BrainstormLaunchSucceeded, BrainstormLaunchFailed},
	}
	ok := false
	for _, s := range allowed[from] {
		ok = ok || s == to
	}
	if !ok {
		return false, fmt.Errorf("store: brainstorm launch attempt cannot move from %s to %s", from, to)
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_task_launch_attempts SET state=?,outcome=CASE WHEN ?='' THEN outcome ELSE ? END,run_id=CASE WHEN ?>0 THEN ? ELSE run_id END,reason=?,updated_at=? WHERE id=? AND state=?`,
		to, outcome, outcome, runID, runID, reason, FormatTime(at.UTC()), id, from)
	if err != nil {
		return false, fmt.Errorf("store: move brainstorm launch attempt: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// BrainstormLaunchStatus is what the launch dispatch reads to decide whether an
// admitted launch has really started Plan.
type BrainstormLaunchStatus struct {
	Admitted        bool
	LaunchID        int64
	InstanceID      int64
	Seed            BrainstormPlanLaunchSeed
	HasSeed         bool
	AgentDispatched bool
}

// BrainstormLaunchState reads the admitted launch, its seed and whether any
// agent node of its instance has been dispatched, for one attempt trigger.
func (db *DB) BrainstormLaunchState(ctx context.Context, sessionID, trigger int64) (BrainstormLaunchStatus, error) {
	var st BrainstormLaunchStatus
	err := db.sql.QueryRowContext(ctx, `SELECT id,instance_id FROM research_launch_definitions WHERE session_id=? AND trigger_id=?`, sessionID, trigger).Scan(&st.LaunchID, &st.InstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("store: brainstorm launch state: %w", err)
	}
	st.Admitted = true
	st.Seed, err = scanBrainstormSeed(db.sql.QueryRowContext(ctx, `SELECT `+brainstormSeedColumns+` FROM brainstorm_plan_launch_seeds WHERE launch_id=?`, st.LaunchID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return st, err
	}
	st.HasSeed = err == nil
	err = db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=? AND node LIKE '%\_agent' ESCAPE '\')`, st.InstanceID).Scan(&st.AgentDispatched)
	if err != nil {
		return st, fmt.Errorf("store: brainstorm launch agent dispatch: %w", err)
	}
	return st, nil
}

// insertBrainstormSeedTx binds the item's frozen ledger to a Plan launch
// admitted for a launch-band trigger, inside the admission transaction. A
// replayed admission keeps the existing row.
func insertBrainstormSeedTx(ctx context.Context, tx *sql.Tx, trigger, launchID int64) error {
	attemptID, ok := BrainstormLaunchAttemptID(trigger)
	if !ok {
		return nil
	}
	var itemID, digest string
	err := tx.QueryRowContext(ctx, `SELECT a.item_id,i.ledger_digest FROM brainstorm_task_launch_attempts a JOIN brainstorm_task_items i ON i.id=a.item_id WHERE a.id=?`, attemptID).Scan(&itemID, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return graphInvariant("brainstorm launch has no attempt or item")
	}
	if err != nil {
		return err
	}
	if digest == "" {
		return graphInvariant("brainstorm launch item has no frozen ledger")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_plan_launch_seeds(launch_id,item_id,attempt_id,ledger_digest,created_at) VALUES(?,?,?,?,?) ON CONFLICT(launch_id) DO NOTHING`, launchID, itemID, attemptID, digest, FormatTime(nowUTC()))
	return err
}

// BrainstormSeedForLaunch reads the seed bound to an admitted launch.
func (db *DB) BrainstormSeedForLaunch(ctx context.Context, launchID int64) (BrainstormPlanLaunchSeed, error) {
	return scanBrainstormSeed(db.sql.QueryRowContext(ctx, `SELECT `+brainstormSeedColumns+` FROM brainstorm_plan_launch_seeds WHERE launch_id=?`, launchID))
}

// BrainstormSeedLedger returns the frozen ledger bytes and digest of the item
// a seed names. ErrNotFound when the item or its ledger is gone.
func (db *DB) BrainstormSeedLedger(ctx context.Context, itemID string) ([]byte, string, error) {
	var ledger []byte
	var digest string
	err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(ledger,''),COALESCE(ledger_digest,'') FROM brainstorm_task_items WHERE id=?`, itemID).Scan(&ledger, &digest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("store: brainstorm seed ledger: %w", err)
	}
	if len(ledger) == 0 {
		return nil, digest, ErrNotFound
	}
	return ledger, digest, nil
}

// RecordBrainstormSeedDigests records the expected file digests and directory
// before any file is written. It only moves a pending seed to writing; a seed
// already writing must carry the same digests.
func (db *DB) RecordBrainstormSeedDigests(ctx context.Context, launchID int64, dir, brief, technical string) (BrainstormPlanLaunchSeed, error) {
	var out BrainstormPlanLaunchSeed
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		s, e := scanBrainstormSeed(tx.QueryRowContext(ctx, `SELECT `+brainstormSeedColumns+` FROM brainstorm_plan_launch_seeds WHERE launch_id=?`, launchID))
		if e != nil {
			return e
		}
		if s.State != BrainstormSeedPending {
			if s.BriefDigest != brief || s.TechnicalDigest != technical || s.SessionDir != dir {
				return errors.New("seed digests differ from the recorded ones")
			}
			out = s
			return nil
		}
		out, e = scanBrainstormSeed(tx.QueryRowContext(ctx, `UPDATE brainstorm_plan_launch_seeds SET state='writing',session_dir=?,brief_digest=?,technical_digest=? WHERE launch_id=? AND state='pending' RETURNING `+brainstormSeedColumns, dir, brief, technical, launchID))
		return e
	})
	if err != nil {
		return BrainstormPlanLaunchSeed{}, fmt.Errorf("store: record brainstorm seed digests: %w", err)
	}
	return out, nil
}

// FinishBrainstormSeed records a writing seed as complete or skipped_existing.
func (db *DB) FinishBrainstormSeed(ctx context.Context, launchID int64, state, reason string, at time.Time) error {
	if state != BrainstormSeedComplete && state != BrainstormSeedSkippedExisting {
		return errors.New("store: finish brainstorm seed: invalid state")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_plan_launch_seeds SET state=?,reason=?,completed_at=? WHERE launch_id=? AND state IN ('pending','writing')`, state, reason, FormatTime(at.UTC()), launchID)
	if err != nil {
		return fmt.Errorf("store: finish brainstorm seed: %w", err)
	}
	return nil
}
