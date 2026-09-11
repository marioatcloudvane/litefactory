package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// RunStatus is a run's position in its lifecycle.
//
// The sequence is running -> succeeded|failed -> reported. There is no
// intermediate "reporting" state on purpose: the report's three effects — the
// result comment id, the watermark advance and the move to reported — land in
// one transaction ([DB.CompleteRunReport]), so there is no window for a state
// that means "posting right now" to describe.
type RunStatus string

// The run statuses.
const (
	// RunRunning is set when the row is allocated, before the subprocess
	// starts.
	RunRunning RunStatus = "running"
	// RunSucceeded means claude exited zero and emitted a terminal result
	// event.
	RunSucceeded RunStatus = "succeeded"
	// RunFailed means it did not: a non-zero exit, a timeout kill, a run
	// abandoned by a killed daemon, or a run that ended without a result event
	// (S-9, SD-3).
	RunFailed RunStatus = "failed"
	// RunReported means the report comment for this run is on the forge and
	// the watermark has moved past its trigger.
	RunReported RunStatus = "reported"
)

// ModeSpike is the only mode M0 runs. The aiteam pipeline modes arrive with
// LF-6.
const ModeSpike = "spike"

// AgentClaude is the agent discriminator value for a Claude run. It is the
// only value this tree writes today — a second implementation is a later
// story's job — and it is also the DEFAULT the segments.agent and runs.agent
// columns backfill to, so every row written before those columns existed
// reads back as a Claude row without a separate UPDATE.
const AgentClaude = "claude"

// AgentCodex is the agent discriminator value a Codex run will use, once a
// second implementation exists to write one. Nothing in this tree writes a
// segments.agent or runs.agent row with it yet; it exists so code that
// selects between the two coding-agent tools — the lf:tools= profile's
// per-agent stanza selection (T-7) — has a named constant to compare
// against instead of a bare "codex" literal.
const AgentCodex = "codex"

// SignalExitCode is the exit code recorded when a run produced no exit status
// of its own — killed by the timeout, or abandoned when the daemon died. It is
// always accompanied by a reason string (S-9).
const SignalExitCode = -1

// ReasonAbandoned is the reason recorded against a run that was still marked
// running when the daemon next started, meaning the process was killed mid
// run (D-B). The run is never retried; it is reported as a failure.
const ReasonAbandoned = "abandoned"

// SyncOutcome is how a run's base-branch auto-sync attempt ended (US-9),
// mirroring internal/turn's own syncOutcome one-for-one so the values that
// leave that package match the values this one persists without translation.
type SyncOutcome string

// The sync outcomes [DB.RecordRunSync] accepts. SyncFailed exists for
// completeness of the persisted vocabulary; nothing in this tree writes it
// today, because a sync that could not even be attempted fails the turn
// before a runs row exists to record it against (internal/turn.syncBase).
const (
	SyncUpToDate SyncOutcome = "up_to_date"
	SyncSynced   SyncOutcome = "synced"
	SyncBlocked  SyncOutcome = "blocked"
	SyncSkipped  SyncOutcome = "skipped"
	SyncFailed   SyncOutcome = "failed"
	// SyncDeferredDirty preserves uncommitted changes and lets recovery run before merging.
	SyncDeferredDirty SyncOutcome = "deferred_dirty"
)

