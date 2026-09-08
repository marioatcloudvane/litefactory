package turn

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCompletedStoriesAllowParentRecoveryAfterFailure(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		for _, failure := range []string{"aggregate", "push"} {
			t.Run(string(kind)+"/"+failure, func(t *testing.T) {
				var f *fixture
				attempt := 0
				runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
					attempt++
					result := succeeded(req, f.paths.Home())
					if attempt == 2 {
						if !strings.Contains(req.Prompt, "## Parent completion recovery") || strings.Contains(req.Prompt, "## Incremental story verification") {
							t.Fatal("retry was not restricted to parent-only aggregate recovery")
						}
						return result, nil
					}
					runs, err := f.db.ListRuns(t.Context(), f.session.ID)
					mustProgress(t, err)
					event := progressEvent(t, f, runs[0].ID, req.WorktreePath)
					writeProgressEvent(t, req.WorktreePath, event)
					mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
					revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
					mustProgress(t, err)
					event.Sequence, event.Story, event.IssueID, event.Revision = 2, "US-2", 2010, revisions[1].Hash
					writeProgressEvent(t, req.WorktreePath, event)
					mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
					if failure == "push" {
						mustProgress(t, os.Rename(f.repo.Origin, f.repo.Origin+".offline"))
					} else {
						result.Outcome, result.ExitCode, result.IsError = agent.OutcomeFailed, 1, true
						result.Reason = "unrelated aggregate check failed"
					}
					return result, nil
				})
				f = newFixture(t, runner)
				forge, _ := setupProgressScope(t, f)
				f.opts.Runners = RunnerSet{kind: runner}
				req := f.request()
				req.Mode, req.Agent = agent.ModeImplement, kind
				req.Command, req.Trigger.Body = ParseCommand("/implement work"), "/implement work"
				first := f.execute(req)
				if first.Succeeded() {
					t.Fatal("injected parent failure was lost")
				}
				if failure == "push" {
					mustProgress(t, os.Rename(f.repo.Origin+".offline", f.repo.Origin))
				}
				if forge.children[200].State != "closed" || forge.children[201].State != "closed" {
					t.Fatal("unrelated parent failure reopened completed stories")
				}
				changes := len(forge.changes)
				req.Trigger.CommentID++
				recovered := f.execute(req)
				if !recovered.Succeeded() || attempt != 2 {
					t.Fatalf("parent recovery failed: %+v, attempts=%d", recovered, attempt)
				}
				feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
				mustProgress(t, err)
				if feature.State != "completed" || len(forge.changes) != changes {
					t.Fatalf("recovery changed story effects or left parent unfinished: %s", feature.State)
				}
			})
		}
	}
}
