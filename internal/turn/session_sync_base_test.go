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
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestSessionSyncBindingUsesFactoryTargetsAndRetainedCheckout(t *testing.T) {
	f, forge, _ := assembledResearchFixture(t, nil)
	ctx := t.Context()
	_, err := f.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	runtime, err := NewGraphBugRuntime(ctx, f.opts, BugRuntimeConfig{Forge: bugRuntimeJourneyForge{forge.client}, OwnerID: 42, Permissions: func(agent.Kind, string) (BugRolePermissions, error) {
		return BugRolePermissions{Tools: []string{"Read"}, AllowedTools: []string{"Read"}}, nil
	}})
	mustProgress(t, err)
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
	runtime.definition = &teamsource.WorkflowDefinition{Profile: profile, Digest: strings.Repeat("a", 64)}
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: profile.Team, Workflow: "bug", ProfileVersion: profile.ProfileVersion, Path: "fixture/bug.dot", Digest: profile.Digest, TeamSnapshotDigest: runtime.definition.Digest, CreatedAt: time.Now()})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: pin.ID, CurrentNode: "sync", StartedAt: time.Now()})
	mustProgress(t, err)
	f.session, err = f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, testBranch)
	mustProgress(t, err)
	clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
	mustProgress(t, err)
	checkout, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "bound-sync"), Branch: testBranch, BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	mustProgress(t, f.db.SetSessionWorktree(ctx, f.session.ID, checkout.Worktree.Path()))
	initial, err := checkout.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	mustProgress(t, err)
	f.repo.Git("push", "origin", "HEAD:refs/heads/"+testBranch)
	locked, err := f.db.AcquireSessionLock(ctx, f.session.ID, f.opts.InstanceID)
	mustProgress(t, err)
	if !locked {
		t.Fatal("fixture writer exclusion not acquired")
	}
	t.Cleanup(func() {
		mustProgress(t, f.db.ReleaseSessionLock(context.WithoutCancel(ctx), f.session.ID, f.opts.InstanceID))
	})
	request := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, Node: "sync", Attrs: map[string]string{"working": "attacker-branch", "base": "attacker-base", "repository": "attacker-repository"}}
	binding := runtime.graphSyncBaseBinding(f.session.ID)
	target, err := binding.Bind(ctx, Options{}, request)
	mustProgress(t, err)
	if target.Working != testBranch || target.Base != gittest.DefaultBranch || target.Checkout == nil {
		t.Fatalf("factory target=%+v", target)
	}
	reason, err := target.Checkout.Guard(ctx, target.Working, initial)
	mustProgress(t, err)
	if reason != "" {
		t.Fatalf("clean checkout deferred: %s", reason)
	}
	f.repo.WriteFile("factory-base-change.txt", "bound base change\n")
	f.repo.Commit("advance factory base")
	f.repo.Git("push", "origin", gittest.DefaultBranch)
	base := f.repo.Git("rev-parse", "HEAD")
	result, err := (SyncBaseActivity{binding}).Execute(ctx, Options{}, request)
	mustProgress(t, err)
	var output syncBaseOutput
	mustProgress(t, json.Unmarshal(result.Output, &output))
	observed, err := checkout.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	mustProgress(t, err)
	if result.Outcome != "synced" || output.Commit != observed || !output.CheckoutVerified || observed == initial {
		t.Fatalf("result=%+v output=%+v checkout=%s", result, output, observed)
	}
	if ancestor := f.repo.Git("merge-base", base, observed); ancestor != base {
		t.Fatal("retained checkout lacks factory base")
	}
	for _, invalid := range []ActivityRequest{{Workflow: agent.WorkflowBug, InstanceID: instance.ID, Node: "completed"}, {Workflow: agent.WorkflowImplement, Mode: agent.ModeImplement, InstanceID: instance.ID, Node: "sync"}} {
		if _, err := binding.Bind(ctx, Options{}, invalid); err == nil {
			t.Fatalf("invalid invocation accepted: %+v", invalid)
		}
	}
	other, err := f.db.GetOrCreateSession(ctx, f.storeRepo, testIssue+1)
	mustProgress(t, err)
	foreign, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: other.ID, DefinitionID: pin.ID, CurrentNode: "sync", StartedAt: time.Now()})
	mustProgress(t, err)
	request.InstanceID = foreign.ID
	if _, err := binding.Bind(ctx, Options{}, request); err == nil {
		t.Fatal("foreign session graph authorized synchronization")
	}
	if _, err := binding.Repo.Begin(ctx, "attacker-branch"); err == nil {
		t.Fatal("repository opened an agent-supplied working branch")
	}
}
