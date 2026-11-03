package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/factory/conversationdiagnosis"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
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
	result, err := conversationdiagnosis.RunAction(ctx, d.db, service, func(ctx context.Context, c doctor.Conversation) (string, error) {
		return renderOwnerActionDiagnosis(ctx, c, sanitizer, replyLimit)
	}, target, note, key)
	if err == nil {
		status := presentation.Success
		if result.State == "failed" {
			status = presentation.ReportableError
		}
		presentation.Assess(ctx, status)
	}
	return result.Summary, err
}
