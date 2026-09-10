package turn

import (
	"context"
	"encoding/json"
	"errors"
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

type retryPublicationFailure struct{ lifecycleForge }

func (*retryPublicationFailure) CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error) {
	return github.Issue{}, errors.New("injected publication outage")
}

func TestPlanFailureRetainsTransportForRetry(t *testing.T) {
	for _, publicationFailure := range []bool{false, true} {
		name := "failed agent"
		if publicationFailure {
			name = "publication failure"
		}
		t.Run(name, func(t *testing.T) {
			var f *fixture
			phase := 0
			var proposal []byte
			var artifact, head string
			calls := 0
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				result := succeeded(req, f.paths.Home())
				if phase == 0 {
					if req.PlanOutcomePath != "" {
						writePlanOutcome(t, req, "completed")
					}
					head = f.repo.GitIn(req.WorktreePath, "rev-parse", "HEAD")
					p := planbrief.PlanProposal{Version: 1, Parent: req.Ref.String(), Generation: 1, Stories: []planbrief.StoryProposal{{Key: "US-1", Intent: "create", Title: "Recover planning", Body: "## User Story\nRecover work.\n## Acceptance Criteria\n- AC-1: preserve work\n## Tasks\nVerify recovery.\n## Architecture\nExisting packages.\n## Dependencies\nNone.\n"}}}
					var err error
					proposal, err = json.Marshal(p)
					mustProgress(t, err)
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, planbrief.ProposalFileName), proposal, 0600))
					session := f.reloadSession()
					slug, err := naming.SlugFromBranch(session.Branch, testIssue)
					mustProgress(t, err)
					dir, err := naming.AITeamSessionDir(session.Branch, time.Now(), slug)
					mustProgress(t, err)
					artifact = filepath.Join(dir, "plan_brief.md")
					mustProgress(t, os.MkdirAll(filepath.Join(req.WorktreePath, dir), 0700))
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, artifact), []byte("# Plan\n\n## Story Index\n"), 0600))
					if !publicationFailure {
						result.Outcome = agent.OutcomeFailed
						result.ExitCode = 2
						result.Reason = "planning failed"
					}
				} else {
					if f.repo.GitIn(req.WorktreePath, "rev-parse", "HEAD") != head {
						t.Error("retry preparation committed failed output")
					}
					got, err := os.ReadFile(filepath.Join(req.WorktreePath, planbrief.ProposalFileName))
					mustProgress(t, err)
					if string(got) != string(proposal) {
						t.Error("proposal not retained")
					}
					got, err = os.ReadFile(filepath.Join(req.WorktreePath, artifact))
					mustProgress(t, err)
					if string(got) != "# Plan\n\n## Story Index\n" {
						t.Error("plan artifact not retained")
					}
				}
				return result, nil
			})
			f = newFixture(t, runner)
			f.opts.AutoSyncBase = true
			if publicationFailure {
				renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit})
				mustProgress(t, err)
				f.opts.SubIssueRenderer = renderer
				f.opts.SubIssueCreator = &retryPublicationFailure{lifecycleForge{progressForge: progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}}}
				_, err = f.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9})
				mustProgress(t, err)
			}
			req := f.request()
			req.Mode = agent.ModePlan
			req.Command = ParseCommand("/plan recover")
			req.Trigger.Body = "/plan recover"
			failed := f.execute(req)
			if failed.Succeeded() || failed.Committed {
				t.Fatalf("failed plan saved: %+v", failed)
			}
			if publicationFailure && (failed.Err == nil || !strings.Contains(failed.Err.Error(), "injected publication outage")) {
				t.Fatalf("wrong publication failure: %+v logs %s", failed, f.logs)
			}
			if f.repo.GitIn(f.worktreeDir(), "rev-parse", "HEAD") != head {
				t.Fatal("failure moved HEAD")
			}
			advanceSyncBase(t, f.repo, 1)
			phase = 1
			req.Mode = agent.ModeSpike
			req.Command = ParseCommand("finish retained work")
			req.Trigger.Body = "finish retained work"
			req.Trigger.CommentID++
			recovery := f.execute(req)
			if !recovery.Succeeded() || !recovery.Committed || calls != 2 {
				t.Fatalf("recovery failed: %+v calls %d logs %s", recovery, calls, f.logs)
			}
			rows := f.runs()
			if rows[len(rows)-1].SyncOutcome != store.SyncDeferredDirty {
				t.Fatalf("recovery did not defer sync: %+v", rows)
			}
			if got := f.repo.GitIn(f.worktreeDir(), "ls-tree", "-r", "--name-only", "HEAD"); strings.Contains(got, planbrief.ProposalFileName) {
				t.Fatal("proposal entered task commit")
			}
			got, err := os.ReadFile(filepath.Join(f.worktreeDir(), planbrief.ProposalFileName))
			mustProgress(t, err)
			if string(got) != string(proposal) {
				t.Fatal("save discarded proposal")
			}
		})
	}
}

func TestProposalOnlyStateSynchronizesNormally(t *testing.T) {
	f := newFixture(t, nil)
	w := newSyncWorktree(t, f.repo, 42, "proposal only")
	const retained = "proposal only"
	mustProgress(t, os.WriteFile(filepath.Join(w.Path(), planbrief.ProposalFileName), []byte(retained), 0600))
	advanceSyncBase(t, f.repo, 1)
	result, err := syncBase(t.Context(), Options{AutoSyncBase: true}, Request{}, w, f.storeRepo, syncTestAuthor, discardLogger())
	mustProgress(t, err)
	if result.Outcome != syncOutcomeSynced {
		t.Fatalf("proposal-only sync outcome %s", result.Outcome)
	}
	got, err := os.ReadFile(filepath.Join(w.Path(), planbrief.ProposalFileName))
	mustProgress(t, err)
	if string(got) != retained {
		t.Fatal("sync changed proposal")
	}
}
