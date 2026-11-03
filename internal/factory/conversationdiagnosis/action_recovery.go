package conversationdiagnosis

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// RecoverAction reconciles only the original source receipt, without running diagnosis again.
func RecoverAction(ctx context.Context, db *store.DB, target, key string, render func(context.Context, doctor.Conversation) (string, error)) (owneraction.Result, bool, error) {
	if db == nil || render == nil {
		return owneraction.Result{}, false, errors.New("diagnosis inspection unavailable")
	}
	receipt, err := db.DoctorDiagnosisBySource(ctx, target, key)
	if errors.Is(err, store.ErrNotFound) {
		return owneraction.Result{}, false, nil
	}
	if err != nil {
		return owneraction.Result{}, false, err
	}
	if len(receipt.Snapshot) > 0 {
		diagnosis, err := doctor.DecodeDiagnosis(receipt.Snapshot)
		if err != nil {
			return owneraction.Result{}, true, err
		}
		text, err := render(ctx, doctor.Conversation{Diagnosis: &diagnosis})
		if err != nil {
			return owneraction.Result{}, true, err
		}
		return owneraction.Result{State: "succeeded", Summary: text, Reference: receipt.AttemptID}, true, nil
	}
	if receipt.State == "failed" {
		return owneraction.Result{State: "failed", Summary: "The retained diagnosis failed before a validated result was accepted. Request a fresh proposal to try again.", Reference: receipt.AttemptID}, true, nil
	}
	return owneraction.Result{State: "uncertain", Summary: "No accepted diagnosis result is retained for this action. Inspect the retained attempt before requesting again.", Reference: receipt.AttemptID}, true, nil
}
