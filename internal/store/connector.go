package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// connectorWrite acquires a write lock before reading. Other local processes
// therefore cannot race rotation or revocation; SQLite's finite busy timeout
// applies to admission, never to a read-to-write lock upgrade.
func (db *DB) connectorWrite(ctx context.Context, fn func(*sql.Conn) error) (err error) {
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, rb := conn.ExecContext(rollbackCtx, "ROLLBACK")
			if rb != nil {
				err = errors.Join(err, rb)
			}
		}
	}()
	if err = fn(conn); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func connectorOwner(ctx context.Context, c interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, pin int64) (ConnectorOwner, error) {
	var o ConnectorOwner
	err := c.QueryRowContext(ctx, `SELECT o.owner_id,o.generation FROM connector_owner o JOIN identities i ON i.forge='github' AND i.role='owner' AND i.user_id=o.owner_id WHERE singleton=1`).Scan(&o.OwnerID, &o.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrConnectorOwner
	}
	if err != nil {
		return o, err
	}
	if pin <= 0 || o.OwnerID != pin {
		return o, ErrConnectorOwner
	}
	return o, nil
}

// ConnectorOwner checks the configured pin against the current persisted owner.
func (db *DB) ConnectorOwner(ctx context.Context, pin int64) (ConnectorOwner, error) {
	return connectorOwner(ctx, db.sql, pin)
}

// RegisterConnectorClient admits bounded metadata without granting data access.
func (db *DB) RegisterConnectorClient(ctx context.Context, c ConnectorClient, now time.Time) error {
	if c.ID == "" || c.Name == "" || len(c.Name) > 200 || len(c.Redirects) == 0 || len(c.Redirects) > 10 || !c.ExpiresAt.After(now) {
		return ErrConnectorInvalid
	}
	redirects, err := json.Marshal(c.Redirects)
	if err != nil {
		return err
	}
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		if err := connectorCleanup(ctx, tx, now, 100); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_clients`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return ErrConnectorCapacity
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO connector_clients(id,name,auth_method,secret_digest,redirects,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, c.ID, c.Name, c.AuthMethod, c.SecretDigest, string(redirects), FormatTime(c.CreatedAt), FormatTime(c.ExpiresAt))
		return err
	})
}

// GetConnectorClient permits retained registrations while a live grant refers to them.
func (db *DB) GetConnectorClient(ctx context.Context, id string, now time.Time) (ConnectorClient, error) {
	return getConnectorClient(ctx, db.sql, id, now)
}
func getConnectorClient(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, now time.Time) (ConnectorClient, error) {
	var c ConnectorClient
	var redirects, created, expires string
	err := q.QueryRowContext(ctx, `SELECT id,name,auth_method,secret_digest,redirects,created_at,expires_at FROM connector_clients c WHERE id=? AND (expires_at>? OR EXISTS(SELECT 1 FROM connector_grants g WHERE g.client_id=c.id AND g.expires_at>?))`, id, FormatTime(now), FormatTime(now)).Scan(&c.ID, &c.Name, &c.AuthMethod, &c.SecretDigest, &redirects, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrConnectorInvalid
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(redirects), &c.Redirects); err != nil {
		return c, err
	}
	c.CreatedAt, err = ParseTime(created)
	if err != nil {
		return c, err
	}
	c.ExpiresAt, err = ParseTime(expires)
	return c, err
}

// CreateConnectorBrowser stores a bounded, ten-minute browser transaction.
func (db *DB) CreateConnectorBrowser(ctx context.Context, b ConnectorBrowser, pin int64, now time.Time) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		o, err := connectorOwner(ctx, tx, pin)
		if err != nil {
			return err
		}
		if b.OwnerGeneration != o.Generation || !b.ExpiresAt.After(now) || b.ExpiresAt.After(now.Add(10*time.Minute)) || b.Verified {
			return ErrConnectorInvalid
		}
		c, err := getConnectorClient(ctx, tx, b.ClientID, now)
		if err != nil {
			return err
		}
		found := false
		for _, r := range c.Redirects {
			if r == b.RedirectURI {
				found = true
			}
		}
		if !found {
			return ErrConnectorInvalid
		}
		if err = connectorCleanup(ctx, tx, now, 100); err != nil {
			return err
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_browsers`).Scan(&count); err != nil {
			return err
		}
		if count >= 100 {
			return ErrConnectorCapacity
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO connector_browsers(id,client_id,redirect_uri,state,resource,scope,pkce_challenge,nonce_digest,upstream_digest,generation,verified,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,0,?)`, b.ID, b.ClientID, b.RedirectURI, b.State, b.Resource, b.Scope, b.PKCEChallenge, b.NonceDigest, b.UpstreamStateDigest, b.OwnerGeneration, FormatTime(b.ExpiresAt))
		return err
	})
}

// GetConnectorBrowser never returns a stale owner generation or expired transaction.
func (db *DB) GetConnectorBrowser(ctx context.Context, id string, pin int64, now time.Time) (ConnectorBrowser, error) {
	var b ConnectorBrowser
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		var err error
		b, err = getConnectorBrowser(ctx, tx, id, pin, now)
		return err
	})
	return b, err
}
func getConnectorBrowser(ctx context.Context, tx *sql.Conn, id string, pin int64, now time.Time) (ConnectorBrowser, error) {
	var b ConnectorBrowser
	var expiry string
	o, err := connectorOwner(ctx, tx, pin)
	if err != nil {
		return b, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,client_id,redirect_uri,state,resource,scope,pkce_challenge,nonce_digest,upstream_digest,generation,verified,expires_at FROM connector_browsers WHERE id=? AND expires_at>? AND generation=?`, id, FormatTime(now), o.Generation).Scan(&b.ID, &b.ClientID, &b.RedirectURI, &b.State, &b.Resource, &b.Scope, &b.PKCEChallenge, &b.NonceDigest, &b.UpstreamStateDigest, &b.OwnerGeneration, &b.Verified, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrConnectorInvalid
	}
	if err != nil {
		return b, err
	}
	b.ExpiresAt, err = ParseTime(expiry)
	return b, err
}

