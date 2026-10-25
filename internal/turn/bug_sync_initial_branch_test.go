package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestBugInitialUnpublishedBranchSynchronizesWithoutPublication(t *testing.T) {
	for _, divergent := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean ancestor", true: "unpublished local repair"}[divergent], func(t *testing.T) {
			f, _, profile, instance := conversationSourceFixture(t, true)
			ctx := t.Context()
			specs, err := SimplifiedBugSpecs()
			mustProgress(t, err)
			definition, err := teamsourcetest.CaptureBug(ctx, specs)
			mustProgress(t, err)
			runtime := &BugRuntime{opts: f.opts, definition: definition}
			f.session, err = f.db.FreezeSessionSlug(ctx, f.session.ID, testSlug, testBranch)
			mustProgress(t, err)
			clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
			mustProgress(t, err)
			retained, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "initial-bug-sync"), Branch: testBranch, BaseBranch: gittest.DefaultBranch})
			mustProgress(t, err)
			mustProgress(t, f.db.SetSessionWorktree(ctx, f.session.ID, retained.Worktree.Path()))
			before, err := retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
			mustProgress(t, err)
			if divergent {
				mustProgress(t, os.WriteFile(filepath.Join(retained.Worktree.Path(), "unpublished-repair.txt"), []byte("keep this repair\n"), 0600))
				f.repo.Git("-C", retained.Worktree.Path(), "add", "unpublished-repair.txt")
				f.repo.Git("-C", retained.Worktree.Path(), "commit", "-m", "retained unpublished repair")
				before, err = retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
				mustProgress(t, err)
			}
			if remote := f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch); remote != "" {
				t.Fatalf("working branch already published: %s", remote)
			}
			f.repo.WriteFile("new-base-snapshot.txt", "new selected base\n")
			f.repo.Commit("advance selected base before first fixer")
			f.repo.Git("push", "origin", gittest.DefaultBranch)
			base := f.repo.Git("rev-parse", "HEAD")
			locked, err := f.db.AcquireSessionLock(ctx, f.session.ID, f.opts.InstanceID)
			mustProgress(t, err)
			if !locked {
				t.Fatal("could not acquire factory writer exclusion")
			}
			t.Cleanup(func() {
				mustProgress(t, f.db.ReleaseSessionLock(context.WithoutCancel(ctx), f.session.ID, f.opts.InstanceID))
			})
			gate, ok := graphexec.BugFixerSyncGate(profile)
			if !ok {
				t.Fatal("revised capture lacks declared sync prerequisite")
			}
			result, err := (SyncBaseActivity{runtime.graphSyncBaseBinding(f.session.ID)}).Execute(ctx, f.opts, ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, Node: gate.Name})
			mustProgress(t, err)
			var evidence syncBaseOutput
			mustProgress(t, json.Unmarshal(result.Output, &evidence))
			head, err := retained.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
			mustProgress(t, err)
			if divergent {
				if result.Outcome != "conflict" || evidence.Result != "conflict" || head != before || evidence.CheckoutVerified {
					t.Fatalf("unpublished repair was changed: result=%+v evidence=%+v head=%s want=%s", result, evidence, head, before)
				}
				if evidence.Reason == "" {
					t.Fatal("unpublished repair deferral lacks explanation")
				}
				data, err := os.ReadFile(filepath.Join(retained.Worktree.Path(), "unpublished-repair.txt"))
				mustProgress(t, err)
				if string(data) != "keep this repair\n" {
					t.Fatal("unpublished repair bytes lost")
				}
			} else {
				if result.Outcome != "current" || !evidence.CheckoutVerified || evidence.Commit != head || head == before {
					t.Fatalf("initial checkout not handed off: result=%+v evidence=%+v head=%s", result, evidence, head)
				}
				if ancestor := f.repo.Git("merge-base", base, head); ancestor != base {
					t.Fatalf("checkout lacks fetched snapshot %s: %s", base, head)
				}
				data, err := os.ReadFile(filepath.Join(retained.Worktree.Path(), "new-base-snapshot.txt"))
				mustProgress(t, err)
				if strings.TrimSpace(string(data)) != "new selected base" {
					t.Fatal("base bytes absent from retained checkout")
				}
			}
			if remote := f.repo.Git("ls-remote", "origin", "refs/heads/"+testBranch); remote != "" {
				t.Fatalf("sync unexpectedly published initial branch: %s", remote)
			}
		})
	}
}
