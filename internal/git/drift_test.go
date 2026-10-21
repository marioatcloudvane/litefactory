package git_test

import (
	"github.com/cloudvane/litefactory/internal/gittest"
	"strings"
	"testing"
)

func TestDriftPinnedComparisonNeverReadsDirtyOrExpandsPathspec(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("docs/[design].md", "before\n")
	base := repo.Commit("base")
	repo.WriteFile("docs/[design].md", "after\n")
	repo.WriteFile("docs/d.md", "other\n")
	head := repo.Commit("head")
	repo.WriteFile("docs/[design].md", "dirty credential canary\n")
	clone := openFixture(t, repo)
	before := repo.Git("status", "--porcelain=v1")
	changes, err := clone.DriftChanges(t.Context(), base, "HEAD", []string{"docs/[design].md"}, 200, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if changes.Base != base || changes.Head != head || len(changes.Changes) != 1 || changes.Changes[0].Path != "docs/[design].md" {
		t.Fatalf("wrong pinned literal comparison: %+v", changes)
	}
	content, err := clone.DriftBlob(t.Context(), changes.Changes[0].NewBlob, 1024)
	if err != nil || content != "after\n" {
		t.Fatalf("read mutable content: %q %v", content, err)
	}
	if repo.Git("status", "--porcelain=v1") != before {
		t.Fatal("comparison changed worktree state")
	}
	if _, err = clone.DriftBlob(t.Context(), changes.Changes[0].NewBlob, 2); err == nil {
		t.Fatal("oversized blob accepted")
	}
	if _, err = clone.DriftChanges(t.Context(), "--output=oops", head, nil, 200, 65536); err == nil {
		t.Fatal("option accepted as endpoint")
	}
}

func TestDriftEnumerationReportsLimitsAndSpecialModes(t *testing.T) {
	repo := gittest.New(t)
	base := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	repo.WriteFile("a", "one")
	repo.WriteFile("b", "two")
	head := repo.Commit("changes")
	clone := openFixture(t, repo)
	changes, err := clone.DriftChanges(t.Context(), base, head, nil, 1, 65536)
	if err != nil || len(changes.Changes) != 1 || !changes.UnknownRemainder {
		t.Fatalf("hidden path limit: %+v %v", changes, err)
	}
	changes, err = clone.DriftChanges(t.Context(), base, head, nil, 200, 1)
	if err == nil || !changes.UnknownRemainder {
		t.Fatal("byte overflow claimed complete enumeration")
	}
}

func TestDriftEndpointBytesCannotDisableOutputBound(t *testing.T) {
	repo := gittest.New(t)
	base := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	repo.WriteFile("architecture.md", "changed")
	head := repo.Commit("change")
	clone := openFixture(t, repo)
	limit := len(base) + len(head) + 2
	changes, err := clone.DriftChanges(t.Context(), base, head, nil, 200, limit)
	if err == nil || !changes.UnknownRemainder || changes.Bytes > limit || len(changes.Changes) != 0 {
		t.Fatalf("endpoint-only allowance disabled the output limit: %+v %v", changes, err)
	}
}