// VerifyConnectorBrowser consumes upstream state exactly once after fresh identity verification.
func (db *DB) VerifyConnectorBrowser(ctx context.Context, id, nonceDigest, upstreamDigest string, pin int64, now time.Time) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		b, err := getConnectorBrowser(ctx, tx, id, pin, now)
		if err != nil {
			return err
		}
		if b.Verified || b.NonceDigest != nonceDigest || b.UpstreamStateDigest != upstreamDigest {
			return ErrConnectorInvalid
		}
		_, err = tx.ExecContext(ctx, `UPDATE connector_browsers SET verified=1,upstream_digest='' WHERE id=?`, id)
		return err
	})
}

// ConsentConnectorBrowser consumes consent once, atomically creating its grant and code.
func (db *DB) ConsentConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time, g ConnectorGrant, c ConnectorCode, allow bool) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		b, err := getConnectorBrowser(ctx, tx, id, pin, now)
		if err != nil {
			return err
		}
		if !b.Verified || b.NonceDigest != nonceDigest {
			return ErrConnectorInvalid
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM connector_browsers WHERE id=?`, id); err != nil {
			return err
		}
		if !allow {
			return nil
		}
		if g.ID == "" || g.ClientID != b.ClientID || g.Resource != b.Resource || !ConnectorScopeSubset(g.Scope, b.Scope) || g.OwnerID != pin || g.OwnerGeneration != b.OwnerGeneration || !g.ExpiresAt.After(now) || g.ExpiresAt.After(ConnectorMaxGrantExpiry(now)) || c.Digest == "" || c.GrantID != g.ID || c.ClientID != b.ClientID || c.RedirectURI != b.RedirectURI || c.Resource != b.Resource || c.Scope != g.Scope || c.PKCEChallenge != b.PKCEChallenge || !c.ExpiresAt.After(now) || c.ExpiresAt.After(now.Add(5*time.Minute)) {
			return ErrConnectorInvalid
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_grants WHERE revoked_at IS NULL AND expires_at>?`, FormatTime(now)).Scan(&count); err != nil {
			return err
		}
		if count >= 128 {
			return ErrConnectorCapacity
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_grants`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return ErrConnectorCapacity
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`, g.ID, g.ClientID, g.Resource, g.Scope, g.OwnerID, g.OwnerGeneration, FormatTime(g.CreatedAt), FormatTime(g.ExpiresAt))
		if err != nil {
			return err
		}
		if ConnectorScopeHas(g.Scope, connectorDiscussionScope) {
			if g.Target == nil {
				return ErrConnectorInvalid
			}
			target, err := resolveConnectorDiscussionTarget(ctx, tx, g.Target.TaskRef)
			if err != nil {
				return err
			}
			if (g.Target.SessionID != 0 && g.Target.SessionID != target.SessionID) || (g.Target.RepoID != 0 && g.Target.RepoID != target.RepoID) || (g.Target.RepositoryRef != "" && g.Target.RepositoryRef != target.RepositoryRef) {
				return ErrConnectorInvalid
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO connector_grant_targets(grant_id,task_ref,session_id,repo_id,repository_ref) VALUES(?,?,?,?,?)`, g.ID, target.TaskRef, target.SessionID, target.RepoID, target.RepositoryRef); err != nil {
				return err
			}
		} else if g.Target != nil {
			return ErrConnectorInvalid
		}
		if err = insertConnectorUsageTargets(ctx, tx, g); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO connector_codes(digest,grant_id,client_id,redirect_uri,resource,scope,pkce_challenge,expires_at) VALUES(?,?,?,?,?,?,?,?)`, c.Digest, c.GrantID, c.ClientID, c.RedirectURI, c.Resource, c.Scope, c.PKCEChallenge, FormatTime(c.ExpiresAt))
		return err
	})
}

