package brainstorm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type fakeTracker struct {
	name  string
	hits  []loganalysis.IssueHit
	err   error
	calls int
	text  string
	limit int
}

func (f *fakeTracker) Name() string { return f.name }

func (f *fakeTracker) Search(_ context.Context, text string, limit int) ([]loganalysis.IssueHit, error) {
	f.calls++
	f.text, f.limit = text, limit
	return f.hits, f.err
}

func (f *fakeTracker) FileBug(context.Context, redact.Body, redact.Body, string, time.Time) (loganalysis.IssueHit, error) {
	return loganalysis.IssueHit{}, errors.New("not used")
}

func issueService(trackers map[string]loganalysis.Tracker) *Service {
	return &Service{issues: TrackerSearcher{Select: func(_ context.Context, ref string) (loganalysis.Tracker, error) {
		if t, ok := trackers[ref]; ok {
			return t, nil
		}
		return nil, ErrNoTracker
	}}}
}

func TestIssueSearchGitHubOpenAndClosed(t *testing.T) {
	gh := &fakeTracker{name: "github:acme/widgets", hits: []loganalysis.IssueHit{
		{Reference: "acme/widgets#12", Title: "Export to CSV", State: "open", Labels: []string{"feature"}, URL: "https://github.com/acme/widgets/issues/12"},
		{Reference: "acme/widgets#40", Title: "CSV export breaks", State: "closed"},
	}}
	s := issueService(map[string]loganalysis.Tracker{"github:acme/widgets": gh})
	view, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/widgets", "  csv   export ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if gh.calls != 1 || gh.text != "csv export" || gh.limit != 10 {
		t.Fatalf("search calls %d text %q limit %d", gh.calls, gh.text, gh.limit)
	}
	want := []IssueMatch{{Reference: "acme/widgets#12", Number: 12, Title: "Export to CSV", State: "open", Labels: []string{"feature"}}, {Reference: "acme/widgets#40", Number: 40, Title: "CSV export breaks", State: "closed"}}
	if len(view.Matches) != 2 || view.Matches[0].Number != want[0].Number || view.Matches[1].State != "closed" || view.Matches[0].Labels[0] != "feature" || view.RepositoryRef != "github:acme/widgets" {
		t.Fatalf("matches %+v", view)
	}
	if view.Summary != "Found 2 possibly related issues: 12 (open), 40 (closed)." {
		t.Fatalf("summary %q", view.Summary)
	}
	if strings.Contains(view.Summary, "#") || strings.Contains(view.Summary, "http") {
		t.Fatalf("summary has a link or hash: %q", view.Summary)
	}
}

func TestIssueSearchLinearKeys(t *testing.T) {
	lin := &fakeTracker{name: "linear:project", hits: []loganalysis.IssueHit{{Reference: "ENG-7", Title: "Brainstorm notes", State: "Done", URL: "https://linear.app/x/issue/ENG-7"}}}
	s := issueService(map[string]loganalysis.Tracker{"github:acme/linear": lin})
	view, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/linear", "brainstorm", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Matches) != 1 || view.Matches[0].Reference != "ENG-7" || view.Matches[0].Number != 0 || view.Summary != "Found 1 possibly related issue: ENG-7 (done)." {
		t.Fatalf("view %+v", view)
	}
	lin.hits = nil
	if view, err = s.SearchIssues(t.Context(), Principal{}, "github:acme/linear", "nothing", 5); err != nil || len(view.Matches) != 0 || view.Matches == nil || view.Summary != "No matching issues were found." {
		t.Fatalf("empty view %+v %v", view, err)
	}
}

func TestIssueSearchRefusesProjectWithoutTracker(t *testing.T) {
	s := issueService(nil)
	if _, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/none", "csv", 5); !errors.Is(err, ErrNoTracker) {
		t.Fatalf("err %v", err)
	}
	if _, err := (&Service{}).SearchIssues(t.Context(), Principal{}, "github:acme/none", "csv", 5); !errors.Is(err, ErrNoTracker) {
		t.Fatalf("no searcher err %v", err)
	}
}

func TestIssueSearchBounds(t *testing.T) {
	gh := &fakeTracker{}
	s := issueService(map[string]loganalysis.Tracker{"github:acme/widgets": gh})
	for _, tc := range []struct {
		text  string
		limit int
	}{{"", 5}, {"   ", 5}, {strings.Repeat("a", MaxIssueSearchText+1), 5}, {strings.Repeat("é", MaxIssueSearchText), 5}, {"csv", 0}, {"csv", MaxIssueSearchLimit + 1}} {
		if _, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/widgets", tc.text, tc.limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("text %d limit %d err %v", len(tc.text), tc.limit, err)
		}
	}
	if _, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/widgets", strings.Repeat("a", MaxIssueSearchText), MaxIssueSearchLimit); err != nil {
		t.Fatal(err)
	}
	if gh.calls != 1 {
		t.Fatalf("provider calls %d", gh.calls)
	}
}

func TestIssueSearchConsentChecked(t *testing.T) {
	gh := &fakeTracker{}
	s := issueService(map[string]loganalysis.Tracker{"github:acme/widgets": gh})
	s.consent = func(context.Context, Principal, string) error { return ErrForbidden }
	if _, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/widgets", "csv", 5); !errors.Is(err, ErrForbidden) || gh.calls != 0 {
		t.Fatalf("err %v calls %d", err, gh.calls)
	}
}

func TestIssueSearchProviderErrorsReturnWaitWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		wait time.Duration
	}{
		{"github", &github.RateLimitError{RetryAfter: 42 * time.Second}, 42 * time.Second},
		{"linear", &ticket.Failure{Operation: "search", Kind: ticket.Refused, Cause: &ticket.RateLimitError{RetryAfter: 3 * time.Minute}}, 3 * time.Minute},
		{"other", errors.New("provider unavailable"), 0},
	} {
		tr := &fakeTracker{err: tc.err}
		s := issueService(map[string]loganalysis.Tracker{"github:acme/widgets": tr})
		_, err := s.SearchIssues(t.Context(), Principal{}, "github:acme/widgets", "csv", 5)
		var searchErr *IssueSearchError
		if !errors.As(err, &searchErr) || searchErr.RetryAfter != tc.wait || !errors.Is(err, tc.err) || tr.calls != 1 {
			t.Fatalf("%s: err %v calls %d", tc.name, err, tr.calls)
		}
		if tc.wait > 0 && !strings.Contains(err.Error(), tc.wait.String()) {
			t.Fatalf("%s: wait missing from %q", tc.name, err)
		}
	}
}
