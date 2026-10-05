package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ScheduleStatus is a scheduled item's durable lifecycle.
type ScheduleStatus string

// Schedule states persist execution progress across daemon restarts.
const (
	SchedulePending   ScheduleStatus = "pending"
	ScheduleRunning   ScheduleStatus = "running"
	ScheduleSucceeded ScheduleStatus = "succeeded"
	ScheduleFailed    ScheduleStatus = "failed"
	ScheduleBlocked   ScheduleStatus = "blocked"
)

// Schedule is one factory-owned unit of future work.
type Schedule struct {
	ID, SessionID, SourceCommentID, RunID int64
	Ordinal                               int
	OwnerHandle, Payload                  string
	DueAt                                 time.Time
	Delay                                 time.Duration
	Status                                ScheduleStatus
	StartedAt, FinishedAt                 time.Time
	CreatedAt, UpdatedAt                  time.Time
}

// scheduleTriggerBase reserves the top half of signed IDs for factory-private
// schedule identities. Forge comment IDs are positive database IDs and never
// approach this range; using a positive value preserves the public run trigger
// invariant that zero is the issue body and negatives are invalid input.
const scheduleTriggerBase int64 = 1 << 62

// TriggerID is the private, stable run identity for this scheduled item. It is
// deliberately distinct from SourceCommentID, which remains the forge-comment
// anchor for acknowledgements and blocked-work replies.
func (s Schedule) TriggerID() int64 { return scheduleTriggerBase + s.ID }

// IsScheduleTrigger reports whether id is a factory-private scheduled-work
// identity rather than a forge comment id.
func IsScheduleTrigger(id int64) bool { return id > scheduleTriggerBase }

// CreateScheduleParams describes a direct schedule. A schedule is either due
// at DueAt, or waits for PrerequisiteIDs and then Delay. SourceCommentID makes
// replaying a forge delivery return the original row rather than new work.
type CreateScheduleParams struct {
	SessionID, SourceCommentID int64
	OwnerHandle, Payload       string
	DueAt                      time.Time
	Delay                      time.Duration
	PrerequisiteIDs            []int64
}

