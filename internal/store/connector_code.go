package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const connectorCodeReviewScope = "task:code-review"

// ConnectorCodeTarget describes only the retained execution destination. It
// contains no current connection settings or client-selected filesystem path.
type ConnectorCodeTarget struct {
	TaskRef, RepositoryRef, ClonePath, RemoteURL, TaskBranch, BaseBranch, CodeRemoteID string
	SessionID, RepoID, CodeVersionID                                                   int64
}

func resolveConnectorCodeTarget(ctx context.Context, q connectorTargetQuery, taskRef string) (ConnectorCodeTarget, error) {
	var t ConnectorCodeTarget
	if taskRef == "" || len(taskRef) > 1024 {
		return t, ErrConnectorInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT s.task_ref,s.id,s.repo_id,e.code_version_id,v.provider||':'||v.destination,v.clone_path,v.remote_url,COALESCE(s.branch,''),v.base_branch,e.code_remote_id FROM sessions s JOIN session_execution_snapshots e ON e.session_id=s.id AND e.repo_id=s.repo_id JOIN project_connection_versions v ON v.id=e.code_version_id AND v.repo_id=s.repo_id AND v.role='code' JOIN session_ticket_identities i ON i.session_id=s.id AND i.task_ref=s.task_ref AND i.remote_id<>'' JOIN project_ticket_bindings b ON b.id=e.binding_id AND b.repo_id=s.repo_id AND b.provider=i.provider AND b.api_origin=i.api_origin AND b.remote_id=i.remote_id AND b.container=i.container AND b.locator_key=i.locator_key JOIN project_connection_versions tv ON tv.id=e.ticket_version_id AND tv.repo_id=s.repo_id AND tv.role='ticket' AND tv.provider=i.provider AND tv.api_origin=i.api_origin AND tv.destination=i.container WHERE s.task_ref=? AND v.clone_path<>'' AND v.destination<>'' AND v.base_branch<>'' AND (v.remote_id='' OR e.code_remote_id=v.remote_id)`, taskRef).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.CodeVersionID, &t.RepositoryRef, &t.ClonePath, &t.RemoteURL, &t.TaskBranch, &t.BaseBranch, &t.CodeRemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrConnectorInvalid
	}
	if err != nil {
		return t, err
	}
	if t.TaskBranch == "" {
		return t, ErrConnectorInvalid
	}
	// Both mapping sources must agree. A missing mapped parent is an integrity
	// gap, rather than permission to substitute the repository's default base.
	var count, invalid int
	var parent string
	err = q.QueryRowContext(ctx, `WITH candidates(parent,exact) AS (
 SELECT d.parent,0 FROM plan_story_drafts d JOIN sessions p ON p.task_ref=d.parent JOIN plan_execution_paths x ON x.session_id=p.id AND x.path='graph' JOIN sessions s ON s.repo_id=p.repo_id AND s.issue_number=d.issue_number WHERE s.id=? AND d.retired=0
 UNION SELECT parent,1 FROM implement_stories WHERE worker_session=?
 ), identities AS (
 SELECT c.parent,c.exact,
 CASE WHEN p.id IS NOT NULL AND pi.task_ref=p.task_ref AND pi.remote_id<>'' AND b.id IS NOT NULL AND tv.id IS NOT NULL THEN 1 ELSE 0 END AS verified,
 CASE WHEN pi.provider=ci.provider AND pi.api_origin=ci.api_origin AND pi.container=ci.container THEN 1 ELSE 0 END AS same_source
 FROM candidates c
 JOIN session_ticket_identities ci ON ci.session_id=?
 LEFT JOIN sessions p ON p.task_ref=c.parent
 LEFT JOIN session_ticket_identities pi ON pi.session_id=p.id
 LEFT JOIN session_execution_snapshots pe ON pe.session_id=p.id AND pe.repo_id=p.repo_id
 LEFT JOIN project_ticket_bindings b ON b.id=pe.binding_id AND b.repo_id=p.repo_id AND b.provider=pi.provider AND b.api_origin=pi.api_origin AND b.remote_id=pi.remote_id AND b.container=pi.container AND b.locator_key=pi.locator_key
 LEFT JOIN project_connection_versions tv ON tv.id=pe.ticket_version_id AND tv.repo_id=p.repo_id AND tv.role='ticket' AND tv.provider=pi.provider AND tv.api_origin=pi.api_origin AND tv.destination=pi.container
 ) SELECT count(DISTINCT parent),COALESCE(min(parent),''),COALESCE(max(CASE WHEN verified=0 OR (exact=1 AND same_source=0) THEN 1 ELSE 0 END),0)
 FROM identities WHERE verified=0 OR exact=1 OR same_source=1`, t.SessionID, t.SessionID, t.SessionID).Scan(&count, &parent, &invalid)
	if err != nil {
		return t, err
	}
	if invalid != 0 || count > 1 {
		return t, ErrConnectorInvalid
	}
	if count == 1 {
		var branch, remote string
		var version, repo int64
		err = q.QueryRowContext(ctx, `SELECT COALESCE(s.branch,''),s.repo_id,e.code_version_id,e.code_remote_id FROM sessions s JOIN session_execution_snapshots e ON e.session_id=s.id WHERE s.task_ref=?`, parent).Scan(&branch, &repo, &version, &remote)
		if errors.Is(err, sql.ErrNoRows) {
			return t, ErrConnectorInvalid
		}
		if err != nil {
			return t, err
		}
		if branch == "" || repo != t.RepoID || (version != t.CodeVersionID && (remote == "" || remote != t.CodeRemoteID)) {
			return t, ErrConnectorInvalid
		}
		t.BaseBranch = branch
	}
	return t, nil
}

// ResolveConnectorCodeTarget resolves the exact retained task/code destination.
func (db *DB) ResolveConnectorCodeTarget(ctx context.Context, taskRef string) (ConnectorCodeTarget, error) {
	return resolveConnectorCodeTarget(ctx, db.sql, taskRef)
}

func insertConnectorCodeTarget(ctx context.Context, tx *sql.Conn, g ConnectorGrant) error {
	if !ConnectorScopeHas(g.Scope, connectorCodeReviewScope) {
		if g.CodeTarget != nil {
			return ErrConnectorInvalid
		}
		return nil
	}
	if !ConnectorScopeHas(g.Scope, connectorDiscussionScope) || g.Target == nil || g.CodeTarget == nil || g.CodeTarget.TaskRef != g.Target.TaskRef {
		return ErrConnectorScope
	}
	target, err := resolveConnectorCodeTarget(ctx, tx, g.Target.TaskRef)
	if err != nil {
		return err
	}
	if target != *g.CodeTarget {
		return ErrConnectorInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO connector_code_targets(grant_id,task_ref,session_id,repo_id,code_version_id,repository_ref,clone_path,remote_url,task_branch,base_branch,code_remote_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, g.ID, target.TaskRef, target.SessionID, target.RepoID, target.CodeVersionID, target.RepositoryRef, target.ClonePath, target.RemoteURL, target.TaskBranch, target.BaseBranch, target.CodeRemoteID)
	return err
}

func connectorCodeTarget(ctx context.Context, q connectorTargetQuery, id string) (*ConnectorCodeTarget, error) {
	var t ConnectorCodeTarget
	err := q.QueryRowContext(ctx, `SELECT task_ref,session_id,repo_id,code_version_id,repository_ref,clone_path,remote_url,task_branch,base_branch,code_remote_id FROM connector_code_targets WHERE grant_id=?`, id).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.CodeVersionID, &t.RepositoryRef, &t.ClonePath, &t.RemoteURL, &t.TaskBranch, &t.BaseBranch, &t.CodeRemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConnectorInvalid
	}
	if err != nil {
		return nil, err
	}
	current, err := resolveConnectorCodeTarget(ctx, q, t.TaskRef)
	if err != nil {
		return nil, err
	}
	if current != t {
		return nil, ErrConnectorInvalid
	}
	return &t, nil
}

// CheckConnectorCodeAccess rechecks the live token, both permissions and the
// immutable code target. Call before acquisition and again before release.
func (db *DB) CheckConnectorCodeAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (ConnectorGrant, error) {
	g, err := db.CheckConnectorDiscussionAccess(ctx, digest, pin, resource, taskRef, now)
	if err != nil {
		return ConnectorGrant{}, err
	}
	if !ConnectorScopeHas(g.Scope, connectorCodeReviewScope) {
		return ConnectorGrant{}, ErrConnectorScope
	}
	g.CodeTarget, err = connectorCodeTarget(ctx, db.sql, g.ID)
	return g, err
}
