package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ConflictProposalStatus is a conflict-resolution proposal's durable
// lifecycle (US-11).
//
// It is deliberately three values wide rather than [ProposalStatus]'s four:
// this story has no confirmation grammar, so nothing ever decides a
// proposal. A proposal only ever moves from pending to superseded (a later
// proposal for the same session arrived) or expired (its TTL passed);
// "accepted" and "rejected" belong to a later story that does not exist yet.
type ConflictProposalStatus string

const (
	ConflictProposalPending    ConflictProposalStatus = "pending"
	ConflictProposalSuperseded ConflictProposalStatus = "superseded"
	ConflictProposalExpired    ConflictProposalStatus = "expired"
)

// MaxConflictProposalFiles caps how many files one [ConflictProposal] may
// carry. It is enforced here, at the store boundary, in addition to
// whatever cap internal/turn already applied when it collected and capped
// the conflicting content (S-9, S-17): a proposal this deep in the call
// stack must not be free to grow an unbounded row just because a future
// caller's own cap is loosened or skipped.
const MaxConflictProposalFiles = 100

// ConflictProposal is a durable suggested resolution for a run that was
// refused because its task branch would not merge cleanly with the base
// branch (US-4, US-11).
//
// It is deliberately narrow: only the conflicting paths and the model's own
// suggested resolution for each are stored. The raw conflicting hunks
// collected from the worktree and sent to the model are never persisted
// here or anywhere else (S-9, S-17, AC-11.9).
type ConflictProposal struct {
	ID, SessionID, TriggerCommentID int64
	// Model names the coding-agent tool this proposal's resolution came
	// from (an [agent.Kind] rendered as a string), for AC-11.1's comment.
	Model                 string
	Status                ConflictProposalStatus
	CreatedAt, ResolvedAt time.Time
	Files                 []ConflictProposalFile
}

// ConflictProposalFile is one conflicting path and its suggested
// resolution.
type ConflictProposalFile struct {
	ID, ProposalID int64
	Ordinal        int
	Path           string
	Resolution     string
}

// CreateConflictProposalParams identifies the blocked run's triggering
// comment and the validated resolution the advisor suggested.
type CreateConflictProposalParams struct {
	SessionID, TriggerCommentID int64
	Model                       string
	Files                       []ConflictProposalFile
}

// CreateOrGetConflictProposal records a conflict-resolution proposal once.
// Replaying the source comment returns the original proposal and never
// replaces its files, the same replay guarantee
// [DB.CreateOrGetScheduleProposal] gives.
//
// A new proposal supersedes every earlier pending proposal for the same
// session in the same transaction (AC-11.8): a session never has two
// pending proposals. Source-comment replay is checked first, so a proposal
// can never supersede itself.
func (db *DB) CreateOrGetConflictProposal(ctx context.Context, params CreateConflictProposalParams) (proposal ConflictProposal, created bool, err error) {
	if err := validateConflictProposalParams(params); err != nil {
		return ConflictProposal{}, false, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		existing, getErr := getConflictProposalBySource(ctx, tx, params.SessionID, params.TriggerCommentID)
		if getErr == nil {
			proposal = existing
			return loadConflictProposalFiles(ctx, tx, &proposal)
		}
		if !errors.Is(getErr, ErrNotFound) {
			return getErr
		}
		now := nowUTC()
		if getErr = supersedePendingConflictProposals(ctx, tx, params.SessionID, now); getErr != nil {
			return getErr
		}
		proposal, getErr = scanConflictProposal(tx.QueryRowContext(ctx, `
INSERT INTO conflict_proposals (session_id, trigger_comment_id, model, status, created_at)
VALUES (?, ?, ?, ?, ?) RETURNING `+conflictProposalColumns,
			params.SessionID, params.TriggerCommentID, params.Model, string(ConflictProposalPending), FormatTime(now)))
		if getErr != nil {
			return getErr
		}
		for i, file := range params.Files {
			stored, fileErr := scanConflictProposalFile(tx.QueryRowContext(ctx, `
INSERT INTO conflict_proposal_files (proposal_id, ordinal, path, resolution) VALUES (?, ?, ?, ?) RETURNING `+conflictProposalFileColumns,
				proposal.ID, i+1, file.Path, file.Resolution))
			if fileErr != nil {
				return fileErr
			}
			proposal.Files = append(proposal.Files, stored)
		}
		created = true
		return nil
	})
	if err != nil {
		return ConflictProposal{}, false, fmt.Errorf("store: create conflict proposal: %w", err)
	}
	return proposal, created, nil
}

