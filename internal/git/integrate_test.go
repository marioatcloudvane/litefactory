package git_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// integrationFixture publishes a feature branch and a story branch that adds a
// file, and returns a detached scratch worktree on the feature head with the
// story merged into it.
func integrationFixture(t *testing.T) (*gittest.Repo, *git.Clone, *git.Worktree, string) {
	t.Helper()
	repo := gittest.New(t)
	repo.Git("checkout", "--quiet", "-b", "feature")
	repo.Git("push", "--quiet", "origin", "feature")
	repo.Git("checkout", "--quiet", "-b", "story")
	repo.WriteFile("story.txt", "story\n")
	repo.Commit("story work")
	repo.Git("push", "--quiet", "origin", "story")
	repo.Git("checkout", "--quiet", gittest.DefaultBranch)
	clone := openFixture(t, repo)
	if err := clone.Fetch(t.Context(), "origin"); err != nil {
		t.Fatal(err)
	}
	head := originHead(repo, "feature")
	scratch, err := clone.WorktreeAdd(t.Context(), filepath.Join(t.TempDir(), "scratch"), git.WorktreeAddOptions{Branch: head, Detach: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scratch.MergeBase(t.Context(), "refs/remotes/origin/story", git.MergeOptions{Author: testAuthor}); err != nil {
		t.Fatal(err)
	}
	return repo, clone, scratch, head
}

// originHead reads the branch tip straight from the bare origin.
func originHead(repo *gittest.Repo, branch string) string {
	out := repo.Git("ls-remote", "--refs", "origin", "refs/heads/"+branch)
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return ""
	}
	return fields[0]
}

func TestPushHeadToPublishesAFastForwardOfTheExpectedHead(t *testing.T) {
	repo, _, scratch, head := integrationFixture(t)
	if err := scratch.PushHeadTo(t.Context(), "origin", "feature", head); err != nil {
		t.Fatalf("PushHeadTo: %v", err)
	}
	want, err := scratch.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := originHead(repo, "feature"); got != want {
		t.Fatalf("remote feature = %q, want %q", got, want)
	}
}

func TestPushHeadToRefusesAMovedHeadWithoutPublishing(t *testing.T) {
	repo, _, scratch, head := integrationFixture(t)
	repo.Git("checkout", "--quiet", "feature")
	repo.WriteFile("other.txt", "other\n")
	moved := repo.Commit("someone else")
	repo.Git("push", "--quiet", "origin", "feature")
	err := scratch.PushHeadTo(t.Context(), "origin", "feature", head)
	if !errors.Is(err, git.ErrIntegrationHeadMoved) {
		t.Fatalf("err = %v", err)
	}
	if got := originHead(repo, "feature"); got != moved {
		t.Fatalf("remote head changed to %s", got)
	}
}

func TestPushHeadToRefusesAHeadThatDoesNotBuildOnTheExpectedOne(t *testing.T) {
	_, _, scratch, _ := integrationFixture(t)
	unrelated := "0123456789abcdef0123456789abcdef01234567"
	if err := scratch.PushHeadTo(t.Context(), "origin", "feature", unrelated); err == nil {
		t.Fatal("expected a refusal")
	}
	if err := scratch.PushHeadTo(t.Context(), "origin", "feature", "main"); !errors.Is(err, git.ErrInvalidArgument) {
		t.Fatalf("short head: %v", err)
	}
}
