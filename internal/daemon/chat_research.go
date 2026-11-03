package daemon

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/factory/conversationdiagnosis"
	"github.com/cloudvane/litefactory/internal/factory/researchanswers"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ChatDiagnoser deliberately exposes no repair capability.
type ChatDiagnoser interface {
	Diagnose(context.Context, doctor.DiagnoseRequest) (doctor.Conversation, error)
}

type chatResearch struct{ daemon *Daemon }

func (a chatResearch) Queue(_ context.Context, r messaging.DiagnosisRequest) error {
	d := a.daemon
	if d.chat.diagnoser == nil && d.doctor.service == nil {
		return errors.New("diagnosis unavailable")
	}
	d.chat.mu.Lock()
	d.chat.notes[r.SourceID] = r.Note
	d.chat.mu.Unlock()
	return nil
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

func (d *Daemon) diagnosisSummary(ctx context.Context, c doctor.Conversation) (string, error) {
	return renderOwnerActionDiagnosis(ctx, c, d.chat.redactor, d.chat.service.ReplyLimit())
}

func renderOwnerActionDiagnosis(ctx context.Context, c doctor.Conversation, sanitizer *redact.Redactor, replyLimit int) (string, error) {
	if c.Diagnosis == nil {
		return "diagnosis failed", nil
	}
	var findings []string
	for _, f := range c.Diagnosis.Findings {
		text := f.Explanation
		if f.Uncertainty != "" {
			text += "\nUncertainty: " + f.Uncertainty
		}
		findings = append(findings, text)
	}
	summary := messaging.DiagnosisSummary{RepairProposed: len(c.Diagnosis.Proposal) > 0 || len(c.CurrentProposal) > 0 || c.Diagnosis.Repair != nil}
	for _, section := range []struct {
		raw    []string
		target *[]string
	}{{findings, &summary.Findings}, {c.Diagnosis.CoverageGaps, &summary.CoverageGaps}, {c.Diagnosis.Questions, &summary.OpenQuestions}} {
		safe, err := sanitizer.Apply(ctx, strings.Join(section.raw, "\n"), 65536)
		if err != nil {
			return "", err
		}
		*section.target = []string{safe.String()}
	}
	return messaging.RenderDiagnosis(summary, max(0, replyLimit-128)), nil
}

// conversationDiagnoses supplies primitive hosts without duplicating workflow policy.
func (d *Daemon) conversationDiagnoses() *conversationdiagnosis.Service {
	options := conversationdiagnosis.Options{Connections: d.connections, Logger: d.log, SessionID: func(ctx context.Context) int64 {
		scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope)
		return scope.sessionID
	}}
	if d.filter != nil {
		options.OwnerID = d.filter.Identities().OwnerUserID
	}
	if d.forge != nil {
		options.ReadIssue = d.readIssue
	}
	if d.doctor.pool != nil {
		options.Try = d.doctor.pool.try
		options.Done = d.doctor.pool.done
	}
	if d.chat != nil {
		options.Redactor = d.chat.redactor
		options.Diagnoser = d.chat.diagnoser
		if options.Diagnoser == nil && d.doctor.service != nil {
			options.Diagnoser = d.doctor.service
		}
		options.Start = d.chat.diagnosisWorkers.Go
		options.TakeNote = func(source string) string {
			d.chat.mu.Lock()
			defer d.chat.mu.Unlock()
			note := d.chat.notes[source]
			delete(d.chat.notes, source)
			return note
		}
		options.RestoreNote = func(source, note string) { d.chat.mu.Lock(); defer d.chat.mu.Unlock(); d.chat.notes[source] = note }
		options.Input = func(ctx context.Context, r store.ChatReply, at time.Time) conversationdiagnosis.Input {
			note, err := d.chat.service.ConversationDiagnosisInput(ctx, r, at)
			input := conversationdiagnosis.Input{Note: note, Err: err}
			var access *messaging.ConversationAccessError
			if errors.As(err, &access) {
				input.Temporary = !access.Permanent
				input.RetryAfter = access.RetryAfter
			}
			return input
		}
		options.Render = d.diagnosisSummary
		if d.chat.service != nil {
			options.Notify = d.chat.service.Notify
			options.Push = d.chat.service.PushConversationDiagnosis
		}
	}
	return conversationdiagnosis.New(d.db, options)
}
func (d *Daemon) dispatchChatDiagnosis(ctx context.Context, at time.Time) {
	if d.chat == nil {
		return
	}
	d.conversationDiagnoses().Dispatch(ctx, at)
}
func (d *Daemon) presentChatProposal(ctx context.Context, r store.ChatReply, issue github.Issue, c doctor.Conversation) error {
	return d.conversationDiagnoses().Present(ctx, r, issue, c)
}
func (d *Daemon) chatDiagnosisResults(ctx context.Context, at time.Time) error {
	return d.conversationDiagnoses().Results(ctx, at)
}

func (d *Daemon) chatResearchNotes(ctx context.Context) error {
	return d.researchAnswers(d.chat.redactor).Reconcile(ctx)
}

var _ messaging.Diagnosis = chatResearch{}
var _ messaging.ResearchAnswers = chatResearch{}
var _ ChatDiagnoser = (*doctor.Service)(nil)
