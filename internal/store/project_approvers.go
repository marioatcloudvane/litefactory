package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// Approver errors distinguish configuration, admission and replacement failures.
var (
	ErrApproverConflict     = errors.New("project approver revision conflict")
	ErrApproverBusy         = errors.New("project approver replacement blocked by outstanding work")
	ErrApproverMissing      = errors.New("project requires a verified explicit ticket approver")
	ErrApproverUnauthorized = errors.New("actor is not the current qualified project approver")
	ErrApproverReplay       = errors.New("reply must have a creation time strictly after approver activation")
)

// ApproverAppointment retains an authority version independently of code ownership.
type ApproverAppointment struct {
	ID, RepoID, Revision    int64
	Actor                   ticket.Identity
	Handle                  string
	VerifiedAt, ActivatedAt time.Time
	Provenance              string
	RequireFresh            bool
}

const approverColumns = `id,repo_id,revision,provider,api_origin,actor_id,handle,verified_at,activated_at,provenance,require_fresh`

func scanApprover(row scanner) (ApproverAppointment, error) {
	var a ApproverAppointment
	var verified, activated string
	err := row.Scan(&a.ID, &a.RepoID, &a.Revision, &a.Actor.Provider, &a.Actor.Origin, &a.Actor.RemoteID, &a.Handle, &verified, &activated, &a.Provenance, &a.RequireFresh)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrApproverMissing
	}
	if err != nil {
		return a, err
	}
	a.VerifiedAt, err = ParseTime(verified)
	if err != nil {
		return a, err
	}
	a.ActivatedAt, err = ParseTime(activated)
	return a, err
}
func currentApprover(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, repoID int64) (ApproverAppointment, error) {
	var conflict string
	err := q.QueryRowContext(ctx, `SELECT reason FROM project_approver_initialization WHERE repo_id=? AND status='conflict'`, repoID).Scan(&conflict)
	if err == nil {
		return ApproverAppointment{}, fmt.Errorf("%w: %s", ErrApproverUnauthorized, conflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ApproverAppointment{}, err
	}
	return rawCurrentApprover(ctx, q, repoID)
}
func rawCurrentApprover(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, repoID int64) (ApproverAppointment, error) {
	return scanApprover(q.QueryRowContext(ctx, `SELECT `+approverColumns+` FROM project_approver_appointments WHERE id=(SELECT appointment_id FROM project_approvers WHERE repo_id=?)`, repoID))
}

// RawCurrentApprover inspects retained configuration even when legacy reconciliation blocks admission.
func (db *DB) RawCurrentApprover(ctx context.Context, repoID int64) (ApproverAppointment, error) {
	return rawCurrentApprover(ctx, db.sql, repoID)
}

// CurrentApprover reads the project's current qualified authority.
func (db *DB) CurrentApprover(ctx context.Context, repoID int64) (ApproverAppointment, error) {
	return currentApprover(ctx, db.sql, repoID)
}

// GetApproverAppointment reads historical attribution without consulting current authority.
func (db *DB) GetApproverAppointment(ctx context.Context, id int64) (ApproverAppointment, error) {
	return scanApprover(db.sql.QueryRowContext(ctx, `SELECT `+approverColumns+` FROM project_approver_appointments WHERE id=?`, id))
}
func validateApprover(a ApproverAppointment, c ConnectionVersion) error {
	if a.RepoID <= 0 || a.RepoID != c.RepoID || a.Handle == "" || a.VerifiedAt.IsZero() || a.Provenance == "" {
		return errors.New("store: verified approver identity and provenance are required")
	}
	if err := a.Actor.Validate(); err != nil {
		return err
	}
	id, err := strconv.ParseInt(a.Actor.RemoteID, 10, 64)
	if a.Actor.Provider == ticket.GitHub && (id <= 0 || err != nil) {
		return errors.New("store: GitHub approver requires a positive immutable account ID")
	}
	if a.Actor.Provider != c.Provider || a.Actor.Origin != c.Origin {
		return errors.New("store: approver identity does not match project ticket provider and origin")
	}
	if !c.Enabled {
		return ErrConnectionDisabled
	}
	return nil
}
func setApprover(ctx context.Context, tx *sql.Tx, a ApproverAppointment, connectionRevision, authorityRevision int64, legacy bool) (ApproverAppointment, error) {
	c, err := currentConnection(ctx, tx, a.RepoID, TicketConnection)
	if err != nil {
		return a, err
	}
	if c.Revision != connectionRevision {
		return a, ErrConnectionConflict
	}
	if err = validateApprover(a, c); err != nil {
		return a, err
	}
	old, err := rawCurrentApprover(ctx, tx, a.RepoID)
	if err != nil && !errors.Is(err, ErrApproverMissing) {
		return a, err
	}
	if legacy && err == nil {
		return old, nil
	}
	if legacy {
		var eligible int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_legacy_approver_eligibility WHERE repo_id=?`, a.RepoID).Scan(&eligible); e != nil {
			return a, e
		}
		if eligible != 1 {
			return a, ErrApproverMissing
		}
	}
	if (err == nil && old.Revision != authorityRevision) || (errors.Is(err, ErrApproverMissing) && authorityRevision != 0) {
		return a, ErrApproverConflict
	}
	if err == nil && old.Actor.Equal(a.Actor) {
		var conflict int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_approver_initialization WHERE repo_id=? AND status='conflict'`, a.RepoID).Scan(&conflict); e != nil {
			return a, e
		}
		if conflict > 0 {
			// The same actor is not a replacement, so outstanding work does not
			// block re-confirmation from clearing the conflict.
			if _, e := tx.ExecContext(ctx, `DELETE FROM project_approver_initialization WHERE repo_id=? AND status='conflict'`, a.RepoID); e != nil {
				return a, e
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE project_approver_appointments SET handle=?,verified_at=?,provenance=? WHERE id=?`, a.Handle, FormatTime(a.VerifiedAt), a.Provenance, old.ID)
		if err != nil {
			return a, err
		}
		return currentApprover(ctx, tx, a.RepoID)
	}
	if legacy {
		if e := checkLegacyOutstandingAttribution(ctx, tx, a.RepoID, a.Actor.RemoteID); e != nil {
			return a, e
		}
		ids, e := legacyApproverOwnerIDs(ctx, tx, a.RepoID)
		if e != nil {
			return a, e
		}
		for _, id := range ids {
			if id > 0 && strconv.FormatInt(id, 10) != a.Actor.RemoteID {
				return a, fmt.Errorf("%w: historical approval actor conflicts with legacy identity", ErrApproverUnauthorized)
			}
		}
	}
	if !legacy {
		blockers, e := approverBlockers(ctx, tx, a.RepoID)
		if e != nil {
			return a, e
		}
		if len(blockers) > 0 {
			return a, fmt.Errorf("%w: %s %s", ErrApproverBusy, blockers[0].Kind, blockers[0].Key)
		}
	}
	// A legacy retry that passed its checks above clears the conflict an
	// earlier start recorded, so startup repairs the project without owner action.
	if _, e := tx.ExecContext(ctx, `DELETE FROM project_approver_initialization WHERE repo_id=? AND status='conflict'`, a.RepoID); e != nil {
		return a, e
	}
	a.Revision = old.Revision + 1
	a.ActivatedAt = nowUTC()
	a.RequireFresh = !legacy
	if legacy {
		a.Provenance = "legacy"
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO project_approver_appointments(repo_id,revision,provider,api_origin,actor_id,handle,verified_at,activated_at,provenance,require_fresh) VALUES(?,?,?,?,?,?,?,?,?,?) RETURNING id`, a.RepoID, a.Revision, a.Actor.Provider, a.Actor.Origin, a.Actor.RemoteID, a.Handle, FormatTime(a.VerifiedAt), FormatTime(a.ActivatedAt), a.Provenance, a.RequireFresh).Scan(&a.ID)
	if err != nil {
		return a, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_approvers(repo_id,appointment_id) VALUES(?,?) ON CONFLICT(repo_id) DO UPDATE SET appointment_id=excluded.appointment_id`, a.RepoID, a.ID)
	if err != nil {
		return a, err
	}
	return currentApprover(ctx, tx, a.RepoID)
}

// SetApprover verifies both observed revisions atomically with all replacement blockers.
func (db *DB) SetApprover(ctx context.Context, a ApproverAppointment, connectionRevision, authorityRevision int64) (ApproverAppointment, error) {
	var out ApproverAppointment
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = setApprover(ctx, tx, a, connectionRevision, authorityRevision, false)
		return e
	})
	return out, err
}

// ImportLegacyApprover seeds missing authority only, without resetting replay boundaries.
func (db *DB) ImportLegacyApprover(ctx context.Context, a ApproverAppointment, connectionRevision int64) (ApproverAppointment, error) {
	var out ApproverAppointment
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = setApprover(ctx, tx, a, connectionRevision, 0, true)
		return e
	})
	return out, err
}

// AuthorityClaim is retained evidence and a durable project replacement barrier.
type AuthorityClaim struct {
	ID, RepoID, AppointmentID, Revision int64
	SessionID                           int64
	Actor, Source                       ticket.Identity
	CreatedAt                           time.Time
	Kind, Key                           string
	Completed                           bool
}

// ClaimProjectAuthority admits work only against the exact observed appointment.
func (db *DB) ClaimProjectAuthority(ctx context.Context, c AuthorityClaim) (AuthorityClaim, error) {
	return db.claimProjectAuthority(ctx, c, false)
}

// ClaimProjectCodeAuthority records the actual separately verified code actor.
// The factory must check code permissions before invoking this trusted admission.
// Kind worker is a polling replacement barrier, not authorization for any reply.
func (db *DB) ClaimProjectCodeAuthority(ctx context.Context, c AuthorityClaim) (AuthorityClaim, error) {
	if c.Kind != "code-control" && c.Kind != "worker" {
		return c, errors.New("store: code authority requires code-control or worker claim")
	}
	return db.claimProjectAuthority(ctx, c, true)
}
func (db *DB) claimProjectAuthority(ctx context.Context, c AuthorityClaim, codeAuthority bool) (AuthorityClaim, error) {
	if c.RepoID <= 0 || c.Kind == "" || c.Key == "" || strings.ContainsAny(c.Kind+c.Key, "\x00") {
		return c, errors.New("store: authority claim requires project, kind and key")
	}
	if c.Kind != "worker" {
		if err := c.Source.Validate(); err != nil {
			return c, err
		}
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		a, e := currentApprover(ctx, tx, c.RepoID)
		if codeAuthority {
			a, e = rawCurrentApprover(ctx, tx, c.RepoID)
			if errors.Is(e, ErrApproverMissing) && c.AppointmentID == 0 && c.Revision == 0 {
				e = nil
			}
		}
		if e != nil {
			return e
		}
		connection, e := currentConnection(ctx, tx, c.RepoID, TicketConnection)
		var retainedCode ConnectionVersion
		if codeAuthority && c.SessionID > 0 {
			retainedCode, connection, e = retainedCodeClaimConnections(ctx, tx, c)
		}
		if e != nil {
			return e
		}
		if !connection.Enabled && (!codeAuthority || c.SessionID <= 0) {
			return ErrConnectionDisabled
		}
		if !codeAuthority && (a.Actor.Provider != connection.Provider || a.Actor.Origin != connection.Origin) {
			return ErrApproverUnauthorized
		}
		if a.ID != c.AppointmentID || a.Revision != c.Revision {
			return ErrApproverConflict
		}
		if codeAuthority {
			code := retainedCode
			if c.SessionID == 0 {
				code, e = currentConnection(ctx, tx, c.RepoID, CodeConnection)
			}
			if e != nil {
				return e
			}
			if c.Actor.Validate() != nil || c.Actor.Provider != code.Provider || c.Actor.Origin != code.Origin {
				return ErrApproverUnauthorized
			}
		} else if !a.Actor.Equal(c.Actor) {
			return ErrApproverUnauthorized
		}
		if c.Kind != "worker" && (c.Source.Provider != connection.Provider || c.Source.Origin != connection.Origin) {
			return ErrApproverUnauthorized
		}
		if !codeAuthority && c.Kind != "worker" && a.RequireFresh && (c.CreatedAt.IsZero() || !c.CreatedAt.After(a.ActivatedAt)) {
			return ErrApproverReplay
		}
		var existing AuthorityClaim
		var created string
		e = tx.QueryRowContext(ctx, `SELECT id,COALESCE(appointment_id,0),revision,actor_provider,actor_origin,actor_id,source_provider,source_origin,source_id,source_created_at,completed FROM project_authority_claims WHERE repo_id=? AND kind=? AND claim_key=?`, c.RepoID, c.Kind, c.Key).Scan(&existing.ID, &existing.AppointmentID, &existing.Revision, &existing.Actor.Provider, &existing.Actor.Origin, &existing.Actor.RemoteID, &existing.Source.Provider, &existing.Source.Origin, &existing.Source.RemoteID, &created, &existing.Completed)
		if e == nil {
			if existing.AppointmentID != c.AppointmentID || existing.Revision != c.Revision || created != FormatTime(c.CreatedAt) || !existing.Actor.Equal(c.Actor) || (c.Kind != "worker" && !existing.Source.Equal(c.Source)) {
				return ErrApproverConflict
			}
			c.ID = existing.ID
			c.Completed = existing.Completed
			if existing.Completed {
				if _, e = tx.ExecContext(ctx, `UPDATE project_authority_claims SET completed=0 WHERE id=?`, existing.ID); e != nil {
					return e
				}
				c.Completed = false
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return tx.QueryRowContext(ctx, `INSERT INTO project_authority_claims(repo_id,appointment_id,revision,actor_provider,actor_origin,actor_id,source_provider,source_origin,source_id,source_created_at,kind,claim_key,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`, c.RepoID, nullID(c.AppointmentID), c.Revision, c.Actor.Provider, c.Actor.Origin, c.Actor.RemoteID, c.Source.Provider, c.Source.Origin, c.Source.RemoteID, FormatTime(c.CreatedAt), c.Kind, c.Key, FormatTime(nowUTC())).Scan(&c.ID)
	})
	return c, err
}

// CompleteProjectAuthority releases a claim while preserving its original evidence.
func (db *DB) CompleteProjectAuthority(ctx context.Context, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE project_authority_claims SET completed=1 WHERE id=?`, id)
	return err
}

// ApproverInitialization records actionable legacy conflicts without rewriting work.
type ApproverInitialization struct {
	RepoID         int64
	Status, Reason string
	UpdatedAt      time.Time
}

// SetApproverInitialization records sanitized initialization evidence.
func (db *DB) SetApproverInitialization(ctx context.Context, s ApproverInitialization) error {
	if s.RepoID <= 0 || len(s.Status) > 64 || len(s.Reason) > 256 {
		return errors.New("store: invalid approver initialization")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO project_approver_initialization(repo_id,status,reason,updated_at) VALUES(?,?,?,?) ON CONFLICT(repo_id) DO UPDATE SET status=excluded.status,reason=excluded.reason,updated_at=excluded.updated_at`, s.RepoID, s.Status, s.Reason, FormatTime(nowUTC()))
	return err
}

// GetApproverInitialization reads retained initialization state.
func (db *DB) GetApproverInitialization(ctx context.Context, repoID int64) (ApproverInitialization, error) {
	var s ApproverInitialization
	var at string
	err := db.sql.QueryRowContext(ctx, `SELECT repo_id,status,reason,updated_at FROM project_approver_initialization WHERE repo_id=?`, repoID).Scan(&s.RepoID, &s.Status, &s.Reason, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.UpdatedAt, err = ParseTime(at)
	return s, err
}

// RecoverProjectWorkerClaims releases abandoned process claims only after the
// daemon has reconciled orphan runs and locks. Other durable owners still block
// replacement independently and remote instruction evidence stays untouched.
func (db *DB) RecoverProjectWorkerClaims(ctx context.Context, repoID int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var taskRef string
		err := tx.QueryRowContext(ctx, `SELECT s.task_ref FROM sessions s WHERE s.repo_id=? AND (s.state='running' OR COALESCE(s.lock_owner,'')<>'' OR EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status IN ('prelaunch','running'))) ORDER BY s.id LIMIT 1`, repoID).Scan(&taskRef)
		if err == nil {
			return fmt.Errorf("%w: session %s", ErrApproverBusy, taskRef)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE project_authority_claims SET completed=1 WHERE repo_id=? AND kind='worker' AND completed=0`, repoID)
		return err
	})
}
