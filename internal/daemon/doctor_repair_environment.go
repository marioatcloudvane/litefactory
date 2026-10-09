package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// LocalDoctorDrain is an authenticated local factory control, never an agent tool.
// An IPC adapter must bind this capability to the installed daemon instance.
type LocalDoctorDrain interface {
	DrainDoctorTarget(context.Context, naming.Ref, string) (string, error)
}

// DoctorRepairEnvironment keeps singleton ownership for a daemon-down repair.
// Its caller must Close only after the repair has stopped or committed handoff.
// A nil Live capability means down must be proven by acquiring the actual lock;
// absence of a PID or a failed socket request is never sufficient proof.
type DoctorRepairEnvironment struct {
	DB          *store.DB
	Paths       config.Paths
	Live        LocalDoctorDrain
	Replacement DoctorReplacement
	mu          sync.Mutex
	lock        *Lock
	operation   string
}

// Preflight verifies required local exclusion dependencies.
func (e *DoctorRepairEnvironment) Preflight(ctx context.Context, ref naming.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.DB == nil || e.Paths.LockPath() == "" || ref.Issue <= 0 {
		return errors.New("doctor exclusion configuration unavailable")
	}
	return nil
}

// Exclude drains the exact held target or owns the daemon singleton while down.
func (e *DoctorRepairEnvironment) Exclude(ctx context.Context, ref naming.Ref, operation string) (string, error) {
	if err := e.Preflight(ctx, ref); err != nil {
		return "", err
	}
	held, err := e.DB.DoctorRepairHeld(ctx, ref.String())
	if err != nil {
		return "", err
	}
	repair, err := e.DB.ReadDoctorRepair(ctx, ref.String())
	if err != nil {
		return "", err
	}
	if !held || repair.ID != operation || repair.Fenced {
		return "", errors.New("doctor exclusion requires the exact active hold")
	}
	var liveErr error
	if e.Live != nil {
		var instance string
		instance, liveErr = e.Live.DrainDoctorTarget(ctx, ref, operation)
		if liveErr == nil {
			return instance, nil
		}
		// Failed IPC is uncertainty, not proof of death. Only acquiring the real
		// singleton below permits the daemon-down path after a stale descriptor.
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lock != nil {
		if e.operation != operation {
			return "", errors.New("another repair owns singleton exclusion")
		}
		return "down:" + operation, nil
	}
	lock, err := AcquireLock(e.Paths.LockPath())
	if err != nil {
		return "", errors.Join(liveErr, err)
	}
	if err = ctx.Err(); err != nil {
		return "", errors.Join(err, lock.Close())
	}
	if err = e.DB.CheckDoctorTargetStopped(ctx, ref.String()); err != nil {
		return "", errors.Join(err, lock.Close())
	}
	e.lock = lock
	e.operation = operation
	return "down:" + operation, nil
}

// Close releases down-daemon ownership after repair execution has ended.
func (e *DoctorRepairEnvironment) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lock == nil {
		return nil
	}
	err := e.lock.Close()
	e.lock = nil
	e.operation = ""
	return err
}
