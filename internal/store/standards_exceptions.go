package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Exception transition errors preserve conflicts, expiry and absent approval distinctly.
var (
	ErrStandardsExceptionConflict         = errors.New("standards exception: conflicting terms or transition")
	ErrStandardsExceptionExpired          = errors.New("standards exception: proposal expired")
	ErrStandardsExceptionApprovalRequired = errors.New("standards exception: approval required")
	ErrStandardsExceptionDigest           = errors.New("standards exception: digest mismatch")
)

// StandardsExceptionProposal retains canonical immutable terms and indexed references.
type StandardsExceptionProposal struct {
	ID            string    `json:"id"`
	OperationKey  string    `json:"operation_key"`
	SchemaVersion int       `json:"schema_version"`
	Digest        string    `json:"digest"`
	TermsJSON     string    `json:"terms_json"`
	CreatedAt     time.Time `json:"created_at"`
	Owner         string    `json:"owner"`
	BookRevision  int64     `json:"book_revision"`
	StandardID    string    `json:"standard_id"`
	RepositoryID  int64     `json:"repository_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// StandardsExceptionApproval binds the exact proposal digest to a trusted owner decision.
type StandardsExceptionApproval struct {
	ProposalID string    `json:"proposal_id"`
	Digest     string    `json:"digest"`
	Owner      string    `json:"owner"`
	Provenance string    `json:"provenance"`
	CreatedAt  time.Time `json:"created_at"`
}

// StandardsExceptionRevocation is the terminal retained withdrawal of an approval.
type StandardsExceptionRevocation struct {
	ProposalID string    `json:"proposal_id"`
	Owner      string    `json:"owner"`
	Provenance string    `json:"provenance"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

// StandardsExceptionRecord reads proposal and event history in one transaction.
type StandardsExceptionRecord struct {
	Proposal   StandardsExceptionProposal    `json:"proposal"`
	Approval   *StandardsExceptionApproval   `json:"approval,omitempty"`
	Revocation *StandardsExceptionRevocation `json:"revocation,omitempty"`
}

// StandardsExceptionValidator must only inspect its arguments; it runs while a
// transaction holds the sole connection and must not call database accessors.
type StandardsExceptionValidator func(StandardsExceptionRecord, StandardsRevision, Repo) error

func readStandardsException(ctx context.Context, tx *sql.Tx, id string) (StandardsExceptionRecord, error) {
	var r StandardsExceptionRecord
	p := &r.Proposal
	var created, expires string
	err := tx.QueryRowContext(ctx, `SELECT id,operation_key,schema_version,digest,terms_json,created_at,owner,book_revision,standard_id,repository_id,expires_at FROM standards_exception_proposals WHERE id=?`, id).Scan(&p.ID, &p.OperationKey, &p.SchemaVersion, &p.Digest, &p.TermsJSON, &created, &p.Owner, &p.BookRevision, &p.StandardID, &p.RepositoryID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	p.CreatedAt, err = ParseTime(created)
	if err != nil {
		return r, fmt.Errorf("%w: exception creation time", ErrStandardsCorrupt)
	}
	p.ExpiresAt, err = ParseTime(expires)
	if err != nil {
		return r, fmt.Errorf("%w: exception expiry", ErrStandardsCorrupt)
	}
	a := StandardsExceptionApproval{ProposalID: id}
	err = tx.QueryRowContext(ctx, `SELECT digest,owner,provenance,created_at FROM standards_exception_approvals WHERE proposal_id=?`, id).Scan(&a.Digest, &a.Owner, &a.Provenance, &created)
	if err == nil {
		a.CreatedAt, err = ParseTime(created)
		if err != nil {
			return r, ErrStandardsCorrupt
		}
		r.Approval = &a
	} else if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	v := StandardsExceptionRevocation{ProposalID: id}
	err = tx.QueryRowContext(ctx, `SELECT owner,provenance,reason,created_at FROM standards_exception_revocations WHERE proposal_id=?`, id).Scan(&v.Owner, &v.Provenance, &v.Reason, &created)
	if err == nil {
		v.CreatedAt, err = ParseTime(created)
		if err != nil {
			return r, ErrStandardsCorrupt
		}
		r.Revocation = &v
	} else if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	return r, nil
}
func validateStandardsException(ctx context.Context, tx *sql.Tx, r StandardsExceptionRecord, validate StandardsExceptionValidator) error {
	if validate == nil {
		return errors.New("standards exception: reference validation required")
	}
	revision, err := readStandardsRevision(ctx, tx, r.Proposal.BookRevision)
	if err != nil {
		return err
	}
	repo, err := scanRepo(tx.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE id=?`, r.Proposal.RepositoryID))
	if err != nil {
		return err
	}
	return validate(r, revision, repo)
}

// CreateStandardsException commits a validated proposal or recovers an identical operation key.
func (db *DB) CreateStandardsException(ctx context.Context, p StandardsExceptionProposal, validate StandardsExceptionValidator) (StandardsExceptionProposal, error) {
	if p.ID == "" || p.OperationKey == "" || p.SchemaVersion != 1 || p.Digest == "" || p.TermsJSON == "" || p.Owner == "" || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return p, errors.New("standards exception: invalid proposal metadata")
	}
	p.CreatedAt = p.CreatedAt.UTC().Truncate(time.Millisecond)
	p.ExpiresAt = p.ExpiresAt.UTC().Truncate(time.Millisecond)
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM standards_exception_proposals WHERE operation_key=?`, p.OperationKey).Scan(&id)
		if err == nil {
			r, e := readStandardsException(ctx, tx, id)
			if e != nil {
				return e
			}
			old := r.Proposal
			if old.TermsJSON != p.TermsJSON || old.Digest != p.Digest || old.Owner != p.Owner || old.BookRevision != p.BookRevision || old.StandardID != p.StandardID || old.RepositoryID != p.RepositoryID || !old.ExpiresAt.Equal(p.ExpiresAt) || old.SchemaVersion != p.SchemaVersion {
				return ErrStandardsExceptionConflict
			}
			p = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := validateStandardsException(ctx, tx, StandardsExceptionRecord{Proposal: p}, validate); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO standards_exception_proposals(id,operation_key,schema_version,digest,terms_json,created_at,owner,book_revision,standard_id,repository_id,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.OperationKey, p.SchemaVersion, p.Digest, p.TermsJSON, FormatTime(p.CreatedAt), p.Owner, p.BookRevision, p.StandardID, p.RepositoryID, FormatTime(p.ExpiresAt))
		return err
	})
	return p, err
}

// GetStandardsException returns a consistent immutable proposal and its events.
func (db *DB) GetStandardsException(ctx context.Context, id string) (StandardsExceptionRecord, error) {
	var r StandardsExceptionRecord
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; r, err = readStandardsException(ctx, tx, id); return err })
	return r, err
}

