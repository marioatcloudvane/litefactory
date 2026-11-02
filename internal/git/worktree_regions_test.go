package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestRegionApplyPreservesCompleteFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, initial, ours, theirs string }{
		{"large summed regions", "data.txt", "prefix\n" + strings.Repeat("o", 5000) + "\n" + strings.Repeat("unchanged\n", 20) + strings.Repeat("o", 5000) + "\ntail", strings.Repeat("o", 5000), strings.Repeat("n", 5000)},
		{"large Go field", "large.go", "package p\n" + strings.Repeat("// untouched context\n", 500) + "type T struct {\n Original bool\n}\n", "Original bool", "Added bool"},
		{"repeated regions", "data.txt", "first\noriginal\n" + strings.Repeat("unchanged\n", 20) + "original\nlast", "original", "changed"},
		{"CRLF", "data.txt", "prefix\r\noriginal\r\nsuffix\r\n", "original", "changed"},
		{"no final newline", "data.txt", "prefix\noriginal\nsuffix", "original", "changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			if err := os.WriteFile(filepath.Join(repo.Dir, tc.path), []byte(tc.initial), 0600); err != nil {
				t.Fatal(err)
			}
			repo.Git("add", "--", tc.path)
			repo.Git("commit", "-m", "seed source")
			repo.Git("push", "origin", gittest.DefaultBranch)
			w, dir := newTaskWorktree(t, repo, 42, "Region preservation")
			baseline := strings.ReplaceAll(tc.initial, tc.ours, tc.theirs+"Task")
			baseContent := strings.ReplaceAll(tc.initial, tc.ours, tc.theirs+"Base")
			if strings.HasSuffix(tc.path, ".go") {
				baseline = strings.ReplaceAll(tc.initial, tc.ours, "Task bool")
				baseContent = strings.ReplaceAll(tc.initial, tc.ours, "Base bool")
			}
			writeAndCommitBoth(t, repo, dir, tc.path, baseContent, baseline)
			base := "origin/" + gittest.DefaultBranch
			branchSHA := repo.GitIn(dir, "rev-parse", "HEAD")
			baseSHA := repo.GitIn(dir, "rev-parse", base)
			preview, err := w.MergePreview(t.Context(), base)
			if err != nil {
				t.Fatal(err)
			}
			snapshots, err := w.CollectConflictSnapshots(t.Context(), preview.TreeSHA, branchSHA, baseSHA, preview.Conflicts, 1<<16)
			if err != nil {
				t.Fatal(err)
			}
			var files []git.ResolvedFile
			for _, s := range snapshots {
				var resolutions []git.RegionResolution
				for _, r := range s.Regions {
					resolutions = append(resolutions, git.RegionResolution{ID: r.ID, Resolution: r.Ours})
				}
				v, err := git.ValidateConflict(s, resolutions)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, git.ResolvedFile{Path: s.Path, Format: 1, BranchSHA: branchSHA, BaseSHA: baseSHA, ContentLimit: 1 << 16, Validated: v})
			}
			if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err != nil {
				t.Fatal(err)
			}
			if err := w.StageResolvedFiles(t.Context(), files); err != nil {
				t.Fatal(err)
			}
			opts := completeMergeOptions(42)
			opts.Files = files
			if _, err := w.CompleteMerge(t.Context(), opts); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dir, tc.path))
			if err != nil || string(got) != baseline {
				t.Fatalf("outside bytes lost: got %q error %v", got, err)
			}
			if mergeInProgress(t, dir) || repo.GitIn(dir, "status", "--porcelain") != "" {
				t.Fatal("merge not completed cleanly")
			}
		})
	}
}

