package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
)

func (d *Daemon) recoverDoctorRepairs(ctx context.Context) error {
	if d.doctor.service == nil || d.doctor.service.RestartRoute == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	targets, err := d.db.DoctorRepairRecoveryTargets(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, target := range targets {
		ref, err := naming.ParseRef(target)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		operation, err := d.doctor.service.RecoverRepair(ctx, ref)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if operation.State != "recovered" {
			continue
		}
		conversation, err := d.doctor.service.ReadConversation(ctx, ref)
		if err != nil {
			failures = append(failures, err)
			continue
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
