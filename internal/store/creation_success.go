package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CreationSource is trusted intake provenance, independent of mutable command
// text and current proposal or capability eligibility.
type CreationSource struct {
	Repository, SourceTask string
	CommentID, OwnerID     int64
}

// GetCreationPreparation observes the original proposal without moving the
// current-proposal pointer or renewing its retention time.
func (db *DB) GetCreationPreparation(ctx context.Context, source CreationSource) (CreationProposal, error) {
	var p CreationProposal
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = creationPreparationTx(ctx, tx, source)
		return err
	})
	return p, err
}

func creationPreparationTx(ctx context.Context, tx *sql.Tx, source CreationSource) (CreationProposal, error) {
	var hash string
	err := tx.QueryRowContext(ctx, `SELECT p.proposal_hash FROM creation_preparations p JOIN sessions s ON s.id=p.session_id JOIN repos r ON r.id=s.repo_id WHERE s.task_ref=? AND r.owner || '/' || r.name=? AND p.comment_id=? AND p.owner_id=?`, source.SourceTask, source.Repository, source.CommentID, source.OwnerID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationProposal{}, ErrNotFound
	}
	if err != nil {
		return CreationProposal{}, err
	}
	p, _, err := creationProposalTx(ctx, tx, hash)
	if err == nil && (p.Repository != source.Repository || p.SourceTask != source.SourceTask) {
		return CreationProposal{}, fmt.Errorf("retained creation source mismatch: %w", ErrCreationConflict)
	}
	return p, err
}

// RetainProposalForSource commits successful intake with its original proposal.
// Repeated calls acknowledge the original association, without re-retaining it.
func (c *CreationStore) RetainProposalForSource(ctx context.Context, p CreationProposal, source CreationSource) (CreationProposal, error) {
	if source.CommentID < 1 || source.OwnerID < 1 || source.Repository != p.Repository || source.SourceTask != p.SourceTask {
		return CreationProposal{}, ErrCreationAuthority
	}
	if err := validateRetention(p); err != nil {
		return CreationProposal{}, err
	}
	var saved CreationProposal
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		old, err := creationPreparationTx(ctx, tx, source)
		if err == nil {
			saved = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := c.retainProposalTx(ctx, tx, p); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO creation_preparations(session_id,comment_id,owner_id,proposal_hash) SELECT s.id,?,?,? FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE s.task_ref=? AND r.owner || '/' || r.name=?`, source.CommentID, source.OwnerID, p.Hash, source.SourceTask, source.Repository)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("retain creation source association: %w", ErrCreationConflict)
		}
		saved = p
		return nil
	})
	return saved, err
}

// GetCreationOwnerIntent recovers legacy and new approvals directly from the
// authoritative intent row. It grants no authority and never changes expiry.
func (db *DB) GetCreationOwnerIntent(ctx context.Context, source CreationSource) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT i.id FROM mcp_creation_intents i JOIN sessions s ON s.task_ref=i.source_task JOIN repos r ON r.id=s.repo_id WHERE i.repository=? AND i.source_task=? AND i.comment_id=? AND i.owner_id=? AND r.owner || '/' || r.name=i.repository`, source.Repository, source.SourceTask, source.CommentID, source.OwnerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// GetPreparation observes the original preparation without refreshing its proposal.
func (c *CreationStore) GetPreparation(ctx context.Context, source CreationSource) (CreationProposal, error) {
	if c == nil || c.db == nil {
		return CreationProposal{}, fmt.Errorf("creation outcome storage unavailable: %w", ErrCreationAuthority)
	}
	return c.db.GetCreationPreparation(ctx, source)
}

// GetOwnerIntent observes an admitted approval without renewing its authority.
func (c *CreationStore) GetOwnerIntent(ctx context.Context, source CreationSource) (int64, error) {
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("creation outcome storage unavailable: %w", ErrCreationAuthority)
	}
	return c.db.GetCreationOwnerIntent(ctx, source)
}

// CreationAcknowledgment is immutable admission evidence, joined to the proposal
// that the intent approved. It says nothing about current publishing eligibility.
type CreationAcknowledgment struct {
	IntentID             int64
	Source               CreationSource
	Hash, Relationship   string
	CreatedAt, ExpiresAt time.Time
}

// GetCreationAcknowledgment observes original consent even after expiry or
// supersession. It neither renews consent nor consults the current proposal.
func (db *DB) GetCreationAcknowledgment(ctx context.Context, source CreationSource) (CreationAcknowledgment, error) {
	a := CreationAcknowledgment{Source: source}
	var created, expires int64
	err := db.sql.QueryRowContext(ctx, `SELECT i.id,p.hash,p.relationship,i.created_ns,i.expires_ns FROM mcp_creation_intents i JOIN mcp_creation_proposals p ON p.hash=i.proposal_hash AND p.repository=i.repository AND p.source_task=i.source_task JOIN sessions s ON s.task_ref=i.source_task JOIN repos r ON r.id=s.repo_id WHERE i.repository=? AND i.source_task=? AND i.comment_id=? AND i.owner_id=? AND r.owner || '/' || r.name=i.repository`, source.Repository, source.SourceTask, source.CommentID, source.OwnerID).Scan(&a.IntentID, &a.Hash, &a.Relationship, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationAcknowledgment{}, ErrNotFound
	}
	if err != nil {
		return CreationAcknowledgment{}, fmt.Errorf("observe creation acknowledgment: %w", err)
	}
	a.CreatedAt, a.ExpiresAt = time.Unix(0, created).UTC(), time.Unix(0, expires).UTC()
	return a, nil
}