// CreateOrGetSchedule atomically records a direct schedule and its
// prerequisites. A repeated source comment returns the original schedule and
// created=false. Prerequisites must belong to the same session and may not
// introduce a cycle.
func (db *DB) CreateOrGetSchedule(ctx context.Context, params CreateScheduleParams) (schedule Schedule, created bool, err error) {
	if err := validateScheduleParams(params); err != nil {
		return Schedule{}, false, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		existing, getErr := getScheduleBySource(ctx, tx, params.SessionID, params.SourceCommentID, 1)
		if getErr == nil {
			schedule = existing
			return nil
		}
		if !errors.Is(getErr, ErrNotFound) {
			return getErr
		}
		dueAt := params.DueAt
		if len(params.PrerequisiteIDs) > 0 {
			var ready bool
			dueAt, ready, getErr = prerequisiteDueAt(ctx, tx, params.SessionID, params.PrerequisiteIDs, params.Delay)
			if getErr != nil {
				return getErr
			}
			if !ready {
				dueAt = time.Time{}
			}
		}
		now := nowUTC()
		schedule, getErr = scanSchedule(tx.QueryRowContext(ctx, `
INSERT INTO schedules (session_id, source_comment_id, ordinal, owner_handle, payload, due_at, delay_ms, status, created_at, updated_at)
VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+scheduleColumns, params.SessionID, params.SourceCommentID, params.OwnerHandle, params.Payload,
			nullTime(dueAt), params.Delay.Milliseconds(), string(SchedulePending), FormatTime(now), FormatTime(now)))
		if getErr != nil {
			return getErr
		}
		if err := insertPrerequisites(ctx, tx, schedule.ID, params.PrerequisiteIDs); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Schedule{}, false, fmt.Errorf("store: create schedule: %w", err)
	}
	return schedule, created, nil
}

// ScheduleAttempted reports whether the schedule a session recorded under a
// source identity has already left the pending state: it started, finished or
// failed, so the same work is not started again by the factory.
func (db *DB) ScheduleAttempted(ctx context.Context, sessionID, sourceID int64) (bool, error) {
	var attempted bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schedules WHERE session_id = ? AND source_comment_id = ? AND ordinal = 1 AND status <> ?)`, sessionID, sourceID, string(SchedulePending)).Scan(&attempted)
	if err != nil {
		return false, fmt.Errorf("store: check schedule attempt: %w", err)
	}
	return attempted, nil
}

// GetSchedule reads a schedule by its stable ID.
func (db *DB) GetSchedule(ctx context.Context, id int64) (Schedule, error) {
	s, err := scanSchedule(db.sql.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = ?`, id))
	if err != nil {
		return Schedule{}, fmt.Errorf("store: read schedule %d: %w", id, err)
	}
	return s, nil
}

// ListEligibleSchedules returns pending, time-due schedules in deterministic
// due-time then ID order. Dependency schedules receive a due time atomically
// when their final prerequisite succeeds.
func (db *DB) ListEligibleSchedules(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	if now.IsZero() {
		return nil, errors.New("store: list eligible schedules: now is required")
	}
	if limit <= 0 {
		return nil, errors.New("store: list eligible schedules: limit must be positive")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules
WHERE status = ? AND due_at IS NOT NULL AND due_at <= ? ORDER BY due_at, id LIMIT ?`, string(SchedulePending), FormatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list eligible schedules: %w", err)
	}
	defer rows.Close()
	return collectSchedules(rows)
}

// ReserveSchedule changes a due pending schedule to running exactly once.
// reserved=false is the ordinary result when another dispatcher won or it is
// not due yet.
func (db *DB) ReserveSchedule(ctx context.Context, id int64, now time.Time) (schedule Schedule, reserved bool, err error) {
	if id == 0 || now.IsZero() {
		return Schedule{}, false, errors.New("store: reserve schedule: id and now are required")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
UPDATE schedules SET status = ?, started_at = ?, updated_at = ?
WHERE id = ? AND status = ? AND due_at IS NOT NULL AND due_at <= ?
RETURNING `+scheduleColumns, string(ScheduleRunning), FormatTime(now), FormatTime(now), id, string(SchedulePending), FormatTime(now))
		var scanErr error
		schedule, scanErr = scanSchedule(row)
		if errors.Is(scanErr, ErrNotFound) {
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		reserved = true
		return nil
	})
	if err != nil {
		return Schedule{}, false, fmt.Errorf("store: reserve schedule %d: %w", id, err)
	}
	return schedule, reserved, nil
}

// RequeueSchedule returns a reservation to pending when its worker could not
// acquire the session lock. No agent ran in that case, so leaving the row
// running would turn transient cross-process contention into lost work.
func (db *DB) RequeueSchedule(ctx context.Context, id int64, now time.Time) (bool, error) {
	if id == 0 || now.IsZero() {
		return false, errors.New("store: requeue schedule: id and now are required")
	}
	result, err := db.sql.ExecContext(ctx, `
UPDATE schedules SET status = ?, started_at = NULL, updated_at = ?
WHERE id = ? AND status = ?`, string(SchedulePending), FormatTime(now), id, string(ScheduleRunning))
	if err != nil {
		return false, fmt.Errorf("store: requeue schedule %d: %w", id, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: requeue schedule %d rows affected: %w", id, err)
	}
	return changed == 1, nil
}

// FinishSchedule records a reserved schedule's terminal result. Failed work
// blocks every pending descendant in the same transaction; successful work
// resolves directly dependent schedules whose full prerequisite set succeeded.
func (db *DB) FinishSchedule(ctx context.Context, id int64, status ScheduleStatus, runID int64, finishedAt time.Time) (Schedule, error) {
	if status != ScheduleSucceeded && status != ScheduleFailed {
		return Schedule{}, errors.New("store: finish schedule: status must be succeeded or failed")
	}
	if finishedAt.IsZero() {
		finishedAt = nowUTC()
	}
	var schedule Schedule
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var scanErr error
		schedule, scanErr = scanSchedule(tx.QueryRowContext(ctx, `
UPDATE schedules SET status = ?, run_id = COALESCE(?, run_id), finished_at = ?, updated_at = ?
WHERE id = ? AND status = ? RETURNING `+scheduleColumns,
			string(status), nullID(runID), FormatTime(finishedAt), FormatTime(finishedAt), id, string(ScheduleRunning)))
		if scanErr != nil {
			return scanErr
		}
		if status == ScheduleFailed {
			return blockDescendants(ctx, tx, id, finishedAt)
		}
		return resolveDependentDueTimes(ctx, tx, id, finishedAt)
	})
	if err != nil {
		return Schedule{}, fmt.Errorf("store: finish schedule %d: %w", id, err)
	}
	return schedule, nil
}

// CountPendingSchedules reports the outstanding count used by daemon logs.
func (db *DB) CountPendingSchedules(ctx context.Context) (int, error) {
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE status = ?`, string(SchedulePending)).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: count pending schedules: %w", err)
	}
	return count, nil
}

// ListUnreportedBlockedDescendants returns blocked descendants of root whose
// factory outcome reply has not been recorded. Callers post first, then call
// MarkScheduleBlockedReply; a crash can duplicate a reply but cannot lose one.
func (db *DB) ListUnreportedBlockedDescendants(ctx context.Context, rootID int64) ([]Schedule, error) {
	if rootID == 0 {
		return nil, errors.New("store: list blocked schedule descendants: id is required")
	}
	rows, err := db.sql.QueryContext(ctx, `
WITH RECURSIVE descendants(id) AS (
 SELECT schedule_id FROM schedule_prerequisites WHERE prerequisite_id = ?
 UNION
 SELECT p.schedule_id FROM schedule_prerequisites p JOIN descendants d ON p.prerequisite_id = d.id
)
SELECT `+scheduleColumns+` FROM schedules
WHERE id IN descendants AND status = ?
  AND NOT EXISTS (SELECT 1 FROM schedule_block_replies r WHERE r.schedule_id = schedules.id)
ORDER BY id`, rootID, string(ScheduleBlocked))
	if err != nil {
		return nil, fmt.Errorf("store: list blocked schedule descendants: %w", err)
	}
	defer rows.Close()
	return collectSchedules(rows)
}

// MarkScheduleBlockedReply records a reply only after it reached the forge.
func (db *DB) MarkScheduleBlockedReply(ctx context.Context, id int64, repliedAt time.Time) error {
	if id == 0 || repliedAt.IsZero() {
		return errors.New("store: mark blocked schedule reply: id and time are required")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO schedule_block_replies (schedule_id, replied_at) VALUES (?, ?) ON CONFLICT (schedule_id) DO NOTHING`, id, FormatTime(repliedAt))
	if err != nil {
		return fmt.Errorf("store: mark blocked schedule reply %d: %w", id, err)
	}
	return nil
}

func validateScheduleParams(p CreateScheduleParams) error {
	if p.SessionID == 0 || p.SourceCommentID == 0 || p.OwnerHandle == "" || p.Payload == "" {
		return errors.New("store: create schedule: session, source comment, owner, and payload are required")
	}
	if p.Delay < 0 {
		return errors.New("store: create schedule: delay cannot be negative")
	}
	if p.DueAt.IsZero() && len(p.PrerequisiteIDs) == 0 {
		return errors.New("store: create schedule: due time or prerequisites are required")
	}
	if !p.DueAt.IsZero() && len(p.PrerequisiteIDs) > 0 {
		return errors.New("store: create schedule: due time and prerequisites are mutually exclusive")
	}
	seen := make(map[int64]struct{}, len(p.PrerequisiteIDs))
	for _, id := range p.PrerequisiteIDs {
		if id <= 0 {
			return errors.New("store: create schedule: prerequisite id must be positive")
		}
		if _, ok := seen[id]; ok {
			return errors.New("store: create schedule: prerequisite ids must not repeat")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func insertPrerequisites(ctx context.Context, tx *sql.Tx, scheduleID int64, ids []int64) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_prerequisites (schedule_id, prerequisite_id) VALUES (?, ?)`, scheduleID, id); err != nil {
			return fmt.Errorf("insert prerequisite %d: %w", id, err)
		}
	}
	return nil
}