func connectorGrant(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, pin int64, now time.Time) (ConnectorGrant, error) {
	var g ConnectorGrant
	var created, expiry string
	o, err := connectorOwner(ctx, tx, pin)
	if err != nil {
		return g, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,client_id,resource,scope,owner_id,generation,created_at,expires_at FROM connector_grants WHERE id=? AND owner_id=? AND generation=? AND revoked_at IS NULL AND expires_at>?`, id, o.OwnerID, o.Generation, FormatTime(now)).Scan(&g.ID, &g.ClientID, &g.Resource, &g.Scope, &g.OwnerID, &g.OwnerGeneration, &created, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrConnectorInvalid
	}
	if err != nil {
		return g, err
	}
	g.CreatedAt, err = ParseTime(created)
	if err != nil {
		return g, err
	}
	g.ExpiresAt, err = ParseTime(expiry)
	if err == nil && ConnectorScopeHas(g.Scope, connectorDiscussionScope) {
		g.Target, err = connectorDiscussionTarget(ctx, tx, g.ID)
	}
	if err == nil {
		err = loadConnectorUsageTargets(ctx, tx, &g)
	}
	return g, err
}

func insertConnectorPair(ctx context.Context, tx *sql.Conn, g ConnectorGrant, p ConnectorTokenPair, now time.Time, parent string) error {
	if p.AccessExpiresAt.After(g.ExpiresAt) {
		p.AccessExpiresAt = g.ExpiresAt
	}
	if err := connectorCleanup(ctx, tx, now, 100); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_refresh`).Scan(&count); err != nil {
		return err
	}
	if count >= 100000 {
		return ErrConnectorCapacity
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_access`).Scan(&count); err != nil {
		return err
	}
	if count >= 100000 {
		return ErrConnectorCapacity
	}
	if p.AccessDigest == "" || p.RefreshDigest == "" || !p.AccessExpiresAt.After(now) || p.AccessExpiresAt.After(now.Add(15*time.Minute)) || p.AccessExpiresAt.After(g.ExpiresAt) {
		return ErrConnectorInvalid
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO connector_access(digest,grant_id,expires_at) VALUES(?,?,?)`, p.AccessDigest, g.ID, FormatTime(p.AccessExpiresAt))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO connector_refresh(digest,grant_id,parent_digest,expires_at) VALUES(?,?,?,?)`, p.RefreshDigest, g.ID, nullString(parent), FormatTime(g.ExpiresAt))
	return err
}

// ExchangeConnectorCode consumes a fully bound code and commits both credentials together.
func (db *DB) ExchangeConnectorCode(ctx context.Context, x ConnectorExchange) (ConnectorGrant, error) {
	var g ConnectorGrant
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		var id string
		var scope string
		err := tx.QueryRowContext(ctx, `SELECT grant_id,scope FROM connector_codes WHERE digest=? AND client_id=? AND redirect_uri=? AND resource=? AND pkce_challenge=? AND consumed_at IS NULL AND expires_at>?`, x.Digest, x.ClientID, x.RedirectURI, x.Resource, x.PKCEChallenge, FormatTime(x.Now)).Scan(&id, &scope)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectorInvalid
		}
		if err != nil {
			return err
		}
		g, err = connectorGrant(ctx, tx, id, x.OwnerID, x.Now)
		if err != nil {
			return err
		}
		if g.Scope != scope || !connectorScopeMatches(x.Scope, g.Scope) {
			return ErrConnectorInvalid
		}
		if err = insertConnectorPair(ctx, tx, g, x.Pair, x.Now, ""); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE connector_codes SET consumed_at=? WHERE digest=?`, FormatTime(x.Now), x.Digest)
		return err
	})
	return g, err
}

