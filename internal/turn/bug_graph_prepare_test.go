package turn

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestGraphSourcePreparationPolicyPreservationAndCheckpointRestart(t *testing.T) {
	for _, variant := range []string{"synchronize", "disabled", "partial index", "lost checkpoint acknowledgment", "conflict", "remote task dirty"} {
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
			file := "retained.txt"
			if variant == "conflict" {
				file = "README.md"
			}
			mustProgress(t, os.WriteFile(filepath.Join(w.Path(), file), []byte("retained unfinished repair\n"), 0600))
			if variant == "partial index" {
				f.repo.GitIn(w.Path(), "add", file)
				mustProgress(t, os.WriteFile(filepath.Join(w.Path(), file), []byte("different unstaged repair\n"), 0600))
			}
			original, e := w.ObserveSource(t.Context(), git.SourceOptions{})
			mustProgress(t, e)
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
			if variant == "lost checkpoint acknowledgment" {
				checkpoint := graphSourceCheckpoint{Record: "source_checkpoint", State: "pending", Operation: "restart-fixture", Original: original, Options: git.SourceOptions{}, Commit: git.CommitOptions{Summary: "Unvalidated recovery checkpoint restart-fixture", Task: f.session.TaskRef, Workflow: "bug", Run: 1, Author: f.opts.Author}}
				raw, e := json.Marshal(checkpoint)
				mustProgress(t, e)
				_, e = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
				mustProgress(t, e)
				_, e = w.CheckpointSource(t.Context(), original, git.SourceOptions{}, checkpoint.Commit)
				mustProgress(t, e)
				mustProgress(t, f.db.Close())
				reopened, e := store.Open(t.Context(), f.paths.DBPath())
				mustProgress(t, e)
				mustProgress(t, reopened.Migrate(t.Context()))
				t.Cleanup(func() {
					if e := reopened.Close(); e != nil {
						t.Error(e)
					}
				})
				f.db = reopened
				f.opts.DB = reopened
			}
			index := f.repo.GitIn(w.Path(), "rev-parse", "--git-path", "index")
			indexBefore, e := os.ReadFile(index)
			mustProgress(t, e)
			out, e := PrepareGraphBugSource(t.Context(), f.opts, request, activity, w, f.storeRepo, f.opts.Author, f.opts.Logger, at, git.SourceOptions{})
			if variant == "partial index" || variant == "conflict" {
				if e == nil {
					t.Fatalf("unsafe preparation passed: %+v", out)
				}
				if variant == "partial index" {
					after, e := os.ReadFile(index)
					mustProgress(t, e)
					head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
					mustProgress(t, e)
					if !bytes.Equal(indexBefore, after) || head != original.Head {
						t.Fatal("unsafe preparation flattened dirty work")
					}
				}
				return
			}
			mustProgress(t, e)
			retained, e := os.ReadFile(filepath.Join(w.Path(), file))
			mustProgress(t, e)
			if string(retained) != "retained unfinished repair\n" {
				t.Fatal("synchronization lost retained edits")
			}
			if variant == "disabled" {
				if out.SyncEnabled || out.SyncOutcome != "skipped" || out.Source.Head != original.Head || out.SyncReason == "" {
					t.Fatalf("disabled policy misrepresented: %+v", out)
				}
			} else {
				if variant == "remote task dirty" {
					if out.TaskSync.Outcome != "synced" || out.SyncOutcome != "up_to_date" {
						t.Fatalf("remote task was not reconciled after preserving dirty work: %+v", out)
					}
					remoteFile, e := os.ReadFile(filepath.Join(w.Path(), "main-change.txt"))
					mustProgress(t, e)
					if string(remoteFile) != "new main\n" {
						t.Fatal("remote task history missing")
					}
				} else if out.SyncOutcome != "synced" || out.SyncCommit == "" {
					t.Fatalf("sync=%+v", out)
				}
				raw, e := f.db.LatestGraphRecord(t.Context(), inst.ID, "source_checkpoint")
				mustProgress(t, e)
				var checkpoint graphSourceCheckpoint
				mustProgress(t, json.Unmarshal(raw, &checkpoint))
				if checkpoint.State != "confirmed" || checkpoint.SavedHead == "" {
					t.Fatalf("checkpoint=%+v", checkpoint)
				}
				parent := f.repo.GitIn(w.Path(), "rev-parse", checkpoint.SavedHead+"^")
				if parent != original.Head {
					t.Fatal("checkpoint duplicated during recovery")
				}
			}
			raw, e := f.db.LatestGraphRecord(t.Context(), inst.ID, "source_preparation")
			mustProgress(t, e)
			if len(raw) == 0 {
				t.Fatal("source facts not retained")
			}
		})
	}
}
