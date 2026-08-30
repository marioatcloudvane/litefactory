package naming

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// day is a fixed session date. It is in UTC so the derived directory does not
// move with the timezone of whatever machine runs the tests.
var day = time.Date(2026, 8, 26, 14, 30, 0, 0, time.UTC)

// TestAITeamSessionDir pins the shape CLAUDE.md derives with three shell lines,
// including the two transformations that are easy to drop: a branch's slashes
// become hyphens, and the whole thing is lowercased.
//
// The worked example is this repository's own session directory for LF-7, so a
// change to the derivation shows up against a path someone can go and look at.
func TestAITeamSessionDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		branch string
		when   time.Time
		slug   string
		want   string
	}{
		{
			name:   "the LF-7 session directory, verbatim",
			branch: "lf/7-headless-mode-should-ask-via-a-comment",
			when:   day,
			slug:   "headless-mode-should-ask-via-a-comment",
			want: ".aiteam/lf-7-headless-mode-should-ask-via-a-comment/2026-08-26/" +
				"headless-mode-should-ask-via-a-comment",
		},
		{
			name:   "a prefix with several slashes flattens to one component",
			branch: "bots/lf/123-add-a-widget",
			when:   day,
			slug:   "add-a-widget",
			want:   ".aiteam/bots-lf-123-add-a-widget/2026-08-26/add-a-widget",
		},
		{
			name:   "an upper-case branch is lowercased, as tr does",
			branch: "LF/123-Add-A-Widget",
			when:   day,
			slug:   "add-a-widget",
			want:   ".aiteam/lf-123-add-a-widget/2026-08-26/add-a-widget",
		},
		{
			name:   "the date is the day the time falls on, in its own location",
			branch: "lf/123-add-a-widget",
			when:   time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
			slug:   "add-a-widget",
			want:   ".aiteam/lf-123-add-a-widget/2026-01-02/add-a-widget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AITeamSessionDir(tt.branch, tt.when, tt.slug)
			if err != nil {
				t.Fatalf("AITeamSessionDir(%q, %s, %q): %v", tt.branch, tt.when, tt.slug, err)
			}
			if got != tt.want {
				t.Errorf("AITeamSessionDir = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "\\") {
				t.Errorf("AITeamSessionDir = %q, which uses a backslash; these are git paths", got)
			}
		})
	}
}

// TestAITeamSessionDirRefusesWhatItCannotName covers the inputs that must not
// produce a plausible-looking path. The traversal cases are the ones that
// matter: the result is interpolated into a forge URL, so a component that
// climbs out of .aiteam/ would link somewhere else in the repository entirely.
func TestAITeamSessionDirRefusesWhatItCannotName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		branch string
		when   time.Time
		slug   string
		want   error
	}{
		{name: "no branch", when: day, slug: "add-a-widget", want: ErrInvalidPath},
		{name: "no date", branch: "lf/123-x", slug: "add-a-widget", want: ErrInvalidPath},
		{name: "no slug", branch: "lf/123-x", when: day, want: ErrInvalidSlug},
		{
			// The hyphenation already makes "../.." harmless — it becomes
			// "..-.." — but a branch that is exactly ".." survives it, and the
			// segment check is what stops the path climbing out of .aiteam/.
			// It is that check, not the hyphenation, which has to hold.
			name:   "a branch that would climb out of .aiteam",
			branch: "..", when: day, slug: "add-a-widget", want: ErrInvalidPath,
		},
		{
			name:   "a branch that is a relative path component",
			branch: ".", when: day, slug: "add-a-widget", want: ErrInvalidPath,
		},
		{
			name:   "a branch carrying a character no path segment may hold",
			branch: "lf/123 x?y", when: day, slug: "add-a-widget", want: ErrInvalidPath,
		},
		{
			name: "a slug carrying a separator", branch: "lf/123-x", when: day,
			slug: "a/b", want: ErrInvalidSlug,
		},
		{
			name: "a slug carrying a traversal", branch: "lf/123-x", when: day,
			slug: "..", want: ErrInvalidSlug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AITeamSessionDir(tt.branch, tt.when, tt.slug)
			if !errors.Is(err, tt.want) {
				t.Fatalf("AITeamSessionDir = (%q, %v), want an error wrapping %v", got, err, tt.want)
			}
			if got != "" {
				t.Errorf("AITeamSessionDir returned %q alongside its error", got)
			}
		})
	}
}