// RotateConnectorRefresh preserves spent ancestors. Replay revocation commits
// even though the caller receives an invalid-grant result.
func (db *DB) RotateConnectorRefresh(ctx context.Context, x ConnectorExchange) (ConnectorGrant, error) {
	var g ConnectorGrant
	replayed := false
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		var id string
		var consumed sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT grant_id,consumed_at FROM connector_refresh WHERE digest=? AND expires_at>?`, x.Digest, FormatTime(x.Now)).Scan(&id, &consumed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectorInvalid
		}
		if err != nil {
			return err
		}
		g, err = connectorGrant(ctx, tx, id, x.OwnerID, x.Now)
		if err != nil {
			return err
		}
		if g.ClientID != x.ClientID || g.Resource != x.Resource || !connectorScopeMatches(x.Scope, g.Scope) {
			return ErrConnectorInvalid
		}
		if consumed.Valid {
			_, err = tx.ExecContext(ctx, `UPDATE connector_grants SET revoked_at=?,reason='refresh_replay' WHERE id=?`, FormatTime(x.Now), id)
			replayed = err == nil
			return err
		}
		if err = insertConnectorPair(ctx, tx, g, x.Pair, x.Now, x.Digest); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE connector_refresh SET consumed_at=?,successor_digest=? WHERE digest=?`, FormatTime(x.Now), x.Pair.RefreshDigest, x.Digest)
		return err
	})
	if err == nil && replayed {
		return ConnectorGrant{}, ErrConnectorInvalid
	}
	return g, err
}

