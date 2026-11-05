package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"slices"
	"strings"
)

// connectorMaxCoveredRepositories bounds one grant's explicit repository list.
const connectorMaxCoveredRepositories = 1000

// insertConnectorCoverage records which repositories a new grant covers. A
// grant with a task-level or repository-level scope needs either all
// repositories or at least one registered repository; any other grant takes
// no coverage. task:code-review also needs task:discussion.
func insertConnectorCoverage(ctx context.Context, tx *sql.Conn, g ConnectorGrant) error {
	if ConnectorScopeHas(g.Scope, connectorCodeReviewScope) && !ConnectorScopeHas(g.Scope, connectorDiscussionScope) {
		return connectorRefusedCode(ErrConnectorScope, "code_review_needs_discussion", "%s also needs %s", connectorCodeReviewScope, connectorDiscussionScope)
	}
	if !ConnectorRepositoryScoped(g.Scope) {
		if g.Coverage != nil {
			return ErrConnectorInvalid
		}
		return nil
	}
	c := g.Coverage
	if (ConnectorScopeHas(g.Scope, "task:advisory") || ConnectorScopeHas(g.Scope, "task:discussion-publish") || ConnectorScopeHas(g.Scope, "repository:brainstorm")) && (c == nil || c.All) {
		return connectorRefusedCode(ErrConnectorScope, "all_repositories_not_allowed", "advisory execution, discussion publication and brainstorms require explicit repository selection")
	}
	if c == nil || !c.All && len(c.Repositories) == 0 {
		return connectorRefusedCode(ErrConnectorInvalid, "repositories_missing", "choose all repositories or at least one registered repository for %s", strings.Join(connectorGrantRepositoryScopes(g.Scope), ", "))
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
			return connectorRefusedCode(ErrConnectorInvalid, "repository_selected_twice", "repository selected twice: %q", requested.RepositoryRef)
		}
		seen[requested.RepositoryRef] = true
		r, err := resolveReadRepository(ctx, tx, requested.RepositoryRef)
		if errors.Is(err, contracts.ErrReadTargetInvalid) {
			return connectorRefusedCode(ErrConnectorInvalid, "repository_not_registered", "repository not registered: %q", requested.RepositoryRef)
		}
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
