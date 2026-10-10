package doctor

import "testing"

func TestTypedRepairInterruptionSettlesWithoutReplay(t *testing.T) {
	tools, schedule := typedExecutionFixture(t)
	ctx := t.Context()
	step, err := tools.service.DB.ApplyDoctorTypedRows(ctx, tools.operation, 1, "cancel_schedule")
	if err != nil {
		t.Fatal(err)
	}
	op, err := tools.service.ReconcileRepair(ctx, tools.target)
	if err != nil || op.State != "failed" || !op.Fenced {
		t.Fatal(op, err)
	}
	held, err := tools.service.DB.DoctorRepairHeld(ctx, tools.target.String())
	if err != nil || held {
		t.Fatal("hold retained", held, err)
	}
	steps, err := tools.service.DB.DoctorRepairSteps(ctx, tools.operation)
	if err != nil || steps[0].BackupReference != step.BackupReference || steps[0].State != "verified" {
		t.Fatal(steps, err)
	}
	current, err := tools.service.DB.GetSchedule(ctx, schedule.ID)
	if err != nil || string(current.Status) != "failed" {
		t.Fatal("recovery changed target", current, err)
	}
}
