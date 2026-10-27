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

func connectorDiscussionTarget(ctx context.Context, q connectorTargetQuery, grantID string) (*ConnectorGrantTarget, error) {
	var t ConnectorGrantTarget
	err := q.QueryRowContext(ctx, `SELECT t.task_ref,t.session_id,t.repo_id,t.repository_ref FROM connector_grant_targets t JOIN sessions s ON s.id=t.session_id AND s.task_ref=t.task_ref AND s.repo_id=t.repo_id JOIN repos r ON r.id=t.repo_id AND r.forge||':'||r.owner||'/'||r.name=t.repository_ref WHERE t.grant_id=?`, grantID).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.RepositoryRef)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConnectorInvalid
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CheckConnectorDiscussionAccess checks the live credential and immutable exact-task binding.
func (db *DB) CheckConnectorDiscussionAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (ConnectorGrant, error) {
	g, err := db.CheckConnectorAccess(ctx, digest, pin, resource, connectorDiscussionScope, now)
	if err != nil {
		return ConnectorGrant{}, err
	}
	if g.Target == nil || g.Target.TaskRef != taskRef {
		return ConnectorGrant{}, ErrConnectorScope
	}
	return g, nil
}
