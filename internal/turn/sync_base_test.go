package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// newSyncGit is a real repository whose feature branch the Implement coordinator
// brings main into.
func newSyncGit(t *testing.T) *implementGit {
	t.Helper()
	g := newImplementGit(t, implementStories(1, nil)...)
	g.auth.BaseBranch = gittest.DefaultBranch
	return g
}

// advance commits one file on a branch and pushes it.
func (g *implementGit) advance(branch, file, content string) string {
	g.t.Helper()
	g.repo.Git("checkout", "--quiet", branch)
	g.repo.WriteFile(file, content)
	g.repo.Commit("change " + file + " on " + branch)
	g.repo.Git("push", "--quiet", "origin", branch)
	head := g.repo.Git("rev-parse", "HEAD")
	g.repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	return head
}

func (g *implementGit) syncBase() (ActivityResult, syncBaseOutput, error) {
	g.t.Helper()
	res, err := SyncBaseActivity{SyncBaseBinding{Repo: g.bindings.Repo, Bind: g.bindings.implementSyncBase}}.Execute(g.t.Context(), Options{}, g.req())
	var out syncBaseOutput
	if err == nil {
		if e := json.Unmarshal(res.Output, &out); e != nil {
			g.t.Fatal(e)
		}
	}
	return res, out, err
}

