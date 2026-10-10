package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
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
			operation, err = d.doctor.service.RecoverRepair(ctx, ref)
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if operation.State != "recovered" && operation.State != "completed" && operation.State != "failed" {
			continue
		}
		steps, err := d.db.DoctorRepairSteps(ctx, operation.ID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		conversation := doctor.Conversation{Repair: &operation, RepairSteps: steps}
		if operation.State == "recovered" {
			restart, readErr := d.db.ReadDoctorRestart(ctx, operation.ID)
			if readErr != nil {
				failures = append(failures, readErr)
				continue
			}
			conversation.Restart = &restart
		}
		safe, err := d.doctor.service.Redactor.Apply(ctx, doctor.RenderConversation(conversation), 262144)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if safe.Truncated() {
			failures = append(failures, errors.New("doctor repair outcome exceeded bound"))
			continue
		}
		parts, err := doctor.PresentationParts(safe.String())
		if err != nil {
			failures = append(failures, err)
			continue
		}
		failures = append(failures, d.db.EnsureDoctorRepairOutcome(ctx, operation.ID, parts, d.filter.Identities().BotUserID))
	}
	return errors.Join(failures...)
}
