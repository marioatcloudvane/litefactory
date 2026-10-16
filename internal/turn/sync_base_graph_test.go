package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Exercise the admitted Bug graph and durable dispatch with real scratch Git
// publication and a retained checkout. The agent boundary reads the checkout
// only after the graph has recorded a successful synchronization.
func TestExplicitBugSyncHandsSuccessfulCheckoutToDependentAgent(t *testing.T) {
	for _, outcome := range []string{"synced", "current"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t, nil)
			g := newSyncGit(t)
			retained, err := git.EnsureWorktree(t.Context(), g.clone, git.EnsureWorktreeOptions{Dir: filepath.Join(t.TempDir(), "retained"), Branch: implementFeature, BaseBranch: gittest.DefaultBranch})
			mustProgress(t, err)
			before, err := retained.Worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			mustProgress(t, err)
			base := g.advance(gittest.DefaultBranch, "base-fix.txt", "base fix\n")
			if outcome == "current" {
				// Publish the base first, while leaving the retained checkout stale.
				res, _, err := g.syncBase()
				mustProgress(t, err)
				if res.Outcome != "synced" {
					t.Fatalf("precondition sync: %+v", res)
				}
			}
			binding := SyncBaseBinding{Repo: g.bindings.Repo, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
				return SyncBaseTarget{Working: implementFeature, Base: gittest.DefaultBranch, Checkout: GitSyncBaseCheckout{Worktree: retained.Worktree, Author: git.Author{Name: "Test", Email: "test@example.invalid"}}, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}, nil
			}}
			raw, err := os.ReadFile("testdata/bug_before_sync.profile")
			mustProgress(t, err)
			dot := strings.Replace(string(raw), `conversation -> analyst`, `conversation -> sync`, 1)
			dot = strings.Replace(dot, `completed [lf_terminal="true"];`, `completed [lf_terminal="true"];
 sync [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="3",lf_park="owner",lf_stop="conflict,retry,blocked,failed,invalid,exhausted"];
 sync -> analyst [lf_on="synced"]; sync -> analyst [lf_on="current"];`, 1)
			profile, err := graphexec.ParseProfile([]byte(dot))
			mustProgress(t, err)
			specs, err := SimplifiedBugSpecs()
			mustProgress(t, err)
			mustProgress(t, graphexec.Validate(profile, specs))
			var activities ActivityRegistry
			mustProgress(t, activities.Register(graphexec.SyncBaseAction, SyncBaseActivity{binding}))
			conversation := &graphTestHandler{results: map[string][]ActivityResult{"conversation": {{Outcome: "analyse"}}}}
			mustProgress(t, activities.Register(graphexec.ResearchLoopAction, conversation))
			_, err = f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
			mustProgress(t, err)
			now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: profile.ProfileVersion, Path: "test/bug.dot", Digest: profile.Digest, CreatedAt: now})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, Status: store.GraphRunning, CurrentNode: profile.Start, StartedAt: now})
			mustProgress(t, err)
			var synced syncBaseOutput
			observed := ""
			dependent := &graphTestHandler{results: map[string][]ActivityResult{"analyst": {{Outcome: "done"}}}, hook: func(ctx context.Context, opts Options, req ActivityRequest) {
				if req.Workflow != agent.WorkflowBug || req.Mode != "" {
					t.Fatalf("agent workflow: %+v", req)
				}
				observed, err = retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
				mustProgress(t, err)
				if observed != synced.Commit || observed == before {
					t.Fatalf("agent checkout=%s output=%s stale=%s", observed, synced.Commit, before)
				}
			}}
			mustProgress(t, activities.Register("lf_agent", dependent))
			request := GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, At: now, NextDueAt: now.Add(time.Minute)}
			first, err := ExecuteNodeAction(t.Context(), f.opts, request)
			mustProgress(t, err)
			if first.NextNode != "sync" || observed != "" {
				t.Fatalf("conversation=%+v observed=%q", first, observed)
			}
			step, err := ExecuteNodeAction(t.Context(), f.opts, request)
			mustProgress(t, err)
			mustProgress(t, json.Unmarshal(step.Activity.Output, &synced))
			if step.Activity.Outcome != outcome || step.NextNode != "analyst" || observed != "" || !synced.CheckoutVerified {
				t.Fatalf("sync=%+v observed=%q", step, observed)
			}
			outputs, err := f.db.ListGraphOutputs(t.Context(), step.AttemptID)
			mustProgress(t, err)
			if len(outputs) != 1 || string(outputs[0].Payload) != string(step.Activity.Output) {
				t.Fatalf("sync output not retained: %+v", outputs)
			}
			if remote := g.remoteFeature(); remote != synced.Commit {
				t.Fatalf("remote=%s output=%s", remote, synced.Commit)
			}
			if ancestor := g.repo.Git("merge-base", base, synced.Commit); ancestor != base {
				t.Fatal("published working branch does not contain fetched base")
			}
			step, err = ExecuteNodeAction(t.Context(), f.opts, request)
			mustProgress(t, err)
			if step.NextNode != "conversation" || len(dependent.calls) != 1 || observed != synced.Commit {
				t.Fatalf("dependent step=%+v observed=%s", step, observed)
			}
		})
	}
}
