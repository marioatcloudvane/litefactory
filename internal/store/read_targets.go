package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

const readTargetRepositoryLimit = 1000

type readTargetQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveReadTaskTarget(ctx context.Context, q readTargetQuery, taskRef string) (contracts.ReadTaskTarget, error) {
	var t contracts.ReadTaskTarget
	if taskRef == "" || len(taskRef) > 1024 {
		return t, contracts.ErrReadTargetInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT s.task_ref,s.id,s.repo_id,r.forge||':'||r.owner||'/'||r.name FROM sessions s JOIN repos r ON r.id=s.repo_id WHERE s.task_ref=?`, taskRef).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.RepositoryRef)
	if errors.Is(err, sql.ErrNoRows) {
		err = contracts.ErrReadTargetInvalid
	}
	return t, err
}

// ReadTaskTarget resolves existing local evidence without creating a task.
func (db *DB) ReadTaskTarget(ctx context.Context, taskRef string) (contracts.ReadTaskTarget, error) {
	return resolveReadTaskTarget(ctx, db.sql, taskRef)
}

func resolveReadCodeTarget(ctx context.Context, q readTargetQuery, taskRef string) (contracts.ReadCodeTarget, error) {
	var t contracts.ReadCodeTarget
	if taskRef == "" || len(taskRef) > 1024 {
		return t, contracts.ErrReadTargetInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT s.task_ref,s.id,s.repo_id,e.code_version_id,v.provider||':'||v.destination,v.clone_path,v.remote_url,COALESCE(s.branch,''),v.base_branch,e.code_remote_id FROM sessions s JOIN session_execution_snapshots e ON e.session_id=s.id AND e.repo_id=s.repo_id JOIN project_connection_versions v ON v.id=e.code_version_id AND v.repo_id=s.repo_id AND v.role='code' JOIN session_ticket_identities i ON i.session_id=s.id AND i.task_ref=s.task_ref AND i.remote_id<>'' JOIN project_ticket_bindings b ON b.id=e.binding_id AND b.repo_id=s.repo_id AND b.provider=i.provider AND b.api_origin=i.api_origin AND b.remote_id=i.remote_id AND b.container=i.container AND b.locator_key=i.locator_key JOIN project_connection_versions tv ON tv.id=e.ticket_version_id AND tv.repo_id=s.repo_id AND tv.role='ticket' AND tv.provider=i.provider AND tv.api_origin=i.api_origin AND tv.destination=i.container WHERE s.task_ref=? AND v.clone_path<>'' AND v.destination<>'' AND v.base_branch<>'' AND (v.remote_id='' OR e.code_remote_id=v.remote_id)`, taskRef).Scan(&t.TaskRef, &t.SessionID, &t.RepoID, &t.CodeVersionID, &t.RepositoryRef, &t.ClonePath, &t.RemoteURL, &t.TaskBranch, &t.BaseBranch, &t.CodeRemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, contracts.ErrReadTargetInvalid
	}
	if err != nil {
		return t, err
	}
	if t.TaskBranch == "" {
		return t, contracts.ErrReadTargetInvalid
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
		return t, contracts.ErrReadTargetInvalid
	}
	if count == 1 {
		var branch, remote string
		var version, repo int64
		err = q.QueryRowContext(ctx, `SELECT COALESCE(s.branch,''),s.repo_id,e.code_version_id,e.code_remote_id FROM sessions s JOIN session_execution_snapshots e ON e.session_id=s.id WHERE s.task_ref=?`, parent).Scan(&branch, &repo, &version, &remote)
		if errors.Is(err, sql.ErrNoRows) {
			return t, contracts.ErrReadTargetInvalid
		}
		if err != nil {
			return t, err
		}
		if branch == "" || repo != t.RepoID || (version != t.CodeVersionID && (remote == "" || remote != t.CodeRemoteID)) {
			return t, contracts.ErrReadTargetInvalid
		}
		t.BaseBranch = branch
	}
	return t, nil
}

// ReadCodeTarget resolves the exact retained task/code destination.
func (db *DB) ReadCodeTarget(ctx context.Context, taskRef string) (contracts.ReadCodeTarget, error) {
	return resolveReadCodeTarget(ctx, db.sql, taskRef)
}

// ReadRepositories returns the registered repositories the consent
// page offers, ordered by reference.
func (db *DB) ReadRepositories(ctx context.Context) ([]contracts.ReadRepository, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,forge||':'||owner||'/'||name AS ref FROM repos ORDER BY ref,id LIMIT ?`, readTargetRepositoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.ReadRepository{}
	for rows.Next() {
		var r contracts.ReadRepository
		if err = rows.Scan(&r.RepoID, &r.RepositoryRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func resolveReadRepository(ctx context.Context, q readTargetQuery, ref string) (contracts.ReadRepository, error) {
	var r contracts.ReadRepository
	if ref == "" || len(ref) > 1024 {
		return r, contracts.ErrReadTargetInvalid
	}
	err := q.QueryRowContext(ctx, `SELECT id,forge||':'||owner||'/'||name FROM repos WHERE forge||':'||owner||'/'||name=?`, ref).Scan(&r.RepoID, &r.RepositoryRef)
	if errors.Is(err, sql.ErrNoRows) {
		return r, contracts.ErrReadTargetInvalid
	}
	return r, err
}

// ReadRepository resolves a registered canonical repository identity.
func (db *DB) ReadRepository(ctx context.Context, ref string) (contracts.ReadRepository, error) {
	return resolveReadRepository(ctx, db.sql, ref)
}
