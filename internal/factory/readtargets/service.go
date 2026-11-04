// Package readtargets resolves authoritative retained factory identities without credentials.
package readtargets

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

type reader interface {
	ReadRepositories(context.Context) ([]contracts.ReadRepository, error)
	ReadRepository(context.Context, string) (contracts.ReadRepository, error)
	ReadTaskTarget(context.Context, string) (contracts.ReadTaskTarget, error)
	ReadCodeTarget(context.Context, string) (contracts.ReadCodeTarget, error)
}

// Service exposes bounded core target reads. Its reader is private to composition.
type Service struct{ reader reader }

// New binds the core target reader; production composition supplies the shared store.
func New(r reader) *Service { return &Service{reader: r} }

// Repositories enumerates registered identities for consent.
func (s *Service) Repositories(ctx context.Context) ([]contracts.ReadRepository, error) {
	return s.reader.ReadRepositories(ctx)
}

// Repository resolves a registered canonical identity.
func (s *Service) Repository(ctx context.Context, ref string) (contracts.ReadRepository, error) {
	return s.reader.ReadRepository(ctx, ref)
}

// Task resolves locally retained task identity without creating a session.
func (s *Service) Task(ctx context.Context, ref string) (contracts.ReadTaskTarget, error) {
	return s.reader.ReadTaskTarget(ctx, ref)
}

// Code resolves the retained execution destination and authoritative branch.
func (s *Service) Code(ctx context.Context, ref string) (contracts.ReadCodeTarget, error) {
	return s.reader.ReadCodeTarget(ctx, ref)
}

// RepositoryUsage resolves the repository identity for a bounded usage read.
func (s *Service) RepositoryUsage(ctx context.Context, ref string) (contracts.ReadUsageTarget, error) {
	r, err := s.Repository(ctx, ref)
	if err != nil {
		return contracts.ReadUsageTarget{}, err
	}
	return contracts.ReadUsageTarget{RepoID: r.RepoID, RepositoryRef: r.RepositoryRef}, nil
}

// TaskUsage resolves usage identity from the retained task, never current configuration.
func (s *Service) TaskUsage(ctx context.Context, ref string) (contracts.ReadUsageTarget, error) {
	t, err := s.Task(ctx, ref)
	if err != nil {
		return contracts.ReadUsageTarget{}, err
	}
	return contracts.ReadUsageTarget(t), nil
}