// Run is one claude invocation.
//
// The row is inserted before the subprocess starts, which is what makes the
// ordinal safe: a run that crashes still owns its number, so NNNN.jsonl files
// never collide (S-1).
type Run struct {
	SemanticOutcome string
	RecoveryReason  string
	// ID is the surrogate key.
	ID int64
	// SessionID and SegmentID are the task and conversation this run belongs
	// to.
	SessionID int64
	SegmentID int64
	// Ordinal is the run's number within its session, starting at 1. It is the
	// NNNN in the log filename.
	Ordinal int
	// Mode is [ModeSpike] at M0.
	Mode string
	// Agent is which coding-agent tool this run invoked — [AgentClaude] for
	// every run this tree can produce today.
	Agent string
	// TriggerCommentID is the forge comment that caused this run. It is unique
	// per session, and that uniqueness is the whole crash-replay guarantee.
	TriggerCommentID int64
	// Status is the lifecycle position.
	Status RunStatus
	// ExitCode is claude's exit status, or [SignalExitCode] for a kill. Nil
	// while the run is still going — which is why this is a pointer: exit 0
	// and "has not exited" are different facts.
	ExitCode *int64
	// Error is the reason accompanying a [SignalExitCode] exit, for example
	// [ReasonAbandoned]. Empty on a clean exit.
	//
	// It has a second reader now beyond the report and the log: [Run.Succeeded]
	// treats an empty Error on a [RunReported] row as the durable signal that a
	// run actually succeeded, and a resumed run's prompt input is built from
	// that signal (US-4). It is prompt input now, not only report/log text — a
	// future write path that leaves Error empty on a genuine failure would
	// silently tell the next run that the failed one worked.
	Error string
	// StartedAt is when the subprocess was launched.
	StartedAt time.Time
	// FinishedAt is when it stopped; zero while it runs.
	FinishedAt time.Time
	// DurationMS is FinishedAt minus StartedAt, computed by the store so the
	// two can never disagree. Nil while the run is still going.
	DurationMS *int64
	// CostUSD comes from the terminal result event. Nil when the run produced
	// no result event, which SD-3 shows is a real outcome and not a
	// theoretical one.
	CostUSD *float64
	// LogPath is the absolute path of this run's NNNN.jsonl.
	LogPath string
	// CommitSHA is the commit this run produced; "" when it changed nothing
	// (AC-3.23) or when the commit failed.
	CommitSHA string
	// ResultCommentID is the forge id of the report comment; 0 until it is
	// posted.
	ResultCommentID int64
	// Published records whether CommitSHA was verified on the remote, and
	// Publication is the sentence the report renders for it. Both are durable
	// because a report whose post fails is rebuilt from this row on a later
	// tick: the push was verified once, and re-deriving it later would answer
	// a different question. Historical rows are false and "".
	Published   bool
	Publication string
	// SyncOutcome, SyncBehind, SyncAhead and SyncCommitSHA are the
	// base-branch auto-sync result [DB.RecordRunSync] recorded for this run
	// (US-9). SyncOutcome is "" for a run written before that column existed,
	// or one no sync attempt was ever recorded against. SyncCommitSHA is ""
	// unless SyncOutcome is [SyncSynced] and the merge actually moved the
	// branch.
	SyncObservations []SyncObservation
	SyncOutcome      SyncOutcome
	SyncBehind       int
	SyncAhead        int
	SyncCommitSHA    string
}

// Duration returns the run's wall-clock duration, and false while it is still
// running.
func (r Run) Duration() (time.Duration, bool) {
	if r.DurationMS == nil {
		return 0, false
	}
	return time.Duration(*r.DurationMS) * time.Millisecond, true
}

// Succeeded reports whether this run genuinely finished as a success, which
// takes two arms rather than one because Status alone stops meaning that the
// moment a run is reported: [RunSucceeded] means it, and so does [RunReported]
// — but only when [Run.Error] is empty, because a reported run started life as
// either [RunSucceeded] or [RunFailed] and Status does not remember which.
// Error does: it is set on the failure path and left empty on the success
// path, and nothing clears it when a run moves on to [RunReported] — so an
// empty Error on a reported row is the durable record that this run was a
// success before it was ever reported, and a non-empty one is the durable
// record that it was not.
//
// This is a second, distinct predicate from [agent.RunResult.Succeeded],
// which reads a live subprocess's own exit and result event. That one has
// nothing to read once the process is gone; this one has nothing else *to*
// read — the stored row is all a later run, or a resumed one, ever gets. The
// two must never be collapsed into one call or converted into each other:
// they answer the same English question from different evidence with
// different lifetimes.
func (r Run) Succeeded() bool {
	if r.SemanticOutcome != "" {
		return r.SemanticOutcome == "succeeded"
	}
	switch r.Status {
	case RunSucceeded:
		return true
	case RunReported:
		return r.Error == ""
	default:
		return false
	}
}