func TestRegionApplyRefusesTamperedProposalBeforeMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*git.ResolvedFile)
	}{
		{"legacy", func(f *git.ResolvedFile) { f.Format = 0 }},
		{"unknown format", func(f *git.ResolvedFile) { f.Format = 999 }},
		{"original digest", func(f *git.ResolvedFile) { f.Validated.OriginalDigest = "bad" }},
		{"result digest", func(f *git.ResolvedFile) { f.Validated.ResultDigest = "bad" }},
		{"metrics", func(f *git.ResolvedFile) { f.Validated.Metrics.BeforeBytes++ }},
		{"resolution", func(f *git.ResolvedFile) { f.Validated.Regions[0].Resolution = "different result\n" }},
		{"region identity", func(f *git.ResolvedFile) { f.Validated.Regions[0].ID = "bad" }},
		{"mode", func(f *git.ResolvedFile) { f.Validated.Mode = "100755" }},
		{"stale", func(f *git.ResolvedFile) { f.BaseSHA = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			w, dir := newTaskWorktree(t, repo, 42, "Tampered proposal")
			conflictOnSharedFile(t, repo, dir)
			base := "origin/" + gittest.DefaultBranch
			files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
			tc.mutate(&files[0])
			gittest.AssertUnchanged(t, dir, func() {
				if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err == nil {
					t.Fatal("tampered proposal accepted")
				}
			})
			if mergeInProgress(t, dir) {
				t.Fatal("MERGE_HEAD left by refusal")
			}
		})
	}
}

func TestRegionApplyStagedTransformationRollsBack(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 42, "Staged transformation")
	conflictOnSharedFile(t, repo, dir)
	base := "origin/" + gittest.DefaultBranch
	files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
	// A clean filter transforms the index without changing the worktree.
	repo.GitIn(dir, "config", "filter.truncate.clean", "sed s/resolved/changed/g")
	attributes := filepath.Join(repo.Dir, ".git", "info", "attributes")
	if err := os.WriteFile(attributes, []byte("README.md filter=truncate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := repo.GitIn(dir, "rev-parse", "HEAD")
	original, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := w.StageResolvedFiles(t.Context(), files); err == nil {
		t.Fatal("staged transformation accepted")
	}
	if repo.GitIn(dir, "rev-parse", "HEAD") != before || mergeInProgress(t, dir) {
		t.Fatal("HEAD/merge state not restored")
	}
	restored, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil || string(restored) != string(original) {
		t.Fatal("tracked bytes not restored")
	}
	if repo.GitIn(dir, "diff", "--cached", "--name-only") != "" {
		t.Fatal("index not restored")
	}
}

func TestRegionFinalBarrierRefusesMissingEvidenceAndIndexMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "index mode", "written bytes"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			w, dir := newTaskWorktree(t, repo, 42, "Final barrier")
			conflictOnSharedFile(t, repo, dir)
			base := "origin/" + gittest.DefaultBranch
			files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
			before := repo.GitIn(dir, "rev-parse", "HEAD")
			original, err := os.ReadFile(filepath.Join(dir, "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err != nil {
				t.Fatal(err)
			}
			if err := w.StageResolvedFiles(t.Context(), files); err != nil {
				t.Fatal(err)
			}
			opts := completeMergeOptions(42)
			opts.Files = files
			switch mode {
			case "missing":
				opts.Files = nil
			case "index mode":
				repo.GitIn(dir, "update-index", "--chmod=+x", "--", "README.md")
			case "written bytes":
				if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed after staging\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, applyErr := w.CompleteMerge(t.Context(), opts)
			if applyErr == nil {
				t.Fatal("final barrier accepted changed or absent evidence")
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != before {
				t.Fatal("refused final barrier committed")
			}
			// Externally changing staged modes/worktree bytes can prevent Git's
			// abort; S-5 permits no reset-based self-heal. Report that truthfully.
			if mode != "missing" && mergeInProgress(t, dir) {
				if !strings.Contains(applyErr.Error(), "aborting the in-progress merge also failed") {
					t.Fatalf("failed restoration hidden: %v", applyErr)
				}
				return
			}
			if repo.GitIn(dir, "rev-parse", "HEAD") != before || mergeInProgress(t, dir) {
				t.Fatal("HEAD/MERGE_HEAD not restored")
			}
			restored, err := os.ReadFile(filepath.Join(dir, "README.md"))
			if err != nil || string(restored) != string(original) {
				t.Fatal("tracked bytes not restored")
			}
			if repo.GitIn(dir, "diff", "--cached", "--name-only") != "" {
				t.Fatal("index not restored")
			}
		})
	}
}

