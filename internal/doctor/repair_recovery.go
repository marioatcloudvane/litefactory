package doctor

import (
	"context"
	"errors"
	"io"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// ReconcileRepair observes possible file writes without restarting an agent.
// Even an exact intended result remains held for reviewed continuation when the
// original execution did not reach verified handoff.
func (s *Service) ReconcileRepair(ctx context.Context, ref naming.Ref) (operation store.DoctorRepair, err error) {
	ref = canonicalTarget(ref)
	unlock, err := s.repairEffectLock(ctx, ref)
	if err != nil {
		return operation, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	operation, err = s.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return operation, err
	}
	if operation.State == "restart_pending" || operation.State == "recovered" || operation.State == "completed" || operation.State == "failed" {
		return operation, nil
	}
	steps, err := s.DB.DoctorRepairSteps(ctx, operation.ID)
	if err != nil {
		return operation, err
	}
	typed := len(steps) > 0
	for _, step := range steps {
		typed = typed && store.DoctorRepairToolKind(step.Kind)
	}
	if typed {
		if err = s.DB.SettleDoctorTypedRepair(ctx, operation.ID, false, "Interrupted repair retained without replay; unfinished effects require owner review"); err != nil {
			return operation, err
		}
		return s.DB.ReadDoctorRepair(ctx, ref.String())
	}
	for _, step := range steps {
		if (step.Kind != "session_file" && step.Kind != "worktree_file") || (step.State != "write_intent" && step.State != "uncertain") {
			continue
		}
		observed := "unresolved"
		if s.verifyReplacedFile(ctx, ref, step) == nil {
			observed = digestBytes([]byte(step.After))
		} else {
			root, parent, file, readErr := s.repairFile(ctx, ref, step)
			if readErr == nil && file == nil {
				observed = absentFile
				_ = closeDoctorHandles(parent, root)
			} else if readErr == nil {
				bytes, readErr := io.ReadAll(io.LimitReader(file, 32769))
				closeErr := errors.Join(file.Close(), parent.Close(), root.Close())
				if readErr == nil && closeErr == nil && digestBytes(bytes) == step.Before {
					observed = step.Before
				}
			}
		}
		if err = s.DB.ReconcileDoctorFileObservation(ctx, operation.ID, step.Number, observed); err != nil {
			return operation, err
		}
	}
	if err = s.DB.StopDoctorRepair(ctx, ref.String(), "Interrupted repair retained for review"); err != nil {
		return operation, err
	}
	return s.DB.ReadDoctorRepair(ctx, ref.String())
}
