package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestPinnedEvidenceLiteralPathsAndSafety(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("literal[1].txt", "literal needle\n")
	repo.WriteFile("literal1.txt", "other needle\n")
	head := repo.Commit("literal paths")
	clone := openFixture(t, repo)
	matches, err := clone.PinnedGrep(t.Context(), head, "needle", "literal[1].txt", false, 10, 4096)
	if err != nil || !strings.Contains(matches, "literal needle") || strings.Contains(matches, "other needle") {
		t.Fatalf("literal matches %q %v", matches, err)
	}
	for _, path := range []string{"/absolute", "a\\b", ":(glob)*", "a\tb", "a\x7fb", "a/../b"} {
		if _, _, err := clone.PinnedRead(t.Context(), head, path, 4096); err == nil {
			t.Errorf("unsafe path accepted: %q", path)
		}
	}
	for _, key := range []string{"remote.origin.promisor", "extensions.partialclone"} {
		repo.Git("config", key, "true")
		if _, _, err := clone.PinnedRead(t.Context(), head, "literal[1].txt", 4096); !errors.Is(err, git.ErrPinnedSourceUnavailable) {
			t.Fatalf("%s source not refused: %v", key, err)
		}
		repo.Git("config", "--unset", key)
	}
	if err := os.Symlink("/outside/secret", filepath.Join(repo.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	linkHead := repo.Commit("committed symlink")
	text, dir, err := clone.PinnedRead(t.Context(), linkHead, "link", 4096)
	if err != nil || dir || text != "/outside/secret" {
		t.Fatalf("symlink entry %q %v %v", text, dir, err)
	}
	if _, _, err = clone.PinnedRead(t.Context(), linkHead, "link/child", 4096); err == nil {
		t.Fatal("symlink traversal accepted")
	}
}

func TestPinnedEvidenceHistoryAndDiffModes(t *testing.T) {
	repo := gittest.New(t)
	clone := openFixture(t, repo)
	root := repo.Git("rev-list", "--max-parents=0", "HEAD")
	change, err := git.PinnedDiff(t.Context(), clone, root, "", root, "", 64<<10)
	if err != nil || change.From == "" || change.To != root || !strings.Contains(change.Semantics, "empty tree") {
		t.Fatalf("root diff %+v %v", change, err)
	}
	repo.WriteFile("shared.txt", "base\n")
	base := repo.Commit("base")
	repo.Git("checkout", "-b", "topic")
	repo.WriteFile("topic.txt", "topic\n")
	topic := repo.Commit("topic")
	repo.Git("checkout", "main")
	repo.WriteFile("main.txt", "main\n")
	main := repo.Commit("main")
	repo.Git("merge", "--no-ff", "topic", "-m", "merge topic")
	head := repo.Git("rev-parse", "HEAD")
	log, err := git.PinnedHistory(t.Context(), clone, head, "", "", 10, 64<<10)
	if err != nil || len(log) < 3 || log[0].ID != head || len(log[0].Parents) != 2 || log[0].Parents[0] != main {
		t.Fatalf("full history %+v %v", log, err)
	}
	change, err = git.PinnedDiff(t.Context(), clone, head, "", head, "", 64<<10)
	if err != nil || change.From != main || len(change.Parents) != 2 || !strings.Contains(change.Text, "topic.txt") || strings.Contains(change.Text, "main.txt") {
		t.Fatalf("first parent diff %+v %v", change, err)
	}
	change, err = git.PinnedDiff(t.Context(), clone, head, base, topic, "", 64<<10)
	if err != nil || change.From != base || change.To != topic || !strings.Contains(change.Text, "topic.txt") {
		t.Fatalf("direct endpoints %+v %v", change, err)
	}
	repo.Git("checkout", "--orphan", "unrelated")
	repo.Git("rm", "-rf", ".")
	repo.WriteFile("foreign.txt", "foreign\n")
	foreign := repo.Commit("unrelated")
	if _, err = git.PinnedDiff(t.Context(), clone, head, "", foreign, "", 64<<10); !errors.Is(err, git.ErrRevisionNotFound) {
		t.Fatalf("unrelated commit accepted: %v", err)
	}
	resolved, err := git.ResolvePinnedRef(t.Context(), clone, "refs/heads/main")
	if err != nil || resolved != head {
		t.Fatalf("full ref %q %v", resolved, err)
	}
	if _, err = git.ResolvePinnedRef(t.Context(), clone, "HEAD"); err == nil {
		t.Fatal("arbitrary revision accepted")
	}
}

func TestPinnedEvidenceDisablesReplacementAndDiffHelpers(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("file.txt", "original\n")
	original := repo.Commit("original")
	repo.WriteFile("file.txt", "replacement\n")
	replacement := repo.Commit("replacement")
	repo.Git("replace", original, replacement)
	clone := openFixture(t, repo)
	text, _, err := clone.PinnedRead(t.Context(), original, "file.txt", 4096)
	if err != nil || text != "original\n" {
		t.Fatalf("replacement observed %q %v", text, err)
	}
	repo.Git("replace", "-d", original)
	repo.WriteFile(".gitattributes", "*.txt diff=hostile\n")
	head := repo.Commit("attributes")
	repo.Git("config", "diff.hostile.textconv", "false")
	repo.Git("config", "diff.external", "false")
	change, err := git.PinnedDiff(t.Context(), clone, head, original, head, "file.txt", 64<<10)
	if err != nil || !strings.Contains(change.Text, "replacement") {
		t.Fatalf("helpers affected diff %+v %v", change, err)
	}
	repo.Git("update-index", "--add", "--cacheinfo", "160000,"+head+",module")
	repo.Git("commit", "-m", "submodule entry")
	module := repo.Git("rev-parse", "HEAD")
	if _, _, err = clone.PinnedRead(t.Context(), module, "module", 4096); !errors.Is(err, git.ErrPinnedUnsupported) {
		t.Fatalf("submodule not named: %v", err)
	}
}

func TestPinnedEvidenceDoesNotVerifySignaturesOrUseDiffFormattingConfig(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("file.txt", "before\n")
	before := repo.Commit("before")
	repo.WriteFile("file.txt", "after\n")
	after := repo.Commit("after")
	clone := openFixture(t, repo)
	expected, err := git.PinnedDiff(t.Context(), clone, after, before, after, "", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	repo.Git("config", "diff.context", "100")
	repo.Git("config", "diff.algorithm", "histogram")
	repo.Git("config", "diff.noprefix", "true")
	repo.Git("config", "core.abbrev", "4")
	actual, err := git.PinnedDiff(t.Context(), clone, after, before, after, "", 64<<10)
	if err != nil || actual.Text != expected.Text {
		t.Fatalf("formatting config changed evidence: %+v %v", actual, err)
	}
	marker := filepath.Join(t.TempDir(), "verifier-ran")
	helper := filepath.Join(t.TempDir(), "verifier")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\necho invoked > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	repo.Git("config", "gpg.program", helper)
	repo.Git("config", "log.showSignature", "true")
	tree := repo.Git("rev-parse", after+"^{tree}")
	raw := "tree " + tree + "\nparent " + after + "\nauthor Fixture <fixture@example.com> 1700000000 +0000\ncommitter Fixture <fixture@example.com> 1700000000 +0000\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n fake\n -----END PGP SIGNATURE-----\n\nSigned evidence fixture\n"
	path := filepath.Join(t.TempDir(), "commit")
	if err = os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	signed := repo.Git("hash-object", "-t", "commit", "-w", path)
	if _, err = git.PinnedHistory(t.Context(), clone, signed, "", "", 10, 64<<10); err != nil {
		t.Fatal(err)
	}
	if _, err = clone.PinnedLog(t.Context(), signed, "", "", 10, 64<<10); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external signature verifier invoked: %v", err)
	}
}

func TestPinnedChangedEntriesExposeCompleteSourceUnits(t *testing.T) {
	repo := gittest.New(t)
	repo.WriteFile("modified.txt", "before\n")
	repo.WriteFile("deleted.txt", "deleted\n")
	repo.WriteFile("rename-before.txt", "rename retained\n")
	repo.WriteFile("binary.dat", "before\x00binary\n")
	before := repo.Commit("before source units")
	repo.WriteFile("modified.txt", "after\n")
	repo.WriteFile("added.txt", "added\n")
	repo.WriteFile("binary.dat", "after\x00binary\n")
	if err := os.Remove(filepath.Join(repo.Dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo.Dir, "rename-before.txt"), filepath.Join(repo.Dir, "rename-after.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("modified.txt", filepath.Join(repo.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	after := repo.Commit("after source units")
	clone := openFixture(t, repo)
	change, err := git.PinnedDiff(t.Context(), clone, after, before, after, "", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]git.PinnedChangedEntry{}
	for _, entry := range change.Entries {
		if len(entry.Before) != 40 || len(entry.After) != 40 {
			t.Fatalf("abbreviated IDs %+v", entry)
		}
		entries[entry.Path] = entry
	}
	if len(entries) != 7 {
		t.Fatalf("entries %+v", entries)
	}
	for _, path := range []string{"added.txt", "rename-after.txt", "link"} {
		if entries[path].BeforeMode != "000000" || entries[path].Before != strings.Repeat("0", 40) {
			t.Fatalf("addition %+v", entries[path])
		}
	}
	for _, path := range []string{"deleted.txt", "rename-before.txt"} {
		if entries[path].AfterMode != "000000" || entries[path].After != strings.Repeat("0", 40) {
			t.Fatalf("deletion %+v", entries[path])
		}
	}
	modified := entries["modified.txt"]
	if modified.Before != repo.Git("rev-parse", before+":modified.txt") || modified.After != repo.Git("rev-parse", after+":modified.txt") {
		t.Fatalf("modified IDs %+v", modified)
	}
	if entries["link"].AfterMode != "120000" {
		t.Fatalf("symlink mode %+v", entries["link"])
	}
	for _, tc := range []struct{ path, want string }{{"modified.txt", "after\n"}, {"binary.dat", "after\x00binary\n"}, {"link", "modified.txt"}} {
		text, err := git.ReadPinnedBlob(t.Context(), clone, entries[tc.path].After, 4096)
		if err != nil || text != tc.want {
			t.Fatalf("complete %s blob %q %v", tc.path, text, err)
		}
	}
	filtered, err := git.PinnedDiff(t.Context(), clone, after, before, after, "modified.txt", 4096)
	if err != nil || len(filtered.Entries) != 1 || filtered.Entries[0].Path != "modified.txt" {
		t.Fatalf("filtered %+v %v", filtered, err)
	}
	root := repo.Git("rev-list", "--max-parents=0", after)
	rootChange, err := git.PinnedDiff(t.Context(), clone, after, "", root, "", 64<<10)
	if err != nil || len(rootChange.Entries) == 0 {
		t.Fatalf("root entries %+v %v", rootChange.Entries, err)
	}
	for _, entry := range rootChange.Entries {
		if entry.BeforeMode != "000000" || entry.Before != strings.Repeat("0", 40) {
			t.Fatalf("root entry %+v", entry)
		}
	}
	if _, err = git.PinnedDiff(t.Context(), clone, after, before, after, "", 1); !errors.Is(err, git.ErrPinnedReadTooLarge) {
		t.Fatalf("raw bound: %v", err)
	}
	repo.Git("replace", modified.Before, modified.After)
	original, err := git.ReadPinnedBlob(t.Context(), clone, modified.Before, 4096)
	if err != nil || original != "before\n" {
		t.Fatalf("blob replacement observed %q %v", original, err)
	}
	if _, err = git.ReadPinnedBlob(t.Context(), clone, modified.After, 1); !errors.Is(err, git.ErrPinnedReadTooLarge) {
		t.Fatalf("blob bound: %v", err)
	}
	if _, err = git.ReadPinnedBlob(t.Context(), clone, "HEAD", 4096); !errors.Is(err, git.ErrInvalidArgument) {
		t.Fatalf("mutable blob selector: %v", err)
	}
}

func TestPinnedSearchPositionsPreservesGitRegexAndLiteralNames(t *testing.T) {
	repo := gittest.New(t)
	names := []string{"普通:名前\".txt", "plain.txt"}
	for _, name := range names {
		repo.WriteFile(name, "no match\nword word\nword WORD\nword word\n")
	}
	head := repo.Commit("regex search source units")
	clone := openFixture(t, repo)
	repo.Git("config", "grep.heading", "true")
	repo.Git("config", "grep.break", "true")
	repo.Git("config", "grep.column", "true")
	matches, acquired, err := git.PinnedSearchPositions(t.Context(), clone, head, `(word) \1|word word`, names[0], false, 2, 4096)
	if err != nil || len(matches) != 1 || matches[0].Path != names[0] || len(matches[0].Lines) != 2 || matches[0].Lines[0] != 2 || matches[0].Lines[1] != 4 || acquired <= 0 {
		t.Fatalf("Git ERE positions %+v acquired=%d %v", matches, acquired, err)
	}
	matches, _, err = git.PinnedSearchPositions(t.Context(), clone, head, `(word) \1|word word`, names[0], true, 2, 4096)
	if err != nil || len(matches) != 1 || len(matches[0].Lines) != 2 || matches[0].Lines[1] != 3 {
		t.Fatalf("case-insensitive positions %+v %v", matches, err)
	}
	matches, _, err = git.PinnedSearchPositions(t.Context(), clone, head, "word word", "", false, 1, 4096)
	if err != nil || len(matches) != 2 {
		t.Fatalf("per-file results %+v %v", matches, err)
	}
	for _, match := range matches {
		if len(match.Lines) != 1 || match.Lines[0] != 2 {
			t.Fatalf("per-file limit %+v", match)
		}
	}
	matches, acquired, err = git.PinnedSearchPositions(t.Context(), clone, head, "absent", "", false, 2, 4096)
	if err != nil || len(matches) != 0 || acquired != 0 {
		t.Fatalf("no match %+v %d %v", matches, acquired, err)
	}
	if _, _, err = git.PinnedSearchPositions(t.Context(), clone, head, "word", "", false, 2, 1); !errors.Is(err, git.ErrPinnedReadTooLarge) {
		t.Fatalf("search acquisition bound %v", err)
	}
	for _, limit := range []int{0, 101} {
		if _, _, err = git.PinnedSearchPositions(t.Context(), clone, head, "word", "", false, limit, 4096); !errors.Is(err, git.ErrInvalidArgument) {
			t.Fatalf("invalid per-file limit %d: %v", limit, err)
		}
	}
}
