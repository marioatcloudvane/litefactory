package projectread

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
)

// ErrCommittedUnsupported withholds binary or invalid text evidence.
var ErrCommittedUnsupported = errors.New("binary or unsupported committed evidence")

// CommittedMatch identifies sanitized text at an exact committed path and line.
type CommittedMatch struct {
	Path string
	Line int
	Text string
}

// CommittedSource is one complete bounded immutable repository source. Text is
// sanitized as one unit before the caller clips or pages any part of it.
type CommittedSource struct {
	Matches                           []CommittedMatch
	CoverageLimited                   bool
	LinePositions                     bool
	Changed                           []git.PinnedChangedEntry
	Text                              string
	Directory                         bool
	EntryKind, EntryMode, EntryObject string
	Commit, From, To, Semantics       string
	Parents                           []string
	History                           []git.PinnedCommit
	Gaps                              []string
}

// CommittedRequest has no mutable revision or arbitrary command capability.
type CommittedRequest struct {
	Sanitize                                          func(context.Context, string) (string, error)
	Operation, Commit, Path, Pattern, Query, From, To string
	IgnoreCase                                        bool
	Limit                                             int
}

// ReadCommitted is shared by project analyst/standards-author reads and remote
// code discussion. The caller supplies the authorized immutable commit.
func ReadCommitted(ctx context.Context, clone *git.Clone, redactor *redact.Redactor, in CommittedRequest) (CommittedSource, error) {
	if in.Operation == "repo_grep" {
		return searchCommitted(ctx, clone, redactor, in)
	}
	source, e := acquireCommitted(ctx, clone, in)
	if e != nil {
		return source, e
	}
	if redactor == nil {
		return CommittedSource{}, errors.New("redaction unavailable")
	}
	if in.Operation == "repo_diff" {
		remaining := (256 << 10) - len(source.Text)
		for _, entry := range source.Changed {
			if in.Sanitize != nil {
				safePath, err := in.Sanitize(ctx, entry.Path)
				if err != nil {
					return CommittedSource{}, err
				}
				if safePath != entry.Path {
					source.Text = ""
					source.Gaps = []string{"diff_withheld_because_changed_path_requires_sanitization"}
					return source, nil
				}
			}
			for _, blob := range []struct{ mode, id string }{{entry.BeforeMode, entry.Before}, {entry.AfterMode, entry.After}} {
				if blob.mode == "000000" {
					continue
				}
				if blob.mode == "160000" {
					source.Text = ""
					source.Gaps = []string{"committed_submodule_changes_unsupported; no traversal"}
					return source, nil
				}
				if remaining < 1 {
					return CommittedSource{}, git.ErrPinnedReadTooLarge
				}
				text, err := git.ReadPinnedBlob(ctx, clone, blob.id, remaining)
				if err != nil {
					return CommittedSource{}, err
				}
				remaining -= len(text)
				if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
					source.Text = ""
					source.Gaps = []string{"binary_changed_source_withheld"}
					return source, nil
				}
				safe, err := redactor.Prepare(ctx, text)
				if err != nil {
					return CommittedSource{}, err
				}
				sanitized := safe.String()
				if in.Sanitize != nil {
					sanitized, err = in.Sanitize(ctx, sanitized)
					if err != nil {
						return CommittedSource{}, err
					}
				}
				if sanitized != text {
					source.Text = ""
					source.Gaps = []string{"diff_withheld_because_complete_changed_source_requires_sanitization; use sanitized file reads"}
					return source, nil
				}
			}
		}
	}
	if !utf8.ValidString(source.Text) || strings.ContainsRune(source.Text, 0) {
		return CommittedSource{}, ErrCommittedUnsupported
	}
	view, e := redactor.Prepare(ctx, source.Text)
	if e != nil {
		return CommittedSource{}, e
	}
	source.LinePositions = strings.Count(source.Text, "\n") == strings.Count(view.String(), "\n")
	source.Text = view.String()
	if in.Sanitize != nil {
		beforeLines := strings.Count(source.Text, "\n")
		source.Text, e = in.Sanitize(ctx, source.Text)
		source.LinePositions = source.LinePositions && beforeLines == strings.Count(source.Text, "\n")
		if e != nil {
			return CommittedSource{}, e
		}
	}
	// All structured commit metadata follows the same complete-unit redaction.
	for i := range source.History {
		v, e := redactor.Prepare(ctx, source.History[i].Subject)
		if e != nil {
			return CommittedSource{}, e
		}
		source.History[i].Subject = v.String()
	}
	return source, nil
}
func acquireCommitted(ctx context.Context, clone *git.Clone, in CommittedRequest) (CommittedSource, error) {
	out := CommittedSource{Commit: in.Commit}
	var e error
	switch in.Operation {
	case "repo_read":
		entry, err := git.ReadPinnedEntry(ctx, clone, in.Commit, in.Path)
		if err != nil {
			return out, err
		}
		out.EntryMode, out.EntryKind, out.EntryObject = entry.Mode, entry.Kind, entry.Object
		if entry.Mode == "160000" {
			out.Text = fmt.Sprintf("Committed submodule entry %s at %s", in.Path, entry.Object)
			out.Gaps = []string{"committed_submodule_entry_only; no traversal"}
			return out, nil
		}
		if entry.Mode == "120000" {
			out.EntryKind = "symbolic_link"
			out.Gaps = []string{"committed_symbolic_link_target_text_only; no dereference"}
		}
		out.Text, out.Directory, e = clone.PinnedRead(ctx, in.Commit, in.Path, 1<<20)
	case "repo_grep":
		out.Text, e = clone.PinnedGrep(ctx, in.Commit, in.Pattern, in.Path, in.IgnoreCase, 20, 256<<10)
		out.Gaps = []string{"search_limited_to_20_matches_per_file; binary files and submodules excluded"}
	case "repo_log":
		out.History, e = git.PinnedHistory(ctx, clone, in.Commit, in.Path, in.Query, 100, 256<<10)
		out.Gaps = []string{"history_limited_to_100_commits; shallow boundaries may omit ancestors"}
	case "repo_diff":
		var change git.PinnedChange
		change, e = git.PinnedDiff(ctx, clone, in.Commit, in.From, in.To, in.Path, 256<<10)
		out.Text, out.From, out.To, out.Parents, out.Semantics, out.Changed = change.Text, change.From, change.To, change.Parents, change.Semantics, change.Entries
	default:
		e = errors.New("unknown committed evidence operation")
	}
	return out, e
}

