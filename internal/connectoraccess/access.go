// Package connectoraccess owns protected-read authorization over bounded core targets.
package connectoraccess

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"time"
)

type targets interface {
	Repositories(context.Context) ([]contracts.ReadRepository, error)
	Task(context.Context, string) (contracts.ReadTaskTarget, error)
	Code(context.Context, string) (contracts.ReadCodeTarget, error)
	RepositoryUsage(context.Context, string) (contracts.ReadUsageTarget, error)
	TaskUsage(context.Context, string) (contracts.ReadUsageTarget, error)
}

// Service checks credentials and coverage before returning a core-owned target.
// Production binding constructs both dependencies from the same database.
type Service struct {
	storage connectorstate.Storage
	targets targets
}

// New composes authorization with its bounded target reader.
func New(storage connectorstate.Storage, reader targets) *Service {
	return &Service{storage: storage, targets: reader}
}

// ListConnectorRepositories enumerates the core's bounded consent choices.
func (s *Service) ListConnectorRepositories(ctx context.Context) ([]connectorstate.ConnectorRepository, error) {
	return s.targets.Repositories(ctx)
}

func (s *Service) coveredTask(ctx context.Context, g connectorstate.ConnectorGrant, ref string) (contracts.ReadTaskTarget, error) {
	t, err := s.targets.Task(ctx, ref)
	if errors.Is(err, contracts.ErrReadTargetInvalid) {
		return contracts.ReadTaskTarget{}, connectorstate.Refused(connectorstate.ErrConnectorInvalid, "task not found: %q", ref)
	}
	if err != nil {
		return contracts.ReadTaskTarget{}, err
	}
	if !g.Coverage.Covers(t.RepoID, t.RepositoryRef) {
		return contracts.ReadTaskTarget{}, connectorstate.Refused(connectorstate.ErrConnectorScope, "repository not covered by this grant: %q (task %q)", t.RepositoryRef, t.TaskRef)
	}
	return t, nil
}

// CheckConnectorDiscussionAccess resolves and authorizes the exact task on each call.
func (s *Service) CheckConnectorDiscussionAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorGrantTarget, error) {
	g, err := s.storage.CheckConnectorAccess(ctx, digest, pin, resource, "task:discussion", now)
	if err != nil {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorGrantTarget{}, err
	}
	t, err := s.coveredTask(ctx, g, taskRef)
	if err != nil {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorGrantTarget{}, err
	}
	return g, t, nil
}

// CheckConnectorCodeAccess preserves discussion coverage and the independent code scope.
func (s *Service) CheckConnectorCodeAccess(ctx context.Context, digest string, pin int64, resource, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorCodeTarget, error) {
	g, task, err := s.CheckConnectorDiscussionAccess(ctx, digest, pin, resource, taskRef, now)
	if err != nil {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorCodeTarget{}, err
	}
	if !connectorstate.ConnectorScopeHas(g.Scope, "task:code-review") {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorCodeTarget{}, connectorstate.Refused(connectorstate.ErrConnectorScope, "this grant does not include %s", "task:code-review")
	}
	t, err := s.targets.Code(ctx, task.TaskRef)
	if errors.Is(err, contracts.ErrReadTargetInvalid) {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorCodeTarget{}, connectorstate.Refused(connectorstate.ErrConnectorInvalid, "no retained code for task %q: it has no recorded code destination and task branch", task.TaskRef)
	}
	if err != nil {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorCodeTarget{}, err
	}
	if t.SessionID != task.SessionID || t.RepoID != task.RepoID {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorCodeTarget{}, connectorstate.ErrConnectorInvalid
	}
	return g, t, nil
}

// CheckConnectorUsageAccess checks the independent usage permission and target coverage.
func (s *Service) CheckConnectorUsageAccess(ctx context.Context, digest string, pin int64, resource, scope, repositoryRef, taskRef string, now time.Time) (connectorstate.ConnectorGrant, connectorstate.ConnectorUsageTarget, error) {
	if scope != "factory:usage" && scope != "repository:usage" && scope != "task:usage" {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, connectorstate.ErrConnectorScope
	}
	g, err := s.storage.CheckConnectorAccess(ctx, digest, pin, resource, scope, now)
	if err != nil {
		return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, err
	}
	target := connectorstate.ConnectorUsageTarget{Scope: scope}
	switch scope {
	case "repository:usage":
		r, e := s.targets.RepositoryUsage(ctx, repositoryRef)
		if errors.Is(e, contracts.ErrReadTargetInvalid) {
			e = connectorstate.RefusedCode(connectorstate.ErrConnectorInvalid, "repository_not_registered", "repository not registered: %q", repositoryRef)
		}
		if e != nil {
			return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, e
		}
		if !g.Coverage.Covers(r.RepoID, r.RepositoryRef) {
			return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, connectorstate.Refused(connectorstate.ErrConnectorScope, "repository not covered by this grant: %q", r.RepositoryRef)
		}
		target.RepoID, target.RepositoryRef = r.RepoID, r.RepositoryRef
	case "task:usage":
		t, e := s.targets.TaskUsage(ctx, taskRef)
		if errors.Is(e, contracts.ErrReadTargetInvalid) {
			e = connectorstate.Refused(connectorstate.ErrConnectorInvalid, "task not found: %q", taskRef)
		}
		if e != nil {
			return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, e
		}
		if !g.Coverage.Covers(t.RepoID, t.RepositoryRef) {
			return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, connectorstate.Refused(connectorstate.ErrConnectorScope, "repository not covered by this grant: %q (task %q)", t.RepositoryRef, t.TaskRef)
		}
		if t.RepositoryRef != repositoryRef {
			return connectorstate.ConnectorGrant{}, connectorstate.ConnectorUsageTarget{}, connectorstate.Refused(connectorstate.ErrConnectorInvalid, "task %q is not in repository %q", t.TaskRef, repositoryRef)
		}
		target.TaskRef, target.RepositoryRef, target.SessionID, target.RepoID = t.TaskRef, t.RepositoryRef, t.SessionID, t.RepoID
	}
	return g, target, nil
}
