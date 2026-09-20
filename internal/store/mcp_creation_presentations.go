package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreationPresentation is the per-run bookkeeping row that says which retained
// creation proposal still has to be shown to the owner and whether it has
// been shown. A run has at most one row, so only its last proposal is ever
// presented. It carries no authority: approvals, grants and operations stay
// in their own tables.
//
// RetainProposalForRun reads only RunID from its argument; every other field
// is derived from the retained proposal, the run's own session and the
// store's clock.
type CreationPresentation struct {
	RunID        int64
	SessionID    int64
	SourceTask   string
	ProposalHash string
	Relationship string
	Artifacts    int
	RetainedNS   int64
	DeliveryKey  string
	Presented    bool
}

// RetainProposalForRun retains p, moves the source task's current-proposal
// pointer and writes the run's presentation row in one transaction; a failure
// in any part leaves none of them written.
//
// The run's session is derived in SQL and must own p.SourceTask, otherwise
// ErrCreationAuthority is returned. A different proposal in the same run
// replaces the run's row (and clears its unbound delivery key); retaining the
// same proposal again changes nothing. Once the run's row is marked presented,
// a different proposal is refused with ErrCreationConflict.
func (c *CreationStore) RetainProposalForRun(ctx context.Context, p CreationProposal, pres CreationPresentation) error {
	if pres.RunID < 1 {
		return ErrCreationAuthority
	}
	if err := validateRetention(p); err != nil {
		return err
	}
	return c.db.withTx(ctx, func(tx *sql.Tx) error {
		var owned int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND s.task_ref=?`, pres.RunID, p.SourceTask).Scan(&owned); err != nil {
			return fmt.Errorf("retain proposal for run: check session: %w", err)
		}
		if owned != 1 {
			return ErrCreationAuthority
		}
		var hash string
		var presented bool
		err := tx.QueryRowContext(ctx, `SELECT proposal_hash,presented FROM mcp_creation_presentations WHERE run_id=?`, pres.RunID).Scan(&hash, &presented)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("retain proposal for run: read presentation: %w", err)
		case presented && hash != p.Hash:
			return fmt.Errorf("retain proposal for run: run already presented a proposal: %w", ErrCreationConflict)
		}
		if err := c.retainProposalTx(ctx, tx, p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_creation_presentations(run_id,session_id,source_task,proposal_hash,relationship,artifacts,retained_ns) SELECT r.id,r.session_id,s.task_ref,?,?,?,? FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND s.task_ref=? ON CONFLICT(run_id) DO UPDATE SET proposal_hash=excluded.proposal_hash,relationship=excluded.relationship,artifacts=excluded.artifacts,retained_ns=excluded.retained_ns,delivery_key='' WHERE mcp_creation_presentations.proposal_hash<>excluded.proposal_hash AND mcp_creation_presentations.presented=0`, p.Hash, p.Relationship, p.Artifacts, c.now().UnixNano(), pres.RunID, p.SourceTask); err != nil {
			return fmt.Errorf("retain proposal for run: write presentation: %w", err)
		}
		return nil
	})
}

// PendingCreationPresentation returns the run's presentation row while it is
// still owed. ErrNotFound is the only "nothing owed" signal: the row is absent
// or already presented.
func (c *CreationStore) PendingCreationPresentation(ctx context.Context, runID int64) (CreationPresentation, error) {
	var out CreationPresentation
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = creationPresentationTx(ctx, tx, runID)
		if err != nil {
			return err
		}
		if out.Presented {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return CreationPresentation{}, fmt.Errorf("pending creation presentation for run %d: %w", runID, err)
	}
	return out, nil
}

// BindCreationPresentationDelivery records the delivery key the presentation
// is posted under. Binding the same key again is a no-op; a different key is
// refused with ErrCreationConflict once one is set.
func (c *CreationStore) BindCreationPresentationDelivery(ctx context.Context, runID int64, key string) error {
	if key == "" {
		return ErrCreationAuthority
	}
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := creationPresentationTx(ctx, tx, runID)
		if err != nil {
			return err
		}
		if cur.DeliveryKey == key {
			return nil
		}
		if cur.DeliveryKey != "" || cur.Presented {
			return ErrCreationConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE mcp_creation_presentations SET delivery_key=? WHERE run_id=?`, key, runID)
		return err
	})
	if err != nil {
		return fmt.Errorf("bind creation presentation delivery for run %d: %w", runID, err)
	}
	return nil
}

// MarkCreationPresented records that the presentation reached the owner. It is
// idempotent and refuses with ErrCreationConflict while no delivery key is
// bound, so a presentation cannot be marked shown before its delivery began.
func (c *CreationStore) MarkCreationPresented(ctx context.Context, runID int64) error {
	err := c.db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := creationPresentationTx(ctx, tx, runID)
		if err != nil {
			return err
		}
		if cur.Presented {
			return nil
		}
		if cur.DeliveryKey == "" {
			return ErrCreationConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE mcp_creation_presentations SET presented=1 WHERE run_id=?`, runID)
		return err
	})
	if err != nil {
		return fmt.Errorf("mark creation presented for run %d: %w", runID, err)
	}
	return nil
}

func creationPresentationTx(ctx context.Context, tx *sql.Tx, runID int64) (CreationPresentation, error) {
	var p CreationPresentation
	err := tx.QueryRowContext(ctx, `SELECT run_id,session_id,source_task,proposal_hash,relationship,artifacts,retained_ns,delivery_key,presented FROM mcp_creation_presentations WHERE run_id=?`, runID).Scan(&p.RunID, &p.SessionID, &p.SourceTask, &p.ProposalHash, &p.Relationship, &p.Artifacts, &p.RetainedNS, &p.DeliveryKey, &p.Presented)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationPresentation{}, ErrNotFound
	}
	if err != nil {
		return CreationPresentation{}, err
	}
	return p, nil
}