// legacyCommitted retains the established analyst output contract while sharing
// its committed source acquisition with the remote projection.
func legacyCommitted(ctx context.Context, clone *git.Clone, name string, in Request, commit string, redactor *redact.Redactor) (string, error) {
	switch name {
	case "repo_log":
		limit := in.Limit
		if limit == 0 {
			limit = 20
		}
		return clone.PinnedLog(ctx, commit, in.Path, in.Query, min(limit, 50), 256<<10)
	case "repo_grep":
		source, e := ReadCommitted(ctx, clone, redactor, CommittedRequest{Operation: name, Commit: commit, Pattern: in.Pattern, Path: in.Path, IgnoreCase: in.IgnoreCase})
		if e == nil && source.Text == "" {
			if source.CoverageLimited {
				return "", errors.New("matching committed source withheld by sanitization")
			}
			source.Text = "no match"
		}
		return source.Text, e
	default:
		return "", fmt.Errorf("%s is not a repository tool", name)
	}
}

func searchCommitted(ctx context.Context, clone *git.Clone, r *redact.Redactor, in CommittedRequest) (CommittedSource, error) {
	out := CommittedSource{Commit: in.Commit, LinePositions: true, Gaps: []string{"search_limited_to_20_matches_per_file; binary files and submodules excluded"}}
	if r == nil {
		return out, errors.New("redaction unavailable")
	}
	matches, acquired, e := git.PinnedSearchPositions(ctx, clone, in.Commit, in.Pattern, in.Path, in.IgnoreCase, 20, 256<<10)
	if e != nil {
		return out, e
	}
	remaining := (256 << 10) - acquired
	var result strings.Builder
	for _, match := range matches {
		if remaining < 1 {
			return out, git.ErrPinnedReadTooLarge
		}
		text, directory, e := clone.PinnedRead(ctx, in.Commit, match.Path, remaining)
		if e != nil {
			return out, e
		}
		if directory || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return out, ErrCommittedUnsupported
		}
		remaining -= len(text)
		view, e := r.Prepare(ctx, text)
		if e != nil {
			return out, e
		}
		safe := view.String()
		if in.Sanitize != nil {
			safe, e = in.Sanitize(ctx, safe)
			if e != nil {
				return out, e
			}
		}
		if strings.Count(text, "\n") != strings.Count(safe, "\n") {
			out.CoverageLimited = true
			out.Gaps = append(out.Gaps, "matching_source_withheld_because_sanitization_changes_line_positions")
			continue
		}
		lines := strings.Split(safe, "\n")
		for _, line := range match.Lines {
			if line > len(lines) {
				return out, ErrCommittedUnsupported
			}
			out.Matches = append(out.Matches, CommittedMatch{Path: match.Path, Line: line, Text: lines[line-1]})
			fmt.Fprintf(&result, "%s:%d:%s\n", match.Path, line, lines[line-1])
			if result.Len() > 256<<10 {
				return out, git.ErrPinnedReadTooLarge
			}
		}
	}
	out.Text = result.String()
	return out, nil
}
