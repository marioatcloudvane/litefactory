package git_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestCollectConflictSnapshotsBoundsAndCoverage(t *testing.T) {
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 91, "region snapshots")
	conflictOnSharedFile(t, repo, dir)
	preview, err := w.MergePreview(t.Context(), "origin/"+gittest.DefaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(repo.GitIn(dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(repo.Git("rev-parse", "origin/"+gittest.DefaultBranch))
	snapshots, err := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, preview.Conflicts, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].BranchSHA != branch || snapshots[0].Mode != "100644" {
		t.Fatalf("snapshots %+v", snapshots)
	}
	for _, tc := range []struct {
		name  string
		paths []string
		cap   int
	}{{"total cap", preview.Conflicts, 1}, {"missing sibling", []string{"README.md", "missing"}, 4096}, {"duplicate", []string{"README.md", "README.md"}, 4096}, {"100 file limit", make([]string, 101), 4096}} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, tc.paths, tc.cap)
			if e == nil || got != nil {
				t.Fatal("partial/oversized snapshot accepted")
			}
		})
	}
}

func TestCollectConflictSnapshotsOriginalAndRegionBounds(t *testing.T) {
	for _, tc := range []struct{ name, body string }{{"original", strings.Repeat("x", git.MaxConflictOriginalBytes)}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := gittest.New(t)
			w, dir := newTaskWorktree(t, repo, 92, "bounded")
			for i, location := range []string{repo.Dir, dir} {
				content := tc.body + string(rune('a'+i)) + "\n"
				if err := os.WriteFile(filepath.Join(location, "README.md"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				repo.GitIn(location, "add", "README.md")
				repo.GitIn(location, "commit", "-qm", "change")
			}
			base := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
			branch := strings.TrimSpace(repo.GitIn(dir, "rev-parse", "HEAD"))
			preview, err := w.MergePreview(t.Context(), base)
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, preview.Conflicts, 1<<20)
			if err == nil || got != nil {
				t.Fatal("oversized data accepted")
			}
			if !strings.Contains(err.Error(), "original exceeds 1 MiB") || strings.Contains(err.Error(), "at least") {
				t.Fatalf("misleading original limit: %v", err)
			}
		})
	}
}

func TestCollectConflictSnapshotsUnsupportedModesAndTypes(t *testing.T) {
	for _, kind := range []string{"mode", "symlink", "delete", "rename", "binary", "invalid UTF8"} {
		t.Run(kind, func(t *testing.T) {
			repo := gittest.New(t)
			w, dir := newTaskWorktree(t, repo, 93, "unsupported")
			conflictOnSharedFile(t, repo, dir)
			path := filepath.Join(dir, "README.md")
			switch kind {
			case "mode":
				repo.GitIn(dir, "update-index", "--chmod=+x", "README.md")
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", path); err != nil {
					t.Fatal(err)
				}
				repo.GitIn(dir, "add", "README.md")
			case "delete":
				repo.GitIn(dir, "rm", "README.md")
			case "rename":
				repo.GitIn(dir, "mv", "README.md", "renamed.md")
			default:
				content := "\x00"
				if kind == "invalid UTF8" {
					content = "\xff"
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				repo.GitIn(dir, "add", "README.md")
			}
			repo.GitIn(dir, "commit", "-qm", "unsupported change")
			branch := strings.TrimSpace(repo.GitIn(dir, "rev-parse", "HEAD"))
			base := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
			preview, err := w.MergePreview(t.Context(), base)
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, []string{"README.md"}, 4096)
			if err == nil || got != nil {
				t.Fatalf("accepted unsupported %s", kind)
			}
		})
	}
}

func TestCollectConflictSnapshotsConfiguredAggregateBudget(t *testing.T) {
	for _, size := range []int{9000, 270000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			repo := gittest.New(t)
			for _, path := range []string{"first.txt", "second.txt"} {
				if err := os.WriteFile(filepath.Join(repo.Dir, path), []byte("header\ntail\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			repo.Git("add", ".")
			repo.Git("commit", "-qm", "ancestor")
			repo.Git("push", "origin", gittest.DefaultBranch)
			w, dir := newTaskWorktree(t, repo, 94, "budget")
			for i, location := range []string{dir, repo.Dir} {
				for _, path := range []string{"first.txt", "second.txt"} {
					content := "header\n" + strings.Repeat(string(rune('a'+i)), size) + "\ntail\n"
					if err := os.WriteFile(filepath.Join(location, path), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				repo.GitIn(location, "add", ".")
				repo.GitIn(location, "commit", "-qm", "addition")
			}
			branch := repo.GitIn(dir, "rev-parse", "HEAD")
			base := repo.Git("rev-parse", "HEAD")
			preview, err := w.MergePreview(t.Context(), base)
			if err != nil {
				t.Fatal(err)
			}
			// Measure each complete file through the collector, then check the aggregate.
			total := 0
			for _, path := range preview.Conflicts {
				snapshots, e := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, []string{path}, 1<<20)
				if e != nil {
					t.Fatal(e)
				}
				for _, r := range snapshots[0].Regions {
					total += len(r.Content)
				}
				if _, ok := git.ResolveConflictAdditions(snapshots[0]); !ok {
					t.Fatal("interior addition not proven")
				}
			}
			if total <= 1<<20 {
				if _, e := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, preview.Conflicts, total); e != nil {
					t.Fatalf("exact budget: %v", e)
				}
			}
			budget := total - 1
			if total > 1<<20 {
				budget = 1 << 20
			}
			got, e := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branch, base, preview.Conflicts, budget)
			if got != nil || e == nil {
				t.Fatal("aggregate overflow accepted")
			}
			for _, want := range []string{fmt.Sprint(total), fmt.Sprint(budget), "settings.conflict_content_max_bytes"} {
				if !strings.Contains(e.Error(), want) {
					t.Fatalf("missing %q: %v", want, e)
				}
			}
			if total > 1<<20 && (!strings.Contains(e.Error(), "no supported setting suffices") || strings.Contains(e.Error(), "at least")) {
				t.Fatal(e)
			}
		})
	}
}
