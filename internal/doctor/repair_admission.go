package doctor

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
)

// CheckRepairCapability verifies installed isolation and lifecycle support before
// admitting approval. It grants no agent authority and writes no workflow state.
func (s *Service) CheckRepairCapability(ctx context.Context, ref naming.Ref) error {
	if s.RepairEnvironment == nil || s.RepairRunner == nil || s.RestartRoute == nil {
		return errors.New("doctor apply unavailable: verified provider and lifecycle capabilities are not installed")
	}
	evidence, err := s.DB.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		return err
	}
	provider := config.Kind(evidence.Provider)
	if provider == "" {
		var ok bool
		provider, ok = s.Config.DefaultAgent()
		if !ok {
			return errors.New("doctor repair provider unavailable")
		}
	}
	if model, ok := s.Config.Tiers.ResolveModel(provider, config.TierFlagship); !ok || model == "" {
		return errors.New("doctor repair flagship unavailable")
	}
	proof, err := s.RepairRunner.IsolationEvidence(ctx, string(provider))
	if err != nil {
		return err
	}
	if proof == "" {
		return ErrIsolationUnavailable
	}
	return s.RepairEnvironment.Preflight(ctx, ref)
}
