package localprojects

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/store"
)

func grantView(g store.AccessGrant) contracts.ProjectAccessGrantView {
	return contracts.ProjectAccessGrantView(g)
}

func grantViews(rows []store.AccessGrant) []contracts.ProjectAccessGrantView {
	out := make([]contracts.ProjectAccessGrantView, 0, len(rows))
	for _, g := range rows {
		out = append(out, grantView(g))
	}
	return out
}

// GrantAccess records plain text for one person, looked up like an approver on
// the project's ticket connection. A person with an open grant gets the next
// revision. Nothing is stored when any check fails.
func (s *Service) GrantAccess(ctx context.Context, login, text string) (contracts.ProjectAccessGrantView, error) {
	if err := store.ValidAccessGrantText(text); err != nil {
		return contracts.ProjectAccessGrantView{}, usage("%v", err)
	}
	service, err := s.approver(ctx)
	if err != nil {
		return contracts.ProjectAccessGrantView{}, err
	}
	person, err := service.Resolve(ctx, s.repo.ID, login, 0)
	if err != nil {
		return contracts.ProjectAccessGrantView{}, unavailable(err)
	}
	g, err := s.db.GrantAccess(ctx, store.AccessGrant{RepoID: s.repo.ID, Actor: person.Actor, Handle: person.Handle, Text: text})
	if errors.Is(err, store.ErrAccessGrantApprover) {
		return contracts.ProjectAccessGrantView{}, usage("%v", err)
	}
	return grantView(g), unavailable(err)
}

// RevokeAccess closes the open grant with the given id.
func (s *Service) RevokeAccess(ctx context.Context, id int64) (contracts.ProjectAccessGrantView, error) {
	g, err := s.db.RevokeAccessGrant(ctx, s.repo.ID, id)
	if errors.Is(err, store.ErrAccessGrantMissing) {
		return contracts.ProjectAccessGrantView{}, usage("%v", err)
	}
	return grantView(g), unavailable(err)
}

// ListAccess returns the open grants of the project.
func (s *Service) ListAccess(ctx context.Context) ([]contracts.ProjectAccessGrantView, error) {
	rows, err := s.db.ListAccessGrants(ctx, s.repo.ID)
	return grantViews(rows), unavailable(err)
}

// AccessHistory returns every grant revision of the project, oldest first,
// with the revoke or replace that closed each.
func (s *Service) AccessHistory(ctx context.Context) ([]contracts.ProjectAccessGrantView, error) {
	rows, err := s.db.AccessGrantHistory(ctx, s.repo.ID)
	return grantViews(rows), unavailable(err)
}
