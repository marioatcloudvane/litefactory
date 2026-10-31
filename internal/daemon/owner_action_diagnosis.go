package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// OwnerActionDiagnosis reuses the diagnosis service under a durable action key.
func (a chatResearch) OwnerActionDiagnosis(ctx context.Context, target, note, key string) (string, error) {
	d := a.daemon
	var service ChatDiagnoser
	if d.doctor.service != nil {
		service = d.doctor.service
	}
	if d.chat != nil && d.chat.diagnoser != nil {
		service = d.chat.diagnoser
	}
	if d.chat == nil || d.chat.redactor == nil {
		return "", errors.New("diagnosis protection unavailable")
	}
	return runOwnerActionDiagnosis(ctx, d, service, d.chat.redactor, d.chat.service.ReplyLimit(), target, note, key)
}

func runOwnerActionDiagnosis(ctx context.Context, d *Daemon, service ChatDiagnoser, sanitizer *redact.Redactor, replyLimit int, target, note, key string) (string, error) {
	ref, err := naming.ParseRef(target)
	if err != nil {
		return "", err
	}
	if service == nil {
		return "", errors.New("diagnosis unavailable")
	}
	if err := d.db.CheckOwnerActionEffect(ctx); err != nil {
		return "", err
	}
	conversation, err := service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: note, SourceID: key})
	if err != nil {
		return "", err
	}
	text, err := renderOwnerActionDiagnosis(ctx, conversation, sanitizer, replyLimit)
	if err == nil {
		if conversation.Diagnosis == nil {
			presentation.Assess(ctx, presentation.ReportableError)
		} else {
			presentation.Assess(ctx, presentation.Success)
		}
	}
	return text, err
}
