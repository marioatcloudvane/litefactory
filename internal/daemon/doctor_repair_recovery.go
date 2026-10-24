package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func (d *Daemon) recoverDoctorRepairs(ctx context.Context) error {
	if d.doctor.service == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	operations, err := d.db.DoctorRepairRecoveryOperations(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, operation := range operations {
		ref, err := naming.ParseRef(operation.TaskRef)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if operation.State == "restart_pending" {
			operation, err = d.doctor.service.RecoverRepair(ctx, ref, operation.ID)
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if operation.State != "recovered" && operation.State != "completed" && operation.State != "failed" {
			continue
		}
		failures = append(failures, d.retainDoctorRepairOutcome(ctx, operation))
	}
	return errors.Join(failures...)
}

func (d *Daemon) retainDoctorRepairOutcome(ctx context.Context, operation store.DoctorRepair) error {
	steps, err := d.db.DoctorRepairSteps(ctx, operation.ID)
	if err != nil {
		return err
	}
	conversation := doctor.Conversation{Repair: &operation, RepairSteps: steps}
	if operation.State == "recovered" {
		restart, err := d.db.ReadDoctorRestart(ctx, operation.ID)
		if err != nil {
			return err
		}
		conversation.Restart = &restart
	}
	safe, err := d.doctor.service.Redactor.Apply(ctx, doctor.RenderConversation(conversation), 262144)
	if err != nil {
		return err
	}
	if safe.Truncated() {
		return errors.New("doctor repair outcome exceeded bound")
	}
	parts, err := doctor.PresentationParts(safe.String())
	if err != nil {
		return err
	}
	return d.db.EnsureDoctorRepairOutcome(ctx, operation.ID, parts, d.filter.Identities().BotUserID)
}

func (d *Daemon) finishDoctorApply(ctx context.Context, c store.DoctorCommand, runErr error) error {
	operation, err := d.db.DoctorCommandRepair(ctx, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return d.db.CompleteDoctorCommand(ctx, c.ID, "Doctor apply was not admitted; no repair operation is recorded for this command. Next: `/doctor Recheck the current state and recommend a repair` requests a fresh diagnosis.", 0)
	}
	if err != nil {
		return err
	}
	switch operation.State {
	case "completed", "recovered", "failed":
		return d.retainDoctorRepairOutcome(ctx, operation)
	case "restart_pending":
		// The retained restart will settle and publish the same operation later.
		return nil
	}
	steps, err := d.db.DoctorRepairSteps(ctx, operation.ID)
	if err != nil {
		return err
	}
	body := doctor.RenderConversation(doctor.Conversation{Repair: &operation, RepairSteps: steps})
	if runErr != nil {
		body = "Doctor apply did not complete: " + runErr.Error() + "\n\n" + body
	}
	safe, err := d.doctor.service.Redactor.Apply(ctx, body, 262144)
	if err != nil {
		return err
	}
	if safe.Truncated() {
		return errors.New("doctor apply response exceeded bound")
	}
	err = d.db.CompleteDoctorCommand(ctx, c.ID, safe.String(), 0)
	if errors.Is(err, store.ErrDoctorFenced) {
		return nil
	}
	return err
}