// CheckConnectorAccess checks token, family and current owner in one SQL snapshot.
func (db *DB) CheckConnectorAccess(ctx context.Context, digest string, pin int64, resource, scope string, now time.Time) (ConnectorGrant, error) {
	var g ConnectorGrant
	var created, expiry string
	err := db.sql.QueryRowContext(ctx, `SELECT g.id,g.client_id,g.resource,g.scope,g.owner_id,g.generation,g.created_at,g.expires_at FROM connector_access a JOIN connector_grants g ON g.id=a.grant_id JOIN connector_owner o ON o.singleton=1 AND o.owner_id=g.owner_id AND o.generation=g.generation JOIN identities i ON i.forge='github' AND i.role='owner' AND i.user_id=o.owner_id WHERE a.digest=? AND a.revoked_at IS NULL AND a.expires_at>? AND g.revoked_at IS NULL AND g.expires_at>? AND g.owner_id=? AND g.resource=?`, digest, FormatTime(now), FormatTime(now), pin, resource).Scan(&g.ID, &g.ClientID, &g.Resource, &g.Scope, &g.OwnerID, &g.OwnerGeneration, &created, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrConnectorInvalid
	}
	if err != nil {
		return g, err
	}
	g.CreatedAt, err = ParseTime(created)
	if err != nil {
		return g, err
	}
	g.ExpiresAt, err = ParseTime(expiry)
	if err == nil && scope != "" && !ConnectorScopeHas(g.Scope, scope) {
		return ConnectorGrant{}, ErrConnectorScope
	}
	if err == nil && ConnectorScopeHas(g.Scope, connectorDiscussionScope) && (scope == "" || scope == connectorDiscussionScope) {
		g.Target, err = connectorDiscussionTarget(ctx, db.sql, g.ID)
		if scope == "" && errors.Is(err, ErrConnectorInvalid) {
			err = nil
		}
	}
	if err == nil && scope == "" {
		for _, permission := range []string{connectorRepositoryUsageScope, connectorTaskUsageScope} {
			if !ConnectorScopeHas(g.Scope, permission) {
				continue
			}
			target, targetErr := connectorUsageTarget(ctx, db.sql, g.ID, permission)
			if errors.Is(targetErr, ErrConnectorInvalid) {
				continue
			}
			if targetErr != nil {
				return ConnectorGrant{}, targetErr
			}
			g.UsageTargets = append(g.UsageTargets, target)
		}
	}
	return g, err
}

// RevokeConnectorToken compares client ownership and mutates only the stored
// token type. Unknown tokens succeed without exposing token or grant metadata.
func (db *DB) RevokeConnectorToken(ctx context.Context, clientID, digest string, now time.Time) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		var id, owner, kind string
		err := tx.QueryRowContext(ctx, `SELECT g.id,g.client_id,'access' FROM connector_access a JOIN connector_grants g ON g.id=a.grant_id WHERE a.digest=? UNION ALL SELECT g.id,g.client_id,'refresh' FROM connector_refresh r JOIN connector_grants g ON g.id=r.grant_id WHERE r.digest=? LIMIT 1`, digest, digest).Scan(&id, &owner, &kind)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if owner != clientID {
			return ErrConnectorInvalid
		}
		if kind == "access" {
			_, err = tx.ExecContext(ctx, `UPDATE connector_access SET revoked_at=COALESCE(revoked_at,?) WHERE digest=?`, FormatTime(now), digest)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,?),reason=COALESCE(reason,'client_revoked') WHERE id=?`, FormatTime(now), id)
		}
		return err
	})
}

// ListConnectorGrants returns at most the retained bounded grant set without credentials.
func (db *DB) ListConnectorGrants(ctx context.Context, pin int64, now time.Time) ([]ConnectorGrantSummary, error) {
	var out []ConnectorGrantSummary
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		if _, err := connectorOwner(ctx, tx, pin); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT g.id,g.client_id,c.name,g.resource,g.scope,g.created_at,g.expires_at,g.revoked_at FROM connector_grants g JOIN connector_clients c ON c.id=g.client_id WHERE g.expires_at>? ORDER BY g.created_at,g.id LIMIT 1000`, FormatTime(now))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g ConnectorGrantSummary
			var created, expiry string
			var revoked sql.NullString
			if err = rows.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Resource, &g.Scope, &created, &expiry, &revoked); err != nil {
				return err
			}
			g.CreatedAt, err = ParseTime(created)
			if err != nil {
				return err
			}
			g.ExpiresAt, err = ParseTime(expiry)
			if err != nil {
				return err
			}
			g.RevokedAt, err = scanTime(revoked)
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeConnectorGrant is an owner-bound, idempotent local-only operation.
func (db *DB) RevokeConnectorGrant(ctx context.Context, pin int64, id string, now time.Time) error {
	return db.revokeConnectorGrants(ctx, pin, id, false, now)
}

