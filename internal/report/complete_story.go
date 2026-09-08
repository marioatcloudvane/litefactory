package report

import (
	"context"
	"fmt"
	"strings"
)

// RenderCompleteStory retains the full specification or refuses publication.
// It never falls back to an excerpt or a repository source-file link.
func (r *Reporter) RenderCompleteStory(ctx context.Context, story Story) (SubIssue, error) {
	if err := story.Ref.Validate(); err != nil {
		return SubIssue{}, fmt.Errorf("report: invalid story parent: %w", err)
	}
	if !storyIDPattern.MatchString(story.ID) || strings.TrimSpace(story.Title) == "" || len(story.Title) > 256 || strings.ContainsAny(story.Title, "\r\n") || strings.TrimSpace(story.Excerpt) == "" {
		return SubIssue{}, fmt.Errorf("report: %w: invalid complete story", ErrInvalidArgument)
	}
	marker := StoryMarker(story.Ref, story.ID)
	text := marker + "\n\n" + closeOpenFence(sanitize(strings.TrimPrefix(story.Excerpt, marker+"\n\n")))
	body, err := r.apply(ctx, text)
	if err != nil {
		return SubIssue{}, err
	}
	if body.Truncated() {
		return SubIssue{}, fmt.Errorf("report: story %s exceeds body limit after redaction", story.ID)
	}
	title, err := r.apply(ctx, sanitize(story.Title))
	if err != nil {
		return SubIssue{}, err
	}
	if title.Truncated() || title.Len() > 256 {
		return SubIssue{}, fmt.Errorf("report: story %s exceeds title limit after redaction", story.ID)
	}
	return SubIssue{Title: title, Body: body, Marker: marker}, nil
}