func TestRegionCleanupFailureReportedHonestly(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 42, "Failed cleanup")
	conflictOnSharedFile(t, repo, dir)
	base := "origin/" + gittest.DefaultBranch
	files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
	before := repo.GitIn(dir, "rev-parse", "HEAD")
	if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err != nil {
		t.Fatal(err)
	}
	indexPath := repo.GitIn(dir, "rev-parse", "--git-path", "index.lock")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(dir, indexPath)
	}
	if err := os.WriteFile(indexPath, []byte("fixture lock"), 0600); err != nil {
		t.Fatal(err)
	}
	err := w.StageResolvedFiles(t.Context(), files)
	if err == nil || !strings.Contains(err.Error(), "aborting the in-progress merge also failed") {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
	if repo.GitIn(dir, "rev-parse", "HEAD") != before {
		t.Fatal("commit happened on failed stage")
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
}

func TestRegionCanonicalSameSHATransformationRefuses(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 42, "Changed canonical merge")
	conflictOnSharedFile(t, repo, dir)
	base := "origin/" + gittest.DefaultBranch
	files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
	repo.GitIn(dir, "config", "merge.conflictStyle", "diff3")
	gittest.AssertUnchanged(t, dir, func() {
		if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err == nil {
			t.Fatal("same-SHA canonical transformation accepted")
		}
	})
}

func TestRegionResultMayExceedOriginalReadBound(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	prefix := strings.Repeat("unchanged context\n", 50000)
	if err := os.WriteFile(filepath.Join(repo.Dir, "large.txt"), []byte(prefix+"initial\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "large.txt")
	repo.Git("commit", "-m", "large original")
	repo.Git("push", "origin", gittest.DefaultBranch)
	w, dir := newTaskWorktree(t, repo, 42, "Bounded larger result")
	writeAndCommitBoth(t, repo, dir, "large.txt", prefix+"base\n", prefix+"task\n")
	result := prefix + strings.Repeat("generated region text", 12000) + "\n"
	if len(result) <= git.MaxConflictOriginalBytes {
		t.Fatal("fixture must exceed original read cap")
	}
	base := "origin/" + gittest.DefaultBranch
	files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "large.txt", Resolution: result}})
	if err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor); err != nil {
		t.Fatal(err)
	}
	if err := w.StageResolvedFiles(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	opts := completeMergeOptions(42)
	opts.Files = files
	if _, err := w.CompleteMerge(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(dir, "large.txt"))
	if err != nil || string(actual) != result {
		t.Fatal("valid larger result changed")
	}
}

func TestRegionActualMergeByteTransformationRefuses(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 42, "Actual merge transform")
	conflictOnSharedFile(t, repo, dir)
	base := "origin/" + gittest.DefaultBranch
	files := regionFiles(t, w, base, []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}})
	// This is a local fixture filter, not a validator subprocess. merge-tree
	// remains canonical; checkout smudges the actual conflicted worktree file.
	repo.GitIn(dir, "config", "filter.context.smudge", "sed s/fixture/TRANSFORMED/g")
	repo.GitIn(dir, "config", "filter.context.clean", "sed s/TRANSFORMED/fixture/g")
	attributes := filepath.Join(repo.Dir, ".git", "info", "attributes")
	if err := os.WriteFile(attributes, []byte("README.md filter=context\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := repo.GitIn(dir, "rev-parse", "HEAD")
	original, readErr := os.ReadFile(filepath.Join(dir, "README.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	err := w.WriteResolvedFiles(t.Context(), base, files, testAuthor)
	if err == nil || !strings.Contains(err.Error(), "actual merge") {
		t.Fatalf("actual transformation not refused: %v", err)
	}

	restored, readErr := os.ReadFile(filepath.Join(dir, "README.md"))
	if readErr != nil || string(restored) != string(original) {
		if !strings.Contains(err.Error(), "restoration failed") && !strings.Contains(err.Error(), "aborting the in-progress merge also failed") {
			t.Fatalf("changed tracked bytes without failed-restoration disclosure: %v", err)
		}
	}
	if repo.GitIn(dir, "rev-parse", "HEAD") != before {
		t.Fatal("refusal committed")
	}
	if mergeInProgress(t, dir) {
		if !strings.Contains(err.Error(), "aborting the in-progress merge also failed") {
			t.Fatalf("restoration failure hidden: %v", err)
		}
	}
	if repo.GitIn(dir, "diff", "--cached", "--name-only") != "" && !strings.Contains(err.Error(), "aborting the in-progress merge also failed") {
		t.Fatal("restored index claim incorrect")
	}
}
