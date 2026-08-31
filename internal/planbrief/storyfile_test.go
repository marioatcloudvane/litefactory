package planbrief_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// contractShapedStory is the exact per-story shape plan-orchestrator.md's
// contract describes.
const contractShapedStory = `# C1 — The plan ships as one sub-issue

> As an owner, I want each story to arrive as its own issue so that it can be tracked on its own.

Each user story ships as its own GitHub sub-issue.

## Why This Matters
Today a plan is one long document with nowhere to say "this part is done". Splitting it lets
each story be tracked, commented on and closed independently.

## Architecture Notes
- Sub-issues are created via the REST API's dedicated endpoint.
- Platform: no new dependency.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-C1.3 | Create-issue capability, via the SDK | Go | — | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-C1.3 | Every Story Index row carries a working sub-issue link. |
`

func TestParseStoryFile(t *testing.T) {
	t.Parallel()

	got, err := planbrief.ParseStoryFile([]byte(contractShapedStory))
	if err != nil {
		t.Fatalf("ParseStoryFile: %v", err)
	}

	if got.Title != "The plan ships as one sub-issue" {
		t.Errorf("Title = %q", got.Title)
	}
	switch {
	case !strings.Contains(got.Summary, "As an owner, I want each story"):
		t.Errorf("Summary lost the role/action/benefit line:\n%s", got.Summary)
	case !strings.Contains(got.Summary, "Why This Matters"):
		t.Errorf("Summary lost the Why This Matters section:\n%s", got.Summary)
	case strings.Contains(got.Summary, "Architecture Notes"):
		t.Errorf("Summary leaked the technical half:\n%s", got.Summary)
	}
	switch {
	case !strings.HasPrefix(got.Excerpt, "## Architecture Notes"):
		t.Errorf("Excerpt does not start at Architecture Notes:\n%s", got.Excerpt)
	case !strings.Contains(got.Excerpt, "## Tasks"):
		t.Errorf("Excerpt lost the Tasks table:\n%s", got.Excerpt)
	case !strings.Contains(got.Excerpt, "## Acceptance Criteria"):
		t.Errorf("Excerpt lost the Acceptance Criteria table:\n%s", got.Excerpt)
	}
}

// TestParseStoryFileWithNoArchitectureNotes is the "not this package's
// failure to report" path: a file missing the technical heading still
// parses, with its whole body read as Summary and no Excerpt.
func TestParseStoryFileWithNoArchitectureNotes(t *testing.T) {
	t.Parallel()

	data := "# C1 — A story with no technical detail yet\n\nJust an opening.\n"
	got, err := planbrief.ParseStoryFile([]byte(data))
	if err != nil {
		t.Fatalf("ParseStoryFile: %v", err)
	}
	if got.Excerpt != "" {
		t.Errorf("Excerpt = %q, want empty", got.Excerpt)
	}
	if !strings.Contains(got.Summary, "Just an opening.") {
		t.Errorf("Summary = %q", got.Summary)
	}
}

// TestParseStoryFileTitleWithoutASeparator covers a title line with no
// "story id — title" separator: the whole heading, minus "# ", is the title.
func TestParseStoryFileTitleWithoutASeparator(t *testing.T) {
	t.Parallel()

	data := "# Just a title\n\nSome body text.\n"
	got, err := planbrief.ParseStoryFile([]byte(data))
	if err != nil {
		t.Fatalf("ParseStoryFile: %v", err)
	}
	if got.Title != "Just a title" {
		t.Errorf("Title = %q, want %q", got.Title, "Just a title")
	}
}

func TestParseStoryFileEmpty(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{nil, []byte(""), []byte("   \n\n  "), []byte("No title heading at all.\n")} {
		if _, err := planbrief.ParseStoryFile(data); !errors.Is(err, planbrief.ErrEmptyStoryFile) {
			t.Errorf("ParseStoryFile(%q): err = %v, want ErrEmptyStoryFile", data, err)
		}
	}
}
