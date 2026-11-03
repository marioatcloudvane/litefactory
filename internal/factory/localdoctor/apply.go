package localdoctor

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Apply verifies the current displayed proposal and dispatches its retained restart.
func Apply(ctx context.Context, service *doctor.Service, request doctor.ApplyRequest) (applied bool, err error) {
	if err = service.CheckRepairCapability(ctx, request.Target); err != nil {
		return false, ApplyRefusal(ctx, service, request, err)
	}
	request.PresentationID, err = service.DB.CurrentDoctorLocalPresentation(ctx, request.Target.String())
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return false, ApplyRefusal(ctx, service, request, fmt.Errorf("current proposal presentation could not be read: %w", err))
		}
		return false, fmt.Errorf("doctor apply performed no repair: it requires a fully displayed current proposal. Inspect it on the factory host with `litefactory doctor status %s/%s %d`", request.Target.Owner, request.Target.Repo, request.Target.Issue)
	}
	_, err = service.Apply(ctx, request)
	if err == nil {
		operation, readErr := service.DB.ReadDoctorRepair(ctx, request.Target.String())
		if readErr == nil && operation.State == "restart_pending" {
			_, err = service.DispatchRepairRestart(ctx, request.Target)
		}
	}
	return true, err
}

// ApplyRefusal preserves the distinction between this request and retained earlier repair work.
func ApplyRefusal(ctx context.Context, service *doctor.Service, request doctor.ApplyRequest, cause error) error {
	state := "No retained repair was found."
	repair, err := service.DB.ReadDoctorRepair(ctx, request.Target.String())
	if err == nil {
		state = fmt.Sprintf("Retained repair %s has state %s; this command did not cause it.", repair.ID, repair.State)
	} else if !errors.Is(err, store.ErrNotFound) {
		state = "Retained repair status could not be read."
	}
	return fmt.Errorf("doctor apply performed no repair: %w. %s Inspect on the factory host: `%s`", cause, state, report.TaskInspectionCommand(request.Target.String()))
}

// StoppedRun contains only the stop operation's local presentation facts.
type StoppedRun struct {
	Running bool
	Ordinal int
	Summary string
}

// StopRun reads the run selected by the diagnostic owner-stop operation.
func StopRun(ctx context.Context, service *doctor.Service, runID int64) (StoppedRun, error) {
	run, err := service.DB.GetRun(ctx, runID)
	return StoppedRun{Running: run.Status == store.RunRunning, Ordinal: run.Ordinal, Summary: doctor.StopRunSummary(run)}, err
}
