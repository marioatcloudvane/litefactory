package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	connectorFactoryUsageScope    = "factory:usage"
	connectorRepositoryUsageScope = "repository:usage"
	connectorTaskUsageScope       = "task:usage"
)

func resolveConnectorUsageTarget(ctx context.Context, q connectorTargetQuery, scope, selector string) (ConnectorUsageTarget, error) {
	t := ConnectorUsageTarget{Scope: scope}
	if selector == "" || len(selector) > 1024 {
		return t, ErrConnectorInvalid
	}
	switch scope {
	case connectorRepositoryUsageScope:
		err := q.QueryRowContext(ctx, `SELECT id,forge||':'||owner||'/'||name FROM repos WHERE forge||':'||owner||'/'||name=?`, selector).Scan(&t.RepoID, &t.RepositoryRef)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConnectorInvalid
		}
		return t, err
	case connectorTaskUsageScope:
		d, err := resolveConnectorDiscussionTarget(ctx, q, selector)
		t.TaskRef, t.RepositoryRef, t.SessionID, t.RepoID = d.TaskRef, d.RepositoryRef, d.SessionID, d.RepoID
		return t, err
	default:
		return t, ErrConnectorInvalid
	}
}

// ResolveConnectorUsageTarget resolves a registered repository or existing task without mutation.
func (db *DB) ResolveConnectorUsageTarget(ctx context.Context, scope, selector string) (ConnectorUsageTarget, error) {
	return resolveConnectorUsageTarget(ctx, db.sql, scope, selector)
}

func connectorUsageTarget(ctx context.Context, q connectorTargetQuery, id, scope string) (ConnectorUsageTarget, error) {
	t := ConnectorUsageTarget{Scope: scope}
	var session sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT t.repository_ref,t.repo_id,t.task_ref,t.session_id FROM connector_usage_targets t JOIN repos r ON r.id=t.repo_id AND r.forge||':'||r.owner||'/'||r.name=t.repository_ref LEFT JOIN sessions s ON s.id=t.session_id AND s.repo_id=t.repo_id AND s.task_ref=t.task_ref WHERE t.grant_id=? AND t.scope=? AND (t.scope='repository:usage' OR s.id IS NOT NULL)`, id, scope).Scan(&t.RepositoryRef, &t.RepoID, &t.TaskRef, &session)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConnectorInvalid
	}
	t.SessionID = session.Int64
	return t, err
}

func loadConnectorUsageTargets(ctx context.Context, q connectorTargetQuery, g *ConnectorGrant) error {
	for _, scope := range []string{connectorRepositoryUsageScope, connectorTaskUsageScope} {
		if ConnectorScopeHas(g.Scope, scope) {
			t, err := connectorUsageTarget(ctx, q, g.ID, scope)
			if err != nil {
				return err
			}
			g.UsageTargets = append(g.UsageTargets, t)
		}
	}
	return nil
}

func insertConnectorUsageTargets(ctx context.Context, tx *sql.Conn, g ConnectorGrant) error {
	seen := map[string]bool{}
	for _, requested := range g.UsageTargets {
		if seen[requested.Scope] || !ConnectorScopeHas(g.Scope, requested.Scope) {
			return ErrConnectorInvalid
		}
		seen[requested.Scope] = true
		selector := requested.RepositoryRef
		if requested.Scope == connectorTaskUsageScope {
			selector = requested.TaskRef
		}
		target, err := resolveConnectorUsageTarget(ctx, tx, requested.Scope, selector)
		if err != nil {
			return err
		}
		if (requested.RepoID != 0 && requested.RepoID != target.RepoID) || (requested.SessionID != 0 && requested.SessionID != target.SessionID) || (requested.RepositoryRef != "" && requested.RepositoryRef != target.RepositoryRef) {
			return ErrConnectorInvalid
		}
		var session any
		if target.SessionID != 0 {
			session = target.SessionID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO connector_usage_targets(grant_id,scope,repository_ref,repo_id,task_ref,session_id) VALUES(?,?,?,?,?,?)`, g.ID, target.Scope, target.RepositoryRef, target.RepoID, target.TaskRef, session); err != nil {
			return err
		}
	}
	for _, scope := range []string{connectorRepositoryUsageScope, connectorTaskUsageScope} {
		if ConnectorScopeHas(g.Scope, scope) && !seen[scope] {
			return ErrConnectorInvalid
		}
	}
	return nil
}

// CheckConnectorUsageAccess checks live credentials and the independent immutable usage target.
// Scope is the permission token; selectors must exactly match its canonical target.
func (db *DB) CheckConnectorUsageAccess(ctx context.Context, digest string, pin int64, resource, scope, repositoryRef, taskRef string, now time.Time) (ConnectorGrant, error) {
	if scope != connectorFactoryUsageScope && scope != connectorRepositoryUsageScope && scope != connectorTaskUsageScope {
		return ConnectorGrant{}, ErrConnectorScope
	}
	g, err := db.CheckConnectorAccess(ctx, digest, pin, resource, scope, now)
	if err != nil {
		return ConnectorGrant{}, err
	}
	if scope == connectorFactoryUsageScope {
		return g, nil
	}
	t, err := connectorUsageTarget(ctx, db.sql, g.ID, scope)
	if err != nil {
		return ConnectorGrant{}, err
	}
	if t.RepositoryRef != repositoryRef || (scope == connectorTaskUsageScope && t.TaskRef != taskRef) {
		return ConnectorGrant{}, ErrConnectorScope
	}
	g.UsageTargets = []ConnectorUsageTarget{t}
	return g, nil
}
