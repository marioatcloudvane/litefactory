package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// MCPCreationApprovalTTL is the non-sliding lifetime from trusted comment creation.
const MCPCreationApprovalTTL = 30 * time.Minute

// MCPCreationWindow defines the rolling quota window for confirmed artifacts.
const MCPCreationWindow = 24 * time.Hour

// MCPCreationAttemptLimit caps artifact units in the single batch per attempt.
const MCPCreationAttemptLimit = 5

// MCPCreationRepositoryLimit caps charged artifact units per repository.
const MCPCreationRepositoryLimit = 20

// ErrCreationAuthority reports absent, stale or mismatched creation authorization.
var ErrCreationAuthority = errors.New("store: creation authority refused")

// ErrCreationQuota reports an immediate refusal of excess artifact volume.
var ErrCreationQuota = errors.New("store: creation volume limit exceeded")

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

// CreationPermit is private dispatch ownership. JSON cannot serialize its token.
type CreationPermit struct {
	operation int64
	owner     string
	lease     MCPLease
}

// CreationReservation pairs public operation state with private first-dispatch ownership.
type CreationReservation struct {
	Operation CreationOperation `json:"operation"`
	Permit    *CreationPermit   `json:"-"`
}

// RetainProposal stores exact canonical content and advances the current proposal revision.
func (c *CreationStore) RetainProposal(ctx context.Context, p CreationProposal) error {
	if !validSubmissionDigest(p.Hash) || p.Hash != submissionDigest(p.Envelope) || p.Repository == "" || p.SourceTask == "" || (p.Relationship != "linked" && p.Relationship != "attach") || p.Artifacts < 1 || p.Artifacts > MCPCreationAttemptLimit || len(p.Envelope) > MaxResultInputBytes {
		return ErrCreationAuthority
	}
	if err := validateCreationProposal(p); err != nil {
		return err
	}
	return c.db.withTx(ctx, func(tx *sql.Tx) error {
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
	})
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
		return 0, ErrCreationAuthority
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		now := c.now()
		if a.CreatedAt.After(now) || !now.Before(a.CreatedAt.Add(MCPCreationApprovalTTL)) {
			return ErrCreationAuthority
		}
		p, retained, err := creationProposalTx(ctx, tx, a.ProposalHash)
		if err != nil {
			return err
		}
		if p.Repository != a.Repository || p.SourceTask != a.SourceTask || p.Relationship != relationship || a.CreatedAt.UnixNano() < retained {
			return ErrCreationAuthority
		}
		if err := currentCreationProposal(ctx, tx, p); err != nil {
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
				return ErrCreationConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !a.UpdatedAt.IsZero() && !a.UpdatedAt.Equal(a.CreatedAt) {
			return ErrCreationAuthority
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

func (c *CreationStore) checkLease(ctx context.Context, tx *sql.Tx, l MCPLease, now time.Time) error {
	if l.Binding.Mode != "research" {
		return ErrCreationAuthority
	}
	if err := checkMCPLease(ctx, tx, l, now); err != nil {
		return err
	}
	return checkResultAccess(ctx, tx, l.Binding, true)
}

// BindGrant consumes an intent into one next eligible launch; a retry on that
// same launch returns its grant but cannot transfer it to resume or fallback.
func (c *CreationStore) BindGrant(ctx context.Context, l MCPLease, intentID int64) (int64, error) {
	var grant int64
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		now := c.now()
		if err := c.checkLease(ctx, tx, l, now); err != nil {
			return err
		}
		var task, hash string
		var created, expiry, proposalRevision int64
		if err := tx.QueryRowContext(ctx, `SELECT source_task,proposal_hash,created_ns,expires_ns,proposal_revision FROM mcp_creation_intents WHERE id=?`, intentID).Scan(&task, &hash, &created, &expiry, &proposalRevision); err != nil {
			return ErrCreationAuthority
		}
		if task != l.Binding.Task || now.UnixNano() >= expiry {
			return ErrCreationAuthority
		}
		p, _, err := creationProposalTx(ctx, tx, hash)
		if err != nil {
			return err
		}
		if err := currentCreationProposal(ctx, tx, p); err != nil {
			return err
		}
		var currentRevision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM mcp_creation_current WHERE source_task=?`, task).Scan(&currentRevision); err != nil {
			return err
		}
		if currentRevision != proposalRevision {
			return ErrCreationAuthority
		}
		var bound int64
		if err := tx.QueryRowContext(ctx, `SELECT bound_ns FROM mcp_attempt_leases WHERE run_id=? AND attempt=?`, l.Binding.RunID, l.Binding.Attempt).Scan(&bound); err != nil {
			return err
		}
		if bound < created {
			return ErrCreationAuthority
		}
		var run int64
		var attempt, instance string
		err = tx.QueryRowContext(ctx, `SELECT id,run_id,attempt,instance FROM mcp_creation_grants WHERE intent_id=?`, intentID).Scan(&grant, &run, &attempt, &instance)
		if err == nil {
			if run != l.Binding.RunID || attempt != l.Binding.Attempt || instance != l.Instance {
				return ErrCreationAuthority
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if l.ExpiresAt.UnixNano() < expiry {
			expiry = l.ExpiresAt.UnixNano()
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mcp_creation_grants(intent_id,run_id,attempt,instance,expires_ns) VALUES(?,?,?,?,?)`, intentID, l.Binding.RunID, l.Binding.Attempt, l.Instance, expiry)
		if err != nil {
			return err
		}
		grant, err = result.LastInsertId()
		return err
	})
	return grant, err
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
func (c *CreationStore) grantTx(ctx context.Context, tx *sql.Tx, l MCPLease, now time.Time) (int64, CreationProposal, error) {
	var grant, expiry, approvedRevision, currentRevision int64
	var instance, hash, task string
	err := tx.QueryRowContext(ctx, `SELECT g.id,g.instance,g.expires_ns,i.proposal_hash,i.source_task,i.proposal_revision,c.revision FROM mcp_creation_grants g JOIN mcp_creation_intents i ON i.id=g.intent_id JOIN mcp_creation_current c ON c.source_task=i.source_task WHERE g.run_id=? AND g.attempt=?`, l.Binding.RunID, l.Binding.Attempt).Scan(&grant, &instance, &expiry, &hash, &task, &approvedRevision, &currentRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, CreationProposal{}, ErrCreationAuthority
	}
	if err != nil {
		return 0, CreationProposal{}, err
	}
	if instance != l.Instance || task != l.Binding.Task || now.UnixNano() >= expiry || approvedRevision != currentRevision {
		return 0, CreationProposal{}, ErrCreationAuthority
	}
	p, _, err := creationProposalTx(ctx, tx, hash)
	if err != nil {
		return 0, p, err
	}
	return grant, p, currentCreationProposal(ctx, tx, p)
}
func creationOperationTx(ctx context.Context, tx *sql.Tx, grant int64) (CreationOperation, error) {
	var op CreationOperation
	err := tx.QueryRowContext(ctx, `SELECT id,proposal_hash,artifacts,state,dispatched FROM mcp_creation_operations WHERE grant_id=?`, grant).Scan(&op.ID, &op.ProposalHash, &op.Artifacts, &op.State, &op.Dispatched)
	return op, err
}
func permitDigest(owner string) string {
	hash := sha256.Sum256([]byte(owner))
	return hex.EncodeToString(hash[:])
}

// ReserveCreation rechecks lease/grant/content/quota in one short transaction.
// Only the first successful reservation obtains an internal dispatch permit.
// Every exact retry returns the same operation without another permit.
func (c *CreationStore) ReserveCreation(ctx context.Context, l MCPLease, p CreationProposal) (CreationReservation, error) {
	var reservation CreationReservation
	if err := validateCreationProposal(p); err != nil {
		return reservation, err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return reservation, err
	}
	owner := hex.EncodeToString(random[:])
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		now := c.now()
		if err := c.checkLease(ctx, tx, l, now); err != nil {
			return err
		}
		grant, approved, err := c.grantTx(ctx, tx, l, now)
		if err != nil {
			return err
		}
		if p.Hash != approved.Hash || p.Repository != approved.Repository || p.SourceTask != approved.SourceTask || p.Artifacts != approved.Artifacts || p.Relationship != approved.Relationship || !bytes.Equal(p.Envelope, approved.Envelope) {
			return ErrCreationAuthority
		}
		old, err := creationOperationTx(ctx, tx, grant)
		if err == nil {
			reservation.Operation = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var charged int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(artifacts),0) FROM mcp_creation_operations WHERE repository=? AND state<>'released' AND (state<>'confirmed' OR confirmed_ns>?)`, p.Repository, now.Add(-MCPCreationWindow).UnixNano()).Scan(&charged); err != nil {
			return err
		}
		if charged+int64(p.Artifacts) > MCPCreationRepositoryLimit {
			return ErrCreationQuota
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO mcp_creation_operations(grant_id,repository,proposal_hash,artifacts,owner_digest,state,created_ns) VALUES(?,?,?,?,?,'reserved',?)`, grant, p.Repository, p.Hash, p.Artifacts, permitDigest(owner), now.UnixNano())
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		reservation.Operation = CreationOperation{ID: id, ProposalHash: p.Hash, Artifacts: p.Artifacts, State: "reserved"}
		reservation.Permit = &CreationPermit{operation: id, owner: owner, lease: l}
		return nil
	})
	return reservation, err
}

