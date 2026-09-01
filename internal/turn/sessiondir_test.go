package turn

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

const (
	testSessionBranch = "lf/123-add-dry-run-to-task-clean"
	testSessionIssue  = 123
)

// writeSession lays down .aiteam/<branch>/<date>/<slug>/<marker> under root and
// returns the repository-relative directory it created.
func writeSession(t *testing.T, root, date, marker string) string {
	t.Helper()

	dir := filepath.Join(
		".aiteam",
		"lf-123-add-dry-run-to-task-clean",
		date,
		"add-dry-run-to-task-clean",
	)
	abs := filepath.Join(root, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(abs, marker), []byte("# brief\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", marker, err)
	}
	return filepath.ToSlash(dir)
}

// TestTheSessionDirectoryIsFoundOnADayTheRunDidNotStart is the bug this helper
// was extracted to fix.
//
// A session directory keeps the date it was named on. Deriving it from the
// current run's start day is right only until a task outlives that day — and
// issue #8 outlived it by eight hours, which was enough: the directory said
// 2026-08-31, the second run started on the 1st, and the report advertised a
// brief at a path that had never been created.
func TestTheSessionDirectoryIsFoundOnADayTheRunDidNotStart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := writeSession(t, root, "2026-08-31", naming.ResearchBriefName)

	// The run starts the next morning, as run 2 of issue #8 did.
	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)

	got, ok := findSessionDir(root, testSessionBranch, testSessionIssue, day, naming.ResearchBriefName, nil)
	if !ok {
		t.Fatal("the session directory was not found from a later day")
	}
	if got != want {
		t.Errorf("found %q, want %q", got, want)
	}
}

// TestTheRunsOwnDayIsPreferred keeps the cheap path honest: when the session
// directory does carry the run's own day, that is the answer, and the scan is
// not what produced it.
func TestTheRunsOwnDayIsPreferred(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSession(t, root, "2026-08-31", naming.ResearchBriefName)
	want := writeSession(t, root, "2026-09-01", naming.ResearchBriefName)

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)

	got, ok := findSessionDir(root, testSessionBranch, testSessionIssue, day, naming.ResearchBriefName, nil)
	if !ok {
		t.Fatal("the session directory named on the run's own day was not found")
	}
	if got != want {
		t.Errorf("found %q, want the run's own day %q", got, want)
	}
}

// TestTheNewestSessionWins is what the scan has to decide when a task has been
// worked more than once and neither directory is the run's own day.
func TestTheNewestSessionWins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSession(t, root, "2026-08-20", naming.ResearchBriefName)
	want := writeSession(t, root, "2026-08-31", naming.ResearchBriefName)

	day := time.Date(2026, 9, 15, 6, 10, 0, 0, time.UTC)

	got, ok := findSessionDir(root, testSessionBranch, testSessionIssue, day, naming.ResearchBriefName, nil)
	if !ok {
		t.Fatal("no session directory was found")
	}
	if got != want {
		t.Errorf("found %q, want the newest %q", got, want)
	}
}

// TestADirectoryWithoutTheMarkerIsNotTheAnswer is why the scan stats the file
// rather than trusting the directory. A session that got as far as MANIFEST.md
// and no further has a directory and no brief, and reporting one would be the
// same confident lie the derivation told.
func TestADirectoryWithoutTheMarkerIsNotTheAnswer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSession(t, root, "2026-08-31", "MANIFEST.md")

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)

	if got, ok := findSessionDir(root, testSessionBranch, testSessionIssue, day, naming.ResearchBriefName, nil); ok {
		t.Errorf("a session with no brief was reported as holding one: %q", got)
	}
}

// TestNoAiteamDirectoryIsNotAnError covers the ordinary greenfield case: a task
// that has never run a mode has nothing under .aiteam/, and that is a "no",
// not a failure.
func TestNoAiteamDirectoryIsNotAnError(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 9, 1, 6, 10, 0, 0, time.UTC)

	if _, ok := findSessionDir(t.TempDir(), testSessionBranch, testSessionIssue, day, naming.ResearchBriefName, nil); ok {
		t.Error("an empty worktree reported a session directory")
	}
}

// TestABranchThisToolingDidNotBuildHasNoSession guards the slug recovery: a
// branch with no feature slug frozen into it cannot name a session directory,
// and guessing one would put a link to someone else's work in the report.
func TestABranchThisToolingDidNotBuildHasNoSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSession(t, root, "2026-08-31", naming.ResearchBriefName)

	day := time.Date(2026, 8, 31, 18, 52, 0, 0, time.UTC)

	if _, ok := findSessionDir(root, "main", testSessionIssue, day, naming.ResearchBriefName, nil); ok {
		t.Error("a branch outside the naming scheme was given a session directory")
	}
}
