package localprojects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// SetConnection validates and records an immutable connection version.
func (s *Service) SetConnection(ctx context.Context, request contracts.SetProjectConnectionRequest, present contracts.ConnectionPresentation) error {
	repo := s.repo
	id := repo.ID
	role := store.ConnectionRole(request.Role)
	if request.Revision < 0 {
		return usage("--revision is required")
	}
	origin, e := ticket.CanonicalOrigin(request.Origin)
	if e != nil {
		return usage("invalid API origin")
	}
	owner, name, ok := strings.Cut(request.Destination, "/")
	if !ok || (github.Repo{Owner: owner, Name: name}).Validate() != nil {
		return usage("destination must be owner/name")
	}
	v := store.ConnectionVersion{RepoID: id, Role: role, Provider: ticket.Provider(request.Provider), Origin: origin, Destination: request.Destination, RemoteID: request.RemoteID, Enabled: true, Provenance: "explicit"}
	if v.Provider == ticket.Linear {
		if role != store.TicketConnection || request.Destination != request.Team+"/"+request.Project || request.Workspace == "" || request.Team == "" || request.Project == "" {
			return usage("Linear requires ticket role and workspace/team/project IDs; destination must be team/project")
		}
		v.Linear = store.LinearConfiguration{WorkspaceID: request.Workspace, TeamID: request.Team, ProjectID: request.Project, AuthKind: request.AuthKind, ModeLabelsJSON: request.ModeLabels, LifecycleStatesJSON: request.States}
	} else if v.Provider != ticket.GitHub {
		return usage("provider must be github or linear")
	}
	parts := strings.Split(request.Credential, ":")
	switch {
	case len(parts) == 2 && parts[0] == "env":
		v.CredentialSource = "env"
		v.CredentialEnv = parts[1]
	case len(parts) == 3 && parts[0] == "keychain":
		v.CredentialSource = "keychain"
		v.CredentialService = parts[1]
		v.CredentialAccount = parts[2]
	default:
		return usage("credential must reference env:NAME or keychain:SERVICE:ACCOUNT")
	}
	if role == store.CodeConnection {
		v.ClonePath = repo.Path
		v.RemoteURL = repo.RemoteURL
		v.BaseBranch = repo.BaseBranch
		v.FeaturePrefix = repo.FeaturePrefix
		v.WorktreeRoot = repo.WorktreeRoot
	}
	if v.Provider == ticket.Linear {
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		factory := s.factory(0)
		candidate := v
		candidate.ID = 1
		clients, e := factory.Clients(checkCtx, candidate, 0)
		if e != nil {
			return unavailable(e)
		}
		if _, e = clients.Linear.Inspect(checkCtx); e != nil {
			//nolint:staticcheck // Preserve the existing CLI refusal text during this ownership refactor.
			return unavailable(fmt.Errorf("Linear configuration could not be verified; check workspace, team, project and mappings"))
		}
	}
	saved, e := s.db.SetConnection(ctx, v, request.Revision)
	if e != nil {
		return unavailable(e)
	}
	return s.presentConnection(ctx, saved, present)
}

// DisableConnection records connection disable intent under the expected revision.
func (s *Service) DisableConnection(ctx context.Context, role string, revision int64, present contracts.ConnectionPresentation) error {
	if revision < 1 {
		return usage("--revision is required")
	}
	saved, err := s.db.DisableConnection(ctx, s.repo.ID, store.ConnectionRole(role), revision)
	if err != nil {
		return unavailable(err)
	}
	return s.presentConnection(ctx, saved, present)
}

// CheckConnection checks the retained connection and saves bounded health evidence.
func (s *Service) CheckConnection(ctx context.Context, role string) (contracts.ProjectConnectionView, contracts.ProjectObservationView, error) {
	var view contracts.ProjectConnectionView
	var observation contracts.ProjectObservationView
	v, err := s.db.CurrentConnection(ctx, s.repo.ID, store.ConnectionRole(role))
	if err != nil {
		return view, observation, unavailable(err)
	}
	identities, err := turn.LoadIdentities(ctx, s.db, store.ForgeGitHub)
	if err != nil {
		return view, observation, unavailable(err)
	}
	recorded := s.factory(identities.BotUserID).Check(ctx, v)
	if err = s.db.SaveConnectionObservation(ctx, recorded); err != nil {
		return view, observation, unavailable(err)
	}
	return connectionView(v), contracts.ProjectObservationView(recorded), nil
}

// ListConnections presents retained versions, binding conflicts and health in order.
func (s *Service) ListConnections(ctx context.Context, role string, present contracts.ConnectionPresentation) error {
	versions, err := s.db.ListConnections(ctx, s.repo.ID)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if role != "" && string(v.Role) != role {
			continue
		}
		if err = s.presentConnection(ctx, v, present); err != nil {
			return err
		}
		observation, err := s.db.GetConnectionObservation(ctx, v.ID)
		health := contracts.ProjectConnectionHealth{Enabled: v.Enabled}
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return err
		default:
			health.Checked = true
			health.Observation = contracts.ProjectObservationView(observation)
		}
		if err = present.Health(health); err != nil {
			return err
		}
	}
	state, err := s.db.GetProjectImportState(ctx, s.repo.ID)
	if err == nil {
		return present.Import(contracts.ProjectImportView{Status: state.Status, Reason: state.Reason})
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

func (s *Service) presentConnection(ctx context.Context, v store.ConnectionVersion, present contracts.ConnectionPresentation) error {
	if err := present.Connection(connectionView(v)); err != nil {
		return err
	}
	if v.Role != store.TicketConnection {
		return nil
	}
	bindings, err := s.db.ListTicketBindings(ctx, v.RepoID)
	if err != nil {
		return err
	}
	var findings []error
	for _, binding := range bindings {
		if !binding.Enabled || binding.ConnectionVersionID == v.ID {
			continue
		}
		err := s.db.CheckTicketBindingSource(ctx, binding, v)
		if errors.Is(err, store.ErrBindingConflict) {
			err = present.BindingConflict(contracts.ProjectBindingConflict{BindingID: binding.ID, PriorVersionID: binding.ConnectionVersionID, CurrentVersionID: v.ID, Reason: err.Error()})
		}
		findings = append(findings, err)
	}
	return errors.Join(findings...)
}
