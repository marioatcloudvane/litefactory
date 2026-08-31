package planbrief

import "strings"

// architectureNotesHeading splits a per-story file's business register from
// its technical detail. It is the exact heading plan-orchestrator.md's
// contract puts at the start of the technical half of a sub-issue body.
const architectureNotesHeading = "## Architecture Notes"

// titleSeparator is what plan-orchestrator.md's contract puts between a
// story's id and its title on the file's title line: "# <Story ID> — <title>".
const titleSeparator = "—"

// StoryFile is one per-story file — `$session/<StoryID>.md` — split along the
// same boundary [report.Story] draws between its business register and its
// technical detail.
type StoryFile struct {
	// Title is the story's title, read off the "# <Story ID> — <title>" line
	// with the leading id stripped. Empty when the line carries no separator,
	// in which case the whole heading (minus "# ") is returned instead — see
	// [ParseStoryFile].
	Title string

	// Summary is everything between the title line and "## Architecture
	// Notes": the role/action/benefit line, the one-sentence delivery
	// summary, and "## Why This Matters" — the business register the
	// contract asks for, copied verbatim.
	Summary string

	// Excerpt is "## Architecture Notes" onward — the Tasks and Acceptance
	// Criteria tables and the notes above them — copied verbatim. Empty when
	// the file carries no such heading, which [ParseStoryFile] treats as "no
	// technical detail to excerpt" rather than an error: what matters is that
	// the file has a title.
	Excerpt string
}

// ParseStoryFile reads one per-story sub-issue file into its title, its
// business-register summary and its technical excerpt.
//
// [ErrEmptyStoryFile] is returned for a file with nothing in it, or one whose
// first non-blank line is not a "# " heading — the one structural requirement
// this package holds a story file to. Everything past that line is read
// as-is: a file missing "## Architecture Notes" still parses, with its whole
// body as Summary and no Excerpt, because a story that has not been given
// architecture notes yet is not this package's failure to report.
func ParseStoryFile(data []byte) (StoryFile, error) {
	text := strings.TrimLeft(string(data), "\n\t ")
	if text == "" {
		return StoryFile{}, ErrEmptyStoryFile
	}

	titleLine, rest, _ := strings.Cut(text, "\n")
	titleLine = strings.TrimSpace(titleLine)
	if !strings.HasPrefix(titleLine, "# ") {
		return StoryFile{}, ErrEmptyStoryFile
	}
	title := strings.TrimSpace(strings.TrimPrefix(titleLine, "# "))
	if _, after, ok := strings.Cut(title, titleSeparator); ok {
		title = strings.TrimSpace(after)
	}

	body := strings.TrimLeft(rest, "\n")
	summary := body
	excerpt := ""
	if idx := indexOfHeading(body, architectureNotesHeading); idx >= 0 {
		summary = strings.TrimSpace(body[:idx])
		excerpt = strings.TrimSpace(body[idx:])
	} else {
		summary = strings.TrimSpace(body)
	}

	return StoryFile{Title: title, Summary: summary, Excerpt: excerpt}, nil
}

// indexOfHeading returns the byte offset heading starts at, matched only at
// the beginning of body or immediately after a newline — the same
// line-anchored discipline [report]'s own marker matching holds itself to —
// or -1 when it does not appear that way anywhere.
func indexOfHeading(body, heading string) int {
	if strings.HasPrefix(body, heading) {
		return 0
	}
	idx := strings.Index(body, "\n"+heading)
	if idx < 0 {
		return -1
	}
	return idx + 1
}
