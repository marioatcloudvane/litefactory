package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// connectorMaxCoveredRepositories bounds one grant's explicit repository list.
const connectorMaxCoveredRepositories = 1000

// ListConnectorRepositories returns the registered repositories the consent
// page offers, ordered by reference.
func (db *DB) ListConnectorRepositories(ctx context.Context) ([]ConnectorRepository, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,forge||':'||owner||'/'||name AS ref FROM repos ORDER BY ref,id LIMIT ?`, connectorMaxCoveredRepositories)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectorRepository{}
	for rows.Next() {
		var r ConnectorRepository
		if err = rows.Scan(&r.RepoID, &r.RepositoryRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func resolveConnectorRepository(ctx context.Context, q connectorTargetQuery, ref string) (ConnectorRepository, error) {
	var r ConnectorRepository
	if ref == "" || len(ref) > 1024 {
		return r, connectorRefused(ErrConnectorInvalid, "repository not registered: %q", ref)
	}
	err := q.QueryRowContext(ctx, `SELECT id,forge||':'||owner||'/'||name FROM repos WHERE forge||':'||owner||'/'||name=?`, ref).Scan(&r.RepoID, &r.RepositoryRef)
	if errors.Is(err, sql.ErrNoRows) {
		return r, connectorRefused(ErrConnectorInvalid, "repository not registered: %q", ref)
	}
	return r, err
}

// insertConnectorCoverage records which repositories a new grant covers. A
// grant with a task-level or repository-level scope needs either all
// repositories or at least one registered repository; any other grant takes
// no coverage. task:code-review also needs task:discussion.
func insertConnectorCoverage(ctx context.Context, tx *sql.Conn, g ConnectorGrant) error {
	if ConnectorScopeHas(g.Scope, connectorCodeReviewScope) && !ConnectorScopeHas(g.Scope, connectorDiscussionScope) {
		return connectorRefused(ErrConnectorScope, "%s also needs %s", connectorCodeReviewScope, connectorDiscussionScope)
	}
	if !ConnectorRepositoryScoped(g.Scope) {
		if g.Coverage != nil {
			return ErrConnectorInvalid
		}
		return nil
	}
	c := g.Coverage
	if c == nil || !c.All && len(c.Repositories) == 0 {
		return connectorRefused(ErrConnectorInvalid, "choose all repositories or at least one registered repository for %s", strings.Join(connectorGrantRepositoryScopes(g.Scope), ", "))
	}
	if c.All {
		if len(c.Repositories) != 0 {
			return ErrConnectorInvalid
		}
		return nil
	}
	if len(c.Repositories) > connectorMaxCoveredRepositories {
		return ErrConnectorInvalid
	}
	seen := map[string]bool{}
	for _, requested := range c.Repositories {
		if seen[requested.RepositoryRef] {
			return connectorRefused(ErrConnectorInvalid, "repository selected twice: %q", requested.RepositoryRef)
		}
		seen[requested.RepositoryRef] = true
		r, err := resolveConnectorRepository(ctx, tx, requested.RepositoryRef)
		if err != nil {
			return err
		}
		if requested.RepoID != 0 && requested.RepoID != r.RepoID {
			return ErrConnectorInvalid
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO connector_grant_repositories(grant_id,repo_id,repository_ref) VALUES(?,?,?)`, g.ID, r.RepoID, r.RepositoryRef); err != nil {
			return err
		}
	}
	return nil
}

func connectorGrantRepositoryScopes(scope string) []string {
	var out []string
	for _, s := range connectorRepositoryScopes {
		if ConnectorScopeHas(scope, s) {
			out = append(out, s)
		}
	}
	return out
}

// loadConnectorCoverage reads a grant's coverage. A listed repository that was
// deleted or renamed since consent is no longer covered.
func loadConnectorCoverage(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, grantID string, all bool) (*ConnectorCoverage, error) {
	if all {
		return &ConnectorCoverage{All: true}, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT t.repo_id,t.repository_ref FROM connector_grant_repositories t JOIN repos r ON r.id=t.repo_id AND r.forge||':'||r.owner||'/'||r.name=t.repository_ref WHERE t.grant_id=? ORDER BY t.repository_ref`, grantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c := &ConnectorCoverage{Repositories: []ConnectorRepository{}}
	for rows.Next() {
		var r ConnectorRepository
		if err = rows.Scan(&r.RepoID, &r.RepositoryRef); err != nil {
			return nil, err
		}
		c.Repositories = append(c.Repositories, r)
	}
	return c, rows.Err()
}

// connectorCoveredTask resolves an existing task named on a call and checks
// that the grant covers its repository.
func connectorCoveredTask(ctx context.Context, q connectorTargetQuery, g ConnectorGrant, taskRef string) (ConnectorGrantTarget, error) {
	t, err := resolveConnectorDiscussionTarget(ctx, q, taskRef)
	if errors.Is(err, ErrConnectorInvalid) {
		return ConnectorGrantTarget{}, connectorRefused(ErrConnectorInvalid, "task not found: %q", taskRef)
	}
	if err != nil {
		return ConnectorGrantTarget{}, err
	}
	if !g.Coverage.Covers(t.RepoID, t.RepositoryRef) {
		return ConnectorGrantTarget{}, connectorRefused(ErrConnectorScope, "repository not covered by this grant: %q (task %q)", t.RepositoryRef, t.TaskRef)
	}
	return t, nil
}

// connectorCoveredRepository resolves a registered repository named on a call
// and checks that the grant covers it.
func connectorCoveredRepository(ctx context.Context, q connectorTargetQuery, g ConnectorGrant, ref string) (ConnectorRepository, error) {
	r, err := resolveConnectorRepository(ctx, q, ref)
	if err != nil {
		return r, err
	}
	if !g.Coverage.Covers(r.RepoID, r.RepositoryRef) {
		return ConnectorRepository{}, connectorRefused(ErrConnectorScope, "repository not covered by this grant: %q", r.RepositoryRef)
	}
	return r, nil
}

// splitConnectorRepositories turns the newline-joined stored list into a
// sorted slice.
func splitConnectorRepositories(joined string) []string {
	if joined == "" {
		return nil
	}
	out := strings.Split(joined, "\n")
	slices.Sort(out)
	return out
}
