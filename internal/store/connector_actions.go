package store

import (
	"context"
	"database/sql"
)

// Action coverage is deliberately independent of read coverage: an all-read
// grant never implies permission to change newly registered repositories.
func insertConnectorActionCoverage(ctx context.Context, tx *sql.Conn, g ConnectorGrant) error {
	c := g.ActionCoverage
	if !ConnectorScopeHas(g.Scope, "repository:actions") {
		if c != nil {
			return ErrConnectorInvalid
		}
		return nil
	}
	if c == nil || c.All || len(c.Repositories) == 0 || len(c.Repositories) > connectorMaxCoveredRepositories {
		return connectorRefused(ErrConnectorInvalid, "repository:actions requires an explicit finite repository selection")
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
		if _, err = tx.ExecContext(ctx, `INSERT INTO connector_grant_action_repositories(grant_id,repo_id,repository_ref) VALUES(?,?,?)`, g.ID, r.RepoID, r.RepositoryRef); err != nil {
			return err
		}
	}
	return nil
}

func loadConnectorActionCoverage(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, grantID string) (*ConnectorCoverage, error) {
	rows, err := q.QueryContext(ctx, `SELECT t.repo_id,t.repository_ref FROM connector_grant_action_repositories t JOIN repos r ON r.id=t.repo_id AND r.forge||':'||r.owner||'/'||r.name=t.repository_ref WHERE t.grant_id=? ORDER BY t.repository_ref`, grantID)
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