// AllocateRunParams describes the run to allocate.
type AllocateRunParams struct {
	// SessionID is the task.
	SessionID int64
	// SegmentID is the conversation, from [DB.GetOrCreateSegment].
	SegmentID int64
	// Mode is [ModeSpike] at M0.
	Mode string
	// Agent is which coding-agent tool this run invokes — [AgentClaude] for
	// every run this tree can produce today. Required.
	Agent string
	// TriggerCommentID is the comment that caused the run. A second
	// allocation for the same (session, comment) is refused with
	// [ErrDuplicateRun].
	//
	// Zero means the issue body caused it, not a comment: an issue assigned to
	// the owner runs its body as the first instruction. It is 0 rather than the
	// issue's own forge id because [DB.CompleteRunReport] advances the
	// watermark to this column, and a positive id would consume every comment
	// the issue will ever receive; MAX(0, 0) leaves the watermark where
	// bootstrap planted it. The same uniqueness then gives body-fires-once-ever
	// for free.
	TriggerCommentID int64
	// StartedAt is when the run began. Zero means now.
	StartedAt time.Time
	// LogPath builds the run's JSONL path from the ordinal that was just
	// allocated. It is a function because the path contains the ordinal
	// (…/NNNN.jsonl) and the ordinal is not known until the row is being
	// inserted — and ordinal formatting belongs to internal/naming, not here.
	//
	// It is called exactly once, inside the allocating transaction, and must
	// not touch the database. Returning an empty path is an error.
	LogPath func(ordinal int) string
}

// AllocateRun reserves a run row — and with it the run's ordinal and log path
// — before the subprocess starts.
//
// Allocating first is what stops a crashed run from losing its number to the
// next one and overwriting its JSONL (S-1). The (session, trigger comment)
// uniqueness means a comment can cause at most one run ever: a daemon that
// crashes and replays the same comment gets [ErrDuplicateRun] rather than a
// second run of the same work. A caller that expects to replay should look
// with [DB.GetRunByTrigger] first (AC-3.4).
func (db *DB) AllocateRun(ctx context.Context, params AllocateRunParams) (Run, error) {
	switch {
	case params.SessionID == 0:
		return Run{}, errors.New("store: allocate run: session id is required")
	case params.SegmentID == 0:
		return Run{}, errors.New("store: allocate run: segment id is required")
	case params.Mode == "":
		return Run{}, errors.New("store: allocate run: mode is required")
	case params.Agent == "":
		return Run{}, errors.New("store: allocate run: agent is required")
	// Not "== 0": 0 is the issue body rather than a missing id, and refusing it
	// here would also stop startup reconciliation reporting a body run that was
	// abandoned by a crash. See [AllocateRunParams.TriggerCommentID].
	case params.TriggerCommentID < 0:
		return Run{}, fmt.Errorf(
			"store: allocate run: trigger id %d is neither a comment nor the issue body",
			params.TriggerCommentID)
	case params.LogPath == nil:
		return Run{}, errors.New("store: allocate run: LogPath is required")
	}
	startedAt := params.StartedAt
	if startedAt.IsZero() {
		startedAt = nowUTC()
	}

	var run Run
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if IsAutomaticTrigger(params.TriggerCommentID) {
			if err := validateAutomaticAllocation(ctx, tx, params); err != nil {
				return err
			}
		}
		var ordinal int
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(ordinal), 0) + 1 FROM runs WHERE session_id = ?`,
			params.SessionID).Scan(&ordinal)
		if err != nil {
			return fmt.Errorf("allocate ordinal: %w", err)
		}

		logPath := params.LogPath(ordinal)
		if logPath == "" {
			return fmt.Errorf("LogPath returned an empty path for ordinal %d", ordinal)
		}

		run, err = scanRun(tx.QueryRowContext(ctx, `
INSERT INTO runs (
    session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status,
    started_at, log_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+runColumns,
			params.SessionID, params.SegmentID, ordinal, params.Mode, params.Agent,
			params.TriggerCommentID, string(RunRunning), FormatTime(startedAt), logPath))
		if isUniqueViolation(err) {
			return ErrDuplicateRun
		}
		if err == nil && IsAutomaticTrigger(params.TriggerCommentID) {
			_, err = tx.ExecContext(ctx, `UPDATE automatic_continuations SET status = 'running', allocated_run_id = ? WHERE id = ?`, run.ID, params.TriggerCommentID-automaticTriggerBase)
		}
		return err
	})
	if err != nil {
		return Run{}, fmt.Errorf("store: allocate run on session %d for comment %d: %w",
			params.SessionID, params.TriggerCommentID, err)
	}
	return run, nil
}

