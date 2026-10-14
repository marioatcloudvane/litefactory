package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
)

func TestSessionSyncBaseFetchesFactoryParentBeforeSelectingStoryBase(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()
	// The running story's worker allocation is the factory authority linking
	// this child session to the parent's integration branch.
	seedImplementStory(t, f)
	parent := f.otherSession(900)
	const parentBranch = "lf/900-feature"
	_, err := f.db.FreezeSessionSlug(ctx, parent.ID, "feature", parentBranch)
	mustProgress(t, err)
	f.repo.WriteFile("parent-base.txt", "retained sibling work\n")
	f.repo.Commit("publish integration snapshot")
	parentHead := f.repo.Git("rev-parse", "HEAD")
	f.repo.Git("push", "origin", "HEAD:refs/heads/"+parentBranch)
	f.repo.Git("update-ref", "-d", "refs/remotes/origin/"+parentBranch)
	clone, err := f.opts.Git.Open(ctx, f.repo.Dir)
	mustProgress(t, err)
	if _, err := clone.ShowRef(ctx, "refs/remotes/origin/"+parentBranch); err == nil {
		t.Fatal("fixture unexpectedly has a cached parent branch")
	}
	remote := strings.Fields(f.repo.Git("ls-remote", "origin", "refs/heads/"+parentBranch))
	if len(remote) != 2 || remote[0] != parentHead {
		t.Fatalf("parent remote precondition: %v", remote)
	}
	runtime := BugRuntime{opts: f.opts}
	base, err := runtime.syncBaseBranch(ctx, f.session, f.storeRepo)
	mustProgress(t, err)
	if base != parentBranch {
		t.Fatalf("story base=%q, want factory parent %q", base, parentBranch)
	}
	fetched, err := clone.RevParse(ctx, "refs/remotes/origin/"+parentBranch, git.RevParseOptions{})
	mustProgress(t, err)
	if fetched != parentHead {
		t.Fatalf("fetched parent=%s, want %s", fetched, parentHead)
	}
}