func prerequisiteDueAt(ctx context.Context, tx *sql.Tx, sessionID int64, ids []int64, delay time.Duration) (time.Time, bool, error) {
	var latest time.Time
	for _, id := range ids {
		s, err := scanSchedule(tx.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = ?`, id))
		if err != nil {
			return time.Time{}, false, fmt.Errorf("read prerequisite %d: %w", id, err)
		}
		if s.SessionID != sessionID {
			return time.Time{}, false, errors.New("schedule prerequisites must belong to the same session")
		}
		if s.Status == ScheduleFailed || s.Status == ScheduleBlocked {
			return time.Time{}, false, errors.New("schedule prerequisite has failed or is blocked")
		}
		if s.Status != ScheduleSucceeded || s.FinishedAt.IsZero() {
			return time.Time{}, false, nil
		}
		if s.FinishedAt.After(latest) {
			latest = s.FinishedAt
		}
	}
	return latest.Add(delay), true, nil
}

func resolveDependentDueTimes(ctx context.Context, tx *sql.Tx, prerequisiteID int64, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.id, s.session_id, s.delay_ms FROM schedules s JOIN schedule_prerequisites p ON p.schedule_id = s.id WHERE p.prerequisite_id = ? AND s.status = ? AND s.due_at IS NULL ORDER BY s.id`, prerequisiteID, string(SchedulePending))
	if err != nil {
		return err
	}
	defer rows.Close()
	type candidate struct{ id, sessionID, delayMS int64 }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.sessionID, &c.delayMS); err != nil {
			return err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range candidates {
		ids, err := prerequisiteIDs(ctx, tx, c.id)
		if err != nil {
			return err
		}
		dueAt, ready, err := prerequisiteDueAt(ctx, tx, c.sessionID, ids, time.Duration(c.delayMS)*time.Millisecond)
		if err != nil {
			return err
		}
		if ready {
			if _, err := tx.ExecContext(ctx, `UPDATE schedules SET due_at = ?, updated_at = ? WHERE id = ? AND due_at IS NULL`, FormatTime(dueAt), FormatTime(now), c.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func blockDescendants(ctx context.Context, tx *sql.Tx, id int64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `WITH RECURSIVE descendants(id) AS (
 SELECT schedule_id FROM schedule_prerequisites WHERE prerequisite_id = ?
 UNION
 SELECT p.schedule_id FROM schedule_prerequisites p JOIN descendants d ON p.prerequisite_id = d.id
) UPDATE schedules SET status = ?, finished_at = ?, updated_at = ? WHERE id IN descendants AND status = ?`, id, string(ScheduleBlocked), FormatTime(now), FormatTime(now), string(SchedulePending))
	return err
}

func prerequisiteIDs(ctx context.Context, tx *sql.Tx, scheduleID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT prerequisite_id FROM schedule_prerequisites WHERE schedule_id = ? ORDER BY prerequisite_id`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type scheduleScanner interface{ Scan(dest ...any) error }

const scheduleColumns = `id, session_id, source_comment_id, ordinal, owner_handle, payload, due_at, delay_ms, status, run_id, started_at, finished_at, created_at, updated_at`

func scanSchedule(s scheduleScanner) (Schedule, error) {
	var out Schedule
	var due, started, finished, created, updated sql.NullString
	var status string
	var runID sql.NullInt64
	var delayMS int64
	if err := s.Scan(&out.ID, &out.SessionID, &out.SourceCommentID, &out.Ordinal, &out.OwnerHandle, &out.Payload, &due, &delayMS, &status, &runID, &started, &finished, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, err
	}
	var err error
	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{{due, &out.DueAt}, {started, &out.StartedAt}, {finished, &out.FinishedAt}, {created, &out.CreatedAt}, {updated, &out.UpdatedAt}} {
		if *field.dst, err = scanTime(field.src); err != nil {
			return Schedule{}, err
		}
	}
	out.Delay = time.Duration(delayMS) * time.Millisecond
	out.Status = ScheduleStatus(status)
	out.RunID = scanID(runID)
	return out, nil
}
func getScheduleBySource(ctx context.Context, q querier, sessionID, sourceID int64, ordinal int) (Schedule, error) {
	return scanSchedule(q.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE session_id = ? AND source_comment_id = ? AND ordinal = ?`, sessionID, sourceID, ordinal))
}
func collectSchedules(rows *sql.Rows) ([]Schedule, error) {
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ProposalStatus is the lifecycle of an agent-produced schedule proposal.
type ProposalStatus string

// Proposal states distinguish owner decisions from expired advice.
const (
	ProposalPending  ProposalStatus = "pending"
	ProposalAccepted ProposalStatus = "accepted"
	ProposalRejected ProposalStatus = "rejected"
	ProposalExpired  ProposalStatus = "expired"
)

// ScheduleProposal is a durable proposed sequence. It is deliberately
// separate from schedules: no work exists until the owner accepts it.
type ScheduleProposal struct {
	ID, SessionID, SourceCommentID int64
	OwnerHandle, Description       string
	Status                         ProposalStatus
	CreatedAt, DecidedAt           time.Time
	Items                          []ScheduleProposalItem
}

// ScheduleProposalItem is one concrete suggested time and payload.
type ScheduleProposalItem struct {
	ID, ProposalID int64
	Ordinal        int
	Payload        string
	DueAt          time.Time
}

// CreateScheduleProposalParams identifies a proposal-producing comment and
// the validated concrete work the agent suggested.
type CreateScheduleProposalParams struct {
	SessionID, SourceCommentID int64
	OwnerHandle, Description   string
	Items                      []ScheduleProposalItem
}

// CreateOrGetScheduleProposal records an agent proposal once. Replaying the
// source comment returns the original proposal and never replaces its items.
func (db *DB) CreateOrGetScheduleProposal(ctx context.Context, params CreateScheduleProposalParams) (proposal ScheduleProposal, created bool, err error) {
	if err := validateProposalParams(params); err != nil {
		return ScheduleProposal{}, false, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		existing, getErr := getProposalBySource(ctx, tx, params.SessionID, params.SourceCommentID)
		if getErr == nil {
			proposal = existing
			return loadProposalItems(ctx, tx, &proposal)
		}
		if !errors.Is(getErr, ErrNotFound) {
			return getErr
		}
		now := nowUTC()
		// A new proposal supersedes earlier pending advice from this owner on
		// this issue. Source-comment replay is checked first, so it cannot
		// replace its own durable result.
		if _, getErr = tx.ExecContext(ctx, `UPDATE schedule_proposals
SET status = ?, decided_at = ?
WHERE session_id = ? AND owner_handle = ? AND status = ?`,
			string(ProposalRejected), FormatTime(now), params.SessionID, params.OwnerHandle, string(ProposalPending)); getErr != nil {
			return getErr
		}
		proposal, getErr = scanProposal(tx.QueryRowContext(ctx, `
INSERT INTO schedule_proposals (session_id, source_comment_id, owner_handle, description, status, created_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING `+proposalColumns, params.SessionID, params.SourceCommentID, params.OwnerHandle, params.Description, string(ProposalPending), FormatTime(now)))
		if getErr != nil {
			return getErr
		}
		for i, item := range params.Items {
			stored, itemErr := scanProposalItem(tx.QueryRowContext(ctx, `
INSERT INTO schedule_proposal_items (proposal_id, ordinal, payload, due_at) VALUES (?, ?, ?, ?) RETURNING `+proposalItemColumns,
				proposal.ID, i+1, item.Payload, FormatTime(item.DueAt)))
			if itemErr != nil {
				return itemErr
			}
			proposal.Items = append(proposal.Items, stored)
		}
		created = true
		return nil
	})
	if err != nil {
		return ScheduleProposal{}, false, fmt.Errorf("store: create schedule proposal: %w", err)
	}
	return proposal, created, nil
}

// GetScheduleProposal reads a proposal and its items in ordinal order.
func (db *DB) GetScheduleProposal(ctx context.Context, id int64) (ScheduleProposal, error) {
	p, err := scanProposal(db.sql.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM schedule_proposals WHERE id = ?`, id))
	if err != nil {
		return ScheduleProposal{}, fmt.Errorf("store: read schedule proposal %d: %w", id, err)
	}
	if err := loadProposalItems(ctx, db.sql, &p); err != nil {
		return ScheduleProposal{}, fmt.Errorf("store: read schedule proposal %d items: %w", id, err)
	}
	return p, nil
}

// AcceptScheduleProposal atomically turns every item in a pending proposal
// into a schedule and records acceptance. A second acceptance returns the
// already-created schedules with accepted=false; it cannot duplicate work.
func (db *DB) AcceptScheduleProposal(ctx context.Context, id int64, acceptedAt time.Time) (schedules []Schedule, accepted bool, err error) {
	if id == 0 {
		return nil, false, errors.New("store: accept schedule proposal: id is required")
	}
	if acceptedAt.IsZero() {
		acceptedAt = nowUTC()
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		proposal, getErr := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM schedule_proposals WHERE id = ?`, id))
		if getErr != nil {
			return getErr
		}
		if err := loadProposalItems(ctx, tx, &proposal); err != nil {
			return err
		}
		switch proposal.Status {
		case ProposalAccepted:
			var listErr error
			schedules, listErr = listSchedulesBySource(ctx, tx, proposal.SessionID, proposal.SourceCommentID)
			return listErr
		case ProposalRejected:
			return errors.New("schedule proposal has been rejected")
		case ProposalExpired:
			return errors.New("schedule proposal has expired")
		case ProposalPending:
		default:
			return fmt.Errorf("schedule proposal has unknown status %q", proposal.Status)
		}
		for _, item := range proposal.Items {
			s, itemErr := scanSchedule(tx.QueryRowContext(ctx, `
INSERT INTO schedules (session_id, source_comment_id, ordinal, owner_handle, payload, due_at, delay_ms, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?) RETURNING `+scheduleColumns,
				proposal.SessionID, proposal.SourceCommentID, item.Ordinal, proposal.OwnerHandle, item.Payload, FormatTime(item.DueAt), string(SchedulePending), FormatTime(acceptedAt), FormatTime(acceptedAt)))
			if itemErr != nil {
				return itemErr
			}
			schedules = append(schedules, s)
		}
		res, updateErr := tx.ExecContext(ctx, `UPDATE schedule_proposals SET status = ?, decided_at = ? WHERE id = ? AND status = ?`, string(ProposalAccepted), FormatTime(acceptedAt), proposal.ID, string(ProposalPending))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = requireAffected(res, "store: accept schedule proposal"); updateErr != nil {
			return updateErr
		}
		accepted = true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("store: accept schedule proposal %d: %w", id, err)
	}
	return schedules, accepted, nil
}

