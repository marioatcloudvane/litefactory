package daemon

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// seedManagedStory creates one rendered story issue under the parent through
// the forge and records it as the parent's first-generation managed story, the
// state graph Plan leaves behind once a story set is published.
func seedManagedStory(ctx context.Context, t *testing.T, db *store.DB, forge turn.PlanForge, renderer *report.Reporter, ref naming.Ref, key, title, body string) store.ManagedStory {
	t.Helper()
	rendered, err := renderer.RenderCompleteStory(ctx, report.Story{Ref: ref, ID: key, Title: title, Excerpt: body})
	if err != nil {
		t.Fatal(err)
	}
	repo := github.RepoFromRef(ref)
	var issue github.Issue
	if correlated, ok := forge.(interface {
		CreateIssueEffect(context.Context, string, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
	}); ok {
		issue, err = correlated.CreateIssueEffect(ctx, "story-publication:"+ref.String()+"/"+key, repo, ref.Issue, rendered.Title, rendered.Body)
	} else {
		issue, err = forge.CreateIssue(ctx, repo, rendered.Title, rendered.Body)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := forge.AddSubIssue(ctx, repo, ref.Issue, issue.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	story := store.ManagedStory{ParentTask: ref.String(), Generation: 1, Key: key, IssueNumber: issue.Number, IssueID: issue.ID, BodyHash: planbrief.StoryHash(issue.Title, issue.Body), Dependencies: []string{}, State: "planning"}
	if err := db.PutManagedStory(ctx, story); err != nil {
		t.Fatal(err)
	}
	return story
}
