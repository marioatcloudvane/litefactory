package messaging

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
)

// DiagnosisRequest contains explicit command input, never repair authority.
type DiagnosisRequest struct{ Target, Note, SourceID string }

// DiagnosisSummary contains redacted, read-only observations without repair steps.
type DiagnosisSummary struct {
	Findings, CoverageGaps, OpenQuestions []string
	RepairProposed                        bool
}

// RenderDiagnosis reserves space for every section and the repair boundary.
func RenderDiagnosis(summary DiagnosisSummary, limit int) string {
	repair := ""
	if summary.RepairProposed {
		repair = "\nA repair was proposed and can only be reviewed on the issue."
	}
	fixed := "Findings:\n\nCoverage gaps:\n\nOpen questions:\n" + repair
	budget := max(0, limit-replySize(fixed))
	findings, gaps := budget/2, budget/4
	return "Findings:\n" + shortenLine(strings.Join(summary.Findings, "\n"), findings) + "\nCoverage gaps:\n" + shortenLine(strings.Join(summary.CoverageGaps, "\n"), gaps) + "\nOpen questions:\n" + shortenLine(strings.Join(summary.OpenQuestions, "\n"), budget-findings-gaps) + repair
}

// Diagnosis queues read-only work; the daemon completes its durable inbox row.
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

var errDeferredDiagnosis = errors.New("messaging: deferred diagnosis")

// EnableResearch installs explicit commands before the chat worker starts.
func (s *Service) EnableResearch(diagnosis Diagnosis, answers ResearchAnswers) {
	s.diagnosis, s.research = diagnosis, answers
	// Discord rejects a required option after an optional one, so the
	// optional project follows every required option.
	base := []Argument{{Name: "issue", Type: IntegerArgument, Required: true, Description: "Positive issue number"}}
	project := Argument{Name: "project", Type: TextArgument, Description: "Managed owner/repo or unique repo; defaults to the only registered project"}
	s.RegisterCommand(CommandDefinition{Name: "diagnose", NaturalLanguage: true, Effect: ChangeEffect, Description: "Explain what is wrong or stuck", Arguments: append(append([]Argument(nil), base...), project, Argument{Name: "note", Type: TextArgument, Description: "Optional diagnosis context"})}, s.diagnose)
	s.RegisterCommand(CommandDefinition{Name: "question", NaturalLanguage: true, Effect: ReadEffect, Description: "Show the current research question", Arguments: append(append([]Argument(nil), base...), project)}, s.question)
	s.RegisterCommand(CommandDefinition{Name: "answer", NaturalLanguage: true, Effect: ChangeEffect, Description: "Answer the current research question", Arguments: append(append([]Argument(nil), base...), Argument{Name: "revision", Type: TextArgument, Required: true, Description: "Current 12-character question token"}, Argument{Name: "text", Type: TextArgument, Required: true, Description: "Answer, up to 65536 bytes"}, project)}, s.researchAnswer)
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
	if err := s.db.DeferChatDiagnosisRepository(ctx, m.Transport, m.ID, ref, at, presentation.AssessedRepository(ctx)); err != nil {
		return assessed(ctx, "Diagnosis could not be queued. Ask the factory operator to inspect `"+report.TaskInspectionCommand(ref)+"` on the factory host before another request.", presentation.Refusal) //nolint:nilerr // Reply to the owner; never retry a refused command.
	}
	if err := s.diagnosis.Queue(ctx, DiagnosisRequest{Target: ref, Note: m.Arguments["note"], SourceID: "chat:" + m.Transport + ":" + m.ID}); err != nil {
		if completeErr := s.db.CompleteChatDiagnosisResult(ctx, m.Transport, m.ID, 0, "diagnosis failed", at, string(presentation.ReportableError)); completeErr != nil {
			return "", completeErr
		}
	}
	s.Notify()
	return "", errDeferredDiagnosis
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
	if q.Text != "" {
		if s.Protect != nil {
			var err error
			q.Text, err = s.Protect(ctx, q.Text)
			if err != nil {
				return "", err
			}
		}
		// Leave room for the complete reason and token, plus substitution margin.
		fixed := replySize(questionText(Question{Text: " ", Token: q.Token, Reason: q.Reason}))
		q.Text = shortenLine(q.Text, max(0, s.ReplyLimit()-fixed-128))
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
