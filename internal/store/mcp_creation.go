package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// MCPCreationApprovalTTL is the non-sliding lifetime from trusted comment creation.
const MCPCreationApprovalTTL = 30 * time.Minute

// MCPCreationAttemptLimit caps artifact units in a single proposal.
const MCPCreationAttemptLimit = 5

// ErrCreationAuthority reports absent, stale or mismatched creation authorization.
var ErrCreationAuthority = errors.New("store: creation authority refused")

// ErrCreationConflict reports an attempt to change immutable approved content.
var ErrCreationConflict = errors.New("store: immutable creation approval or content conflict")

// CreationStore owns factory-only approval/operation state. Now is fresh
// bookkeeping time; an injected deterministic clock is for offline tests, never
// an MCP argument. Production callers pass nil to use the store's clock.
type CreationStore struct {
	db  *DB
	now func() time.Time
}

// NewCreationStore binds factory storage and optional deterministic bookkeeping time.
func NewCreationStore(db *DB, now func() time.Time) (*CreationStore, error) {
	if db == nil {
		return nil, ErrCreationAuthority
	}
	if now == nil {
		now = nowUTC
	}
	return &CreationStore{db: db, now: now}, nil
}

// CreationProposal is trusted canonical content from the turn validator.
type CreationProposal struct {
	Hash, Repository, SourceTask, Relationship string
	Artifacts                                  int
	Envelope                                   []byte
}

// CreationApproval is authenticated control metadata, never model arguments.
type CreationApproval struct {
	Repository, SourceTask, ProposalHash string
	CommentID, OwnerID, ActorID          int64
	CreatedAt, UpdatedAt                 time.Time
	Bot                                  bool
}

// CreationOperation is safe application correlation and outcome, not authority.
type CreationOperation struct {
	ID           int64  `json:"id"`
	ProposalHash string `json:"proposal_hash"`
	Artifacts    int    `json:"artifacts"`
	State        string `json:"state"`
	Dispatched   bool   `json:"dispatched"`
}

// RetainProposal stores exact canonical content and advances the current proposal revision.
func (c *CreationStore) RetainProposal(ctx context.Context, p CreationProposal) error {
	if err := validateRetention(p); err != nil {
		return err
	}
	return c.db.withTx(ctx, func(tx *sql.Tx) error {
		return c.retainProposalTx(ctx, tx, p)
	})
}

func validateRetention(p CreationProposal) error {
	if !validSubmissionDigest(p.Hash) || p.Hash != submissionDigest(p.Envelope) || p.Repository == "" || p.SourceTask == "" || (p.Relationship != "linked" && p.Relationship != "attach") || p.Artifacts < 1 || p.Artifacts > MCPCreationAttemptLimit || len(p.Envelope) > MaxResultInputBytes {
		return ErrCreationAuthority
	}
	return validateCreationProposal(p)
}

