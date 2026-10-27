package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrConnectorGrantNotFound identifies an exact grant ID absent from retained state.
var ErrConnectorGrantNotFound = errors.New("store: connector grant not found")

// ConnectorLocalGrant contains only the allowlisted metadata needed for local inspection.
// Unlike remote authorization, local administration does not need an owner pin.
type ConnectorLocalGrant struct {
	ConnectorGrantSummary
	Status, RevocationReason                 string
	AccessTokenCount, UsableAccessTokenCount int
	EarliestAccessExpiry, LatestAccessExpiry time.Time
}

// ListLocalConnectorGrants reads all retained generations and token expiry aggregates
// from one fresh snapshot. It neither cleans up records nor changes owner state.
func (db *DB) ListLocalConnectorGrants(ctx context.Context, now time.Time) ([]ConnectorLocalGrant, error) {
	rows, err := db.sql.QueryContext(ctx, `WITH access_summary AS (
 SELECT grant_id,COUNT(*) AS issued_count,
 SUM(CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END) AS usable_count,
 MIN(expires_at) AS earliest_expiry,MAX(expires_at) AS latest_expiry
 FROM connector_access WHERE expires_at>? GROUP BY grant_id
 )
 SELECT g.id,g.client_id,c.name,g.resource,g.scope,COALESCE((SELECT task_ref FROM connector_grant_targets t WHERE t.grant_id=g.id),''),g.created_at,g.expires_at,g.revoked_at,COALESCE(g.reason,''),
 CASE WHEN g.revoked_at IS NOT NULL THEN CASE WHEN g.reason='owner_changed' THEN 'invalidated' ELSE 'revoked' END
 WHEN g.expires_at<=? THEN 'expired'
 WHEN NOT EXISTS(SELECT 1 FROM connector_owner o JOIN identities i ON i.forge='github' AND i.role='owner' AND i.user_id=o.owner_id WHERE o.singleton=1 AND o.owner_id=g.owner_id AND o.generation=g.generation) THEN 'invalidated'
 ELSE 'active' END,
 COALESCE(a.issued_count,0),COALESCE(a.usable_count,0),a.earliest_expiry,a.latest_expiry
 FROM connector_grants g JOIN connector_clients c ON c.id=g.client_id
 LEFT JOIN access_summary a ON a.grant_id=g.id ORDER BY g.created_at,g.id`, FormatTime(now), FormatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ConnectorLocalGrant, 0)
	for rows.Next() {
		var g ConnectorLocalGrant
		var created, expiry string
		var revoked, earliest, latest sql.NullString
		if err = rows.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Resource, &g.Scope, &g.TaskRef, &created, &expiry, &revoked, &g.RevocationReason, &g.Status, &g.AccessTokenCount, &g.UsableAccessTokenCount, &earliest, &latest); err != nil {
			return nil, err
		}
		for _, v := range []struct {
			raw sql.NullString
			dst *time.Time
		}{{sql.NullString{String: created, Valid: true}, &g.CreatedAt}, {sql.NullString{String: expiry, Valid: true}, &g.ExpiresAt}, {revoked, &g.RevokedAt}, {earliest, &g.EarliestAccessExpiry}, {latest, &g.LatestAccessExpiry}} {
			*v.dst, err = scanTime(v.raw)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeLocalConnectorGrants uses the foundation write serialization shared with
// consent and refresh. Empty id is valid only for the explicit all operation.
// Counts are returned only after a confirmed commit; retained rows stay intact.
func (db *DB) RevokeLocalConnectorGrants(ctx context.Context, id string, all bool, now time.Time) (int64, error) {
	var changed int64
	err := db.connectorWrite(ctx, func(tx *sql.Conn) error {
		if !all {
			var retained string
			err := tx.QueryRowContext(ctx, `SELECT id FROM connector_grants WHERE id=?`, id).Scan(&retained)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConnectorGrantNotFound
			}
			if err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE connector_grants SET revoked_at=?,reason=COALESCE(reason,'owner_revoked') WHERE revoked_at IS NULL AND (? OR id=?)`, FormatTime(now), all, id)
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// CheckLocalConnectorGrantControl probes the durable tables needed by shipped
// administration without aggregating all grants on each protected request.
func (db *DB) CheckLocalConnectorGrantControl(ctx context.Context) error {
	rows, err := db.sql.QueryContext(ctx, `SELECT g.id,g.revoked_at,g.reason,c.name,a.revoked_at,r.grant_id,o.generation FROM connector_grants g LEFT JOIN connector_clients c ON c.id=g.client_id LEFT JOIN connector_access a ON a.grant_id=g.id LEFT JOIN connector_refresh r ON r.grant_id=g.id LEFT JOIN connector_owner o ON o.singleton=1 WHERE 0`)
	if err != nil {
		return err
	}
	return rows.Close()
}
