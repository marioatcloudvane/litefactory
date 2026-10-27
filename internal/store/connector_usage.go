package store

import (
	"context"
	"time"
)

const (
	connectorFactoryUsageScope    = "factory:usage"
	connectorRepositoryUsageScope = "repository:usage"
	connectorTaskUsageScope       = "task:usage"
)

// CheckConnectorUsageAccess checks the live credential and the usage scope,
// then resolves the repository or task named on this call and checks that the
// grant covers its repository. Scope is the permission token. For task usage,
// repositoryRef must be the task's repository.
func (db *DB) CheckConnectorUsageAccess(ctx context.Context, digest string, pin int64, resource, scope, repositoryRef, taskRef string, now time.Time) (ConnectorGrant, ConnectorUsageTarget, error) {
	if scope != connectorFactoryUsageScope && scope != connectorRepositoryUsageScope && scope != connectorTaskUsageScope {
		return ConnectorGrant{}, ConnectorUsageTarget{}, ErrConnectorScope
	}
	g, err := db.CheckConnectorAccess(ctx, digest, pin, resource, scope, now)
	if err != nil {
		return ConnectorGrant{}, ConnectorUsageTarget{}, err
	}
	target := ConnectorUsageTarget{Scope: scope}
	switch scope {
	case connectorRepositoryUsageScope:
		r, err := connectorCoveredRepository(ctx, db.sql, g, repositoryRef)
		if err != nil {
			return ConnectorGrant{}, ConnectorUsageTarget{}, err
		}
		target.RepoID, target.RepositoryRef = r.RepoID, r.RepositoryRef
	case connectorTaskUsageScope:
		t, err := connectorCoveredTask(ctx, db.sql, g, taskRef)
		if err != nil {
			return ConnectorGrant{}, ConnectorUsageTarget{}, err
		}
		if t.RepositoryRef != repositoryRef {
			return ConnectorGrant{}, ConnectorUsageTarget{}, connectorRefused(ErrConnectorInvalid, "task %q is not in repository %q", t.TaskRef, repositoryRef)
		}
		target.TaskRef, target.RepositoryRef, target.SessionID, target.RepoID = t.TaskRef, t.RepositoryRef, t.SessionID, t.RepoID
	}
	return g, target, nil
}
