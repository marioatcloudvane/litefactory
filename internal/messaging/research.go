package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
)

// DiagnosisRequest contains explicit command input, never repair authority.
type DiagnosisRequest struct{ Target, Note, SourceID string }

// Diagnosis starts the Doctor on a task; it refuses with an owner-facing reason.
type Diagnosis interface {
	Queue(context.Context, DiagnosisRequest) error
}

// Question identifies one published owner question.
type Question = owneractions.Question

// ResearchAnswers owns the checks and atomic admission of research answers.
type ResearchAnswers interface {
	Current(context.Context, string) (Question, error)
	Answer(context.Context, string, string, string, time.Time) (Question, error)
}

// EnableResearch installs explicit commands before the chat worker starts.
func (s *Service) EnableResearch(diagnosis Diagnosis, answers ResearchAnswers) {
	s.diagnosis, s.research = diagnosis, answers
	// Discord rejects a required option after an optional one, so the
	// optional project follows every required option.
	base := []Argument{{Name: "issue", Type: IntegerArgument, Required: true, Description: "Positive issue number"}}
	project := Argument{Name: "project", Type: TextArgument, Description: "Managed owner/repo or unique repo; defaults to the only registered project"}
	defdiagnose, _ := owneractions.ChangeDefinition("diagnose")
	s.RegisterCommand(defdiagnose, s.diagnose)
	s.RegisterCommand(CommandDefinition{Name: "question", NaturalLanguage: true, Effect: ReadEffect, Description: "Show the current research question", Arguments: append(append([]Argument(nil), base...), project)}, s.question)
	defanswer, _ := owneractions.ChangeDefinition("answer")
	s.RegisterCommand(defanswer, s.researchAnswer)
}

func (s *Service) researchTarget(ctx context.Context, m Message, taskRequired bool) (string, string) {
	ref, repoID, refusal := s.factory.Targets.ResearchTarget(ctx, m.Arguments, taskRequired)
	if repoID > 0 {
		presentation.AssessRepository(ctx, repoID)
	}
	return ref, refusal
}

func (s *Service) diagnose(ctx context.Context, m Message, at time.Time) (string, error) {
	ref, refusal := s.researchTarget(ctx, m, false)
	if refusal != "" {
		return assessed(ctx, refusal, presentation.Refusal)
	}
	if s.diagnosis == nil {
		return assessed(ctx, "Diagnosis is unavailable; no diagnosis was started. Ask the factory operator to inspect `"+report.TaskInspectionCommand(ref)+"` on the factory host.", presentation.Refusal)
	}
	if len(m.Arguments["note"]) > 8192 {
		return assessed(ctx, "diagnosis note is too long", presentation.Refusal)
	}
	// The Doctor runs on the task and reports on its issue; the start is the
	// whole effect of this command.
	if err := s.diagnosis.Queue(ctx, DiagnosisRequest{Target: ref, Note: m.Arguments["note"], SourceID: "chat:" + m.Transport + ":" + m.ID}); err != nil {
		return assessed(ctx, err.Error(), presentation.Refusal)
	}
	return assessed(ctx, "The Doctor started on "+ref+". It reports on the task's issue and asks there when it needs you.", presentation.Success)
}

func questionText(q Question) string {
	text := q.Reason
	if q.Text != "" {
		if text != "" {
			text += "\n"
		}
		text += q.Text + "\nToken: " + q.Token
	}
	return text
}

func (s *Service) renderQuestion(ctx context.Context, q Question) (string, error) {
	return RenderQuestion(ctx, q, s.Protect, s.ReplyLimit())
}

// RenderQuestion formats a protected question for the supplied delivery budget.
func RenderQuestion(ctx context.Context, q Question, protect func(context.Context, string) (string, error), limit int) (string, error) {
	if q.Text != "" {
		if protect != nil {
			var err error
			q.Text, err = protect(ctx, q.Text)
			if err != nil {
				return "", err
			}
		}
		// Leave room for the complete reason and token, plus substitution margin.
		fixed := replySize(questionText(Question{Text: " ", Token: q.Token, Reason: q.Reason}))
		q.Text = shortenLine(q.Text, max(0, limit-fixed-128))
	}
	return questionText(q), nil
}

func (s *Service) question(ctx context.Context, m Message, _ time.Time) (string, error) {
	ref, refusal := s.researchTarget(ctx, m, true)
	if refusal != "" {
		return assessed(ctx, refusal, presentation.Refusal)
	}
	if s.research == nil {
		return assessed(ctx, "there is no open research question", presentation.Refusal)
	}
	q, err := s.research.Current(ctx, ref)
	if err != nil {
		return assessed(ctx, "Question lookup failed. Inspect the current question on "+researchIssueURL(ref)+" before answering.", presentation.Refusal) //nolint:nilerr // Storage failure is a final user-visible refusal.
	}
	if q.Text != "" {
		presentation.Assess(ctx, presentation.Success)
	} else {
		presentation.Assess(ctx, presentation.Refusal)
	}
	return s.renderQuestion(ctx, q)
}

func (s *Service) researchAnswer(ctx context.Context, m Message, at time.Time) (string, error) {
	ref, refusal := s.researchTarget(ctx, m, true)
	if refusal != "" {
		return assessed(ctx, refusal, presentation.Refusal)
	}
	result, err := (owneractions.AnswerAdmission{Research: s.research, Protect: s.Protect, Render: s.renderQuestion}).Answer(ctx, ref, m.Arguments, at)
	switch result.State {
	case "succeeded":
		presentation.Assess(ctx, presentation.Success)
	case "failed":
		presentation.Assess(ctx, presentation.Refusal)
	case "uncertain":
		presentation.Assess(ctx, presentation.Uncertain)
	}
	return result.Summary, err
}

func researchIssueURL(task string) string {
	ref, err := naming.ParseRef(task)
	if err != nil {
		return "the affected issue"
	}
	return ref.IssueURL()
}
