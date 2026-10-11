package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// DeliveryCodeAuthority pins independently verified GitHub controls to the
// original execution connection. Ticket publisher slots cannot authorize code.
type DeliveryCodeAuthority struct {
	SessionID, CodeVersionID, OwnerID, PublisherID int64
	Origin                                         ticket.Origin
	Repository, RepositoryID                       string
}

const codeAuthorityColumns = `session_id,code_version_id,api_origin,repository,repository_id,owner_id,publisher_id`

func scanCodeAuthority(s scanner) (a DeliveryCodeAuthority, err error) {
	err = s.Scan(&a.SessionID, &a.CodeVersionID, &a.Origin, &a.Repository, &a.RepositoryID, &a.OwnerID, &a.PublisherID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// EnsureDeliveryCodeAuthority retains installation code-owner evidence once.
// Init's gh identity is explicitly public GitHub evidence, never portable to a
// different origin merely because a numeric account ID happens to match.
func (db *DB) EnsureDeliveryCodeAuthority(ctx context.Context, sessionID int64) (out DeliveryCodeAuthority, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = db.ensureDeliveryCodeAuthority(ctx, tx, sessionID)
		return e
	})
	return
}

func (db *DB) ensureDeliveryCodeAuthority(ctx context.Context, tx *sql.Tx, sessionID int64) (out DeliveryCodeAuthority, err error) {
	err = func() error {
		a, e := scanCodeAuthority(tx.QueryRowContext(ctx, `SELECT `+codeAuthorityColumns+` FROM delivery_code_authorities WHERE session_id=?`, sessionID))
		if e == nil {
			out = a
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		s, e := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, sessionID))
		if e != nil {
			return e
		}
		v, e := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, s.CodeVersionID))
		if e != nil {
			return e
		}
		var ownerID int64
		var source IdentitySource
		if e = tx.QueryRowContext(ctx, `SELECT user_id,source FROM identities WHERE forge='github' AND role='owner'`).Scan(&ownerID, &source); e != nil {
			return e
		}
		publisher := s.CodeActorID
		if publisher == 0 {
			publisher = s.ActorID
		}
		repositoryID := s.CodeRemoteID
		if repositoryID == "" {
			repositoryID = v.RemoteID
		}
		if v.Provider != ticket.GitHub || v.Origin != "https://api.github.com" || source != SourceGH || ownerID <= 0 || publisher <= 0 || publisher == ownerID || repositoryID == "" {
			return errors.New("delivery requires independently verified public GitHub code authority and original repository")
		}
		out = DeliveryCodeAuthority{SessionID: sessionID, CodeVersionID: v.ID, Origin: v.Origin, Repository: v.Destination, RepositoryID: repositoryID, OwnerID: ownerID, PublisherID: publisher}
		_, e = tx.ExecContext(ctx, `INSERT INTO delivery_code_authorities(`+codeAuthorityColumns+`) VALUES(?,?,?,?,?,?,?)`, out.SessionID, out.CodeVersionID, out.Origin, out.Repository, out.RepositoryID, out.OwnerID, out.PublisherID)
		return e
	}()
	return
}

// DeliveryPRBinding enrolls only a confirmed immutable GitHub PR.
type DeliveryPRBinding struct {
	Key             string
	SessionID, PRID int64
	PRNumber        int
	EnrolledAt      time.Time
}

// BindDeliveryPR pins a confirmed PR to its original session.
func (db *DB) BindDeliveryPR(ctx context.Context, q DeliveryProposal, sessionID int64) (out DeliveryPRBinding, err error) {
	if q.PRID <= 0 || q.PRNumber <= 0 {
		return out, errors.New("confirmed PR identity required")
	}
	if _, err = db.EnsureDeliveryCodeAuthority(ctx, sessionID); err != nil {
		return
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var at string
		e := tx.QueryRowContext(ctx, `SELECT proposal_key,session_id,pr_id,pr_number,enrolled_at FROM delivery_pr_bindings WHERE proposal_key=?`, q.Key).Scan(&out.Key, &out.SessionID, &out.PRID, &out.PRNumber, &at)
		if e == nil {
			out.EnrolledAt, e = ParseTime(at)
			if out.SessionID != sessionID || out.PRID != q.PRID || out.PRNumber != q.PRNumber {
				return ErrBindingConflict
			}
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		out = DeliveryPRBinding{Key: q.Key, SessionID: sessionID, PRID: q.PRID, PRNumber: q.PRNumber, EnrolledAt: nowUTC().Truncate(time.Millisecond)}
		_, e = tx.ExecContext(ctx, `INSERT INTO delivery_pr_bindings(proposal_key,session_id,pr_id,pr_number,enrolled_at) VALUES(?,?,?,?,?)`, q.Key, sessionID, q.PRID, q.PRNumber, FormatTime(out.EnrolledAt))
		return e
	})
	return
}

