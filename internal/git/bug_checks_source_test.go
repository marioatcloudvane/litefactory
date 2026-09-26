package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestBugChecksPinnedRecipeAndSourceObservations(t *testing.T) {
	repo := gittest.New(t)
	const recipe = "version: 1\n# trusted target bytes\n"
	repo.WriteFile(".litefactory/bug-checks.yaml", recipe)
	repo.WriteFile(".gitignore", "generated/\n")
	target := repo.Commit("target recipe")
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, dir := task(t, worktreeRoot(repo), 493, "bug checks")
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	// EnsureWorktree follows the remote base; explicitly bring the fixture to the pinned target.
	repo.GitIn(dir, "checkout", "--detach", target)
	tree := repo.GitIn(dir, "rev-parse", "HEAD^{tree}")
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), target, tree); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "generated"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "generated", "cache"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), target, tree); err != nil {
		t.Fatalf("ignored generated output refused: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, ".litefactory", "bug-checks.yaml"), []byte("branch-selected command"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := result.Worktree.ReadBugCheckRecipe(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != recipe {
		t.Fatalf("recipe selected from branch: %q", got)
	}
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), target, tree); err == nil {
		t.Fatal("tracked source dirt accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, ".litefactory", "bug-checks.yaml"), []byte(recipe), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "new-test.go"), []byte("unexpected input"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), target, tree); err == nil {
		t.Fatal("nonignored untracked input accepted")
	}
	if err = os.Remove(filepath.Join(dir, "new-test.go")); err != nil {
		t.Fatal(err)
	}
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), strings.Repeat("a", 40), tree); err == nil {
		t.Fatal("wrong source commit accepted")
	}
	if err = result.Worktree.ObserveBugCheckSource(t.Context(), target, strings.Repeat("b", 40)); err == nil {
		t.Fatal("wrong source tree accepted")
	}
	for _, identity := range []string{"HEAD", target[:8], target + "^{commit}", strings.Repeat("f", 40)} {
		if _, err = result.Worktree.ReadBugCheckRecipe(t.Context(), identity); err == nil {
			t.Errorf("unresolved/nonexact identity accepted: %q", identity)
		}
	}
}

func TestBugChecksRecipeRejectsSymlinkAndOversizedBlob(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			repo := gittest.New(t)
			switch kind {
			case "symlink":
				if err := os.MkdirAll(filepath.Join(repo.Dir, ".litefactory"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../README.md", filepath.Join(repo.Dir, ".litefactory", "bug-checks.yaml")); err != nil {
					t.Fatal(err)
				}
				repo.Commit("symlink recipe")
			case "oversized":
				repo.WriteFile(".litefactory/bug-checks.yaml", strings.Repeat("x", (1<<20)+1))
				repo.Commit("oversized recipe")
			}
			target := repo.Git("rev-parse", "HEAD")
			binary, err := git.Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			clone, err := binary.Open(t.Context(), repo.Dir)
			if err != nil {
				t.Fatal(err)
			}
			branch, dir := task(t, worktreeRoot(repo), 494, "invalid recipe")
			result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = result.Worktree.ReadBugCheckRecipe(t.Context(), target); err == nil {
				t.Fatalf("%s recipe accepted", kind)
			}
		})
	}
}
