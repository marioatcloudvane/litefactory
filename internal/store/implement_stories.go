package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// States of one story in the Implement ledger.
const (
	ImplementWaiting = "waiting"
	ImplementRunning = "running"
	ImplementDone    = "done"
	ImplementMerged  = "merged"
	ImplementParked  = "parked"
)

// MaxImplementRunning is the most stories one parent may have running at once.
const MaxImplementRunning = 4

// maxImplementReason bounds the plain-language reason a parked story carries.
const maxImplementReason = 4096

// Implement ledger faults. A caller tells a lost race from a bad request.
var (
	// ErrImplementConflict means the row moved since the caller read it.
	ErrImplementConflict = errors.New("store: implement story changed since it was read")
	// ErrImplementTransition means the requested move is not an allowed one.
	ErrImplementTransition = errors.New("store: implement story transition is not allowed")
	// ErrImplementCap means the parent already has the most stories running.
	ErrImplementCap = errors.New("store: implement running cap reached")
)

// ImplementStory is one ledger row.
type ImplementStory struct {
	Parent      string
	Generation  int64
	Key         string
	Position    int
	Branch      string
	State       string
	Reason      string
	MergeCommit string
	Revision    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// StateAt is when the row last changed state; the oldest done story is the
	// one whose StateAt is earliest.
	StateAt time.Time
	// Attempts counts the failed attempts since the story last started or
	// resumed. It never exceeds MaxImplementAttempts.
	Attempts int
	// ParkOrigin is ImplementOriginWorker or ImplementOriginMerge while the
	// story is parked, and empty otherwise.
	ParkOrigin string
	// WorkerSession is the session of the story's own worker run, or 0.
	WorkerSession int64
	// LastFailure is the plain reason of the latest failed attempt.
	LastFailure string
	// Guidance is the owner's reply a resumed story is given.
	Guidance string
}

// MaxImplementAttempts is the failed attempts a story may record: the first
// attempt and its two repairs.
const MaxImplementAttempts = 3

// Who parked a story.
const (
	ImplementOriginWorker = "worker"
	ImplementOriginMerge  = "merge"
)

// maxImplementGuidance bounds the owner reply kept as guidance.
const maxImplementGuidance = 65536

// ImplementStorySeed names a story the ledger must hold, in plan order.
type ImplementStorySeed struct {
	Key      string
	Position int
	Branch   string
}

// ImplementTransition is one guarded move of one story. Reason is required to
// park and MergeCommit to merge; neither is accepted for any other move.
type ImplementTransition struct {
	Parent           string
	Generation       int64
	Key              string
	From, To         string
	ExpectedRevision int64
	Reason           string
	MergeCommit      string
	// Guidance is the owner's reply; only a resume (parked to waiting) takes it.
	Guidance string
	At       time.Time
}

// validImplementMove reports the moves the ledger allows. Merged is final. A
// parked story only leaves by an owner resume.
func validImplementMove(from, to string) bool {
	switch from + ">" + to {
	case ImplementWaiting + ">" + ImplementRunning,
		ImplementRunning + ">" + ImplementDone,
		ImplementRunning + ">" + ImplementParked,
		ImplementParked + ">" + ImplementWaiting,
		ImplementDone + ">" + ImplementMerged,
		ImplementDone + ">" + ImplementParked:
		return true
	}
	return false
}

const implementColumns = `parent,generation,story_key,position,branch,state,reason,merge_commit,revision,created_at,updated_at,state_at,attempts,park_origin,worker_session,last_failure,guidance`