// RejectScheduleProposal marks a pending proposal rejected. It cannot reject
// an accepted proposal, because accepted work is already durable.
func (db *DB) RejectScheduleProposal(ctx context.Context, id int64, rejectedAt time.Time) (ScheduleProposal, bool, error) {
	if id == 0 {
		return ScheduleProposal{}, false, errors.New("store: reject schedule proposal: id is required")
	}
	if rejectedAt.IsZero() {
		rejectedAt = nowUTC()
	}
	var proposal ScheduleProposal
	var rejected bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM schedule_proposals WHERE id = ?`, id))
		if err != nil {
			return err
		}
		switch current.Status {
		case ProposalRejected:
			proposal = current
			return loadProposalItems(ctx, tx, &proposal)
		case ProposalAccepted:
			return errors.New("schedule proposal has been accepted")
		case ProposalExpired:
			return errors.New("schedule proposal has expired")
		case ProposalPending:
			proposal, err = scanProposal(tx.QueryRowContext(ctx, `UPDATE schedule_proposals SET status = ?, decided_at = ? WHERE id = ? AND status = ? RETURNING `+proposalColumns, string(ProposalRejected), FormatTime(rejectedAt), id, string(ProposalPending)))
			if err != nil {
				return err
			}
			rejected = true
			return loadProposalItems(ctx, tx, &proposal)
		default:
			return fmt.Errorf("schedule proposal has unknown status %q", current.Status)
		}
	})
	if err != nil {
		return ScheduleProposal{}, false, fmt.Errorf("store: reject schedule proposal %d: %w", id, err)
	}
	return proposal, rejected, nil
}

// ExpireScheduleProposals closes proposals created before cutoff. The daemon
// provides its injected clock, so expiry needs neither a timer nor a goroutine.
func (db *DB) ExpireScheduleProposals(ctx context.Context, cutoff, expiredAt time.Time) (int, error) {
	if cutoff.IsZero() || expiredAt.IsZero() {
		return 0, errors.New("store: expire schedule proposals: cutoff and time are required")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE schedule_proposals SET status = ?, decided_at = ?
WHERE status = ? AND created_at < ?`, string(ProposalExpired), FormatTime(expiredAt), string(ProposalPending), FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: expire schedule proposals: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: expire schedule proposals rows affected: %w", err)
	}
	return int(count), nil
}

