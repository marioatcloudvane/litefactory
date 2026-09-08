package git

import (
	"context"
	"fmt"
	"strings"
)

// statusArgs is the one status invocation this package makes.
//
// Every flag is load-bearing. --untracked-files=all lists the files inside a
// new directory rather than the directory itself, because a run that creates
// internal/foo/ must report the files it wrote there. --ignored=no is git's
// default and is spelled out anyway: a build directory or a .env inside the
// worktree must never reach a commit or a report. -z is what makes the output
// safe to parse — a path containing a newline or a quote cannot forge an entry.
var statusArgs = []string{"status", "--porcelain", "-z", "--untracked-files=all", "--ignored=no"}

// ChangeKind classifies what happened to one path. It is a string so that a
// report can print it directly and a log can carry it without a lookup table.
type ChangeKind string

// The kinds a status entry can carry.
const (
	// ChangeAdded is a path that did not exist in HEAD and is now staged.
	ChangeAdded ChangeKind = "added"
	// ChangeModified is an existing tracked path whose content changed.
	ChangeModified ChangeKind = "modified"
	// ChangeDeleted is a tracked path that is gone.
	ChangeDeleted ChangeKind = "deleted"
	// ChangeRenamed is a tracked path that moved; From holds the old name.
	ChangeRenamed ChangeKind = "renamed"
	// ChangeCopied is a tracked path copied from another; From holds the
	// source.
	ChangeCopied ChangeKind = "copied"
	// ChangeTypeChanged is a path that became a symlink, or stopped being one.
	ChangeTypeChanged ChangeKind = "typechanged"
	// ChangeUntracked is a new file git is not tracking and is not ignoring.
	// This is the kind litefactory sees most: a run's new files.
	ChangeUntracked ChangeKind = "untracked"
	// ChangeUnmerged is a path with conflict markers or a conflicted index
	// entry. It should not happen — litefactory never merges — so it is
	// reported rather than folded into "modified".
	ChangeUnmerged ChangeKind = "unmerged"
)

// FileChange is one entry of the files-touched list.
//
// Path is relative to the worktree root and is exactly the bytes git reported:
// -z output is unquoted and unescaped, so no unquoting step can get it wrong.
type FileChange struct {
	// Path is the file's current path, relative to the worktree root.
	Path string
	// From is the previous path of a rename or a copy, empty otherwise.
	From string
	// Kind says what happened to it.
	Kind ChangeKind
}

// String renders one line of the files-touched list: "modified internal/a.go",
// or "renamed old.go -> new.go" when the path moved.
func (f FileChange) String() string {
	if f.From != "" {
		return fmt.Sprintf("%s %s -> %s", f.Kind, f.From, f.Path)
	}
	return fmt.Sprintf("%s %s", f.Kind, f.Path)
}

// Status lists the worktree's uncommitted changes: tracked modifications,
// deletions and renames, plus untracked files, with ignored files excluded.
//
// This is the files-touched list. Because [Worktree.Commit] stages exactly this
// set, the answer here immediately before a commit is the same set as a diff
// between the run's sha_before and the commit it produces — which is what the
// report needs and why no separate diff call exists.
//
// An empty result is the no-changes case (AC-3.23): a normal outcome, not an
// error.
func (w *Worktree) Status(ctx context.Context) ([]FileChange, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	out, err := w.runner.run(ctx, readTimeout, statusArgs...)
	if err != nil {
		return nil, err
	}
	files, err := parseStatus(out)
	if err != nil {
		return nil, err
	}
	filtered := files[:0]
	for _, f := range files {
		if f.Path == ".litefactory-plan-proposal.json" || f.Path == ".litefactory-story-events" || strings.HasPrefix(f.Path, ".litefactory-story-events/") {
			continue
		}
		filtered = append(filtered, f)
	}
	return filtered, nil
}

// parseStatus decodes NUL-delimited "git status --porcelain -z" output.
//
// Each entry is "XY <path>", NUL-terminated. A rename or a copy is followed by
// a second NUL-terminated field holding the path it came from, so the parser
// has to consume fields rather than lines.
func parseStatus(out string) ([]FileChange, error) {
	fields := strings.Split(out, "\x00")
	changes := make([]FileChange, 0, len(fields))

	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if entry == "" {
			// The output ends with a NUL, so the final field is always empty.
			continue
		}
		// "XY " plus at least one character of path.
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, fmt.Errorf("git status entry %q: %w: not in \"XY <path>\" form", entry, ErrInvalidArgument)
		}

		index, work := entry[0], entry[1]
		change := FileChange{Path: entry[3:], Kind: classifyStatus(index, work)}

		if change.Kind == ChangeRenamed || change.Kind == ChangeCopied {
			i++
			if i >= len(fields) || fields[i] == "" {
				return nil, fmt.Errorf("git status entry %q: %w: no source path followed the rename", entry, ErrInvalidArgument)
			}
			change.From = fields[i]
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// classifyStatus reduces the two porcelain status columns to one kind.
//
// The index column wins when it says anything, because it describes what would
// actually be committed; the worktree column is consulted only for a path that
// is unstaged. Conflicts are checked first: they are the one state where both
// columns have to be read together.
func classifyStatus(index, work byte) ChangeKind {
	switch {
	case index == '?' || work == '?':
		return ChangeUntracked
	case index == 'U' || work == 'U',
		index == 'A' && work == 'A',
		index == 'D' && work == 'D':
		return ChangeUnmerged
	}

	primary := index
	if primary == ' ' {
		primary = work
	}
	switch primary {
	case 'A':
		return ChangeAdded
	case 'D':
		return ChangeDeleted
	case 'R':
		return ChangeRenamed
	case 'C':
		return ChangeCopied
	case 'T':
		return ChangeTypeChanged
	default:
		// 'M' and anything a future git adds. Calling an unknown code
		// "modified" keeps the file in the report, which matters more than
		// naming the change precisely.
		return ChangeModified
	}
}
