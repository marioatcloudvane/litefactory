package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type lifecycleForge struct{ progressForge }

func (f *lifecycleForge) ListIssues(context.Context, github.Repo, time.Time) ([]github.Issue, error) {
	var issues []github.Issue
	for n := 200; n < 200+len(f.children); n++ {
		issues = append(issues, f.children[n])
	}
	return issues, nil
}
func (f *lifecycleForge) ListSubIssues(ctx context.Context, repo github.Repo, _ int) ([]github.Issue, error) {
	return f.ListIssues(ctx, repo, time.Time{})
}
func (f *lifecycleForge) CreateIssue(_ context.Context, _ github.Repo, title, body redact.Body) (github.Issue, error) {
	n := 200 + len(f.children)
	issue := github.Issue{ID: int64(n * 10), Number: n, Title: title.String(), Body: body.String(), State: "open", Author: github.User{ID: 9}}
	f.children[n] = issue
	return issue, nil
}
func (f *lifecycleForge) UpdateIssue(_ context.Context, _ github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	issue := f.children[n]
	issue.Title, issue.Body = title.String(), body.String()
	f.children[n] = issue
	return issue, nil
}

func TestManagedPlanRefinementFreezeResumeLiveCompletionLifecycle(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			forge := &lifecycleForge{progressForge: progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}}
			planRuns, implementCalls := 0, 0
			runner := newStubRunner(func(_ context.Context, request agent.Request) (*agent.RunResult, error) {
				if request.Mode == string(agent.ModePlan) {
					planRuns++
					writePlanOutcome(t, request, "completed")
					proposal := planbrief.PlanProposal{Version: 1, Parent: request.Ref.String(), Generation: 1}
					for idx := range 2 {
						story := planbrief.StoryProposal{Key: fmt.Sprintf("US-%d", idx+1), Intent: "create", Title: fmt.Sprintf("Story %d", idx+1), Body: "## User Story\nOwner sees progress.\n## Acceptance Criteria\n- AC-1: the scoped result passes\n## Tasks\nImplement and verify.\n## Architecture\nUse existing package.\n## Dependencies\nNone.\n"}
						if planRuns == 2 {
							current := forge.children[200+idx]
							story.Intent, story.IssueNumber, story.IssueID = "update", current.Number, current.ID
							story.ExpectedBase = planbrief.StoryHash(current.Title, current.Body)
							story.Body += "\nRefined from the child discussion.\n"
						}
						proposal.Stories = append(proposal.Stories, story)
					}
					if planRuns == 2 && !strings.Contains(request.Prompt, "issuecomment-901") {
						t.Fatal("refinement lost original source")
					}
					bytes, err := json.Marshal(proposal)
					mustProgress(t, err)
					mustProgress(t, os.WriteFile(filepath.Join(request.WorktreePath, request.PlanProposalPath), bytes, 0o600))
					session := f.reloadSession()
					slug, err := naming.SlugFromBranch(session.Branch, testIssue)
					mustProgress(t, err)
					dir, err := naming.AITeamSessionDir(session.Branch, time.Now(), slug)
					mustProgress(t, err)
					mustProgress(t, os.MkdirAll(filepath.Join(request.WorktreePath, dir), 0o700))
					mustProgress(t, os.WriteFile(filepath.Join(request.WorktreePath, dir, "plan_brief.md"), []byte("# Plan\n\n## Story Index\n| ID | Title | Size | Depends On | Sub-issue |\n|----|-------|------|------------|-----------|\n"), 0o600))
				} else {
					implementCalls++
					if implementCalls == 1 {
						return nil, agent.ErrSessionNotResumable
					}
					if request.Path != agent.PathColdAfterResumeRefused {
						t.Fatal("implementation did not exercise resume fallback")
					}
					bundle := approvedPromptBundle(t, request.Prompt)
					if !strings.Contains(bundle.Stories[0].Body, "Refined from the child discussion") {
						t.Fatal("freeze did not use revised GitHub contract")
					}
					event := progressEvent(t, f, bundle.RunID, request.WorktreePath)
					writeProgressEvent(t, request.WorktreePath, event)
					mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
					if forge.children[200].State != "closed" || forge.children[201].State != "open" {
						t.Fatal("live completion did not preserve unfinished sibling")
					}
					run, err := f.db.GetRun(t.Context(), bundle.RunID)
					mustProgress(t, err)
					if run.Status != store.RunRunning || run.CommitSHA != "" {
						t.Fatal("close waited for final save")
					}
					event.Sequence, event.Kind, event.Evidence, event.Reason = 2, "invalidated", nil, "affected scoped regression"
					writeProgressEvent(t, request.WorktreePath, event)
					mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
					if forge.children[200].State != "open" {
						t.Fatal("affected completed story not reopened")
					}
				}
				result := succeeded(request, f.paths.Home())
				result.ResumeToken = "lifecycle-conversation"
				return result, nil
			})
			f = newFixture(t, runner)
			_, err := f.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9})
			mustProgress(t, err)
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit})
			mustProgress(t, err)
			f.opts.SubIssueCreator, f.opts.SubIssueRenderer = forge, renderer
			f.opts.Runners = RunnerSet{kind: runner}
			service, err := NewStoryProgress(f.db, forge, renderer, 9, f.paths.StoryProgressLocksDir())
			mustProgress(t, err)
			f.opts.StoryProgress = service
			reporter, err := NewReporter(ReporterOptions{DB: f.db, Forge: forge, Renderer: renderer, Logger: discardLogger(), BotUserID: 9, StoryLockDir: f.paths.StoryProgressLocksDir()})
			mustProgress(t, err)
			req := f.request()
			req.Mode, req.Agent = agent.ModePlan, kind
			req.Command, req.Trigger.Body = ParseCommand("/plan feature"), "/plan feature"
			first := f.execute(req)
			if !first.Succeeded() {
				t.Fatalf("publication: %+v", first)
			}
			_, err = reporter.ReportTurn(t.Context(), first)
			mustProgress(t, err)
			mustProgress(t, f.db.AdmitStoryRefinement(t.Context(), store.StoryRefinement{SessionID: f.session.ID, CommentID: 901, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}))
			req.Trigger.CommentID, req.Trigger.Body, req.Command = 901, "refine", ParseCommand("refine")
			refined := f.execute(req)
			if !refined.Succeeded() {
				t.Fatalf("refinement: %+v", refined)
			}
			_, err = reporter.ReportTurn(t.Context(), refined)
			mustProgress(t, err)
			req.Mode, req.Command, req.Trigger.CommentID, req.Trigger.Body = agent.ModeImplement, ParseCommand("/implement feature"), 902, "/implement feature"
			implemented := f.execute(req)
			if !implemented.Succeeded() {
				t.Fatalf("implementation: %+v", implemented)
			}
			if implementCalls != 2 || len(forge.children) != 2 {
				t.Fatal("workflow duplicated story or skipped resume")
			}
			for _, result := range []Result{first, refined, implemented} {
				for _, path := range result.Files() {
					if strings.Contains(path, agent.StoryEventsDirectory) || strings.Contains(path, planbrief.ProposalFileName) || strings.Contains(path, "US-1.md") {
						t.Fatalf("tracked story transport: %s", path)
					}
				}
			}
		})
	}
}