// EnsureImplementStories adds a waiting row for every seed the ledger does not
// hold yet and leaves existing rows untouched, so it is safe to repeat.
func (db *DB) EnsureImplementStories(ctx context.Context, parent string, generation int64, seeds []ImplementStorySeed, at time.Time) error {
	if strings.TrimSpace(parent) == "" || generation < 1 || at.IsZero() {
		return errors.New("store: invalid implement ledger scope")
	}
	for _, seed := range seeds {
		if strings.TrimSpace(seed.Key) == "" || strings.TrimSpace(seed.Branch) == "" || seed.Position < 0 {
			return fmt.Errorf("store: invalid implement story %q", seed.Key)
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		stamp := FormatTime(at)
		for _, seed := range seeds {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO implement_stories(parent,generation,story_key,position,branch,state,revision,created_at,updated_at,state_at) VALUES(?,?,?,?,?,?,1,?,?,?)`,
				parent, generation, seed.Key, seed.Position, seed.Branch, ImplementWaiting, stamp, stamp, stamp); err != nil {
				return fmt.Errorf("store: ensure implement story: %w", err)
			}
		}
		return nil
	})
}

// ListImplementStories returns the ledger of one parent generation in plan order.
func (db *DB) ListImplementStories(ctx context.Context, parent string, generation int64) ([]ImplementStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? ORDER BY position,story_key`, parent, generation)
	if err != nil {
		return nil, fmt.Errorf("store: list implement stories: %w", err)
	}
	defer rows.Close()
	var out []ImplementStory
	for rows.Next() {
		story, err := scanImplementStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, story)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list implement stories: %w", err)
	}
	return out, nil
}

// CountImplementRunning counts the stories of one parent generation that are running.
func (db *DB) CountImplementRunning(ctx context.Context, parent string, generation int64) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM implement_stories WHERE parent=? AND generation=? AND state=?`, parent, generation, ImplementRunning).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count running implement stories: %w", err)
	}
	return n, nil
}

// TransitionImplementStory moves one story by compare-and-set on its state and
// revision, so of two callers racing for the same row only one wins and the
// other sees ErrImplementConflict. Moving to running also checks the running
// cap in the same statement. It returns the updated row.
func (db *DB) TransitionImplementStory(ctx context.Context, in ImplementTransition) (ImplementStory, error) {
	if strings.TrimSpace(in.Parent) == "" || in.Generation < 1 || strings.TrimSpace(in.Key) == "" || in.ExpectedRevision < 1 || in.At.IsZero() {
		return ImplementStory{}, errors.New("store: invalid implement transition")
	}
	if !validImplementMove(in.From, in.To) {
		return ImplementStory{}, fmt.Errorf("%w: %s to %s", ErrImplementTransition, in.From, in.To)
	}
	reason := strings.TrimSpace(in.Reason)
	switch {
	case in.To == ImplementParked && (reason == "" || len(reason) > maxImplementReason):
		return ImplementStory{}, fmt.Errorf("%w: parking needs a reason of at most %d bytes", ErrImplementTransition, maxImplementReason)
	case in.To != ImplementParked && reason != "":
		return ImplementStory{}, fmt.Errorf("%w: only parking carries a reason", ErrImplementTransition)
	case in.To == ImplementMerged && strings.TrimSpace(in.MergeCommit) == "":
		return ImplementStory{}, fmt.Errorf("%w: merging needs the merge commit", ErrImplementTransition)
	case in.To != ImplementMerged && in.MergeCommit != "":
		return ImplementStory{}, fmt.Errorf("%w: only merging carries a commit", ErrImplementTransition)
	case in.To != ImplementWaiting && in.Guidance != "":
		return ImplementStory{}, fmt.Errorf("%w: only a resume carries guidance", ErrImplementTransition)
	case len(in.Guidance) > maxImplementGuidance:
		return ImplementStory{}, fmt.Errorf("%w: guidance of at most %d bytes", ErrImplementTransition, maxImplementGuidance)
	}
	var out ImplementStory
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		stamp := FormatTime(in.At)
		set := ""
		var extra []any
		switch {
		case in.To == ImplementParked && in.From == ImplementRunning:
			set = ",park_origin=?"
			extra = []any{ImplementOriginWorker}
		case in.To == ImplementParked:
			set = ",park_origin=?"
			extra = []any{ImplementOriginMerge}
		case in.To == ImplementWaiting && in.From == ImplementParked:
			set = ",park_origin='',attempts=0,last_failure='',worker_session=0,guidance=?"
			extra = []any{in.Guidance}
		case in.To == ImplementRunning:
			set = ",worker_session=0"
		}
		query := `UPDATE implement_stories SET state=?,reason=?,merge_commit=?,revision=revision+1,updated_at=?,state_at=?` + set + `
 WHERE parent=? AND generation=? AND story_key=? AND state=? AND revision=?`
		args := []any{in.To, reason, in.MergeCommit, stamp, stamp}
		args = append(args, extra...)
		args = append(args, in.Parent, in.Generation, in.Key, in.From, in.ExpectedRevision)
		if in.To == ImplementRunning {
			query += ` AND (SELECT COUNT(*) FROM implement_stories WHERE parent=? AND generation=? AND state=?)<?`
			args = append(args, in.Parent, in.Generation, ImplementRunning, MaxImplementRunning)
		}
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("store: transition implement story: %w", err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: transition implement story: %w", err)
		}
		row, err := scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, in.Parent, in.Generation, in.Key))
		if err != nil {
			return err
		}
		if changed == 0 {
			if in.To == ImplementRunning && row.State == in.From && row.Revision == in.ExpectedRevision {
				return ErrImplementCap
			}
			return ErrImplementConflict
		}
		out = row
		return nil
	})
	if err != nil {
		return ImplementStory{}, err
	}
	return out, nil
}

func scanImplementStory(row scanner) (ImplementStory, error) {
	var s ImplementStory
	var created, updated, stateAt string
	if err := row.Scan(&s.Parent, &s.Generation, &s.Key, &s.Position, &s.Branch, &s.State, &s.Reason, &s.MergeCommit, &s.Revision, &created, &updated, &stateAt, &s.Attempts, &s.ParkOrigin, &s.WorkerSession, &s.LastFailure, &s.Guidance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImplementStory{}, ErrNotFound
		}
		return ImplementStory{}, fmt.Errorf("store: read implement story: %w", err)
	}
	var err error
	if s.CreatedAt, err = ParseTime(created); err != nil {
		return ImplementStory{}, fmt.Errorf("store: read implement story: %w", err)
	}
	if s.UpdatedAt, err = ParseTime(updated); err != nil {
		return ImplementStory{}, fmt.Errorf("store: read implement story: %w", err)
	}
	if s.StateAt, err = ParseTime(stateAt); err != nil {
		return ImplementStory{}, fmt.Errorf("store: read implement story: %w", err)
	}
	return s, nil
}

// GetImplementStory reads one ledger row.
func (db *DB) GetImplementStory(ctx context.Context, parent string, generation int64, key string) (ImplementStory, error) {
	return scanImplementStory(db.sql.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, parent, generation, key))
}

// ImplementStoryByWorkerSession reads the row whose worker run belongs to a
// session, or ErrNotFound.
func (db *DB) ImplementStoryByWorkerSession(ctx context.Context, session int64) (ImplementStory, error) {
	if session < 1 {
		return ImplementStory{}, ErrNotFound
	}
	return scanImplementStory(db.sql.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE worker_session=? ORDER BY generation DESC, updated_at DESC LIMIT 1`, session))
}

