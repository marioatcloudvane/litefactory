package brainstorm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// TrackerSearcher searches the tracker that Select picks for a repository's
// project. Select returns ErrNoTracker when the project has neither a GitHub
// nor a Linear ticket connection.
type TrackerSearcher struct {
	Select func(ctx context.Context, repositoryRef string) (loganalysis.Tracker, error)
}

// SearchIssues implements IssueSearcher with one provider search and no retry.
func (t TrackerSearcher) SearchIssues(ctx context.Context, repositoryRef, text string, limit int) ([]IssueMatch, error) {
	if t.Select == nil {
		return nil, ErrNoTracker
	}
	tracker, err := t.Select(ctx, repositoryRef)
	if err != nil {
		return nil, err
	}
	if tracker == nil {
		return nil, ErrNoTracker
	}
	hits, err := tracker.Search(ctx, text, limit)
	if err != nil {
		return nil, err
	}
	out := make([]IssueMatch, 0, len(hits))
	for _, h := range hits {
		out = append(out, IssueMatch{Reference: h.Reference, Number: issueNumber(h.Reference), Title: h.Title, State: h.State, Labels: h.Labels})
	}
	return out, nil
}

// issueNumber is the number after the last '#' of a GitHub reference; Linear
// keys have none.
func issueNumber(reference string) int {
	i := strings.LastIndexByte(reference, '#')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(reference[i+1:])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// IssueSearchError is a provider failure of one issue search. RetryAfter is
// the provider's wait time, zero when it gave none.
type IssueSearchError struct {
	RetryAfter time.Duration
	Err        error
}

func (e *IssueSearchError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("the issue provider is rate limited; try again in %s", e.RetryAfter.Round(time.Second))
	}
	return "issue search failed: " + e.Err.Error()
}

func (e *IssueSearchError) Unwrap() error { return e.Err }

// SearchIssues searches open and closed issues of the repository's project
// through its ticket provider. Text is 1 to MaxIssueSearchText characters and
// limit 1 to MaxIssueSearchLimit; nothing is retried.
func (s *Service) SearchIssues(ctx context.Context, p Principal, repositoryRef, text string, limit int) (IssueSearchView, error) {
	text = strings.Join(strings.Fields(text), " ")
	if repositoryRef == "" || text == "" || utf8.RuneCountInString(text) > MaxIssueSearchText || len(text) > MaxIssueSearchText || limit < 1 || limit > MaxIssueSearchLimit {
		return IssueSearchView{}, ErrInvalid
	}
	if s.consent != nil {
		if err := s.consent(ctx, p, repositoryRef); err != nil {
			return IssueSearchView{}, err
		}
	}
	if s.issues == nil {
		return IssueSearchView{}, ErrNoTracker
	}
	matches, err := s.issues.SearchIssues(ctx, repositoryRef, text, limit)
	if err != nil {
		if errors.Is(err, ErrNoTracker) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return IssueSearchView{}, err
		}
		return IssueSearchView{}, &IssueSearchError{RetryAfter: retryAfter(err), Err: err}
	}
	if matches == nil {
		matches = []IssueMatch{}
	}
	return IssueSearchView{RepositoryRef: repositoryRef, Matches: matches, Summary: issueSummary(matches)}, nil
}

func retryAfter(err error) time.Duration {
	var gh *github.RateLimitError
	if errors.As(err, &gh) {
		return gh.RetryAfter
	}
	var tk *ticket.RateLimitError
	if errors.As(err, &tk) {
		return tk.RetryAfter
	}
	return 0
}

// issueSummary states the hits as one plain spoken sentence: numbers as plain
// numbers, Linear keys as they are, never links.
func issueSummary(matches []IssueMatch) string {
	if len(matches) == 0 {
		return "No matching issues were found."
	}
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		name := m.Reference
		if m.Number > 0 {
			name = strconv.Itoa(m.Number)
		}
		if m.State != "" {
			name += " (" + strings.ToLower(m.State) + ")"
		}
		parts = append(parts, name)
	}
	noun := "issues"
	if len(matches) == 1 {
		noun = "issue"
	}
	return fmt.Sprintf("Found %d possibly related %s: %s.", len(matches), noun, strings.Join(parts, ", "))
}
