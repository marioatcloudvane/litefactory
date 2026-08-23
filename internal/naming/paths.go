package naming

import (
	"fmt"
	"path/filepath"
	"strconv"
)

const (
	// WorktreeDirPrefix precedes the issue number in a worktree directory
	// name, giving "<worktree_root>/task-123".
	WorktreeDirPrefix = "task-"

	// DefaultWorktreeRootDirName is the directory litefactory puts worktree
	// roots in when a repository configures none. It is a sibling of the
	// clone, not a child: a worktree inside the clone would show up in the
	// clone's own status and index.
	DefaultWorktreeRootDirName = ".lf-worktrees"

	// FirstRunOrdinal is the ordinal of a task's first run. Run ordinals are
	// per task, not global.
	FirstRunOrdinal = 1

	// RunOrdinalDigits is the zero-padded width of a run ordinal in a file
	// name, so run 1 is "0001".
	RunOrdinalDigits = 4

	// RunLogExt is the extension of the raw Claude Code event stream.
	RunLogExt = ".jsonl"
	// RunMetaExt is the extension of the run metadata document, the sibling
	// of the event stream under the same ordinal stem.
	RunMetaExt = ".meta.json"

	// logsDirName and runsDirName spell out $LF_HOME/logs/runs from S-1.
	logsDirName = "logs"
	runsDirName = "runs"
)

// DefaultWorktreeRoot returns the worktree root for a clone that configures
// none: a .lf-worktrees directory beside the clone, named after it. A clone at
// /Users/x/dev/acme-api gets /Users/x/dev/.lf-worktrees/acme-api, so several
// registered repositories share the parent without colliding.
//
// clonePath must be absolute. Resolving a relative path would need a working
// directory, and this package has no I/O.
func DefaultWorktreeRoot(clonePath string) (string, error) {
	clean, err := cleanAbs("clone path", clonePath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(clean)
	if name == string(filepath.Separator) || name == "." || name == ".." {
		return "", fmt.Errorf("%w: clone path %q has no repository directory name", ErrInvalidPath, clonePath)
	}
	return filepath.Join(filepath.Dir(clean), DefaultWorktreeRootDirName, name), nil
}

// WorktreePath returns the worktree directory for an issue:
// <worktreeRoot>/task-<issue>. The root comes from the repos row — global
// config, "repo add --worktrees" or [DefaultWorktreeRoot] — and is not
// resolved here.
func WorktreePath(worktreeRoot string, issue int) (string, error) {
	if err := validateIssue(issue); err != nil {
		return "", err
	}
	clean, err := cleanAbs("worktree root", worktreeRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(clean, WorktreeDirPrefix+strconv.Itoa(issue)), nil
}

// RunsDir returns the directory holding every run artefact for one task:
// <home>/logs/runs/<forge>/<owner>/<repo>/<issue>, per S-1.
//
// home is $LF_HOME as resolved by internal/config; this package neither reads
// the environment nor creates the directory.
func RunsDir(home string, ref Ref) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}
	clean, err := cleanAbs("LF_HOME", home)
	if err != nil {
		return "", err
	}
	return filepath.Join(clean, logsDirName, runsDirName,
		ref.Forge, ref.Owner, ref.Repo, strconv.Itoa(ref.Issue)), nil
}

// RunLogPath returns the raw Claude Code event stream for one run:
// <RunsDir>/NNNN.jsonl.
func RunLogPath(home string, ref Ref, ordinal int) (string, error) {
	return runArtefactPath(home, ref, ordinal, RunLogExt)
}

// RunMetaPath returns the run metadata document for one run:
// <RunsDir>/NNNN.meta.json. It is the sibling of [RunLogPath] and shares its
// ordinal stem.
func RunMetaPath(home string, ref Ref, ordinal int) (string, error) {
	return runArtefactPath(home, ref, ordinal, RunMetaExt)
}

// FormatRunOrdinal renders a run ordinal as the zero-padded NNNN stem used in
// run file names, starting at "0001".
//
// The ordinal is allocated by inserting the runs row before the subprocess
// starts, so a crashed run keeps its number and no two runs of a task ever
// share a stem. Past 9999 the stem widens rather than wrapping: a wider name
// is ugly, a reused one would overwrite a previous run's log.
func FormatRunOrdinal(ordinal int) (string, error) {
	if ordinal < FirstRunOrdinal {
		return "", fmt.Errorf("%w: run ordinal %d is below %d", ErrInvalidOrdinal, ordinal, FirstRunOrdinal)
	}
	return fmt.Sprintf("%0*d", RunOrdinalDigits, ordinal), nil
}

// runArtefactPath builds one of a run's two files.
func runArtefactPath(home string, ref Ref, ordinal int, ext string) (string, error) {
	stem, err := FormatRunOrdinal(ordinal)
	if err != nil {
		return "", err
	}
	dir, err := RunsDir(home, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stem+ext), nil
}

// cleanAbs validates a caller-supplied path root and returns it cleaned.
// Requiring absolute roots keeps every derived path stable regardless of the
// daemon's working directory, which under launchd is not the one the operator
// installed from.
func cleanAbs(field, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrInvalidPath, field)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s %q is not absolute", ErrInvalidPath, field, path)
	}
	return filepath.Clean(path), nil
}
