package store

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Conflict proposal states distinguish pending advice from terminal outcomes.
const (
	ConflictProposalPending    ConflictProposalStatus = "pending"
	ConflictProposalSuperseded ConflictProposalStatus = "superseded"
	ConflictProposalExpired    ConflictProposalStatus = "expired"
	// ConflictProposalApplied is US-12's addition: a confirmed proposal
	// whose files were written, staged, and whose merge was completed. It
	// is terminal — nothing moves a proposal off "applied" — and is what
	// makes a second confirmation of the same proposal a no-op reported as
	// already applied (AC-12.9) rather than a second commit.
	ConflictProposalApplied ConflictProposalStatus = "applied"
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
	Format                int

	// BaseSHA and BranchSHA are the merge-target and local tips this proposal's
	// suggestion was computed against (US-12, S-15): captured once, at
	// proposal time, and re-verified fresh inside the CloneGate immediately
	// before a confirmation is applied. A mismatch means the branch or base
	// moved since the suggestion was computed, and marks the proposal
	// superseded rather than applying content confirmed against code that
	// no longer exists.
	BaseSHA, BranchSHA string
	Inputs             ConflictProposalInputs

	// ConfirmedBy and ConfirmedCommentID record who confirmed this proposal
	// and which comment did it, set only once a confirmation has passed
	// every gate (S-12). AppliedCommitSHA and AppliedAt are set only once a
	// merge commit has actually been made; all four are zero until then.
	ConfirmedBy        string
	ConfirmedCommentID int64
	AppliedCommitSHA   string
	AppliedAt          time.Time
}

// ConflictProposalInputs binds both remote inputs observed under the clone gate.
// Empty TaskSHA means confirmed absence; Observed distinguishes old unbound history.
type ConflictProposalInputs struct {
	TargetPhase string `json:"target_phase"`
	TargetRef   string `json:"target_ref"`
	BaseRef     string `json:"base_ref"`
	BaseSHA     string `json:"base_sha"`
	TaskRef     string `json:"task_ref"`
	TaskSHA     string `json:"task_sha"`
	Observed    bool   `json:"observed"`
}

// ConflictProposalFile is one conflicting path and its suggested
// resolution.
type ConflictProposalFile struct {
	ID, ProposalID int64
	Ordinal        int
	Path           string
	Resolution     string
	Details        ConflictFileDetails
}

// ConflictRegionFormat identifies region-only proposals. Zero is legacy.
const ConflictRegionFormat = 1

// ConflictFileDetails contains generated regions and identities, never raw originals.
type ConflictFileDetails struct {
	OriginalDigest, ResultDigest, Mode string
	Regions                            []ConflictRegionResolution
	Metrics                            ConflictFileMetrics
	Diff                               string
}

// ConflictRegionResolution binds generated text to one canonical region identity.
type ConflictRegionResolution struct{ ID, Resolution string }

// ConflictFileMetrics records raw baseline-relative sizes and line changes.
type ConflictFileMetrics struct{ BeforeBytes, AfterBytes, BeforeLines, AfterLines, Additions, Deletions int }

