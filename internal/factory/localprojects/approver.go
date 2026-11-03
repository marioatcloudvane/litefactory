package localprojects

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
)

// InspectApprover returns the current verified appointment and authority blockers.
func (s *Service) InspectApprover(ctx context.Context) (contracts.ProjectApproverInspection, error) {
	var result contracts.ProjectApproverInspection
	inspection, err := (projectconnection.ApproverService{DB: s.db}).Inspect(ctx, s.repo.ID)
	if err != nil && !errors.Is(err, store.ErrApproverMissing) && !errors.Is(err, store.ErrNotFound) {
		return result, unavailable(err)
	}
	result.Connection = connectionView(inspection.Connection)
	result.Appointment = contracts.ProjectApproverView(inspection.Appointment)
	result.Present = err == nil
	result.Initialization = contracts.ProjectImportView{Status: inspection.Initialization.Status, Reason: inspection.Initialization.Reason}
	for _, blocker := range inspection.Blockers {
		result.Blockers = append(result.Blockers, contracts.ProjectApproverBlocker(blocker))
	}
	return result, nil
}
func (s *Service) approver(ctx context.Context) (projectconnection.ApproverService, error) {
	publisher, err := s.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot)
	if err != nil {
		return projectconnection.ApproverService{}, unavailable(err)
	}
	return projectconnection.ApproverService{DB: s.db, Factory: s.factory(publisher.UserID)}, nil
}

// SetApprover verifies and records an appointment against both retained revisions.
func (s *Service) SetApprover(ctx context.Context, login string, connectionRevision, revision int64) (contracts.ProjectApproverView, error) {
	service, err := s.approver(ctx)
	if err != nil {
		return contracts.ProjectApproverView{}, err
	}
	row, err := service.Set(ctx, s.repo.ID, login, connectionRevision, revision)
	return contracts.ProjectApproverView(row), unavailable(err)
}

// RefreshApprover refreshes the retained immutable account identity.
func (s *Service) RefreshApprover(ctx context.Context, connectionRevision, revision int64) (contracts.ProjectApproverView, error) {
	service, err := s.approver(ctx)
	if err != nil {
		return contracts.ProjectApproverView{}, err
	}
	row, err := service.Refresh(ctx, s.repo.ID, connectionRevision, revision)
	return contracts.ProjectApproverView(row), unavailable(err)
}
