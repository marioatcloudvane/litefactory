package daemon

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/researchanswers"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/redact"
)

type chatResearch struct{ daemon *Daemon }

// Queue starts the Doctor on the named task for a chat diagnosis request. The
// Doctor reports on the task's issue; nothing is deferred here.
func (a chatResearch) Queue(ctx context.Context, r messaging.DiagnosisRequest) error {
	err := a.daemon.startDoctor(ctx, r.Target, r.Note, r.SourceID)
	var refused *doctorRefusal
	if err == nil || errors.As(err, &refused) {
		return err
	}
	a.daemon.log.ErrorContext(ctx, "the Doctor could not start for a chat request", slog.String("task", r.Target), slog.Any("error", err))
	return errors.New("The Doctor could not start; the factory logged why. Ask again later.") //nolint:staticcheck // Owner-facing sentence.
}

func questionToken(key string) string { return researchanswers.QuestionToken(key) }

func (d *Daemon) researchAnswers(sanitizer *redact.Redactor) *researchanswers.Service {
	var ids researchanswers.Identities
	if d.filter != nil {
		configured := d.filter.Identities()
		ids = researchanswers.Identities{OwnerUserID: configured.OwnerUserID, BotUserID: configured.BotUserID}
	}
	notes, _ := d.forge.(researchanswers.Notes)
	return researchanswers.New(d.db, researchanswers.Options{ReadIssue: d.readIssue, Notes: notes, Identities: ids, Redactor: sanitizer, Logger: d.log})
}
func (a chatResearch) Current(ctx context.Context, ref string) (messaging.Question, error) {
	return a.daemon.researchAnswers(nil).Current(ctx, ref)
}
func (a chatResearch) Answer(ctx context.Context, ref, token, text string, at time.Time) (messaging.Question, error) {
	var sanitizer *redact.Redactor
	if a.daemon.chat != nil {
		sanitizer = a.daemon.chat.redactor
	}
	return a.answerProtected(ctx, ref, token, text, at, sanitizer)
}
func (a chatResearch) answerProtected(ctx context.Context, ref, token, text string, at time.Time, sanitizer *redact.Redactor) (messaging.Question, error) {
	return a.daemon.researchAnswers(sanitizer).Answer(ctx, ref, token, text, at)
}

func (d *Daemon) chatResearchNotes(ctx context.Context) error {
	return d.researchAnswers(d.chat.redactor).Reconcile(ctx)
}

var _ messaging.Diagnosis = chatResearch{}
var _ messaging.ResearchAnswers = chatResearch{}
