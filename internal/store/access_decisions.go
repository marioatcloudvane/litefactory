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

// Access decision states and outcomes.
const (
	AccessDecisionPending = "pending"
	AccessDecisionFinal   = "final"

	AccessAllow       = "allow"
	AccessDeny        = "deny"
	AccessFloor       = "floor"
	AccessError       = "error"
	AccessLimited     = "limited"
	AccessRateLimited = "rate_limited"
)

// MaxAccessDecisionReason is the longest decision reason, in runes.
const MaxAccessDecisionReason = 300

// ErrAccessDecisionFinal refuses a second finish of the same check.
var ErrAccessDecisionFinal = errors.New("access decision is already final")

// AccessDecision is one grantee access check, keyed by the source comment that
// asked. It is written pending before the check runs and made final once.
type AccessDecision struct {
	ID, RepoID, SessionID  int64
	Source                 ticket.Identity
	CoveredThrough         string
	Actor                  ticket.Identity
	Handle                 string
	GrantID, GrantRevision int64 // 0 when no grant (stored NULL)
	TargetJSON             string
	RequestDigest          string
	State                  string
	Decision, Reason       string
	Model                  string
	Classified             bool // the classifier was called for this row
	CreatedAt, DecidedAt   time.Time
}

// AccessDecisionOutcome is what finishing a check records.
type AccessDecisionOutcome struct {
	Decision, Reason, Model string
	Classified              bool
}

const accessDecisionColumns = `id,repo_id,session_id,source_provider,source_origin,source_id,covered_through,actor_provider,actor_origin,actor_id,handle,grant_id,grant_revision,target_json,request_digest,state,decision,reason,model,classified,created_at,decided_at`

func scanAccessDecision(row scanner) (AccessDecision, error) {
	var d AccessDecision
	var covered, decision, reason, decided sql.NullString
	var grantID, grantRevision sql.NullInt64
	var created string
	if err := row.Scan(&d.ID, &d.RepoID, &d.SessionID, &d.Source.Provider, &d.Source.Origin, &d.Source.RemoteID, &covered,
		&d.Actor.Provider, &d.Actor.Origin, &d.Actor.RemoteID, &d.Handle, &grantID, &grantRevision, &d.TargetJSON, &d.RequestDigest,
		&d.State, &decision, &reason, &d.Model, &d.Classified, &created, &decided); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return d, ErrNotFound
		}
		return d, err
	}
	d.CoveredThrough, d.Decision, d.Reason = covered.String, decision.String, reason.String
	d.GrantID, d.GrantRevision = grantID.Int64, grantRevision.Int64
	var err error
	if d.CreatedAt, err = ParseTime(created); err != nil {
		return d, err
	}
	if decided.Valid {
		if d.DecidedAt, err = ParseTime(decided.String); err != nil {
			return d, err
		}
	}
	return d, nil
}

func validAccessDecision(decision string) bool {
	switch decision {
	case AccessAllow, AccessDeny, AccessFloor, AccessError, AccessLimited, AccessRateLimited:
		return true
	}
	return false
}