// RevokeAllConnectorGrants is never exposed by the public OAuth service.
func (db *DB) RevokeAllConnectorGrants(ctx context.Context, pin int64, now time.Time) error {
	return db.revokeConnectorGrants(ctx, pin, "", true, now)
}
func (db *DB) revokeConnectorGrants(ctx context.Context, pin int64, id string, all bool, now time.Time) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		o, err := connectorOwner(ctx, tx, pin)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,?),reason=COALESCE(reason,'owner_revoked') WHERE owner_id=? AND generation=? AND (? OR id=?)`, FormatTime(now), pin, o.Generation, all, id)
		return err
	})
}

// CleanupConnectorState batches expiry scans by limit. Expired families cascade
// to their credentials, bounded by the retained credential caps. Spent refresh
// lineage remains until the absolute family expiry, including revocation.
func (db *DB) CleanupConnectorState(ctx context.Context, now time.Time, limit int) error {
	if limit < 1 || limit > 1000 {
		return fmt.Errorf("store: connector cleanup limit must be 1..1000")
	}
	return db.connectorWrite(ctx, func(tx *sql.Conn) error { return connectorCleanup(ctx, tx, now, limit) })
}
func connectorCleanup(ctx context.Context, tx *sql.Conn, now time.Time, limit int) error {
	statements := []string{
		`DELETE FROM connector_browsers WHERE id IN (SELECT id FROM connector_browsers WHERE expires_at<=? LIMIT ?)`,
		`DELETE FROM connector_codes WHERE digest IN (SELECT digest FROM connector_codes WHERE expires_at<=? LIMIT ?)`,
		`DELETE FROM connector_access WHERE digest IN (SELECT digest FROM connector_access WHERE expires_at<=? LIMIT ?)`,
		`DELETE FROM connector_grants WHERE id IN (SELECT id FROM connector_grants WHERE expires_at<=? LIMIT ?)`,
		`DELETE FROM connector_clients WHERE id IN (SELECT id FROM connector_clients c WHERE expires_at<=? AND NOT EXISTS(SELECT 1 FROM connector_grants g WHERE g.client_id=c.id) AND NOT EXISTS(SELECT 1 FROM connector_browsers b WHERE b.client_id=c.id) LIMIT ?)`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt, FormatTime(now), limit); err != nil {
			return err
		}
	}
	return nil
}

// AbandonConnectorBrowser irreversibly consumes a failed browser transaction.
// A wrong nonce cannot terminate another browser's pending authorization.
func (db *DB) AbandonConnectorBrowser(ctx context.Context, id, nonceDigest string, pin int64, now time.Time) error {
	return db.connectorWrite(ctx, func(tx *sql.Conn) error {
		b, err := getConnectorBrowser(ctx, tx, id, pin, now)
		if err != nil {
			return err
		}
		if b.NonceDigest != nonceDigest {
			return ErrConnectorInvalid
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM connector_browsers WHERE id=?`, id)
		return err
	})
}

// ReconcileConnectorOwner applies the startup owner pin before any listener is
// bound. A mismatched or missing owner irrevocably invalidates old authority;
// restoring configuration cannot revive credentials or pending authorizations.
func (db *DB) ReconcileConnectorOwner(ctx context.Context, pin int64, now time.Time) (ConnectorOwner, error) {
	var owner ConnectorOwner
	invalid := false
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		var err error
		owner, err = connectorOwner(ctx, tx, pin)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrConnectorOwner) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE connector_owner SET generation=generation+1 WHERE singleton=1`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE connector_grants SET revoked_at=COALESCE(revoked_at,?),reason=COALESCE(reason,'owner_pin_mismatch')`, FormatTime(now)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM connector_browsers`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM connector_codes`); err != nil {
			return err
		}
		invalid = true
		return nil
	})
	if err != nil {
		return ConnectorOwner{}, err
	}
	if invalid {
		return ConnectorOwner{}, ErrConnectorOwner
	}
	return owner, nil
}
