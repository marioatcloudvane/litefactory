package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

// Fixer preparation observes both phases without committing retained work.
// The analyst reads the existing worktree without synchronization.
func TestGraphSourcePreparationPreservesThenObserves(t *testing.T) {
	for _, variant := range []string{"synchronize", "disabled", "conflict", "remote task dirty", "analyst"} {
		t.Run(variant, func(t *testing.T) {
			f, _, inst := approvedGraphRepairFixture(t)
			at := f.opts.MCP.Now()
			segment, e := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 2, "prepare-fixer", store.AgentClaude)
			mustProgress(t, e)
			run, e := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 140, StartedAt: at, LogPath: func(int) string { return "prepare.log" }})
			mustProgress(t, e)
			attempt, e := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: inst.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: at})
			mustProgress(t, e)
			binary, e := git.Resolve(t.Context())
			mustProgress(t, e)
			clone, e := binary.Open(t.Context(), f.repo.Dir)
			mustProgress(t, e)
			wresult, e := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: filepath.Join(f.storeRepo.WorktreeRoot, "source-preparation"), Branch: "lf/source-preparation", BaseBranch: gittest.DefaultBranch})
			mustProgress(t, e)
			w := wresult.Worktree
			original, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			mustProgress(t, e)
			file := "retained.txt"
			if variant == "conflict" {
				file = "README.md"
			}
			mustProgress(t, os.WriteFile(filepath.Join(w.Path(), file), []byte("retained unfinished repair\n"), 0600))
			f.repo.WriteFile("main-change.txt", "new main\n")
			if variant == "conflict" {
				f.repo.WriteFile("README.md", "different main content\n")
			}
			f.repo.Commit("advance main fixture")
			if variant == "remote task dirty" {
				f.repo.Git("push", "origin", "HEAD:refs/heads/"+w.Branch())
			} else {
				f.repo.Git("push", "origin", gittest.DefaultBranch)
			}
			mustProgress(t, clone.Fetch(t.Context(), "origin"))
			activity := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: inst.ID, NodeAttemptID: attempt.ID, RunID: run.ID, Node: "fixer"}
			request := f.request()
			request.Workflow = agent.WorkflowBug
			request.Mode = ""
			request.researchInstance = inst.ID
			f.opts.AutoSyncBase = true
			if variant == "disabled" {
				off := false
				request.SyncOverride = &off
			}
			note, e := PrepareGraphBugSource(t.Context(), f.opts, request, activity, w, f.storeRepo, f.opts.Author, f.opts.Logger, at, variant != "analyst")
			mustProgress(t, e)
			head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			mustProgress(t, e)
			if !strings.Contains(note, head) {
				t.Fatalf("note lacks HEAD %s: %q", head, note)
			}
			retained, e := os.ReadFile(filepath.Join(w.Path(), file))
			mustProgress(t, e)
			if string(retained) != "retained unfinished repair\n" {
				t.Fatal("preparation lost retained edits")
			}
			if variant == "analyst" {
				if head != original || !strings.Contains(f.repo.GitIn(w.Path(), "status", "--porcelain"), file) {
					t.Fatal("analyst preparation changed the worktree")
				}
				return
			}
			if head != original || !strings.Contains(f.repo.GitIn(w.Path(), "status", "--porcelain"), file) {
				t.Fatal("fixer observation changed retained edits, index or branch")
			}
			if variant != "disabled" && !strings.Contains(note, "synchronization remains unresolved") {
				t.Fatalf("unresolved freshness not disclosed: %s", note)
			}
			observed, e := f.db.GetRun(t.Context(), run.ID)
			mustProgress(t, e)
			if len(observed.SyncObservations) != 2 {
				t.Fatalf("source observations not durable: %+v", observed)
			}

			if outputs, e := f.db.ListGraphOutputs(t.Context(), attempt.ID); e != nil || len(outputs) != 0 {
				t.Fatalf("source facts were stored in the database: %d outputs, %v", len(outputs), e)
			}
		})
	}
}
