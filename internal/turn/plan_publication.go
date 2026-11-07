package turn

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// PlanForge is the managed publication surface. All mutation inputs have
// already passed report's redaction and completeness checks.
type PlanForge interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error)
	ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error)
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	AddSubIssue(context.Context, github.Repo, int, int64) error
	UpdateIssue(context.Context, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
}

// CompleteStoryRenderer ensures publication receives redacted, complete story content.
type CompleteStoryRenderer interface {
	RenderCompleteStory(context.Context, report.Story) (report.SubIssue, error)
}

// Publication intent is transport metadata, not an issue requirement. Reusing
// an exact projection leaves its historical bytes immutable across replanning.
func retainTypedPublicationProjection(ctx context.Context, db *store.DB, projection store.SubmissionProjection, typed planbrief.TypedStory) error {
	prior, err := db.GetSubmissionProjection(ctx, projection.Parent, projection.Generation, projection.Story, projection.Revision)
	if errors.Is(err, store.ErrNotFound) {
		history, err := db.ListCriterionHistory(ctx, projection.Parent, projection.Story)
		if err != nil {
			return err
		}
		active := map[string]bool{}
		for _, id := range projection.Criteria {
			active[id] = true
		}
		retired := map[string]bool{}
		for _, id := range projection.RetiredCriteria {
			retired[id] = true
		}
		for _, h := range history {
			if h.RetiredRevision == "" && !active[h.ID] && !retired[h.ID] {
				projection.RetiredCriteria = append(projection.RetiredCriteria, h.ID)
			}
		}
		return db.PutSubmissionProjection(ctx, projection)
	}
	if err != nil {
		return err
	}
	var original planbrief.TypedStory
	if err := planbrief.DecodeTypedContent(prior.Content, &original); err != nil {
		return err
	}
	original.Intent = typed.Intent
	if prior.IssueID != projection.IssueID || prior.Title != projection.Title || prior.Body != projection.Body || !reflect.DeepEqual(original, typed) {
		return fmt.Errorf("typed publication conflicts with immutable issue projection")
	}
	return nil
}
