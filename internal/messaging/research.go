package messaging

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
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
type Question struct{ Text, Token, Reason string }

// ResearchAnswers owns the checks and atomic admission of research answers.
type ResearchAnswers interface {
	Current(context.Context, string) (Question, error)
	Answer(context.Context, string, string, string, time.Time) (Question, error)
}

var errDeferredDiagnosis = errors.New("messaging: deferred diagnosis")

// EnableResearch installs explicit commands before the chat worker starts.
func (s *Service) EnableResearch(diagnosis Diagnosis, answers ResearchAnswers) {
	s.diagnosis, s.research = diagnosis, answers
	base := []Argument{{Name: "project", Type: TextArgument, Required: true, Description: "Managed owner/repo or unique repo"}, {Name: "issue", Type: IntegerArgument, Required: true, Description: "Positive issue number"}}
	s.RegisterCommand(CommandDefinition{Name: "diagnose", Description: "Explain what is wrong or stuck", Arguments: append(append([]Argument(nil), base...), Argument{Name: "note", Type: TextArgument, Description: "Optional diagnosis context"})}, s.diagnose)
	s.RegisterCommand(CommandDefinition{Name: "question", Description: "Show the current research question", Arguments: base}, s.question)
	s.RegisterCommand(CommandDefinition{Name: "answer", Description: "Answer the current research question", Arguments: append(append([]Argument(nil), base...), Argument{Name: "revision", Type: TextArgument, Required: true, Description: "Current 12-character question token"}, Argument{Name: "text", Type: TextArgument, Required: true, Description: "Answer, up to 65536 bytes"})}, s.researchAnswer)
}

func (s *Service) researchTarget(ctx context.Context, m Message, taskRequired bool) (string, string) {
	n, err := strconv.Atoi(m.Arguments["issue"])
	if err != nil || n < 1 {
		return "", "issue must be a positive number"
	}
	if strings.TrimSpace(m.Arguments["project"]) == "" {
		return "", "please name a project"
	}
	repositories, err := s.db.ListRepos(ctx)
	if err != nil {
		return "", "project lookup failed; nothing was applied"
	}
	project := strings.TrimSpace(m.Arguments["project"])
	var matches []string
	for _, repo := range repositories {
		if repo.Forge == store.ForgeGitHub && (project == repo.Slug() || (!strings.Contains(project, "/") && project == repo.Name)) {
			matches = append(matches, repo.Slug())
		}
	}
	if len(matches) == 0 {
		return "", "unknown project"
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", "ambiguous project; use one of: " + strings.Join(matches, ", ")
	}
	for _, repo := range repositories {
		if repo.Slug() == matches[0] && repo.Forge == store.ForgeGitHub {
			presentation.AssessRepository(ctx, repo.ID)
		}
	}
	ref := fmt.Sprintf("github:%s#%d", matches[0], n)
	if taskRequired {
		if _, err := s.db.GetSessionByTaskRef(ctx, ref); errors.Is(err, store.ErrNotFound) {
			return "", "there is no factory task for this issue"
		} else if err != nil {
			return "", "task lookup failed; nothing was applied"
		}
	}
	return ref, ""
}

func (s *Service) diagnose(ctx context.Context, m Message, at time.Time) (string, error) {
	ref, refusal := s.researchTarget(ctx, m, false)
	if refusal != "" {
		return assessed(ctx, refusal, presentation.Refusal)
	}
	if s.diagnosis == nil {
		return assessed(ctx, "diagnosis is unavailable", presentation.Refusal)
	}
	if len(m.Arguments["note"]) > 8192 {
		return assessed(ctx, "diagnosis note is too long", presentation.Refusal)
	}
	if err := s.db.DeferChatDiagnosisRepository(ctx, m.Transport, m.ID, ref, at, presentation.AssessedRepository(ctx)); err != nil {
		return assessed(ctx, "diagnosis could not be queued", presentation.Refusal) //nolint:nilerr // Reply to the owner; never retry a refused command.
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
	return assessed(ctx, questionText(q), presentation.Success)
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
		return assessed(ctx, "question lookup failed", presentation.Refusal) //nolint:nilerr // Storage failure is a final user-visible refusal.
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
	text := m.Arguments["text"]
	if s.research != nil && strings.TrimSpace(text) == "" {
		// After a restart, raw answer text is gone. A previously committed
		// claim must still be reported honestly, without reapplying anything.
		q, err := s.research.Current(ctx, ref)
		if err == nil && q.Reason == "already answered" {
			return assessed(ctx, questionText(q), presentation.Success)
		}
		if _, present := m.Arguments["text"]; !present {
			// The inbox may have lost its acknowledgement after a successful
			// claim and a newer question. Missing volatile input cannot prove
			// that the original answer was never recorded.
			reason := "your answer could not be confirmed; check the issue before resending"
			if err == nil {
				q.Reason = reason
				if rendered, renderErr := s.renderQuestion(ctx, q); renderErr == nil {
					return assessed(ctx, rendered, presentation.Uncertain)
				}
			}
			return assessed(ctx, reason, presentation.Uncertain)
		}
	}
	if s.research == nil || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return assessed(ctx, "your answer was not recorded; nothing was applied", presentation.Refusal)
	}
	if s.Protect != nil {
		var err error
		text, err = s.Protect(ctx, text)
		if err != nil {
			return assessed(ctx, "your answer was not recorded; nothing was applied", presentation.Refusal) //nolint:nilerr // An answer must never be retried automatically.
		}
	}
	q, err := s.research.Answer(ctx, ref, m.Arguments["revision"], text, at)
	if err != nil {
		return assessed(ctx, "your answer was not recorded; nothing was applied", presentation.Refusal) //nolint:nilerr // An answer must never be retried automatically.
	}
	text, renderErr := s.renderQuestion(ctx, q)
	if renderErr != nil {
		return q.Reason + "; current question details unavailable", nil //nolint:nilerr // Refused answers must not be retried after rendering fails.
	}
	status := presentation.Refusal
	if q.Reason == "answer recorded" || q.Reason == "already answered" {
		status = presentation.Success
	}
	return assessed(ctx, text, status)
}