// OpenAccessGrant returns the open grant for repo and actor, ErrNotFound when none.
func (db *DB) OpenAccessGrant(ctx context.Context, repoID int64, actor ticket.Identity) (AccessGrant, error) {
	g, err := scanAccessGrant(db.sql.QueryRowContext(ctx, `SELECT `+accessGrantColumns+` FROM project_access_grants WHERE repo_id=? AND provider=? AND api_origin=? AND actor_id=? AND closed_at IS NULL`, repoID, actor.Provider, actor.Origin, actor.RemoteID))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// BeginAccessDecision inserts d as pending and returns it with created true.
// When the source already has a row, that row is returned unchanged with
// created false, whether pending or final.
func (db *DB) BeginAccessDecision(ctx context.Context, d AccessDecision) (AccessDecision, bool, error) {
	if d.RepoID <= 0 || d.SessionID <= 0 {
		return d, false, errors.New("store: access decision requires project and session")
	}
	if err := d.Source.Validate(); err != nil {
		return d, false, err
	}
	if err := d.Actor.Validate(); err != nil {
		return d, false, err
	}
	if strings.TrimSpace(d.RequestDigest) == "" {
		return d, false, errors.New("store: access decision requires a request digest")
	}
	var out AccessDecision
	var created bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO project_access_decisions(repo_id,session_id,source_provider,source_origin,source_id,covered_through,actor_provider,actor_origin,actor_id,handle,grant_id,grant_revision,target_json,request_digest,state,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',?) ON CONFLICT(repo_id,source_provider,source_origin,source_id) DO NOTHING`,
			d.RepoID, d.SessionID, d.Source.Provider, d.Source.Origin, d.Source.RemoteID, nullString(d.CoveredThrough),
			d.Actor.Provider, d.Actor.Origin, d.Actor.RemoteID, d.Handle, nullID(d.GrantID), nullID(d.GrantRevision),
			d.TargetJSON, d.RequestDigest, FormatTime(nowUTC()))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		created = n == 1
		out, err = scanAccessDecision(tx.QueryRowContext(ctx, `SELECT `+accessDecisionColumns+` FROM project_access_decisions WHERE repo_id=? AND source_provider=? AND source_origin=? AND source_id=?`, d.RepoID, d.Source.Provider, d.Source.Origin, d.Source.RemoteID))
		return err
	})
	if err != nil {
		return AccessDecision{}, false, err
	}
	return out, created, nil
}

// FinishAccessDecision moves a pending row to final once. A final row is
// refused with ErrAccessDecisionFinal. The reason must be one non-blank line of
// at most MaxAccessDecisionReason runes.
func (db *DB) FinishAccessDecision(ctx context.Context, id int64, out AccessDecisionOutcome) (AccessDecision, error) {
	if !validAccessDecision(out.Decision) {
		return AccessDecision{}, errors.New("store: unknown access decision " + out.Decision)
	}
	reason := strings.TrimSpace(out.Reason)
	if reason == "" || strings.ContainsAny(reason, "\r\n") || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > MaxAccessDecisionReason {
		return AccessDecision{}, errors.New("store: access decision reason must be one non-blank line of at most 300 characters")
	}
	var d AccessDecision
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanAccessDecision(tx.QueryRowContext(ctx, `SELECT `+accessDecisionColumns+` FROM project_access_decisions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if cur.State != AccessDecisionPending {
			return ErrAccessDecisionFinal
		}
		classified := 0
		if out.Classified {
			classified = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_access_decisions SET state='final',decision=?,reason=?,model=?,classified=?,decided_at=? WHERE id=? AND state='pending'`,
			out.Decision, reason, out.Model, classified, FormatTime(nowUTC()), id); err != nil {
			return err
		}
		d, err = scanAccessDecision(tx.QueryRowContext(ctx, `SELECT `+accessDecisionColumns+` FROM project_access_decisions WHERE id=?`, id))
		return err
	})
	return d, err
}

// GetAccessDecision reads one row by id, ErrNotFound when missing.
func (db *DB) GetAccessDecision(ctx context.Context, id int64) (AccessDecision, error) {
	return scanAccessDecision(db.sql.QueryRowContext(ctx, `SELECT `+accessDecisionColumns+` FROM project_access_decisions WHERE id=?`, id))
}

// AccessDecisionBySource reads the row for a source comment, ErrNotFound when missing.
func (db *DB) AccessDecisionBySource(ctx context.Context, repoID int64, source ticket.Identity) (AccessDecision, error) {
	return scanAccessDecision(db.sql.QueryRowContext(ctx, `SELECT `+accessDecisionColumns+` FROM project_access_decisions WHERE repo_id=? AND source_provider=? AND source_origin=? AND source_id=?`, repoID, source.Provider, source.Origin, source.RemoteID))
}

// CountAccessChecks counts the actor's checks on the project created at or
// after since that reached the classifier: classified rows, and pending rows
// still under way. The row excludeID is not counted; 0 excludes none.
func (db *DB) CountAccessChecks(ctx context.Context, repoID int64, actor ticket.Identity, since time.Time, excludeID int64) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_access_decisions WHERE repo_id=? AND actor_provider=? AND actor_origin=? AND actor_id=? AND created_at>=? AND id<>? AND (classified=1 OR state='pending')`,
		repoID, actor.Provider, actor.Origin, actor.RemoteID, FormatTime(since), excludeID).Scan(&n)
	return n, err
}