// SetImplementWorkerSession records the session a running story's worker uses.
// It refuses a row that is not running, and leaves the row's revision alone so
// it never races a guarded move.
func (db *DB) SetImplementWorkerSession(ctx context.Context, parent string, generation int64, key string, session int64, at time.Time) error {
	if strings.TrimSpace(parent) == "" || generation < 1 || strings.TrimSpace(key) == "" || session < 1 || at.IsZero() {
		return errors.New("store: invalid implement worker session")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE implement_stories SET worker_session=?,updated_at=? WHERE parent=? AND generation=? AND story_key=? AND state=?`, session, FormatTime(at), parent, generation, key, ImplementRunning)
	if err != nil {
		return fmt.Errorf("store: set implement worker session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set implement worker session: %w", err)
	}
	if n == 0 {
		return ErrImplementConflict
	}
	return nil
}

// RecordImplementFailure counts one failed attempt of a running story and keeps
// the plain reason. The count stops at MaxImplementAttempts. It returns the
// updated row.
func (db *DB) RecordImplementFailure(ctx context.Context, parent string, generation int64, key, reason string, at time.Time) (ImplementStory, error) {
	reason = strings.TrimSpace(reason)
	if strings.TrimSpace(parent) == "" || generation < 1 || strings.TrimSpace(key) == "" || reason == "" || len(reason) > maxImplementReason || at.IsZero() {
		return ImplementStory{}, errors.New("store: invalid implement failure")
	}
	var out ImplementStory
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE implement_stories SET attempts=MIN(attempts+1,?),last_failure=?,revision=revision+1,updated_at=? WHERE parent=? AND generation=? AND story_key=? AND state=?`,
			MaxImplementAttempts, reason, FormatTime(at), parent, generation, key, ImplementRunning)
		if err != nil {
			return fmt.Errorf("store: record implement failure: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: record implement failure: %w", err)
		}
		if n == 0 {
			return ErrImplementConflict
		}
		out, err = scanImplementStory(tx.QueryRowContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE parent=? AND generation=? AND story_key=?`, parent, generation, key))
		return err
	})
	if err != nil {
		return ImplementStory{}, err
	}
	return out, nil
}

// ListImplementStoriesNeedingWorker lists, across every parent, the running
// stories whose worker has no live session: the story has no worker session
// yet, or its session is not running a turn right now. Rows are in parent and
// plan order.
func (db *DB) ListImplementStoriesNeedingWorker(ctx context.Context) ([]ImplementStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+implementColumns+` FROM implement_stories WHERE state=? AND (worker_session=0 OR NOT EXISTS (SELECT 1 FROM sessions s WHERE s.id=worker_session AND s.state=?)) AND parent NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY parent,generation,position`, ImplementRunning, SessionRunning)
	if err != nil {
		return nil, fmt.Errorf("store: list running implement stories: %w", err)
	}
	defer rows.Close()
	var out []ImplementStory
	for rows.Next() {
		story, err := scanImplementStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, story)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list running implement stories: %w", err)
	}
	return out, nil
}

// CountFailedImplementInstances counts the Implement graph instances of a
// session that failed since a time: runs that ended on an infrastructure fault
// rather than a decision, so the discovery of story work can stop retrying a
// fault that does not go away.
func (db *DB) CountFailedImplementInstances(ctx context.Context, session int64, since time.Time) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode='implement' AND i.status=? AND i.started_at>=?`, session, GraphFailed, FormatTime(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count failed implement instances: %w", err)
	}
	return n, nil
}

// FailImplementWorkerInstance retains an infrastructure cause and terminalizes
// its instance together, without spending the post-process repair budget.
func (db *DB) FailImplementWorkerInstance(ctx context.Context, instanceID int64, reason string, at time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > maxImplementReason || at.IsZero() {
		return errors.New("store: invalid implement infrastructure failure")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if err != nil {
			return err
		}
		if instance.Status != GraphRunning && instance.Status != GraphFailed {
			return ErrGraphInstanceTerminal
		}
		res, err := tx.ExecContext(ctx, `UPDATE implement_stories SET last_failure=?,revision=revision+1,updated_at=? WHERE worker_session=? AND state=? AND EXISTS (SELECT 1 FROM graph_definitions WHERE id=? AND mode='implement')`, reason, FormatTime(at), instance.SessionID, ImplementRunning, instance.DefinitionID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrImplementConflict
		}
		if instance.Status == GraphFailed {
			return nil
		}
		return setGraphInstanceTx(ctx, tx, instance.ID, instance.CurrentNode, GraphFailed, time.Time{}, at)
	})
}
