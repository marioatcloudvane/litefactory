package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const connectorDiscussionScope = "task:discussion"

type connectorTargetQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveConnectorDiscussionTarget(ctx context.Context, q connectorTargetQuery, taskRef string) (ConnectorGrantTarget, error) {
	var t ConnectorGrantTarget
	if taskRef == "" || len(taskRef) > 1024 {
		return t, ErrConnectorInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT s.task_ref,s.id,s.repo_id,r.forge||':'||r.owner||'/'||r.name FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE s.task_ref=?`, taskRef).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.RepositoryRef)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConnectorInvalid
	}
	return t, err
}

// ResolveConnectorDiscussionTarget resolves existing local evidence without creating a task.
func (db *DB) ResolveConnectorDiscussionTarget(ctx context.Context, taskRef string) (ConnectorGrantTarget, error) {
	return resolveConnectorDiscussionTarget(ctx, db.sql, taskRef)
}

// CheckConnectorDiscussionAccess checks the live credential and task:discussion,
// then resolves the task named on this call and checks that the grant covers
// its repository. Call it again before releasing evidence.
func (db *DB) CheckConnectorDiscussionAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (ConnectorGrant, ConnectorGrantTarget, error) {
	g, err := db.CheckConnectorAccess(ctx, digest, pin, resource, connectorDiscussionScope, now)
	if err != nil {
		return ConnectorGrant{}, ConnectorGrantTarget{}, err
	}
	t, err := connectorCoveredTask(ctx, db.sql, g, taskRef)
	if err != nil {
		return ConnectorGrant{}, ConnectorGrantTarget{}, err
	}
	return g, t, nil
}