// retainProposalTx is the retention body shared by RetainProposal and
// RetainProposalForRun; the caller owns the transaction and has validated p.
func (c *CreationStore) retainProposalTx(ctx context.Context, tx *sql.Tx, p CreationProposal) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE s.task_ref=? AND r.owner || '/' || r.name=?`, p.SourceTask, p.Repository).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return ErrCreationAuthority
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_creation_proposals(hash,repository,source_task,relationship,artifacts,envelope,retained_ns) VALUES(?,?,?,?,?,?,?) ON CONFLICT(hash) DO NOTHING`, p.Hash, p.Repository, p.SourceTask, p.Relationship, p.Artifacts, p.Envelope, c.now().UnixNano()); err != nil {
		return err
	}
	old, _, err := creationProposalTx(ctx, tx, p.Hash)
	if err != nil {
		return err
	}
	if old.Repository != p.Repository || old.SourceTask != p.SourceTask || old.Relationship != p.Relationship || old.Artifacts != p.Artifacts || !bytes.Equal(old.Envelope, p.Envelope) {
		return ErrCreationConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mcp_creation_current(source_task,proposal_hash) VALUES(?,?) ON CONFLICT(source_task) DO UPDATE SET revision=CASE WHEN proposal_hash<>excluded.proposal_hash THEN revision+1 ELSE revision END,proposal_hash=excluded.proposal_hash`, p.SourceTask, p.Hash)
	return err
}
func creationProposalTx(ctx context.Context, tx *sql.Tx, hash string) (CreationProposal, int64, error) {
	var p CreationProposal
	var retained int64
	err := tx.QueryRowContext(ctx, `SELECT hash,repository,source_task,relationship,artifacts,envelope,retained_ns FROM mcp_creation_proposals WHERE hash=?`, hash).Scan(&p.Hash, &p.Repository, &p.SourceTask, &p.Relationship, &p.Artifacts, &p.Envelope, &retained)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrCreationAuthority
	}
	return p, retained, err
}
func currentCreationProposal(ctx context.Context, tx *sql.Tx, p CreationProposal) error {
	var hash string
	if err := tx.QueryRowContext(ctx, `SELECT proposal_hash FROM mcp_creation_current WHERE source_task=?`, p.SourceTask).Scan(&hash); err != nil {
		return err
	}
	if hash != p.Hash {
		return ErrCreationAuthority
	}
	return nil
}

// AdmitOwnerIntent records one immutable approval from authenticated factory metadata.
func (c *CreationStore) AdmitOwnerIntent(ctx context.Context, a CreationApproval, relationship string) (int64, error) {
	var id int64
	if a.OwnerID < 1 || a.ActorID != a.OwnerID || a.CommentID < 1 || a.Bot || a.CreatedAt.IsZero() || !validSubmissionDigest(a.ProposalHash) {
		return 0, &CreationPolicyRefusal{}
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		now := c.now()
		if a.CreatedAt.After(now) || !now.Before(a.CreatedAt.Add(MCPCreationApprovalTTL)) {
			return &CreationPolicyRefusal{}
		}
		p, retained, err := creationProposalTx(ctx, tx, a.ProposalHash)
		if err == ErrCreationAuthority { //nolint:errorlint // Only the exact observation sentinel proves refusal; wrapped storage errors stay retryable.
			return &CreationPolicyRefusal{}
		}
		if err != nil {
			return err
		}
		if p.Repository != a.Repository || p.SourceTask != a.SourceTask || p.Relationship != relationship || a.CreatedAt.UnixNano() < retained {
			return &CreationPolicyRefusal{}
		}
		if err := currentCreationProposal(ctx, tx, p); err != nil {
			if err == ErrCreationAuthority || errors.Is(err, sql.ErrNoRows) { //nolint:errorlint // Wrapped authority errors may conceal failed storage observations.
				return &CreationPolicyRefusal{}
			}
			return err
		}
		var source, hash string
		var proposalRevision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM mcp_creation_current WHERE source_task=?`, p.SourceTask).Scan(&proposalRevision); err != nil {
			return err
		}
		var owner, created, recordedRevision int64
		err = tx.QueryRowContext(ctx, `SELECT id,source_task,proposal_hash,owner_id,created_ns,proposal_revision FROM mcp_creation_intents WHERE repository=? AND comment_id=?`, a.Repository, a.CommentID).Scan(&id, &source, &hash, &owner, &created, &recordedRevision)
		if err == nil {
			if source != a.SourceTask || hash != a.ProposalHash || owner != a.OwnerID || created != a.CreatedAt.UnixNano() || recordedRevision != proposalRevision {
				return &CreationPolicyRefusal{Cause: ErrCreationConflict}
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !a.UpdatedAt.IsZero() && !a.UpdatedAt.Equal(a.CreatedAt) {
			return &CreationPolicyRefusal{}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mcp_creation_intents(repository,source_task,comment_id,owner_id,proposal_hash,created_ns,expires_ns,proposal_revision) VALUES(?,?,?,?,?,?,?,?)`, a.Repository, a.SourceTask, a.CommentID, a.OwnerID, a.ProposalHash, a.CreatedAt.UnixNano(), a.CreatedAt.Add(MCPCreationApprovalTTL).UnixNano(), proposalRevision)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

func validateCreationProposal(p CreationProposal) error {
	var content struct {
		Version      int               `json:"version"`
		Repository   string            `json:"repository"`
		SourceTask   string            `json:"source_task"`
		Relationship string            `json:"relationship"`
		Artifacts    []json.RawMessage `json:"artifacts"`
	}
	if err := json.Unmarshal(p.Envelope, &content); err != nil {
		return ErrCreationAuthority
	}
	if content.Version != 1 || content.Repository != p.Repository || content.SourceTask != p.SourceTask || content.Relationship != p.Relationship || len(content.Artifacts) != p.Artifacts || p.Artifacts < 1 || p.Artifacts > MCPCreationAttemptLimit || p.Hash != submissionDigest(p.Envelope) {
		return ErrCreationAuthority
	}
	return nil
}

// ReconcileOperation is factory-only recovery from independent effect evidence.
// It cannot create a permit, renew a grant or choose another dispatch owner.
func (c *CreationStore) ReconcileOperation(ctx context.Context, id int64, outcome string) (CreationOperation, error) {
	return c.recordOutcome(ctx, id, outcome)
}
func (c *CreationStore) recordOutcome(ctx context.Context, id int64, outcome string) (CreationOperation, error) {
	var op CreationOperation
	if id < 1 || !submissionState(outcome, "confirmed", "uncertain", "failed", "never_dispatched") {
		return op, ErrCreationAuthority
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		var confirmed int64
		if err := tx.QueryRowContext(ctx, `SELECT id,proposal_hash,artifacts,state,dispatched,confirmed_ns FROM mcp_creation_operations WHERE id=?`, id).Scan(&op.ID, &op.ProposalHash, &op.Artifacts, &op.State, &op.Dispatched, &confirmed); err != nil {
			return err
		}
		state := outcome
		if state == "never_dispatched" {
			state = "released"
		}
		if op.State == state {
			return nil
		}
		if op.State == "confirmed" || op.State == "released" {
			return ErrCreationConflict
		}
		if state == "released" && (op.Dispatched || (op.State != "reserved" && op.State != "failed")) {
			return ErrCreationAuthority
		}
		if state == "confirmed" && !op.Dispatched {
			return ErrCreationAuthority
		}
		if state == "confirmed" {
			confirmed = c.now().UnixNano()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mcp_creation_operations SET state=?,confirmed_ns=? WHERE id=?`, state, confirmed, id); err != nil {
			return err
		}
		op.State = state
		return nil
	})
	return op, err
}

// GetCreationOperation reads retained application state for internal recovery,
// including after restart. It is not exposed as an MCP grant/read endpoint.
func (c *CreationStore) GetCreationOperation(ctx context.Context, id int64) (CreationOperation, error) {
	var op CreationOperation
	err := c.db.sql.QueryRowContext(ctx, `SELECT id,proposal_hash,artifacts,state,dispatched FROM mcp_creation_operations WHERE id=?`, id).Scan(&op.ID, &op.ProposalHash, &op.Artifacts, &op.State, &op.Dispatched)
	return op, err
}