// GetConflictProposal reads a proposal and its files in ordinal order.
func (db *DB) GetConflictProposal(ctx context.Context, id int64) (ConflictProposal, error) {
	p, err := scanConflictProposal(db.sql.QueryRowContext(ctx, `SELECT `+conflictProposalColumns+` FROM conflict_proposals WHERE id = ?`, id))
	if err != nil {
		return ConflictProposal{}, fmt.Errorf("store: read conflict proposal %d: %w", id, err)
	}
	if err := loadConflictProposalFiles(ctx, db.sql, &p); err != nil {
		return ConflictProposal{}, fmt.Errorf("store: read conflict proposal %d files: %w", id, err)
	}
	return p, nil
}

// ExpireConflictProposals closes proposals created before cutoff. The
// caller supplies its own clock, so expiry needs neither a timer nor a
// goroutine, the same as [DB.ExpireScheduleProposals].
func (db *DB) ExpireConflictProposals(ctx context.Context, cutoff, expiredAt time.Time) (int, error) {
	if cutoff.IsZero() || expiredAt.IsZero() {
		return 0, errors.New("store: expire conflict proposals: cutoff and time are required")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE conflict_proposals SET status = ?, resolved_at = ?
WHERE status = ? AND created_at < ?`, string(ConflictProposalExpired), FormatTime(expiredAt), string(ConflictProposalPending), FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("store: expire conflict proposals: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: expire conflict proposals rows affected: %w", err)
	}
	return int(count), nil
}

// supersedePendingConflictProposals marks every pending proposal for
// sessionID superseded, as the write half of the read-then-write
// [DB.CreateOrGetConflictProposal] performs under one BEGIN IMMEDIATE
// transaction (AC-11.8). It is unexported: a session's proposals are only
// ever superseded as part of recording the proposal that supersedes them,
// the same way [DB.CreateOrGetScheduleProposal] never exposes its own
// supersede step as a standalone call.
func supersedePendingConflictProposals(ctx context.Context, tx *sql.Tx, sessionID int64, at time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE conflict_proposals
SET status = ?, resolved_at = ?
WHERE session_id = ? AND status = ?`,
		string(ConflictProposalSuperseded), FormatTime(at), sessionID, string(ConflictProposalPending))
	return err
}

func validateConflictProposalParams(p CreateConflictProposalParams) error {
	if p.SessionID == 0 || p.Model == "" {
		return errors.New("store: create conflict proposal: session and model are required")
	}
	if p.TriggerCommentID < 0 {
		return errors.New("store: create conflict proposal: trigger comment id must not be negative")
	}
	if len(p.Files) == 0 {
		return errors.New("store: create conflict proposal: at least one file is required")
	}
	if len(p.Files) > MaxConflictProposalFiles {
		return fmt.Errorf("store: create conflict proposal: %d files exceeds the %d-file cap",
			len(p.Files), MaxConflictProposalFiles)
	}
	for _, file := range p.Files {
		if file.Path == "" || file.Resolution == "" {
			return errors.New("store: create conflict proposal: every file requires a path and a resolution")
		}
	}
	return nil
}

func getConflictProposalBySource(ctx context.Context, q querier, sessionID, triggerCommentID int64) (ConflictProposal, error) {
	return scanConflictProposal(q.QueryRowContext(ctx, `SELECT `+conflictProposalColumns+` FROM conflict_proposals WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerCommentID))
}

const conflictProposalColumns = `id, session_id, trigger_comment_id, model, status, created_at, resolved_at`
const conflictProposalFileColumns = `id, proposal_id, ordinal, path, resolution`

func scanConflictProposal(s scheduleScanner) (ConflictProposal, error) {
	var p ConflictProposal
	var status string
	var created, resolved sql.NullString
	if err := s.Scan(&p.ID, &p.SessionID, &p.TriggerCommentID, &p.Model, &status, &created, &resolved); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConflictProposal{}, ErrNotFound
		}
		return ConflictProposal{}, err
	}
	var err error
	if p.CreatedAt, err = scanTime(created); err != nil {
		return ConflictProposal{}, err
	}
	if p.ResolvedAt, err = scanTime(resolved); err != nil {
		return ConflictProposal{}, err
	}
	p.Status = ConflictProposalStatus(status)
	return p, nil
}

func scanConflictProposalFile(s scheduleScanner) (ConflictProposalFile, error) {
	var f ConflictProposalFile
	if err := s.Scan(&f.ID, &f.ProposalID, &f.Ordinal, &f.Path, &f.Resolution); err != nil {
		return ConflictProposalFile{}, err
	}
	return f, nil
}

func loadConflictProposalFiles(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, proposal *ConflictProposal) error {
	rows, err := q.QueryContext(ctx, `SELECT `+conflictProposalFileColumns+` FROM conflict_proposal_files WHERE proposal_id = ? ORDER BY ordinal`, proposal.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	proposal.Files = nil
	for rows.Next() {
		file, err := scanConflictProposalFile(rows)
		if err != nil {
			return err
		}
		proposal.Files = append(proposal.Files, file)
	}
	return rows.Err()
}