// RunOutcome is how a run ended. It is applied by [DB.FinishRun].
type RunOutcome struct {
	SemanticOutcome string
	RecoveryReason  string
	// Status must be [RunSucceeded] or [RunFailed]; [RunReported] is reached
	// only through [DB.CompleteRunReport].
	Status RunStatus
	// ExitCode is claude's exit status, or [SignalExitCode] for a kill or an
	// abandonment. Nil is allowed only when there genuinely was no exit
	// status to observe.
	ExitCode *int64
	// Error is the reason string that accompanies a [SignalExitCode] exit:
	// "timeout", [ReasonAbandoned], "no result event" (SD-3). Empty otherwise.
	Error string
	// FinishedAt is when the run stopped. Zero means now.
	FinishedAt time.Time
	// CostUSD comes from the terminal result event; nil when there was none.
	CostUSD *float64
	// CommitSHA is the commit the run produced, "" if it produced none.
	CommitSHA string
}

// FinishRun records a run's terminal outcome and computes its duration from
// the started_at already on the row, so the two can never contradict each
// other. The stored run is returned.
//
// An empty CommitSHA leaves whatever is stored alone rather than clearing it,
// so a turn that commits before it finishes the row does not lose the commit.
func (db *DB) FinishRun(ctx context.Context, runID int64, outcome RunOutcome) (Run, error) {
	if outcome.Status != RunSucceeded && outcome.Status != RunFailed {
		return Run{}, fmt.Errorf("store: finish run %d: status must be %q or %q, got %q",
			runID, RunSucceeded, RunFailed, outcome.Status)
	}
	finishedAt := outcome.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = nowUTC()
	}

	var run Run
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		run, err = finishRunTx(ctx, tx, runID, outcome, finishedAt)
		if err != nil {
			return err
		}
		return settleContinuationRun(ctx, tx, run)
	})
	if err != nil {
		return Run{}, fmt.Errorf("store: finish run %d: %w", runID, err)
	}
	return run, nil
}

func finishRunTx(ctx context.Context, tx *sql.Tx, runID int64, outcome RunOutcome, finishedAt time.Time) (Run, error) {
	current, err := scanRun(tx.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE id = ?`, runID))
	if err != nil {
		return Run{}, err
	}
	durationMS := finishedAt.Sub(current.StartedAt).Milliseconds()
	if durationMS < 0 {
		// A clock that went backwards must not produce a negative
		// duration in a report.
		durationMS = 0
	}

	run, err := scanRun(tx.QueryRowContext(ctx, `
UPDATE runs
SET status = ?, exit_code = ?, error = ?, finished_at = ?, duration_ms = ?,
    cost_usd = ?, commit_sha = COALESCE(?, commit_sha), semantic_outcome = ?, recovery_reason = ?
WHERE id = ?
RETURNING `+runColumns,
		string(outcome.Status), outcome.ExitCode, nullString(outcome.Error),
		FormatTime(finishedAt), durationMS, outcome.CostUSD,
		nullString(outcome.CommitSHA), outcome.SemanticOutcome, outcome.RecoveryReason, runID))
	return run, err
}

// SetRunCommit records the commit a run produced, for the case where the
// commit is made after the run has already been finished. Passing "" leaves
// the stored value alone; use [DB.FinishRun] to record a run that committed
// nothing.
func (db *DB) SetRunCommit(ctx context.Context, runID int64, commitSHA string) error {
	if commitSHA == "" {
		return errors.New("store: set run commit: commit sha is required")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE runs SET commit_sha = ? WHERE id = ?`, commitSHA, runID)
	if err != nil {
		return fmt.Errorf("store: set commit on run %d: %w", runID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: set commit on run %d", runID))
}

// SetRunPublication records whether this run's commit reached the remote, and
// the sentence the report renders for it.
//
// It is separate from [DB.SetRunCommit] because the two facts are established
// at different moments and a run can have one without the other: a run that
// committed nothing still has publication text ("not published by this run"),
// and a commit is recorded even when the push that followed it failed.
func (db *DB) SetRunPublication(ctx context.Context, runID int64, published bool, publication string) error {
	res, err := db.sql.ExecContext(ctx,
		`UPDATE runs SET published = ?, publication = ? WHERE id = ?`, published, publication, runID)
	if err != nil {
		return fmt.Errorf("store: set publication on run %d: %w", runID, err)
	}
	return requireAffected(res, fmt.Sprintf("store: set publication on run %d", runID))
}

// RunSyncOutcome is what [DB.RecordRunSync] writes onto a run row: the
// outcome [syncBase] (internal/turn) reached for the task branch, the
// divergence it was measured from, and the merge commit when one was
// produced.
type RunSyncOutcome struct {
	// Outcome must be one of the [SyncOutcome] values.
	Outcome SyncOutcome
	// Behind and Ahead are the divergence syncBase measured before deciding.
	Behind, Ahead int
	// CommitSHA is the merge commit syncBase produced. Valid only when
	// Outcome is [SyncSynced]; RecordRunSync refuses a non-empty CommitSHA
	// paired with any other outcome.
	CommitSHA string
}

// RecordRunSync persists the outcome of a run's base-branch auto-sync attempt
// (US-9), so the answer to "did this run touch my branch, and why" survives
// independently of the report comment (US-3, US-4) that first disclosed it.
//
// It reads the run first inside the same BEGIN IMMEDIATE transaction, the way
// every other run-row mutation in this file does (see [DB.FinishRun],
// [DB.CompleteRunReport]): a runID that does not exist reports [ErrNotFound]
// rather than an UPDATE that silently affects zero rows, and the caller gets
// back the row exactly as stored.
func (db *DB) RecordRunSync(ctx context.Context, runID int64, outcome RunSyncOutcome) (Run, error) {
	switch outcome.Outcome {
	case SyncUpToDate, SyncSynced, SyncBlocked, SyncSkipped, SyncFailed, SyncDeferredDirty:
	default:
		return Run{}, fmt.Errorf("store: record run sync on run %d: %q is not a recognised sync outcome",
			runID, outcome.Outcome)
	}
	if outcome.CommitSHA != "" && outcome.Outcome != SyncSynced {
		return Run{}, fmt.Errorf(
			"store: record run sync on run %d: a commit sha is only valid when the outcome is %q, got %q",
			runID, SyncSynced, outcome.Outcome)
	}

	var run Run
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, runID)); err != nil {
			return err
		}
		var err error
		run, err = scanRun(tx.QueryRowContext(ctx, `
UPDATE runs
SET sync_outcome = ?, sync_behind = ?, sync_ahead = ?, sync_commit_sha = ?
WHERE id = ?
RETURNING `+runColumns,
			string(outcome.Outcome), outcome.Behind, outcome.Ahead, nullString(outcome.CommitSHA), runID))
		return err
	})
	if err != nil {
		return Run{}, fmt.Errorf("store: record run sync on run %d: %w", runID, err)
	}
	return run, nil
}

