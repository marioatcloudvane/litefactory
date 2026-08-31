package planbrief

import "errors"

// The sentinel errors this package returns. Every error it returns wraps
// exactly one of these, so a caller can branch with errors.Is without
// matching on message text.
var (
	// ErrNoStoryIndex reports that the text has no "## Story Index" heading,
	// or one with no table under it — including a table missing the ID or
	// Sub-issue column the contract requires.
	ErrNoStoryIndex = errors.New("planbrief: no Story Index table")

	// ErrMalformedRow reports a Story Index row this package cannot read: one
	// whose ID cell is empty.
	ErrMalformedRow = errors.New("planbrief: malformed Story Index row")

	// ErrStoryNotFound reports that [RewriteSubIssueCell] was asked to
	// rewrite a story id no row in the Story Index carries.
	ErrStoryNotFound = errors.New("planbrief: story not found in Story Index")

	// ErrEmptyStoryFile reports a per-story file with no title heading to
	// read a story from — either genuinely empty, or missing the "# <Story
	// ID> — <title>" line the contract requires at the top.
	ErrEmptyStoryFile = errors.New("planbrief: story file has no title heading")
)