// CreateConflictProposalParams identifies the blocked run's triggering
// comment and the validated resolution the advisor suggested.
type CreateConflictProposalParams struct {
	SessionID, TriggerCommentID int64
	Model                       string
	Files                       []ConflictProposalFile
	Format                      int
	// BaseSHA and BranchSHA are the merge-target and local tips the suggestion was
	// computed against (US-12, S-15). Both are required: a proposal with no
	// stored starting point can never be confirmed, since nothing could
	// ever compare equal to an empty sha.
	BaseSHA, BranchSHA string
	Inputs             ConflictProposalInputs
	// At is when the proposal is created; zero means now. Callers with their
	// own clock pass it so expiry compares times from one clock.
	At time.Time
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
		now := params.At.UTC()
		if params.At.IsZero() {
			now = nowUTC()
		}
		if getErr = supersedePendingConflictProposals(ctx, tx, params.SessionID, now); getErr != nil {
			return getErr
		}
		inputs, marshalErr := json.Marshal(params.Inputs)
		if marshalErr != nil {
			return marshalErr
		}
		proposal, getErr = scanConflictProposal(tx.QueryRowContext(ctx, `
INSERT INTO conflict_proposals (session_id, trigger_comment_id, model, status, created_at, base_sha, branch_sha, format, inputs)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING `+conflictProposalColumns,
			params.SessionID, params.TriggerCommentID, params.Model, string(ConflictProposalPending), FormatTime(now),
			params.BaseSHA, params.BranchSHA, params.Format, string(inputs)))
		if getErr != nil {
			return getErr
		}
		for i, file := range params.Files {
			details, marshalErr := json.Marshal(file.Details)
			if marshalErr != nil {
				return marshalErr
			}
			stored, fileErr := scanConflictProposalFile(tx.QueryRowContext(ctx, `
INSERT INTO conflict_proposal_files (proposal_id, ordinal, path, resolution, details) VALUES (?, ?, ?, ?, ?) RETURNING `+conflictProposalFileColumns,
				proposal.ID, i+1, file.Path, file.Resolution, string(details)))
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

// GetConflictProposalBySource reads the proposal bound to the original request
// without regenerating it or changing its confirmation status.
func (db *DB) GetConflictProposalBySource(ctx context.Context, sessionID, triggerCommentID int64) (ConflictProposal, error) {
	p, err := getConflictProposalBySource(ctx, db.sql, sessionID, triggerCommentID)
	if err != nil {
		return ConflictProposal{}, err
	}
	if err := loadConflictProposalFiles(ctx, db.sql, &p); err != nil {
		return ConflictProposal{}, err
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

// SupersedeConflictProposal marks one pending proposal superseded (US-12,
// S-15): the confirmation-time counterpart of
// [DB.CreateOrGetConflictProposal]'s own supersede-on-new-proposal step,
// reached when the base or branch tip re-verified inside the CloneGate at
// apply time no longer matches the sha this proposal's suggestion was
// computed against. The update is conditional on the row still being
// pending, so a proposal already decided by something else is left exactly
// as it was rather than clobbered by a stale confirmation that lost a race.
func (db *DB) SupersedeConflictProposal(ctx context.Context, id int64, at time.Time) (ConflictProposal, error) {
	if id <= 0 || at.IsZero() {
		return ConflictProposal{}, errors.New("store: supersede conflict proposal: id and time are required")
	}
	proposal, err := scanConflictProposal(db.sql.QueryRowContext(ctx, `
UPDATE conflict_proposals SET status = ?, resolved_at = ?
WHERE id = ? AND status = ? RETURNING `+conflictProposalColumns,
		string(ConflictProposalSuperseded), FormatTime(at), id, string(ConflictProposalPending)))
	if err != nil {
		return ConflictProposal{}, fmt.Errorf("store: supersede conflict proposal %d: %w", id, err)
	}
	if err := loadConflictProposalFiles(ctx, db.sql, &proposal); err != nil {
		return ConflictProposal{}, fmt.Errorf("store: supersede conflict proposal %d files: %w", id, err)
	}
	return proposal, nil
}

// MarkConflictProposalApplied records that a confirmed proposal's resolution
// was written, staged, and its merge completed (US-12, AC-12.5). The update
// is conditional on the row still being pending — the same guard
// [DB.SupersedeConflictProposal] applies — so a proposal already applied by
// a concurrent confirmation is reported back as [ErrNotFound] rather than
// silently overwritten with a second commit's sha (AC-12.9).
func (db *DB) MarkConflictProposalApplied(ctx context.Context, id int64, confirmedBy string, confirmedCommentID int64, commitSHA string, appliedAt time.Time) (ConflictProposal, error) {
	if id <= 0 || confirmedBy == "" || commitSHA == "" || appliedAt.IsZero() {
		return ConflictProposal{}, errors.New("store: mark conflict proposal applied: id, confirmer, commit sha and time are required")
	}
	if confirmedCommentID < 0 {
		return ConflictProposal{}, errors.New("store: mark conflict proposal applied: confirmed comment id must not be negative")
	}
	proposal, err := scanConflictProposal(db.sql.QueryRowContext(ctx, `
UPDATE conflict_proposals
SET status = ?, resolved_at = ?, confirmed_by = ?, confirmed_comment_id = ?, applied_commit_sha = ?, applied_at = ?
WHERE id = ? AND status = ? RETURNING `+conflictProposalColumns,
		string(ConflictProposalApplied), FormatTime(appliedAt), confirmedBy, confirmedCommentID, commitSHA, FormatTime(appliedAt),
		id, string(ConflictProposalPending)))
	if err != nil {
		return ConflictProposal{}, fmt.Errorf("store: mark conflict proposal %d applied: %w", id, err)
	}
	if err := loadConflictProposalFiles(ctx, db.sql, &proposal); err != nil {
		return ConflictProposal{}, fmt.Errorf("store: mark conflict proposal %d applied files: %w", id, err)
	}
	return proposal, nil
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
	if p.Inputs.Observed {
		i := p.Inputs
		targetSHA := i.BaseSHA
		targetRef := i.BaseRef
		if i.TargetPhase == "task" {
			targetSHA, targetRef = i.TaskSHA, i.TaskRef
		}
		if (i.TargetPhase != "base" && i.TargetPhase != "task") || i.TargetRef != targetRef || i.BaseRef == "" || i.BaseSHA == "" || i.TaskRef == "" || targetSHA == "" || targetSHA != p.BaseSHA {
			return errors.New("store: inconsistent conflict proposal input binding")
		}
	}

	if p.SessionID == 0 || p.Model == "" {
		return errors.New("store: create conflict proposal: session and model are required")
	}
	if p.BaseSHA == "" || p.BranchSHA == "" {
		return errors.New("store: create conflict proposal: base and branch shas are required")
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
		if p.Format == ConflictRegionFormat && (file.Resolution != "" || len(file.Details.Regions) == 0 || file.Details.OriginalDigest == "" || file.Details.ResultDigest == "") {
			return errors.New("store: region proposal requires identities and regions, without whole-file resolution")
		}
		if file.Path == "" || (p.Format == 0 && file.Resolution == "") {
			return errors.New("store: create conflict proposal: every file requires a path and a resolution")
		}
	}
	return nil
}

func getConflictProposalBySource(ctx context.Context, q querier, sessionID, triggerCommentID int64) (ConflictProposal, error) {
	return scanConflictProposal(q.QueryRowContext(ctx, `SELECT `+conflictProposalColumns+` FROM conflict_proposals WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerCommentID))
}

const conflictProposalColumns = `id, session_id, trigger_comment_id, model, status, created_at, resolved_at,
	base_sha, branch_sha, confirmed_by, confirmed_comment_id, applied_commit_sha, applied_at, format, inputs`
const conflictProposalFileColumns = `id, proposal_id, ordinal, path, resolution, details`

func scanConflictProposal(s scheduleScanner) (ConflictProposal, error) {
	var p ConflictProposal
	var status, inputs string
	var created, resolved, appliedAt sql.NullString
	var confirmedBy, appliedCommitSHA sql.NullString
	var confirmedCommentID sql.NullInt64
	if err := s.Scan(&p.ID, &p.SessionID, &p.TriggerCommentID, &p.Model, &status, &created, &resolved,
		&p.BaseSHA, &p.BranchSHA, &confirmedBy, &confirmedCommentID, &appliedCommitSHA, &appliedAt, &p.Format, &inputs); err != nil {
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
	if p.AppliedAt, err = scanTime(appliedAt); err != nil {
		return ConflictProposal{}, err
	}
	if err := json.Unmarshal([]byte(inputs), &p.Inputs); err != nil {
		return ConflictProposal{}, fmt.Errorf("decode conflict inputs: %w", err)
	}
	p.Status = ConflictProposalStatus(status)
	p.ConfirmedBy = confirmedBy.String
	p.ConfirmedCommentID = confirmedCommentID.Int64
	p.AppliedCommitSHA = appliedCommitSHA.String
	return p, nil
}

func scanConflictProposalFile(s scheduleScanner) (ConflictProposalFile, error) {
	var f ConflictProposalFile
	var details string
	if err := s.Scan(&f.ID, &f.ProposalID, &f.Ordinal, &f.Path, &f.Resolution, &details); err != nil {
		return ConflictProposalFile{}, err
	}
	if err := json.Unmarshal([]byte(details), &f.Details); err != nil {
		return ConflictProposalFile{}, fmt.Errorf("decode conflict details: %w", err)
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