func validateProposalParams(p CreateScheduleProposalParams) error {
	if p.SessionID == 0 || p.SourceCommentID == 0 || p.OwnerHandle == "" || p.Description == "" {
		return errors.New("store: create schedule proposal: session, source comment, owner, and description are required")
	}
	if len(p.Items) == 0 {
		return errors.New("store: create schedule proposal: at least one item is required")
	}
	for _, item := range p.Items {
		if item.Payload == "" || item.DueAt.IsZero() {
			return errors.New("store: create schedule proposal: every item requires payload and due time")
		}
	}
	return nil
}

func getProposalBySource(ctx context.Context, q querier, sessionID, sourceID int64) (ScheduleProposal, error) {
	return scanProposal(q.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM schedule_proposals WHERE session_id = ? AND source_comment_id = ?`, sessionID, sourceID))
}
func listSchedulesBySource(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, sessionID, sourceID int64) ([]Schedule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE session_id = ? AND source_comment_id = ? ORDER BY ordinal`, sessionID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectSchedules(rows)
}

const proposalColumns = `id, session_id, source_comment_id, owner_handle, description, status, created_at, decided_at`
const proposalItemColumns = `id, proposal_id, ordinal, payload, due_at`

func scanProposal(s scheduleScanner) (ScheduleProposal, error) {
	var p ScheduleProposal
	var status string
	var created, decided sql.NullString
	if err := s.Scan(&p.ID, &p.SessionID, &p.SourceCommentID, &p.OwnerHandle, &p.Description, &status, &created, &decided); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ScheduleProposal{}, ErrNotFound
		}
		return ScheduleProposal{}, err
	}
	var err error
	if p.CreatedAt, err = scanTime(created); err != nil {
		return ScheduleProposal{}, err
	}
	if p.DecidedAt, err = scanTime(decided); err != nil {
		return ScheduleProposal{}, err
	}
	p.Status = ProposalStatus(status)
	return p, nil
}
func scanProposalItem(s scheduleScanner) (ScheduleProposalItem, error) {
	var item ScheduleProposalItem
	var due sql.NullString
	if err := s.Scan(&item.ID, &item.ProposalID, &item.Ordinal, &item.Payload, &due); err != nil {
		return ScheduleProposalItem{}, err
	}
	dueAt, err := scanTime(due)
	if err != nil {
		return ScheduleProposalItem{}, err
	}
	item.DueAt = dueAt
	return item, nil
}
func loadProposalItems(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, proposal *ScheduleProposal) error {
	rows, err := q.QueryContext(ctx, `SELECT `+proposalItemColumns+` FROM schedule_proposal_items WHERE proposal_id = ? ORDER BY ordinal`, proposal.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	proposal.Items = nil
	for rows.Next() {
		item, err := scanProposalItem(rows)
		if err != nil {
			return err
		}
		proposal.Items = append(proposal.Items, item)
	}
	return rows.Err()
}
