package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// MaxAccessGrantText is the largest grant text, in bytes.
const MaxAccessGrantText = 4096

// Access grant errors.
var (
	ErrAccessGrantText     = errors.New("grant text must be non-blank UTF-8 of at most 4096 bytes")
	ErrAccessGrantApprover = errors.New("the project approver already has full authority and needs no grant")
	ErrAccessGrantMissing  = errors.New("no active grant with that id on this project")
)

// AccessGrant is one revision of a plain-text grant. The text is never parsed.
type AccessGrant struct {
	ID, RepoID, Revision int64
	Actor                ticket.Identity
	Handle, Text         string
	CreatedAt            time.Time
	ClosedAt             time.Time
	CloseReason          string
}

// Open reports whether the revision is still in force.
func (g AccessGrant) Open() bool { return g.ClosedAt.IsZero() }

const accessGrantColumns = `id,repo_id,revision,provider,api_origin,actor_id,handle,grant_text,created_at,closed_at,close_reason`

func scanAccessGrant(row scanner) (AccessGrant, error) {
	var g AccessGrant
	var created string
	var closed, reason sql.NullString
	if err := row.Scan(&g.ID, &g.RepoID, &g.Revision, &g.Actor.Provider, &g.Actor.Origin, &g.Actor.RemoteID, &g.Handle, &g.Text, &created, &closed, &reason); err != nil {
		return g, err
	}
	var err error
	if g.CreatedAt, err = ParseTime(created); err != nil {
		return g, err
	}
	if closed.Valid {
		if g.ClosedAt, err = ParseTime(closed.String); err != nil {
			return g, err
		}
		g.CloseReason = reason.String
	}
	return g, nil
}

// ValidAccessGrantText applies the text rules without interpreting the text.
func ValidAccessGrantText(text string) error {
	if len(text) > MaxAccessGrantText || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return ErrAccessGrantText
	}
	return nil
}

// GrantAccess stores text as revision 1, or closes the person's open grant and
// stores the next revision, in one transaction. Nothing is stored on refusal.
func (db *DB) GrantAccess(ctx context.Context, g AccessGrant) (AccessGrant, error) {
	if err := ValidAccessGrantText(g.Text); err != nil {
		return g, err
	}
	if g.RepoID <= 0 || g.Handle == "" {
		return g, errors.New("store: grant requires project and handle")
	}
	if err := g.Actor.Validate(); err != nil {
		return g, err
	}
	var out AccessGrant
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		now := FormatTime(nowUTC())
		var approver int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_approver_appointments WHERE id=(SELECT appointment_id FROM project_approvers WHERE repo_id=?) AND provider=? AND api_origin=? AND actor_id=?`, g.RepoID, g.Actor.Provider, g.Actor.Origin, g.Actor.RemoteID).Scan(&approver); err != nil {
			return err
		}
		if approver > 0 {
			return ErrAccessGrantApprover
		}
		var open, last int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM project_access_grants WHERE repo_id=? AND provider=? AND api_origin=? AND actor_id=? AND closed_at IS NULL`, g.RepoID, g.Actor.Provider, g.Actor.Origin, g.Actor.RemoteID).Scan(&open)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE project_access_grants SET closed_at=?,close_reason='replaced' WHERE id=?`, now, open); err != nil {
				return err
			}
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM project_access_grants WHERE repo_id=? AND provider=? AND api_origin=? AND actor_id=?`, g.RepoID, g.Actor.Provider, g.Actor.Origin, g.Actor.RemoteID).Scan(&last); err != nil {
			return err
		}
		revision := last + 1
		res, err := tx.ExecContext(ctx, `INSERT INTO project_access_grants(repo_id,provider,api_origin,actor_id,handle,revision,grant_text,created_at) VALUES(?,?,?,?,?,?,?,?)`, g.RepoID, g.Actor.Provider, g.Actor.Origin, g.Actor.RemoteID, g.Handle, revision, g.Text, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		out, err = scanAccessGrant(tx.QueryRowContext(ctx, `SELECT `+accessGrantColumns+` FROM project_access_grants WHERE id=?`, id))
		return err
	})
	return out, err
}

// RevokeAccessGrant closes the open grant with the given id. It never deletes.
func (db *DB) RevokeAccessGrant(ctx context.Context, repoID, id int64) (AccessGrant, error) {
	var out AccessGrant
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE project_access_grants SET closed_at=?,close_reason='revoked' WHERE id=? AND repo_id=? AND closed_at IS NULL`, FormatTime(nowUTC()), id, repoID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrAccessGrantMissing
		}
		out, err = scanAccessGrant(tx.QueryRowContext(ctx, `SELECT `+accessGrantColumns+` FROM project_access_grants WHERE id=?`, id))
		return err
	})
	return out, err
}

// ListAccessGrants returns the project's open grants, oldest first.
func (db *DB) ListAccessGrants(ctx context.Context, repoID int64) ([]AccessGrant, error) {
	return db.queryAccessGrants(ctx, `SELECT `+accessGrantColumns+` FROM project_access_grants WHERE repo_id=? AND closed_at IS NULL ORDER BY id`, repoID)
}

// AccessGrantHistory returns every revision of every grant, oldest first.
func (db *DB) AccessGrantHistory(ctx context.Context, repoID int64) ([]AccessGrant, error) {
	return db.queryAccessGrants(ctx, `SELECT `+accessGrantColumns+` FROM project_access_grants WHERE repo_id=? ORDER BY id`, repoID)
}

func (db *DB) queryAccessGrants(ctx context.Context, query string, repoID int64) ([]AccessGrant, error) {
	rows, err := db.sql.QueryContext(ctx, query, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessGrant
	for rows.Next() {
		g, err := scanAccessGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