// BeginDispatch elects no new owner. It consumes the original internal permit
// once, after a fresh lease/grant check, before any external I/O. Repeated calls
// fail even if a prior remote reply was lost.
func (c *CreationStore) BeginDispatch(ctx context.Context, permit *CreationPermit) (CreationOperation, error) {
	var operation CreationOperation
	if permit == nil {
		return operation, ErrCreationAuthority
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		now := c.now()
		if err := c.checkLease(ctx, tx, permit.lease, now); err != nil {
			return err
		}
		grant, _, err := c.grantTx(ctx, tx, permit.lease, now)
		if err != nil {
			return err
		}
		old, err := creationOperationTx(ctx, tx, grant)
		if err != nil {
			return err
		}
		if old.ID != permit.operation || old.State != "reserved" || old.Dispatched {
			return ErrCreationAuthority
		}
		result, err := tx.ExecContext(ctx, `UPDATE mcp_creation_operations SET state='dispatched',dispatched=1 WHERE id=? AND owner_digest=? AND state='reserved' AND dispatched=0`, permit.operation, permitDigest(permit.owner))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrCreationAuthority
		}
		operation = old
		operation.State = "dispatched"
		operation.Dispatched = true
		return nil
	})
	return operation, err
}

// RecordOperationOutcome is factory effect bookkeeping, not renewed agent
// authority. A revoked attempt's already-owned operation can still be reconciled.
func (c *CreationStore) RecordOperationOutcome(ctx context.Context, permit *CreationPermit, outcome string) (CreationOperation, error) {
	if permit == nil {
		return CreationOperation{}, ErrCreationAuthority
	}
	return c.recordOutcome(ctx, permit.operation, permitDigest(permit.owner), outcome)
}

// ReconcileOperation is factory-only recovery from independent effect evidence.
// It cannot create a permit, renew a grant or choose another dispatch owner.
func (c *CreationStore) ReconcileOperation(ctx context.Context, id int64, outcome string) (CreationOperation, error) {
	return c.recordOutcome(ctx, id, "", outcome)
}
func (c *CreationStore) recordOutcome(ctx context.Context, id int64, owner, outcome string) (CreationOperation, error) {
	var op CreationOperation
	if id < 1 || !submissionState(outcome, "confirmed", "uncertain", "failed", "never_dispatched") {
		return op, ErrCreationAuthority
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		var digest string
		var confirmed int64
		if err := tx.QueryRowContext(ctx, `SELECT id,proposal_hash,artifacts,state,dispatched,owner_digest,confirmed_ns FROM mcp_creation_operations WHERE id=?`, id).Scan(&op.ID, &op.ProposalHash, &op.Artifacts, &op.State, &op.Dispatched, &digest, &confirmed); err != nil {
			return err
		}
		if owner != "" && owner != digest {
			return ErrCreationAuthority
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
