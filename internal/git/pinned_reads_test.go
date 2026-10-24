package git_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestPinnedReadsSeeOnlyThePinnedCommit(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("internal/run/limit.go", "package run\n\nconst Limit = 3\n")
	pinned := repo.Commit("Run: raise the attempt limit")
	repo.WriteFile("internal/run/limit.go", "package run\n\nconst Limit = 9\n")
	repo.Commit("Run: later change")
	repo.WriteFile("internal/run/limit.go", "dirty")
	clone := openFixture(t, repo)
	before := repo.Git("status", "--porcelain=v1")

	log, err := clone.PinnedLog(t.Context(), pinned, "internal/run", "attempt", 10, 64<<10)
	if err != nil || !strings.Contains(log, "Run: raise the attempt limit") || !strings.Contains(log, "internal/run/limit.go") || strings.Contains(log, "later change") {
		t.Fatalf("log = %q, %v", log, err)
	}
	matches, err := clone.PinnedGrep(t.Context(), pinned, "Limit = [0-9]", "", false, 5, 64<<10)
	if err != nil || strings.TrimSpace(matches) != "internal/run/limit.go:3:const Limit = 3" {
		t.Fatalf("grep = %q, %v", matches, err)
	}
	if none, err := clone.PinnedGrep(t.Context(), pinned, "no such text", "", true, 5, 64<<10); err != nil || none != "" {
		t.Fatalf("no match = %q, %v", none, err)
	}
	file, dir, err := clone.PinnedRead(t.Context(), pinned, "internal/run/limit.go", 64<<10)
	if err != nil || dir || !strings.Contains(file, "Limit = 3") {
		t.Fatalf("read = %q %v %v", file, dir, err)
	}
	listing, dir, err := clone.PinnedRead(t.Context(), pinned, "internal", 64<<10)
	if err != nil || !dir || listing != "run/" {
		t.Fatalf("directory = %q %v %v", listing, dir, err)
	}
	if _, _, err = clone.PinnedRead(t.Context(), pinned, "internal/run/limit.go", 8); !errors.Is(err, git.ErrPinnedReadTooLarge) {
		t.Fatalf("oversized read: %v", err)
	}
	for _, bad := range []string{"../x", "-x", "a/../b"} {
		if _, _, err = clone.PinnedRead(t.Context(), pinned, bad, 1024); err == nil {
			t.Fatalf("accepted path %q", bad)
		}
	}
	if _, err = clone.PinnedLog(t.Context(), "HEAD", "", "", 1, 1024); err == nil {
		t.Fatal("mutable revision accepted")
	}
	if after := repo.Git("status", "--porcelain=v1"); after != before {
		t.Fatal("pinned reads changed repository state")
	}
}