// EnrollDeliveryPRNotice pins the replay boundary to the confirmed provider
// publication time. A confirmed report receipt freezes that boundary.
func (db *DB) EnrollDeliveryPRNotice(ctx context.Context, proposal, reportKey string, publishedAt time.Time) error {
	if publishedAt.IsZero() {
		return errors.New("GitHub proposal publication time missing")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var commentID int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(comment_id,0) FROM report_deliveries WHERE delivery_key=? AND trigger_namespace='code-pr-proposal'`, reportKey).Scan(&commentID); err != nil {
			return err
		}
		var at string
		if err := tx.QueryRowContext(ctx, `SELECT enrolled_at FROM delivery_pr_bindings WHERE proposal_key=?`, proposal).Scan(&at); err != nil {
			return err
		}
		if commentID > 0 {
			if at != FormatTime(publishedAt) {
				return ErrBindingConflict
			}
			return nil
		}
		_, err := tx.ExecContext(ctx, `UPDATE delivery_pr_bindings SET enrolled_at=? WHERE proposal_key=?`, FormatTime(publishedAt), proposal)
		return err
	})
}

// GetDeliveryPRBinding reads the original PR and enrollment boundary.
func (db *DB) GetDeliveryPRBinding(ctx context.Context, key string) (b DeliveryPRBinding, err error) {
	var at string
	err = db.sql.QueryRowContext(ctx, `SELECT proposal_key,session_id,pr_id,pr_number,enrolled_at FROM delivery_pr_bindings WHERE proposal_key=?`, key).Scan(&b.Key, &b.SessionID, &b.PRID, &b.PRNumber, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err == nil {
		b.EnrolledAt, err = ParseTime(at)
	}
	return
}

// DeliveryControlSource records a real control source. A CLI request has its
// own immutable ID and never impersonates a remote comment.
type DeliveryControlSource struct {
	ID, Kind, ProposalKey, Body   string
	SessionID, ActorID, CommentID int64
	Generation                    int
	CreatedAt                     time.Time
	Completed                     bool
}

const deliverySourceColumns = `source_id,session_id,kind,proposal_key,generation,actor_id,comment_id,body,created_at,completed`

func scanDeliverySource(s scanner) (v DeliveryControlSource, err error) {
	var at string
	err = s.Scan(&v.ID, &v.SessionID, &v.Kind, &v.ProposalKey, &v.Generation, &v.ActorID, &v.CommentID, &v.Body, &at, &v.Completed)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		v.CreatedAt, err = ParseTime(at)
	}
	return
}

// GetDeliveryControlSource reads retained code authorization evidence.
func (db *DB) GetDeliveryControlSource(ctx context.Context, id string) (DeliveryControlSource, error) {
	return scanDeliverySource(db.sql.QueryRowContext(ctx, `SELECT `+deliverySourceColumns+` FROM delivery_control_sources WHERE source_id=?`, id))
}

// RetainDeliveryControlSource journals qualified GitHub authorization without consuming it.
func (db *DB) RetainDeliveryControlSource(ctx context.Context, w DeliveryControlSource) (DeliveryControlSource, error) {
	if w.ID == "" || w.SessionID <= 0 || w.Generation <= 0 || w.ActorID <= 0 || w.CreatedAt.IsZero() || w.Body == "" || len(w.Body) > 512 || w.Completed {
		return w, errors.New("invalid typed delivery source")
	}
	w.CreatedAt = w.CreatedAt.UTC().Truncate(time.Millisecond)
	a, err := db.EnsureDeliveryCodeAuthority(ctx, w.SessionID)
	if err != nil {
		return w, err
	}
	if w.ActorID != a.OwnerID {
		return w, ErrApproverUnauthorized
	}
	switch w.Kind {
	case "github-cli-request":
		if w.CommentID != 0 || w.ProposalKey != "" || w.Body != fmt.Sprintf("/deliver %d", w.Generation) {
			return w, ErrApproverUnauthorized
		}
	case "github-pr-comment":
		q, e := db.GetDelivery(ctx, w.ProposalKey)
		if e != nil {
			return w, e
		}
		binding, e := db.GetDeliveryPRBinding(ctx, w.ProposalKey)
		if e != nil {
			return w, e
		}
		if binding.SessionID != w.SessionID || q.Generation != w.Generation || !q.Proposed || q.Superseded || !w.CreatedAt.After(binding.EnrolledAt) || w.CommentID <= 0 {
			return w, ErrApproverUnauthorized
		}
	default:
		return w, errors.New("unknown delivery source kind")
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO delivery_control_sources(`+deliverySourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,0) ON CONFLICT(source_id) DO NOTHING`, w.ID, w.SessionID, w.Kind, w.ProposalKey, w.Generation, w.ActorID, w.CommentID, w.Body, FormatTime(w.CreatedAt))
	if err != nil {
		return w, err
	}
	old, err := db.GetDeliveryControlSource(ctx, w.ID)
	if err != nil {
		return old, err
	}
	completed := old.Completed
	old.Completed = false
	if old != w {
		return old, ErrBindingConflict
	}
	old.Completed = completed
	return old, nil
}

// CompleteDeliveryControlSource confirms that the existing delivery service handled a source.
func (db *DB) CompleteDeliveryControlSource(ctx context.Context, id string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE delivery_control_sources SET completed=1 WHERE source_id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return ErrNotFound
	}
	return err
}

// PendingDeliveryControlSources lists unconsumed code authorization sources.
func (db *DB) PendingDeliveryControlSources(ctx context.Context) ([]DeliveryControlSource, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+deliverySourceColumns+` FROM delivery_control_sources WHERE completed=0 ORDER BY created_at,source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryControlSource
	for rows.Next() {
		v, e := scanDeliverySource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ApproveDeliverySource qualifies a real GitHub PR comment in addition to the
// unchanged legacy numeric approval columns and proposal digest.
func (db *DB) ApproveDeliverySource(ctx context.Context, key, sourceID string) error {
	source, err := db.GetDeliveryControlSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if source.Kind != "github-pr-comment" || source.ProposalKey != key {
		return ErrApproverUnauthorized
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		proposal, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM delivery_proposals WHERE proposal_key=?`, key))
		if err != nil {
			return err
		}
		if !proposal.Proposed || proposal.Superseded || proposal.PRNumber <= 0 || proposal.PRID <= 0 || proposal.Generation != source.Generation {
			return ErrApproverUnauthorized
		}
		var retainedSource string
		if err = tx.QueryRowContext(ctx, `SELECT approval_source_id FROM delivery_proposals WHERE proposal_key=?`, key).Scan(&retainedSource); err != nil {
			return err
		}
		if proposal.ApprovalComment > 0 {
			if retainedSource != sourceID || proposal.ApprovalComment != source.CommentID || proposal.ActorID != source.ActorID {
				return ErrBindingConflict
			}
			return nil
		}
		if retainedSource != "" {
			return ErrBindingConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE delivery_proposals SET approval_comment=?,actor_id=?,approval_source_id=? WHERE proposal_key=?`, source.CommentID, source.ActorID, sourceID, key)
		return err
	})
}

// CompleteDeliveryPRNotice confirms code publication without consuming a ticket
// reply or advancing the Linear workflow watermark.
func (db *DB) CompleteDeliveryPRNotice(ctx context.Context, key string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE report_deliveries SET completed=1 WHERE delivery_key=? AND trigger_namespace='code-pr-proposal' AND comment_id>0`, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return ErrNotFound
	}
	return err
}