// CompleteRunReport closes out a run once its report comment is on the forge:
// it records the result comment id, advances the session's watermark past the
// comment that triggered the run, and moves the run to [RunReported] — all in
// one transaction (AC-4.10).
//
// The three writes are one call rather than three because they must succeed or
// fail together. If the post fails, the caller simply does not call this: the
// watermark stays put, the run stays un-reported, and the next tick retries
// the post without re-running claude (guaranteed by the (session, trigger
// comment) uniqueness). A caller that could advance the watermark separately
// would eventually advance it past a report that was never posted, and the
// outcome would be silence — which AC-4.1 forbids.
//
// Calling it twice is safe: the second call overwrites the result comment id
// and re-advances the watermark, which is what should happen if the post
// succeeded but this transaction did not.
func (db *DB) CompleteRunReport(ctx context.Context, runID, resultCommentID int64) (Run, error) {
	if resultCommentID == 0 {
		return Run{}, errors.New("store: complete run report: result comment id is required")
	}

	var run Run
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanRun(tx.QueryRowContext(ctx,
			`SELECT `+runColumns+` FROM runs WHERE id = ?`, runID))
		if err != nil {
			return err
		}
		switch current.Status {
		case RunSucceeded, RunFailed, RunReported:
		case RunRunning:
			return errors.New("run has not finished: finish it before reporting it")
		default:
			return fmt.Errorf("run is in unknown status %q", current.Status)
		}

		run, err = scanRun(tx.QueryRowContext(ctx, `
UPDATE runs SET status = ?, result_comment_id = ? WHERE id = ?
RETURNING `+runColumns,
			string(RunReported), resultCommentID, runID))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE automatic_continuations SET announcement_comment_id = ? WHERE source_run_id = ?`, resultCommentID, runID); err != nil {
			return err
		}
		// Factory-private schedule triggers had their source comments consumed
		// when scheduling was acknowledged, so advancing a
		// watermark with one would be both meaningless and unsafe.
		refinement, err := consumeRefinement(ctx, tx, run.SessionID, run.TriggerCommentID)
		if err != nil {
			return fmt.Errorf("consume story refinement: %w", err)
		}
		if !refinement && !IsScheduleTrigger(run.TriggerCommentID) && !IsAutomaticTrigger(run.TriggerCommentID) {
			if _, err := advanceWatermark(ctx, tx, run.SessionID, run.TriggerCommentID); err != nil {
				return fmt.Errorf("advance watermark: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Run{}, fmt.Errorf("store: complete report for run %d: %w", runID, err)
	}
	return run, nil
}

// GetRun reads a run by id, returning [ErrNotFound] when there is none.
func (db *DB) GetRun(ctx context.Context, id int64) (Run, error) {
	run, err := scanRun(db.sql.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id))
	if err != nil {
		return Run{}, fmt.Errorf("store: read run %d: %w", id, err)
	}
	return run, nil
}

// GetRunByTrigger looks for the run a comment already caused. exists is false
// when the comment has not been run yet, which is the ordinary case; a run
// that comes back [RunReported] means this comment is a replay and the turn is
// a no-op (AC-3.4).
func (db *DB) GetRunByTrigger(ctx context.Context, sessionID, triggerCommentID int64) (Run, bool, error) {
	run, err := scanRun(db.sql.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE session_id = ? AND trigger_comment_id = ?`,
		sessionID, triggerCommentID))
	switch {
	case errors.Is(err, ErrNotFound):
		return Run{}, false, nil
	case err != nil:
		return Run{}, false, fmt.Errorf("store: read run for comment %d on session %d: %w",
			triggerCommentID, sessionID, err)
	}
	return run, true, nil
}

