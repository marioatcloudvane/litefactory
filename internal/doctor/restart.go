package doctor

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// RestartRoute belongs to the factory process manager, never the agent. Observe
// must prove new singleton ownership for the exact token, not merely a live PID.
type RestartRoute interface {
	Dispatch(context.Context, store.DoctorRestart) error
	Observe(context.Context, store.DoctorRestart) (newInstance string, confirmed bool, err error)
}

// DispatchRepairRestart claims one external effect. Any lost reply stays uncertain.
func (s *Service) DispatchRepairRestart(ctx context.Context, ref naming.Ref) (store.DoctorRestart, error) {
	ref = canonicalTarget(ref)
	if s.RestartRoute == nil {
		return store.DoctorRestart{}, errors.New("doctor restart route unavailable")
	}
	operation, err := s.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return store.DoctorRestart{}, err
	}
	intent, err := s.DB.ClaimDoctorRestart(ctx, operation.ID)
	if err != nil {
		return store.DoctorRestart{}, err
	}
	if err = s.RestartRoute.Dispatch(ctx, intent); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return intent, errors.Join(err, s.DB.MarkDoctorRestartUncertain(cleanup, operation.ID))
	}
	return intent, nil
}

// RecoverRepair observes a prior dispatch and verifies state before releasing its hold.
// It never dispatches a replacement restart when observation is missing.
func (s *Service) RecoverRepair(ctx context.Context, ref naming.Ref, expectedID ...string) (operation store.DoctorRepair, err error) {
	ref = canonicalTarget(ref)
	if s.RestartRoute == nil {
		return operation, errors.New("doctor recovery observer unavailable")
	}
	operation, err = s.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return operation, err
	}
	if len(expectedID) > 0 && operation.ID != expectedID[0] {
		return operation, errors.New("doctor restart recovery operation changed")
	}
	intent, err := s.DB.ReadDoctorRestart(ctx, operation.ID)
	if err != nil {
		return operation, err
	}
	if intent.State == "acknowledged" {
		return operation, nil
	}
	if intent.State != "dispatched" && intent.State != "uncertain" {
		return operation, errors.New("doctor restart has not been dispatched")
	}
	instance, confirmed, err := s.RestartRoute.Observe(ctx, intent)
	if err != nil {
		return operation, err
	}
	if !confirmed {
		return operation, errors.New("doctor restart remains unconfirmed; no repeat dispatch")
	}
	unlock, err := s.repairEffectLock(ctx, ref)
	if err != nil {
		return operation, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	tools := &RepairTools{service: s, target: ref, operation: operation.ID}
	if err = tools.verifyAll(ctx); err != nil {
		return operation, err
	}
	if err = s.DB.AcknowledgeDoctorRestart(ctx, operation.ID, intent.Token, instance); err != nil {
		return operation, err
	}
	return s.DB.ReadDoctorRepair(ctx, ref.String())
}