func TestSyncBaseGitMergesMainAheadIntoTheFeatureBranch(t *testing.T) {
	g := newSyncGit(t)
	g.advance(implementFeature, "story.txt", "story work\n")
	fix := g.advance(gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	mainBefore := g.repo.Git("rev-parse", "origin/"+gittest.DefaultBranch)

	res, out, err := g.syncBase()
	if err != nil || res.Outcome != "synced" || out.Working != implementFeature || out.Base != gittest.DefaultBranch {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	head := g.remoteFeature()
	if head != out.Commit {
		t.Fatalf("feature head %s, merge commit %s", head, out.Commit)
	}
	if ancestor := g.repo.Git("merge-base", fix, head); ancestor != fix {
		t.Fatal("the feature branch does not contain main's fix")
	}
	if g.remoteFile("fix.txt") != "fixed on main" || g.remoteFile("story.txt") != "story work" {
		t.Fatal("the merge must keep the feature's work and add main's")
	}
	if g.repo.Git("rev-parse", "origin/"+gittest.DefaultBranch) != mainBefore {
		t.Fatal("main must never move")
	}
	if g.scratchLeft() != 0 {
		t.Fatal("scratch worktree was not discarded")
	}

	// Once main is in, a second sync changes nothing and pushes nothing.
	res, out, err = g.syncBase()
	if err != nil || res.Outcome != "current" || g.remoteFeature() != head {
		t.Fatalf("an up-to-date branch: %s %+v %v", res.Outcome, out, err)
	}
}

func TestSyncBaseCurrentWhenMainIsAlreadyInPushesNothing(t *testing.T) {
	g := newSyncGit(t)
	g.advance(implementFeature, "story.txt", "story work\n")
	before := g.remoteFeature()
	pushes := 0
	g.bindings.Repo = &countingRepo{ImplementRepo: g.bindings.Repo, pushes: &pushes}
	res, _, err := g.syncBase()
	if err != nil || res.Outcome != "current" || g.remoteFeature() != before || pushes != 0 {
		t.Fatalf("%s %v pushes=%d", res.Outcome, err, pushes)
	}
}

func TestSyncBaseBusyLockRetriesAndChangesNothing(t *testing.T) {
	g := newSyncGit(t)
	g.advance(gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	before := g.remoteFeature()
	unlock, err := g.db.LockStoryProgress(t.Context(), implementParent.String(), g.lockDir)
	if err != nil {
		t.Fatal(err)
	}
	res, out, err := g.syncBase()
	if unlockErr := unlock(); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	if err != nil || res.Outcome != "retry" || out.Reason == "" || g.remoteFeature() != before {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if res, _, err = g.syncBase(); err != nil || res.Outcome != "synced" {
		t.Fatalf("after the lock is free: %s %v", res.Outcome, err)
	}
}

func TestSyncBaseMovedHeadRetriesAndChangesNothing(t *testing.T) {
	g := newSyncGit(t)
	g.advance(gittest.DefaultBranch, "fix.txt", "fixed on main\n")
	var rival string
	g.bindings.Repo = &movingRepo{ImplementRepo: g.bindings.Repo, advance: func() {
		rival = g.advance(implementFeature, "rival.txt", "rival\n")
	}}
	res, out, err := g.syncBase()
	if err != nil || res.Outcome != "retry" || rival == "" || g.remoteFeature() != rival {
		t.Fatalf("%s %+v %v (rival %s, head %s)", res.Outcome, out, err, rival, g.remoteFeature())
	}
	if g.scratchLeft() != 0 {
		t.Fatal("scratch worktree was not discarded")
	}
}

func TestSyncBaseConflictNamesThePathsAndLeavesTheFeatureBranch(t *testing.T) {
	g := newSyncGit(t)
	g.advance(implementFeature, "shared.txt", "feature version\n")
	g.advance(gittest.DefaultBranch, "shared.txt", "main version\n")
	before := g.remoteFeature()
	res, out, err := g.syncBase()
	if err != nil || res.Outcome != "conflict" {
		t.Fatalf("%s %+v %v", res.Outcome, out, err)
	}
	if !slices.Equal(out.Conflicts, []string{"shared.txt"}) {
		t.Fatalf("conflicts = %v", out.Conflicts)
	}
	if g.remoteFeature() != before || g.remoteFile("shared.txt") != "feature version" {
		t.Fatal("a conflict must leave the feature branch unchanged")
	}
	if g.scratchLeft() != 0 {
		t.Fatal("scratch worktree was not discarded")
	}
}

// TestSyncBaseBindsAnyWorkingBranchUnderAnyMode registers the generic step for
// another mode with its own binding: nothing in it is tied to Implement.
func TestSyncBaseBindsAnyWorkingBranchUnderAnyMode(t *testing.T) {
	repo := &implementRepoFake{merged: true, path: t.TempDir()}
	locked := 0
	binding := SyncBaseBinding{Repo: repo, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
		return SyncBaseTarget{Working: "lf/7-spike", Base: "trunk", Lock: func(context.Context) (func() error, bool, error) {
			locked++
			return func() error { return nil }, true, nil
		}}, nil
	}}
	var specs graphexec.Registry
	var activities ActivityRegistry
	if err := RegisterSyncBase(&specs, &activities, agent.ModeSpike, binding); err != nil {
		t.Fatal(err)
	}
	spec, ok := specs.Resolve(graphexec.SyncBaseAction)
	if !ok || spec.Mode != agent.ModeSpike || !slices.Equal(spec.Outcomes, []string{"synced", "current", "conflict", "retry", "deferred"}) {
		t.Fatalf("%+v %v", spec, ok)
	}
	res, err := SyncBaseActivity{binding}.Execute(t.Context(), Options{}, ActivityRequest{Mode: agent.ModeSpike})
	if err != nil || res.Outcome != "synced" || locked != 1 {
		t.Fatalf("%s %v locked=%d", res.Outcome, err, locked)
	}
	if !slices.Equal(repo.merges, []string{"trunk"}) || !slices.Equal(repo.pushes, []string{"lf/7-spike@" + fakeIntegrationHead}) || repo.closed != 1 {
		t.Fatalf("merges=%v pushes=%v closed=%d", repo.merges, repo.pushes, repo.closed)
	}
	if err := RegisterSyncBase(&specs, &activities, agent.ModeSpike, SyncBaseBinding{Repo: repo}); err == nil {
		t.Fatal("a binding without bind must be refused")
	}
}

type countingRepo struct {
	ImplementRepo
	pushes *int
}

func (r *countingRepo) Begin(ctx context.Context, branch string) (ImplementScratch, error) {
	s, err := r.ImplementRepo.Begin(ctx, branch)
	if err != nil {
		return nil, err
	}
	return &countingScratch{ImplementScratch: s, pushes: r.pushes}, nil
}

type countingScratch struct {
	ImplementScratch
	pushes *int
}

func (s *countingScratch) Publish(ctx context.Context, branch, expected string) error {
	*s.pushes++
	return s.ImplementScratch.Publish(ctx, branch, expected)
}

func TestPreWorkSyncConflictDefersWithoutPublishing(t *testing.T) {
	g := newSyncGit(t)
	g.advance(implementFeature, "shared.txt", "feature\n")
	g.advance(gittest.DefaultBranch, "shared.txt", "base\n")
	before := g.remoteFeature()
	req := g.req()
	req.Attrs = map[string]string{"lf_outcomes": "synced,current,conflict,retry,deferred"}
	res, err := (SyncBaseActivity{SyncBaseBinding{Repo: g.bindings.Repo, Bind: g.bindings.implementSyncBase}}).Execute(t.Context(), Options{}, req)
	if err != nil || res.Outcome != "deferred" {
		t.Fatalf("%+v %v", res, err)
	}
	var out syncBaseOutput
	mustProgress(t, json.Unmarshal(res.Output, &out))
	if out.Reason == "" || len(out.Conflicts) != 1 || out.Conflicts[0] != "shared.txt" || g.remoteFeature() != before || g.scratchLeft() != 0 {
		t.Fatalf("deferral lost evidence or mutated source: %+v", out)
	}
}

func TestPreWorkCurrentRetainedCheckoutStaysQuietAndUnchanged(t *testing.T) {
	for _, scenario := range []string{"dirty current", "ahead current", "absent remote current", "dirty behind"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, nil)
			dir := seedTaskWorktree(t, f)
			_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, testBranch)
			mustProgress(t, err)
			mustProgress(t, f.db.SetSessionWorktree(t.Context(), f.session.ID, dir))
			session := f.reloadSession()
			if scenario != "absent remote current" {
				f.repo.GitIn(dir, "push", "origin", testBranch)
			}
			if scenario == "ahead current" || scenario == "absent remote current" {
				commitInSyncDir(t, f.repo, dir, "local.txt", "local authorized work")
			}
			if scenario == "dirty current" || scenario == "dirty behind" {
				mustProgress(t, os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("retained edits"), 0600))
			}
			if scenario == "dirty behind" {
				advanceSyncBase(t, f.repo, 1)
			}
			clone, err := f.opts.Git.Open(t.Context(), f.repo.Dir)
			mustProgress(t, err)
			mustProgress(t, clone.Fetch(t.Context(), "origin"))
			beforeRemote := f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch)
			checkout := sessionSyncCheckout{runtime: &BugRuntime{opts: f.opts}, session: session}
			target := SyncBaseTarget{Working: testBranch, Base: gittest.DefaultBranch, Checkout: checkout, Lock: func(context.Context) (func() error, bool, error) { return func() error { return nil }, true, nil }}
			if scenario == "absent remote current" {
				target.PreparationReason = "retained working branch has unpublished history; preserve it before retrying base synchronization"
			}
			binding := SyncBaseBinding{Repo: GitImplementRepo{Clone: clone, Remote: "origin", ScratchRoot: t.TempDir(), Author: syncTestAuthor}, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) { return target, nil }}
			var result ActivityResult
			gittest.AssertUnchanged(t, dir, func() {
				result, err = (SyncBaseActivity{binding}).Execute(t.Context(), f.opts, ActivityRequest{Attrs: map[string]string{"lf_outcomes": "synced, current, conflict, retry, deferred"}})
			})
			mustProgress(t, err)
			var evidence syncBaseOutput
			mustProgress(t, json.Unmarshal(result.Output, &evidence))
			if scenario == "dirty behind" {
				if result.Outcome != "deferred" || evidence.Reason == "" {
					t.Fatalf("stale dirty checkout was hidden: %+v", evidence)
				}
			} else if result.Outcome != "current" || evidence.Reason != "" || !evidence.CheckoutVerified || !evidence.CheckoutUnchanged {
				t.Fatalf("healthy retained checkout warned or refreshed: %+v", evidence)
			}
			if after := f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch); after != beforeRemote {
				t.Fatal("observing preparation published retained work")
			}
			if scenario == "dirty current" || scenario == "dirty behind" {
				body, readErr := os.ReadFile(filepath.Join(dir, "retained.txt"))
				mustProgress(t, readErr)
				if string(body) != "retained edits" {
					t.Fatal("retained edits changed")
				}
			}
		})
	}
}
