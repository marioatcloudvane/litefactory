package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/factory/conversationdiagnosis"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// RecoverOwnerActionDiagnosis reconciles only the original source's durable
// receipt. It never asks the doctor to run again or adopts a later diagnosis.
func (a chatResearch) RecoverOwnerActionDiagnosis(ctx context.Context, target, key string) (owneraction.Result, bool, error) {
	d := a.daemon
	if d == nil || d.db == nil || d.chat == nil || d.chat.redactor == nil || d.chat.service == nil {
		return owneraction.Result{}, false, errors.New("diagnosis inspection unavailable")
	}
	return recoverOwnerActionDiagnosis(ctx, d.db, target, key, d.diagnosisSummary)
}

func recoverOwnerActionDiagnosis(ctx context.Context, db *store.DB, target, key string, render func(context.Context, doctor.Conversation) (string, error)) (owneraction.Result, bool, error) {
	return conversationdiagnosis.RecoverAction(ctx, db, target, key, render)
}
