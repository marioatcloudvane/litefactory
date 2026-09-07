package team

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAddCreatesGitignoreWithLitefactoryEntry covers AC-1's base case: a
// fresh repo with no .gitignore at all gets one created, carrying exactly
// the litefactoryDir ignore rule, the first time a team command touches
// .litefactory (here, via Add).
func TestAddCreatesGitignoreWithLitefactoryEntry(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")

	if _, err := Add(root, []AgentRender{ar}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if countOccurrences(string(data), ".litefactory/") != 1 {
		t.Fatalf(".gitignore = %q, want exactly one .litefactory/ line", data)
	}
}

// TestAddIsIdempotentOnGitignoreAcrossRepeatedRuns is AC-1's headline case:
// .litefactory/ appears exactly once in .gitignore no matter how many times
// team add/update runs against the same worktree.
func TestAddIsIdempotentOnGitignoreAcrossRepeatedRuns(t *testing.T) {
	root := t.TempDir()

	for i := 0; i < 4; i++ {
		ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")
		if _, err := Add(root, []AgentRender{ar}); err != nil {
			t.Fatalf("Add run %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if got := countOccurrences(string(data), ".litefactory/"); got != 1 {
		t.Fatalf(".gitignore contains %d .litefactory/ lines after 4 Add runs, want exactly 1: %q", got, data)
	}
}

// TestAddAppendsGitignoreEntryWithoutDisturbingExistingContent proves the
// append preserves whatever a person already put in .gitignore, rather than
// overwriting the file.
func TestAddAppendsGitignoreEntryWithoutDisturbingExistingContent(t *testing.T) {
	root := t.TempDir()
	existing := "node_modules/\ndist/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(existing), 0o666); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")
	if _, err := Add(root, []AgentRender{ar}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	got := string(data)
	if !contains(got, "node_modules/\n") || !contains(got, "dist/\n") {
		t.Errorf(".gitignore lost pre-existing content: %q", got)
	}
	if countOccurrences(got, ".litefactory/") != 1 {
		t.Errorf(".gitignore = %q, want exactly one .litefactory/ line appended", got)
	}
}

// TestAddDoesNotDuplicateAHandWrittenGitignoreEntry proves a person's own,
// already-present ignore rule for .litefactory is recognized rather than
// getting a redundant second line appended alongside it.
func TestAddDoesNotDuplicateAHandWrittenGitignoreEntry(t *testing.T) {
	root := t.TempDir()
	existing := "# my rules\n.litefactory\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(existing), 0o666); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")
	if _, err := Add(root, []AgentRender{ar}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != existing {
		t.Errorf(".gitignore changed despite an existing hand-written entry already covering it: got %q, want unchanged %q", data, existing)
	}
}

// TestGitignoreEntryAppliesUniformlyAcrossMultipleWorktrees exercises AC-1's
// "in every worktree of the repo" clause: each worktree of a repo keeps its
// own independent .litefactory (US-15) and, by the same mechanism, its own
// independent .gitignore management — running team add against several
// separate roots (standing in for separate worktrees of one repository)
// gives each exactly one .litefactory/ line, entirely independently of what
// happened in any of the others.
func TestGitignoreEntryAppliesUniformlyAcrossMultipleWorktrees(t *testing.T) {
	worktrees := []string{t.TempDir(), t.TempDir(), t.TempDir()}

	for _, wt := range worktrees {
		ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")
		if _, err := Add(wt, []AgentRender{ar}); err != nil {
			t.Fatalf("Add(%s): %v", wt, err)
		}
		// A second run against the same worktree stays idempotent.
		if _, err := Add(wt, []AgentRender{ar}); err != nil {
			t.Fatalf("second Add(%s): %v", wt, err)
		}
	}

	for _, wt := range worktrees {
		data, err := os.ReadFile(filepath.Join(wt, ".gitignore"))
		if err != nil {
			t.Fatalf("read .gitignore in %s: %v", wt, err)
		}
		if got := countOccurrences(string(data), ".litefactory/"); got != 1 {
			t.Errorf("worktree %s: .gitignore has %d .litefactory/ lines, want exactly 1: %q", wt, got, data)
		}
	}
}

// TestSetOverrideAlsoEnsuresGitignoreEntry proves the ignore rule is
// established from every write entry point that creates .litefactory, not
// only Add — SetOverride acquires the same per-repo lock and must trigger
// the same guarantee.
func TestSetOverrideAlsoEnsuresGitignoreEntry(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "local.md")
	if err := os.WriteFile(mdPath, []byte("# local\n"), 0o666); err != nil {
		t.Fatalf("write local.md: %v", err)
	}

	if err := SetOverride(root, "go-architect", "local.md"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if countOccurrences(string(data), ".litefactory/") != 1 {
		t.Fatalf(".gitignore = %q, want exactly one .litefactory/ line after SetOverride", data)
	}
}