// TestResearchBriefPath is the artefact the run report links to. It is the
// session directory plus one file name, and it must stay a relative path with
// no leading slash — a caller joins it onto a forge URL.
func TestResearchBriefPath(t *testing.T) {
	t.Parallel()

	got, err := ResearchBriefPath("lf/123-add-a-widget", day, "add-a-widget")
	if err != nil {
		t.Fatalf("ResearchBriefPath: %v", err)
	}
	want := ".aiteam/lf-123-add-a-widget/2026-08-26/add-a-widget/RESEARCH_BRIEF.md"
	if got != want {
		t.Errorf("ResearchBriefPath = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, "/") {
		t.Errorf("ResearchBriefPath = %q, which is absolute", got)
	}

	if _, err := ResearchBriefPath("lf/123-add-a-widget", time.Time{}, "add-a-widget"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("ResearchBriefPath with no date: error = %v, want ErrInvalidPath", err)
	}
}

// TestSlugFromBranchInvertsBranch is the property that matters: whatever
// [Branch] builds, [SlugFromBranch] takes apart again. Holding the two together
// is what lets a caller carry only the branch and still know the slug, with no
// way for the two to disagree.
func TestSlugFromBranchInvertsBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefix string
		issue  int
		slug   string
	}{
		{"", 123, "add-a-widget"},
		{"lf", 7, "headless-mode-should-ask-via-a-comment"},
		{"bots/lf", 4321, "a"},
		{"lf", 1, "a1"},
		{"lf", 999999, "fix-the-thing-that-broke-on-the-second-t"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"/"+tt.slug, func(t *testing.T) {
			t.Parallel()

			branch, err := Branch(tt.prefix, tt.issue, tt.slug)
			if err != nil {
				t.Fatalf("Branch(%q, %d, %q): %v", tt.prefix, tt.issue, tt.slug, err)
			}
			got, err := SlugFromBranch(branch, tt.issue)
			if err != nil {
				t.Fatalf("SlugFromBranch(%q, %d): %v", branch, tt.issue, err)
			}
			if got != tt.slug {
				t.Errorf("SlugFromBranch(%q, %d) = %q, want %q", branch, tt.issue, got, tt.slug)
			}
		})
	}
}

// TestSlugFromBranchRefusesToGuess covers every branch that is not one Branch
// built for this issue. Each one must be an error rather than a wrong answer:
// a slug recovered from the wrong branch would derive a session directory that
// belongs to another task.
func TestSlugFromBranchRefusesToGuess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		branch string
		issue  int
		want   error
	}{
		{"no prefix at all", "123-add-a-widget", 123, ErrInvalidBranch},
		{"a different issue", "lf/124-add-a-widget", 123, ErrInvalidBranch},
		// The separator is part of the match, so 1234 is not read as 123
		// followed by a slug beginning "4".
		{"an issue number this one is a prefix of", "lf/1234-add-a-widget", 123, ErrInvalidBranch},
		{"no slug after the issue", "lf/123-", 123, ErrInvalidSlug},
		{"a hand-made branch", "main", 123, ErrInvalidBranch},
		{"an empty branch", "", 123, ErrInvalidBranch},
		{"a slug that is not one Slug would derive", "lf/123-Add_A_Widget", 123, ErrInvalidSlug},
		{"a non-positive issue", "lf/123-add-a-widget", 0, ErrInvalidRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := SlugFromBranch(tt.branch, tt.issue)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SlugFromBranch(%q, %d) = (%q, %v), want an error wrapping %v",
					tt.branch, tt.issue, got, err, tt.want)
			}
			if got != "" {
				t.Errorf("SlugFromBranch returned %q alongside its error", got)
			}
		})
	}
}
