package conversationdiagnosis

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// RunAction rechecks effect authority and invokes diagnosis under its durable source key.
func RunAction(ctx context.Context, db *store.DB, service Diagnoser, render func(context.Context, doctor.Conversation) (string, error), target, note, key string) (owneraction.Result, error) {
	ref, err := naming.ParseRef(target)
	if err != nil {
		return owneraction.Result{}, err
	}
	if service == nil {
		return owneraction.Result{}, errors.New("diagnosis unavailable")
	}
	if err := db.CheckOwnerActionEffect(ctx); err != nil {
		return owneraction.Result{}, err
	}
	conversation, err := service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: note, SourceID: key})
	if err != nil {
		return owneraction.Result{}, err
	}
	text, err := render(ctx, conversation)
	result := owneraction.Result{Summary: text}
	if err == nil {
		result.State = "succeeded"
		if conversation.Diagnosis == nil {
			result.State = "failed"
		}
	}
	return result, err
}
