package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

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
func TestManagedPublicationRecoversDurableCreateAndAttach(t *testing.T) {
	for _, failure := range []string{"lost create response", "attach failure", "after attach before index"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			db, err := store.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
			if err != nil {
				t.Fatal(err)
			}
			forge := &publicationForge{loseCreate: failure == "lost create response", failAttach: failure == "attach failure"}
			ref, err := naming.ParseRef("github:o/r#1")
			if err != nil {
				t.Fatal(err)
			}
			o := PublicationOptions{DB: db, Forge: forge, Renderer: renderer, Parent: ref, Generation: 1, BotID: 9}
			p := planbrief.PlanProposal{Version: 1, Parent: ref.String(), Generation: 1, Stories: []planbrief.StoryProposal{{Key: "US-1", Intent: "create", Title: "Complete story", Body: "## User Story\nOwner sees full details.\n## Acceptance Criteria\nFull acceptance.\n## Tasks\nEvery task.\n## Architecture\nEvery technical detail.\n## Dependencies\nNone.\n"}}}
			_, firstErr := PublishPlan(t.Context(), o, p)
			if failure != "after attach before index" && firstErr == nil {
				t.Fatal("expected first attempt failure")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = store.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			o.DB = db
			index, err := PublishPlan(t.Context(), o, p)
			if err != nil {
				t.Fatal(err)
			}
			if forge.creates != 1 {
				t.Fatalf("created %d issues", forge.creates)
			}
			if !strings.Contains(string(index), "issues/2") {
				t.Fatal("missing verified link")
			}
			if !strings.Contains(forge.issues[0].Body, "Every technical detail.") {
				t.Fatal("technical detail lost")
			}
			feature, err := db.GetManagedFeature(t.Context(), ref.String())
			if err != nil {
				t.Fatal(err)
			}
			if feature.Ready {
				t.Fatal("publication without saved index was marked ready")
			}
			if err := db.ApproveManagedStories(t.Context(), ref.String(), 1, nil); err == nil {
				t.Fatal("unsaved plan was allowed to freeze")
			}
			p.Stories[0].Intent = "update"
			p.Stories[0].IssueID = 200
			p.Stories[0].IssueNumber = 2
			p.Stories[0].ExpectedBase = planbrief.StoryHash("old", "old")
			if _, err := PublishPlan(t.Context(), o, p); err == nil {
				t.Fatal("stale update accepted")
			}
			if forge.updates != 0 {
				t.Fatal("stale base was overwritten")
			}
		})
	}
}

type linearPlanForge struct{ *linearScopeForge }

func (f *linearPlanForge) CreateIssue(ctx context.Context, repo github.Repo, title, body redact.Body) (github.Issue, error) {
	issue, err := f.publicationForge.CreateIssue(ctx, repo, title, body)
	issue.Author.ID = f.bot
	f.issues[len(f.issues)-1] = issue
	return issue, err
}
func TestManagedPlanHandoffUsesLinearPublisherWithoutGlobalIdentity(t *testing.T) {
	ctx := t.Context()
	f := newFixture(t, newStubRunner(nil))
	clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
	mustProgress(t, err)
	branch := "lf/1-linear-plan"
	worktree, err := clone.WorktreeAdd(ctx, filepath.Join(filepath.Dir(f.repo.Dir), "linear-plan"), git.WorktreeAddOptions{Branch: branch, CreateFrom: "HEAD"})
	mustProgress(t, err)
	ref := naming.Ref{Forge: "linear", Owner: "team", Repo: "project", Issue: 1}
	actor := ticket.Identity{Provider: ticket.Linear, Origin: "https://linear.example/graphql", RemoteID: "verified-publisher"}
	bot, err := f.db.LocalTicketSlot(ctx, "actor", actor)
	mustProgress(t, err)
	forge := &linearPlanForge{&linearScopeForge{scopeTestForge: scopeTestForge{comments: map[int][]github.Comment{}}, actor: actor, bot: bot}}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	mustProgress(t, err)
	f.opts.SubIssueCreator, f.opts.SubIssueRenderer = forge, renderer
	dir, err := naming.AITeamSessionDir(branch, implementClock, "linear-plan")
	mustProgress(t, err)
	req := agent.Request{Ref: ref, WorktreePath: worktree.Path(), PlanGeneration: 1, PlanRunID: 1, PlanAttempt: "current", PlanOutcomePath: planbrief.OutcomeFileName, PlanProposalPath: planbrief.ProposalFileName, PlanContextPath: filepath.Join(dir, "PLAN_CONTEXT.json")}
	writeCompletedProposal(t, req)
	prep := &preparation{ref: ref, worktree: worktree, session: store.Session{Branch: branch}, run: store.Run{ID: 1, Mode: "plan", StartedAt: implementClock}, promptInput: harness.PromptInput{PlanProposalPath: planbrief.ProposalFileName, PlanGeneration: 1, PlanAttempt: "current"}}
	out := publishPlanHandoff(ctx, f.opts, prep, Result{}, &agent.RunResult{Outcome: agent.OutcomeSucceeded}, f.opts.Logger)
	if out.Err != nil || !prep.planCompleted {
		t.Fatalf("publication failed: %v", out.Err)
	}
	if forge.creates != 1 || forge.issues[0].Author.ID != bot {
		t.Fatalf("wrong publisher: %+v", forge.issues)
	}
	index, err := os.ReadFile(filepath.Join(worktree.Path(), dir, planbrief.FileName))
	mustProgress(t, err)
	if !strings.Contains(string(index), "US-1") {
		t.Fatalf("missing story index: %s", index)
	}
}