// GetRunByOrdinal reads a run by its per-session ordinal — the NNNN a person
// types into `litefactory logs 123 --run 2`. exists is false when the session
// has no such run.
func (db *DB) GetRunByOrdinal(ctx context.Context, sessionID int64, ordinal int) (Run, bool, error) {
	run, err := scanRun(db.sql.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE session_id = ? AND ordinal = ?`, sessionID, ordinal))
	switch {
	case errors.Is(err, ErrNotFound):
		return Run{}, false, nil
	case err != nil:
		return Run{}, false, fmt.Errorf("store: read run %d on session %d: %w", ordinal, sessionID, err)
	}
	return run, true, nil
}

// ListRuns returns a session's runs in ordinal order — oldest first, newest
// last, which is the order `logs` prints them in (AC-5.1).
func (db *DB) ListRuns(ctx context.Context, sessionID int64) ([]Run, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE session_id = ? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list runs for session %d: %w", sessionID, err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list runs for session %d: %w", sessionID, err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list runs for session %d: %w", sessionID, err)
	}
	return out, nil
}

// ReclaimedLock describes a per-task lock that was taken from a daemon
// instance that no longer exists, so the reclaim can be logged (AC-3.2).
type ReclaimedLock struct {
	// SessionID and TaskRef identify the task whose lock was freed.
	SessionID int64
	TaskRef   string
	// PreviousOwner is the daemon instance id that held it.
	PreviousOwner string
	// AcquiredAt is when that instance took it.
	AcquiredAt time.Time
}

// Reconciliation is what [DB.ReconcileOrphans] found and fixed.
type Reconciliation struct {
	// AbandonedRuns are the runs that were still marked running, now failed.
	// The caller must post the ordinary failure comment for each and then call
	// [DB.CompleteRunReport], which is what advances the watermark past their
	// triggering comments (AC-2.21). They are never re-run.
	AbandonedRuns []Run
	// ReclaimedLocks are the per-task locks that were freed.
	ReclaimedLocks []ReclaimedLock
}

// ReconcileOrphans repairs the state a killed daemon left behind, and is the
// first thing a starting daemon does (D-B).
//
// Two kinds of wreckage, one transaction because they describe the same event:
// runs still marked running are failed with the [ReasonAbandoned] reason and
// their sessions marked failed, and per-task locks held by any instance other
// than instanceID are released.
//
// Without this the daemon would violate its own (session, trigger comment)
// uniqueness the first time it was rebooted mid-run, and the task would stay
// locked by a process that no longer exists.
//
// instanceID must be unique per daemon process — a fresh value each start, not
// something derived from the host — because "not the current instance" is
// exactly how a dead owner is recognised.
func (db *DB) ReconcileOrphans(ctx context.Context, instanceID string) (Reconciliation, error) {
	if instanceID == "" {
		return Reconciliation{}, errors.New("store: reconcile orphans: instance id is required")
	}

	var result Reconciliation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		orphans, err := collectRuns(ctx, tx,
			`SELECT `+runColumns+` FROM runs WHERE status = ? ORDER BY id`, string(RunRunning))
		if err != nil {
			return fmt.Errorf("find abandoned runs: %w", err)
		}

		now := nowUTC()
		exitCode := int64(SignalExitCode)
		for _, orphan := range orphans {
			durationMS := now.Sub(orphan.StartedAt).Milliseconds()
			if durationMS < 0 {
				durationMS = 0
			}
			failed, err := scanRun(tx.QueryRowContext(ctx, `
UPDATE runs
SET status = ?, exit_code = ?, error = ?, finished_at = ?, duration_ms = ?
WHERE id = ?
RETURNING `+runColumns,
				string(RunFailed), exitCode, ReasonAbandoned, FormatTime(now), durationMS, orphan.ID))
			if err != nil {
				return fmt.Errorf("abandon run %d: %w", orphan.ID, err)
			}
			result.AbandonedRuns = append(result.AbandonedRuns, failed)
			if err := abandonMeterAttempts(ctx, tx, orphan.ID, now); err != nil {
				return fmt.Errorf("abandon meter attempts: %w", err)
			}

			_, err = tx.ExecContext(ctx, `UPDATE sessions SET state = ?, updated_at = ? WHERE id = ?`,
				string(SessionFailed), FormatTime(now), failed.SessionID)
			if err != nil {
				return fmt.Errorf("fail session %d: %w", failed.SessionID, err)
			}
		}

		if err := reconcileContinuations(ctx, tx); err != nil {
			return err
		}

		// A schedule reservation precedes turn allocation. After a crash, a
		// running schedule with no run never reached an agent and is safe to
		// retry; one with a run inherits that run's durable terminal outcome.
		// This is intentionally in the same transaction as orphan reconciliation
		// so a restart cannot observe a reconciled run and stale reservation.
		rows, err := tx.QueryContext(ctx,
			`SELECT `+scheduleColumns+` FROM schedules WHERE status = ? ORDER BY id`, string(ScheduleRunning))
		if err != nil {
			return fmt.Errorf("find abandoned schedule reservations: %w", err)
		}
		scheduled, err := collectSchedules(rows)
		closeErr := rows.Close()
		if err != nil {
			return fmt.Errorf("scan abandoned schedule reservations: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("close abandoned schedule reservations: %w", closeErr)
		}
		for _, item := range scheduled {
			run, runErr := scanRun(tx.QueryRowContext(ctx,
				`SELECT `+runColumns+` FROM runs WHERE session_id = ? AND trigger_comment_id = ?`, item.SessionID, item.TriggerID()))
			if errors.Is(runErr, ErrNotFound) {
				if _, err := tx.ExecContext(ctx, `UPDATE schedules SET status = ?, started_at = NULL, updated_at = ? WHERE id = ? AND status = ?`,
					string(SchedulePending), FormatTime(now), item.ID, string(ScheduleRunning)); err != nil {
					return fmt.Errorf("requeue abandoned schedule %d: %w", item.ID, err)
				}
				continue
			}
			if runErr != nil {
				return fmt.Errorf("read scheduled run %d: %w", item.ID, runErr)
			}
			status := ScheduleFailed
			if run.Succeeded() {
				status = ScheduleSucceeded
			}
			if _, err := tx.ExecContext(ctx, `UPDATE schedules SET status = ?, run_id = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status = ?`,
				string(status), run.ID, FormatTime(run.FinishedAt), FormatTime(now), item.ID, string(ScheduleRunning)); err != nil {
				return fmt.Errorf("finish abandoned schedule %d: %w", item.ID, err)
			}
			if status == ScheduleFailed {
				if err := blockDescendants(ctx, tx, item.ID, now); err != nil {
					return fmt.Errorf("block descendants of abandoned schedule %d: %w", item.ID, err)
				}
			} else if err := resolveDependentDueTimes(ctx, tx, item.ID, now); err != nil {
				return fmt.Errorf("resolve descendants of abandoned schedule %d: %w", item.ID, err)
			}
		}

		locks, err := collectReclaimedLocks(ctx, tx, instanceID)
		if err != nil {
			return fmt.Errorf("find stale locks: %w", err)
		}
		if len(locks) > 0 {
			_, err = tx.ExecContext(ctx, `
UPDATE sessions
SET lock_owner = NULL, lock_acquired_at = NULL, updated_at = ?
WHERE lock_owner IS NOT NULL AND lock_owner <> ?`,
				FormatTime(now), instanceID)
			if err != nil {
				return fmt.Errorf("release stale locks: %w", err)
			}
		}
		result.ReclaimedLocks = locks
		return nil
	})
	if err != nil {
		return Reconciliation{}, fmt.Errorf("store: reconcile orphans: %w", err)
	}
	return result, nil
}

// collectReclaimedLocks reads the locks held by any instance other than
// instanceID. The rows are fully consumed before the caller issues its update,
// because a transaction can only have one result set open at a time.
func collectReclaimedLocks(ctx context.Context, tx *sql.Tx, instanceID string) ([]ReclaimedLock, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, task_ref, lock_owner, lock_acquired_at
FROM sessions
WHERE lock_owner IS NOT NULL AND lock_owner <> ?
ORDER BY id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReclaimedLock
	for rows.Next() {
		var (
			lock       ReclaimedLock
			owner      sql.NullString
			acquiredAt sql.NullString
		)
		if err := rows.Scan(&lock.SessionID, &lock.TaskRef, &owner, &acquiredAt); err != nil {
			return nil, err
		}
		lock.PreviousOwner = owner.String
		if lock.AcquiredAt, err = scanTime(acquiredAt); err != nil {
			return nil, err
		}
		out = append(out, lock)
	}
	return out, rows.Err()
}

// collectRuns reads a set of runs to completion, so the caller may issue
// further statements on the same transaction.
func collectRuns(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Run, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// isUniqueViolation reports whether err is SQLite refusing a duplicate on a
// UNIQUE constraint, as opposed to any other constraint or transport failure.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return true
	default:
		return false
	}
}

// runColumns is the column list every run read shares, in the order scanRun
// expects.
const runColumns = `
    id, session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status,
    exit_code, error, started_at, finished_at, duration_ms, cost_usd,
    log_path, commit_sha, result_comment_id, sync_outcome, sync_behind,
    sync_ahead, sync_commit_sha, semantic_outcome, recovery_reason, sync_observations,
    published, publication`

func scanRun(s scanner) (Run, error) {
	var (
		run              Run
		status           string
		exitCode         sql.NullInt64
		reason           sql.NullString
		startedAt        sql.NullString
		finishedAt       sql.NullString
		durationMS       sql.NullInt64
		costUSD          sql.NullFloat64
		commitSHA        sql.NullString
		resultCommentID  sql.NullInt64
		syncOutcome      string
		syncObservations string
		syncCommitSHA    sql.NullString
	)
	err := s.Scan(&run.ID, &run.SessionID, &run.SegmentID, &run.Ordinal, &run.Mode, &run.Agent,
		&run.TriggerCommentID, &status, &exitCode, &reason, &startedAt, &finishedAt,
		&durationMS, &costUSD, &run.LogPath, &commitSHA, &resultCommentID, &syncOutcome,
		&run.SyncBehind, &run.SyncAhead, &syncCommitSHA, &run.SemanticOutcome, &run.RecoveryReason, &syncObservations,
		&run.Published, &run.Publication)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}
	if err := json.Unmarshal([]byte(syncObservations), &run.SyncObservations); err != nil {
		return Run{}, fmt.Errorf("decode synchronization observations: %w", err)
	}
	run.Status = RunStatus(status)
	run.ExitCode = scanInt(exitCode)
	run.Error = reason.String
	run.DurationMS = scanInt(durationMS)
	run.CostUSD = scanFloat(costUSD)
	run.CommitSHA = commitSHA.String
	run.ResultCommentID = scanID(resultCommentID)
	run.SyncOutcome = SyncOutcome(syncOutcome)
	run.SyncCommitSHA = syncCommitSHA.String

	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{
		{startedAt, &run.StartedAt},
		{finishedAt, &run.FinishedAt},
	} {
		t, err := scanTime(field.src)
		if err != nil {
			return Run{}, err
		}
		*field.dst = t
	}
	return run, nil
}
