package turn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

// publicationForge is an in-memory PlanForge holding parent #1 (ID 100) and
// the story issues the test creates or seeds.
type publicationForge struct {
	issues     []github.Issue
	attached   bool
	creates    int
	loseCreate bool
	failAttach bool
	updates    int
}

func (f *publicationForge) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	if n == 1 {
		return github.Issue{ID: 100, Number: 1}, nil
	}
	for _, i := range f.issues {
		if i.Number == n {
			return i, nil
		}
	}
	return github.Issue{}, errors.New("missing")
}
func (f *publicationForge) GetParentIssue(context.Context, github.Repo, int) (github.Issue, error) {
	if f.attached {
		return github.Issue{ID: 100, Number: 1}, nil
	}
	return github.Issue{}, errors.New("not attached")
}
func (f *publicationForge) ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error) {
	return f.issues, nil
}
func (f *publicationForge) ListSubIssues(context.Context, github.Repo, int) ([]github.Issue, error) {
	if f.attached {
		return f.issues, nil
	}
	return nil, nil
}
func (f *publicationForge) CreateIssue(_ context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	f.creates++
	i := github.Issue{ID: 200, Number: 2, Title: title.String(), Body: body.String(), Author: github.User{ID: 9}}
	f.issues = append(f.issues, i)
	if f.loseCreate {
		f.loseCreate = false
		return github.Issue{}, errors.New("response lost")
	}
	return i, nil
}
func (f *publicationForge) AddSubIssue(context.Context, github.Repo, int, int64) error {
	if f.failAttach {
		f.failAttach = false
		return errors.New("attach failed")
	}
	f.attached = true
	return nil
}
func (f *publicationForge) UpdateIssue(_ context.Context, _ github.Repo, _ int, title, body redact.Body) (github.Issue, error) {
	f.updates++
	f.issues[0].Title = title.String()
	f.issues[0].Body = body.String()
	return f.issues[0], nil
}

type typedPlanForge struct{ publicationForge }

func (*typedPlanForge) CreateSubIssues(context.Context, github.Repo, int, []github.SubIssueSpec) ([]github.SubIssueResult, error) {
	panic("unexpected legacy subissue publication")
}

func typedPlanFixture(t *testing.T) (planbrief.TypedPlan, *preparation, *report.Reporter, planbrief.ContentScope) {
	t.Helper()
	const raw = `{"summary":"Factory-owned structure","stories":[{"key":"US-001","intent":"create","title":"Typed requirements","user_story":{"actor":"owner","want":"explicit requirements","benefit":"reliable evidence","notes":["Evidence contract: explanation only"]},"acceptance_criteria":[{"id":"AC-001","text":"String must be x"}],"tasks":[{"id":"T-001","description":"Implement requirement","owner_role":"go-implementation-engineer","criterion_ids":["AC-001"],"depends_on":[]}],"architecture":{"summary":"Pure contract","components":[],"interfaces":[],"constraints":[],"decisions":[]},"dependencies":{"story_keys":[],"issue_refs":[]}}]}`
	var p planbrief.TypedPlan
	mustProgress(t, json.Unmarshal([]byte(raw), &p))
	ref, err := naming.ParseRef("github:o/r#1")
	mustProgress(t, err)
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	mustProgress(t, err)
	return p, &preparation{ref: ref}, renderer, planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}, IssueRefs: []string{}}
}