// ListStandardsExceptions returns a bounded page from a single consistent transaction.
func (db *DB) ListStandardsExceptions(ctx context.Context, limit, offset int) ([]StandardsExceptionRecord, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("standards exception: limit must be 1..100 and offset nonnegative")
	}
	out := []StandardsExceptionRecord{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM standards_exception_proposals ORDER BY created_at,id LIMIT ? OFFSET ?`, limit, offset)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := readStandardsException(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// ApproveStandardsException rechecks references and expiry atomically before recording approval.
func (db *DB) ApproveStandardsException(ctx context.Context, id, digest, owner string, now time.Time, validate StandardsExceptionValidator) (StandardsExceptionRecord, error) {
	var r StandardsExceptionRecord
	if owner == "" || now.IsZero() {
		return r, ErrStandardsExceptionApprovalRequired
	}
	now = now.UTC().Truncate(time.Millisecond)
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = readStandardsException(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Proposal.Digest != digest {
			return ErrStandardsExceptionDigest
		}
		if r.Approval != nil {
			if r.Approval.Owner != owner || r.Approval.Digest != digest {
				return ErrStandardsExceptionConflict
			}
			return nil
		}
		if !now.Before(r.Proposal.ExpiresAt) {
			return ErrStandardsExceptionExpired
		}
		if now.Before(r.Proposal.CreatedAt) {
			return ErrStandardsExceptionConflict
		}
		if err := validateStandardsException(ctx, tx, r, validate); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO standards_exception_approvals(proposal_id,digest,owner,provenance,created_at) VALUES(?,?,?,'local-cli',?)`, id, digest, owner, FormatTime(now))
		if err != nil {
			return err
		}
		r.Approval = &StandardsExceptionApproval{ProposalID: id, Digest: digest, Owner: owner, Provenance: "local-cli", CreatedAt: now}
		return nil
	})
	return r, err
}

// RevokeStandardsException records terminal revocation and recovers identical retries.
func (db *DB) RevokeStandardsException(ctx context.Context, id, owner, reason string, now time.Time) (StandardsExceptionRecord, error) {
	var r StandardsExceptionRecord
	if owner == "" || now.IsZero() {
		return r, ErrStandardsExceptionApprovalRequired
	}
	now = now.UTC().Truncate(time.Millisecond)
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = readStandardsException(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Approval == nil {
			return ErrStandardsExceptionApprovalRequired
		}
		if r.Revocation != nil {
			if r.Revocation.Owner != owner || r.Revocation.Reason != reason {
				return ErrStandardsExceptionConflict
			}
			return nil
		}
		if now.Before(r.Approval.CreatedAt) {
			return ErrStandardsExceptionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO standards_exception_revocations(proposal_id,owner,provenance,reason,created_at) VALUES(?,?,'local-cli',?,?)`, id, owner, reason, FormatTime(now))
		if err != nil {
			return err
		}
		r.Revocation = &StandardsExceptionRevocation{ProposalID: id, Owner: owner, Provenance: "local-cli", Reason: reason, CreatedAt: now}
		return nil
	})
	return r, err
}
