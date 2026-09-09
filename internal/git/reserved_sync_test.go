package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// AC-171-7: reserved transport must survive both safe sync and rejected collisions.
func TestReservedTransportSynchronization(t *testing.T) {
	for _, name := range []string{".litefactory-plan-proposal.json", ".litefactory-story-events/244/000001.json"} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name                             string
				staged, tracked, incoming, clean bool
			}{
				{"untracked", false, false, false, false}, {"staged", true, false, false, false},
				{"incoming untracked collision", false, false, true, false}, {"incoming staged collision", true, false, true, false},
				{"tracked dirty", false, true, false, false}, {"tracked incoming collision", false, true, true, false},
				{"clean tracked incoming collision", false, true, true, true},
				{"clean tracked unchanged", false, true, false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					repo := gittest.New(t)
					if err := os.MkdirAll(filepath.Dir(filepath.Join(repo.Dir, name)), 0700); err != nil {
						t.Fatal(err)
					}
					if tc.tracked {
						if err := os.WriteFile(filepath.Join(repo.Dir, name), []byte("old transport"), 0600); err != nil {
							t.Fatal(err)
						}
						repo.Git("add", name)
						repo.Git("commit", "-qm", "legacy transport")
						repo.Git("push", "-q", "origin", gittest.DefaultBranch)
					}
					w, dir := newTaskWorktree(t, repo, 42, tc.name)
					if tc.incoming {
						if err := os.WriteFile(filepath.Join(repo.Dir, name), []byte("incoming transport"), 0600); err != nil {
							t.Fatal(err)
						}
						repo.Git("add", name)
						repo.Git("commit", "-qm", "incoming transport")
						repo.Git("push", "-q", "origin", gittest.DefaultBranch)
					} else {
						advanceBase(t, repo, 1)
					}
					path := filepath.Join(dir, name)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					retained := "retained proposal bytes"
					if tc.clean {
						retained = "old transport"
					}
					if err := os.WriteFile(path, []byte(retained), 0600); err != nil {
						t.Fatal(err)
					}
					if tc.staged {
						repo.GitIn(dir, "add", name)
					}
					head := repo.GitIn(dir, "rev-parse", "HEAD")
					indexPath := repo.GitIn(dir, "rev-parse", "--git-path", "index")
					if !filepath.IsAbs(indexPath) {
						indexPath = filepath.Join(dir, indexPath)
					}
					index, err := os.ReadFile(indexPath)
					if err != nil {
						t.Fatal(err)
					}
					t.Log(repo.Git("--version"))
					result, mergeErr := w.MergeBase(t.Context(), "origin/"+gittest.DefaultBranch, git.MergeOptions{Author: testAuthor})
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if string(got) != retained {
						t.Fatalf("transport overwritten: %q (merge error %v)", got, mergeErr)
					}
					if mergeErr != nil {
						t.Logf("explicit merge refusal: %v", mergeErr)
						if result.Merged {
							t.Errorf("failed merge reports commit: %+v", result)
						}
						if repo.GitIn(dir, "rev-parse", "HEAD") != head {
							t.Error("failed merge changed HEAD")
						}
						after, err := os.ReadFile(indexPath)
						if err != nil {
							t.Fatal(err)
						}
						if string(index) != string(after) {
							t.Error("failed merge changed raw index")
						}
						return
					}
					if tc.incoming {
						t.Fatal("reserved-path collision silently synchronized")
					}
					if err := os.WriteFile(filepath.Join(dir, "recovered.txt"), []byte("finished"), 0600); err != nil {
						t.Fatal(err)
					}
					saved, err := w.Commit(t.Context(), git.CommitOptions{Summary: "recovery", Task: "github:o/r#42", Mode: "spike", Run: 2, Author: testAuthor})
					if err != nil {
						t.Fatal(err)
					}
					if !saved.Committed {
						t.Fatal("recovery not saved")
					}
					if got := repo.GitIn(dir, "diff-tree", "--no-commit-id", "--name-only", "-r", saved.SHA); got != "recovered.txt" {
						t.Fatalf("transport entered task save: %s", got)
					}
					got, err = os.ReadFile(path)
					if err != nil || string(got) != retained {
						t.Fatalf("save lost transport: %q %v", got, err)
					}
				})
			}
		})
	}
}
