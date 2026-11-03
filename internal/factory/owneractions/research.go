package owneractions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Question identifies one published owner question.
type Question struct{ Text, Token, Reason string }

// ResearchAnswers owns the existing atomic research answer admission.
type ResearchAnswers interface {
	Current(context.Context, string) (Question, error)
	Answer(context.Context, string, string, string, time.Time) (Question, error)
}

func (t *Targets) selectAllProjects(ctx context.Context, option string) (Resolution, error) {
	repos, err := t.db.ListRepos(ctx)
	if err != nil {
		return Resolution{}, err
	}
	refs := make([]Repository, len(repos))
	for i, r := range repos {
		refs[i] = repository(r)
	}
	return SelectProject(refs, option, nil), nil
}

// ResearchTarget resolves the registered research task and its repository evidence.
func (t *Targets) ResearchTarget(ctx context.Context, args map[string]string, taskRequired bool) (string, int64, string) {
	n, err := strconv.Atoi(args["issue"])
	if err != nil || n < 1 {
		return "", 0, "issue must be a positive number"
	}
	project := strings.TrimSpace(args["project"])
	if project == "" {
		resolution, resolveErr := t.selectAllProjects(ctx, "")
		if resolveErr != nil {
			return "", 0, "Project lookup failed; nothing was applied. Ask the factory operator to inspect project registration on the factory host before repeating this command."
		}
		if resolution.Refusal != "" {
			return "", 0, resolution.Refusal
		}
		project = resolution.Repository.Slug()
	}
	repositories, err := t.db.ListRepos(ctx)
	if err != nil {
		return "", 0, "Project lookup failed; nothing was applied. Ask the factory operator to inspect project registration on the factory host before repeating this command."
	}
	var matches []string
	for _, repo := range repositories {
		if repo.Forge == store.ForgeGitHub && (project == repo.Slug() || (!strings.Contains(project, "/") && project == repo.Name)) {
			matches = append(matches, repo.Slug())
		}
	}
	if len(matches) == 0 {
		return "", 0, "unknown project"
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", 0, "ambiguous project; use one of: " + strings.Join(matches, ", ")
	}
	var repoID int64
	for _, repo := range repositories {
		if repo.Slug() == matches[0] && repo.Forge == store.ForgeGitHub {
			repoID = repo.ID
		}
	}

	ref := fmt.Sprintf("github:%s#%d", matches[0], n)
	if taskRequired {
		if _, err := t.db.GetSessionByTaskRef(ctx, ref); errors.Is(err, store.ErrNotFound) {
			return "", repoID, "there is no factory task for this issue"
		} else if err != nil {
			return "", repoID, "Task lookup failed; nothing was applied. Ask the factory operator to inspect retained task state on the factory host before repeating this command."
		}
	}
	return ref, repoID, ""
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

func researchIssueURL(task string) string {
	ref, err := naming.ParseRef(task)
	if err != nil {
		return "the affected issue"
	}
	return ref.IssueURL()
}

// AnswerAdmission preserves missing-input and lost-acknowledgment handling.
type AnswerAdmission struct {
	Research ResearchAnswers
	Protect  func(context.Context, string) (string, error)
	Render   func(context.Context, Question) (string, error)
}

func answerResult(text, state string) (owneraction.Result, error) {
	return owneraction.Result{State: state, Summary: text}, nil
}

// Answer admits the exact research answer while preserving lost-acknowledgment handling.
func (a AnswerAdmission) Answer(ctx context.Context, ref string, args map[string]string, at time.Time) (owneraction.Result, error) {
	text := args["text"]
	if a.Research != nil && strings.TrimSpace(text) == "" {
		// After a restart, raw answer text is gone. A previously committed
		// claim must still be reported honestly, without reapplying anything.
		q, err := a.Research.Current(ctx, ref)
		if err == nil && q.Reason == "already answered" {
			return answerResult(questionText(q), "succeeded")
		}
		if _, present := args["text"]; !present {
			// The inbox may have lost its acknowledgement after a successful
			// claim and a newer question. Missing volatile input cannot prove
			// that the original answer was never recorded.
			reason := "Your answer could not be confirmed. Inspect " + researchIssueURL(ref) + " before resending; it may already be recorded."
			if err == nil {
				q.Reason = reason
				if rendered, renderErr := a.Render(ctx, q); renderErr == nil {
					return answerResult(rendered, "uncertain")
				}
			}
			return answerResult(reason, "uncertain")
		}
	}
	if a.Research == nil || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return answerResult("Your answer was not recorded; nothing was applied. Inspect the current question on "+researchIssueURL(ref)+" before submitting a corrected answer.", "failed")
	}
	if a.Protect != nil {
		var err error
		text, err = a.Protect(ctx, text)
		if err != nil {
			return answerResult("Your answer was not recorded; nothing was applied. Inspect the current question on "+researchIssueURL(ref)+" before submitting a corrected answer.", "failed") //nolint:nilerr // An answer must never be retried automatically.
		}
	}
	q, err := a.Research.Answer(ctx, ref, args["revision"], text, at)
	if err != nil {
		return answerResult("Your answer was not recorded; nothing was applied. Inspect the current question on "+researchIssueURL(ref)+" before submitting a corrected answer.", "failed") //nolint:nilerr // An answer must never be retried automatically.
	}
	text, renderErr := a.Render(ctx, q)
	if renderErr != nil {
		return owneraction.Result{Summary: q.Reason + "; current question details unavailable. Inspect " + researchIssueURL(ref) + " for the current question before replying."}, nil //nolint:nilerr // Refused answers must not be retried after rendering fails.
	}
	status := "failed"
	if q.Reason == "answer recorded" || q.Reason == "already answered" {
		status = "succeeded"
	}
	return answerResult(text, status)
}
