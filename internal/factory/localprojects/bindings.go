package localprojects

import (
	"context"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ListBindings presents the project’s retained qualified ticket bindings.
func (s *Service) ListBindings(ctx context.Context, present func(contracts.ProjectBindingView) error) error {
	rows, err := s.db.ListTicketBindings(ctx, s.repo.ID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := present(contracts.ProjectBindingView(row)); err != nil {
			return err
		}
	}
	return nil
}

// DisableBinding disables only a binding owned by the selected project.
func (s *Service) DisableBinding(ctx context.Context, request contracts.ProjectBindingRequest) (contracts.ProjectBindingView, error) {
	var result contracts.ProjectBindingView
	if request.ID <= 0 || request.Revision < 1 {
		return result, usage("--binding and --revision are required")
	}
	old, err := s.db.GetTicketBindingByID(ctx, request.ID)
	if err != nil {
		return result, unavailable(err)
	}
	if old.RepoID != s.repo.ID {
		return result, unavailableText("binding belongs to a different project")
	}
	row, err := s.db.DisableTicketBinding(ctx, request.ID, request.Revision)
	return contracts.ProjectBindingView(row), unavailable(err)
}

// AddBinding records a qualified ticket against the retained ticket connection.
func (s *Service) AddBinding(ctx context.Context, request contracts.ProjectBindingRequest) (contracts.ProjectBindingView, error) {
	return s.saveBinding(ctx, request, false)
}

// CorrectBinding replaces a qualified binding under its expected revision.
func (s *Service) CorrectBinding(ctx context.Context, request contracts.ProjectBindingRequest) (contracts.ProjectBindingView, error) {
	return s.saveBinding(ctx, request, true)
}
func (s *Service) saveBinding(ctx context.Context, request contracts.ProjectBindingRequest, correct bool) (contracts.ProjectBindingView, error) {
	var result contracts.ProjectBindingView
	v, err := s.db.CurrentConnection(ctx, s.repo.ID, store.TicketConnection)
	if err != nil {
		return result, unavailable(err)
	}
	if request.Container == "" {
		request.Container = v.Destination
	}
	owner, name, ok := strings.Cut(request.Container, "/")
	if !ok || (github.Repo{Owner: owner, Name: name}).Validate() != nil || (v.Provider == ticket.GitHub && request.Number <= 0) || (v.Provider == ticket.Linear && request.Key == "") || request.RemoteID == "" {
		return result, usage("valid --container, --number and --remote-id are required")
	}
	key := strconv.Itoa(request.Number)
	if v.Provider == ticket.Linear {
		key = request.Key
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: request.RemoteID}, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: request.Container, Key: key}}
	proposed := store.TicketBinding{RepoID: s.repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "explicit"}
	var row store.TicketBinding
	if correct {
		if request.ID <= 0 || request.Revision < 1 {
			return result, usage("--binding and --revision are required")
		}
		proposed.ID = request.ID
		row, err = s.db.CorrectTicketBinding(ctx, proposed, request.Revision)
	} else {
		row, err = s.db.AddTicketBinding(ctx, proposed)
	}
	return contracts.ProjectBindingView(row), unavailable(err)
}
