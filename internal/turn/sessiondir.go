package turn

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// findSessionDir locates the aiteam session directory a task's artefacts
// actually live in, and reports whether one was found.
//
// The path .aiteam/<branch>/<date>/<feature>/ carries a date, and nothing
// afterwards knows which date without looking. Deriving it from the day the
// current run started is right only while a task finishes on the day its
// session directory was named. It is wrong for every run after that: a task
// answered over two days, a retry, a resumed session. The directory does not
// move, so the second run derives a path that has never existed.
//
// That is not hypothetical — it is what put a link to a 2026-09-01 brief in the
// report of a run whose brief was at 2026-08-31, on a task whose only session
// directory had been named the evening before. The report was correct about the
// file existing and wrong about where, which is the worst of both.
//
// So: try the run's own day first, because it is exact and free in the ordinary
// case and costs one [os.Stat]; fall back to one bounded, one-level read of
// .aiteam/<branchDir>/, newest date first. marker is the file whose presence
// makes a date directory the right one — a session directory that exists but
// does not hold the artefact being looked for is not an answer.
//
// The returned path is repository-relative and slash-separated, ready to be
// interpolated into a forge URL. worktreeDir is only where the looking happens.
func findSessionDir(worktreeDir, branch string, issue int, day time.Time, marker string, log *slog.Logger) (string, bool) {
	slug, err := naming.SlugFromBranch(branch, issue)
	if err != nil {
		// Not a branch this tooling built, so there is no slug to derive a
		// session directory from. Nothing is wrong; there is just nothing here.
		return "", false
	}

	if dir, ok := sessionDirOn(worktreeDir, branch, slug, day, marker); ok {
		return dir, true
	}
	return scanSessionDates(worktreeDir, branch, slug, marker, log)
}

// sessionDirOn derives the session directory for one candidate day and reports
// whether marker actually exists in it — one [os.Stat] and nothing more.
func sessionDirOn(worktreeDir, branch, slug string, day time.Time, marker string) (string, bool) {
	dir, err := naming.AITeamSessionDir(branch, day, slug)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, filepath.FromSlash(dir), marker)); err != nil {
		return "", false
	}
	return dir, true
}

// scanSessionDates is the fallback [findSessionDir] takes when the run's own
// day is not where the session was named: every date directory under
// .aiteam/<branchDir>/, newest first, until one holds marker.
//
// It reads exactly one directory, never walks, and stops at the first match —
// the same discipline internal/repos.DetectAiteam holds its own scan to.
func scanSessionDates(worktreeDir, branch, slug, marker string, log *slog.Logger) (string, bool) {
	branchDir, err := naming.AITeamBranchDir(branch)
	if err != nil {
		return "", false
	}

	root := filepath.Join(worktreeDir, filepath.FromSlash(branchDir))
	entries, err := os.ReadDir(root)
	if err != nil {
		if log != nil && !errors.Is(err, os.ErrNotExist) {
			log.Debug("list the aiteam branch directory",
				slog.String("path", root), slog.Any("error", err))
		}
		return "", false
	}

	dates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dates = append(dates, entry.Name())
		}
	}
	// Lexicographic order is chronological order for YYYY-MM-DD, so this is
	// "newest first" with no date parsing needed to sort.
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	for _, date := range dates {
		day, err := time.ParseInLocation(naming.SessionDateLayout, date, time.Local)
		if err != nil {
			continue
		}
		if dir, ok := sessionDirOn(worktreeDir, branch, slug, day, marker); ok {
			return dir, true
		}
	}
	return "", false
}
