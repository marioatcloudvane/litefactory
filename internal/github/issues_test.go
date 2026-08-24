package github_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// issuesAndPullRequestsJSON is what GitHub actually returns from the issues
// endpoint: issues and pull requests mixed together, distinguishable only by
// the presence of a "pull_request" member.
const issuesAndPullRequestsJSON = `[
	{"number": 11, "title": "Real issue", "state": "open"},
	{"number": 12, "title": "A pull request", "state": "open",
	 "pull_request": {"url": "https://api.github.com/repos/acme/acme-api/pulls/12"}},
	{"number": 13, "title": "Another issue", "state": "closed"},
	{"number": 14, "title": "A merged pull request", "state": "closed",
	 "pull_request": {"url": "https://api.github.com/repos/acme/acme-api/pulls/14",
	                  "merged_at": "2026-08-20T10:00:00Z"}}
]`

// TestListIssuesFiltersPullRequests is AC-2.8's filter. GitHub's issues
// endpoint returns pull requests too, and it is dropped here rather than in the
// daemon so that no caller can forget: a PR that reached the turn would get a
// worktree, a branch and a comment on someone else's review thread.
func TestListIssuesFiltersPullRequests(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, issuesAndPullRequestsJSON))

	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want the 2 that are not pull requests: %+v", len(issues), issues)
	}
	for _, issue := range issues {
		if issue.Number == 12 || issue.Number == 14 {
			t.Errorf("issue #%d is a pull request and should have been dropped", issue.Number)
		}
	}
	if issues[0].Number != 11 || issues[1].Number != 13 {
		t.Errorf("issue numbers = %d, %d; want 11, 13", issues[0].Number, issues[1].Number)
	}
	// The count is logged, so a repository whose listing is mostly pull
	// requests is visible rather than mysterious.
	if got := f.Logs(); !strings.Contains(got, `"pull_requests_skipped":2`) {
		t.Errorf("the cost line does not report the skipped pull requests:\n%s", got)
	}
}

// TestListIssuesQuery pins the request D-A settled on. The parameters are the
// decision: state=all because a closed issue can still receive an instruction,
// and since as the cursor.
func TestListIssuesQuery(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 23, 9, 30, 15, 0, time.FixedZone("CEST", 2*60*60))
	f := newFixture(t, respondJSON(http.StatusOK, `[]`))

	if _, err := f.Client.ListIssues(t.Context(), testRepo, since); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	req := f.Only(t)
	if got, want := req.Method, http.MethodGet; got != want {
		t.Errorf("method = %s, want %s", got, want)
	}
	if got, want := req.Path, "/repos/acme/acme-api/issues"; got != want {
		t.Errorf("path = %q, want %q — the per-repo listing, never /search/issues", got, want)
	}
	if got, want := req.Query.Get("state"), "all"; got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if got, want := req.Query.Get("per_page"), "100"; got != want {
		t.Errorf("per_page = %q, want %q", got, want)
	}
	// Sent in UTC, whatever zone the cursor came from — GitHub's since filter
	// is ISO 8601 and a local offset here is a bug that only shows up twice a
	// year.
	if got, want := req.Query.Get("since"), "2026-08-23T07:30:15Z"; got != want {
		t.Errorf("since = %q, want %q", got, want)
	}
}

// TestListIssuesOmitsAZeroSince covers the first-ever tick, before a cursor
// exists. Sending since=0001-01-01 would work but reads as a bug; sending
// nothing is what "every issue" means to the API.
func TestListIssuesOmitsAZeroSince(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{}); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if _, present := f.Only(t).Query["since"]; present {
		t.Error("a zero since was sent as a query parameter")
	}
}

// TestListIssuesNeverUsesSearch is the executable half of D-A. The rationale
// lives in a comment on ListIssues; this is what fails if someone "optimises"
// the tick into a single search call and the comment survives as decoration.
func TestListIssuesNeverUsesSearch(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	if _, err := f.Client.ListIssues(t.Context(), testRepo, time.Now()); err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	req := f.Only(t)
	if strings.Contains(req.Path, "search") {
		t.Errorf("path = %q; issue search was dropped in D-A — its query string caps "+
			"at 256 characters, it is 30 req/min, and it is eventually consistent", req.Path)
	}
	if _, present := req.Query["q"]; present {
		t.Error("the request carries a search query parameter")
	}
}

// TestListIssuesReturnsAnEmptySliceForAQuietRepository keeps "nothing changed"
// distinguishable from an error at the call site.
func TestListIssuesReturnsAnEmptySliceForAQuietRepository(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `[]`))
	issues, err := f.Client.ListIssues(t.Context(), testRepo, time.Time{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("got %d issues, want none", len(issues))
	}
}

// TestIssueFields covers the decode, including the numeric author identity that
// D-I makes load-bearing.
func TestIssueFields(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `{
		"number": 123,
		"title": "Add a rate limiter",
		"body": "It should back off.",
		"state": "open",
		"html_url": "https://github.com/acme/acme-api/issues/123",
		"user": {"id": 99, "login": "mario", "type": "User"},
		"created_at": "2026-08-20T10:00:00Z",
		"updated_at": "2026-08-23T11:22:33Z"
	}`))

	issue, err := f.Client.GetIssue(t.Context(), testRepo, 123)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	if issue.Number != 123 {
		t.Errorf("Number = %d, want 123", issue.Number)
	}
	if issue.Title != "Add a rate limiter" {
		t.Errorf("Title = %q", issue.Title)
	}
	if issue.Body != "It should back off." {
		t.Errorf("Body = %q", issue.Body)
	}
	if issue.State != "open" {
		t.Errorf("State = %q, want open", issue.State)
	}
	if issue.HTMLURL == "" {
		t.Error("HTMLURL is empty")
	}
	if issue.Author.ID != 99 {
		t.Errorf("Author.ID = %d, want 99 — classification compares the id, not the handle", issue.Author.ID)
	}
	if issue.Author.Login != "mario" {
		t.Errorf("Author.Login = %q, want mario", issue.Author.Login)
	}
	if issue.Author.IsBot {
		t.Error("Author.IsBot = true for a User account")
	}
	if want := time.Date(2026, 8, 23, 11, 22, 33, 0, time.UTC); !issue.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %s, want %s", issue.UpdatedAt, want)
	}
	if want := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC); !issue.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %s, want %s", issue.CreatedAt, want)
	}
}

// TestIssueFromADeletedAccountDecodes covers GitHub's null user. A nil
// dereference here would take the daemon down over an issue opened by someone
// who has since deleted their account.
func TestIssueFromADeletedAccountDecodes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `{"number": 5, "title": "orphan", "user": null}`))

	issue, err := f.Client.GetIssue(t.Context(), testRepo, 5)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Author != (github.User{}) {
		t.Errorf("Author = %+v, want the zero User for a deleted account", issue.Author)
	}
}

// TestGetIssueRefusesAPullRequest closes the door the listing filter leaves
// open: GitHub serves a PR from /issues/{number}, so a number that arrived from
// elsewhere could otherwise reach one.
func TestGetIssueRefusesAPullRequest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK,
		`{"number": 12, "title": "A pull request", "pull_request": {"url": "https://api.github.com/x"}}`))

	_, err := f.Client.GetIssue(t.Context(), testRepo, 12)
	if !errors.Is(err, github.ErrInvalidArgument) {
		t.Fatalf("GetIssue on a pull request: error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "pull request") {
		t.Errorf("error %q does not say why", err)
	}
}

// TestGetIssuePath checks the URL, since a wrong one produces a 404 that reads
// exactly like a permissions problem.
func TestGetIssuePath(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK, `{"number": 7}`))
	if _, err := f.Client.GetIssue(t.Context(), testRepo, 7); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got, want := f.Only(t).Path, "/repos/acme/acme-api/issues/7"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// TestRepositoryComponentsAreRefusedBeforeAnyRequest is a path-traversal guard.
// Owner and name are interpolated into the request path without escaping — a
// deliberate choice, because url.URL re-escapes an already-escaped path — so
// the validation is what keeps that safe. It has to run before the request, not
// after a 404.
func TestRepositoryComponentsAreRefusedBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	repos := map[string]github.Repo{
		"an empty owner":          {Owner: "", Name: "acme-api"},
		"an empty name":           {Owner: "acme", Name: ""},
		"a slash in the name":     {Owner: "acme", Name: "acme-api/pulls"},
		"a parent directory":      {Owner: "..", Name: "acme-api"},
		"a percent escape":        {Owner: "acme", Name: "acme%2Fapi"},
		"a query separator":       {Owner: "acme", Name: "acme?x=1"},
		"a space":                 {Owner: "acme co", Name: "acme-api"},
		"an at sign in the owner": {Owner: "acme@evil", Name: "acme-api"},
	}

	for name, repo := range repos {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, respondJSON(http.StatusOK, `[]`))
			if _, err := f.Client.ListIssues(t.Context(), repo, time.Time{}); !errors.Is(err, github.ErrInvalidArgument) {
				t.Errorf("ListIssues error = %v, want ErrInvalidArgument", err)
			}
			if _, err := f.Client.GetIssue(t.Context(), repo, 1); !errors.Is(err, github.ErrInvalidArgument) {
				t.Errorf("GetIssue error = %v, want ErrInvalidArgument", err)
			}
			if got := len(f.Requests()); got != 0 {
				t.Errorf("made %d requests with a refused repository; want none", got)
			}
			if err := repo.Validate(); err == nil {
				t.Error("Repo.Validate accepted it")
			}
		})
	}
}

// TestIssueNumbersAreRefusedBeforeAnyRequest keeps a zero or negative number
// from being interpolated into a path where it would address something else.
func TestIssueNumbersAreRefusedBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	for _, number := range []int{0, -1} {
		f := newFixture(t, respondJSON(http.StatusOK, `{}`))
		if _, err := f.Client.GetIssue(t.Context(), testRepo, number); !errors.Is(err, github.ErrInvalidArgument) {
			t.Errorf("GetIssue(%d) error = %v, want ErrInvalidArgument", number, err)
		}
		if got := len(f.Requests()); got != 0 {
			t.Errorf("GetIssue(%d) made %d requests; want none", number, got)
		}
	}
}

// TestRepoRefAndValidateAcceptRealRepositoryNames guards against the validator
// being tightened past GitHub's own charset — a repository with a dot in its
// name is ordinary, and refusing one would make it unregisterable.
func TestRepoRefAndValidateAcceptRealRepositoryNames(t *testing.T) {
	t.Parallel()

	repo := github.Repo{Owner: "acme-corp", Name: "acme.api_v2"}
	if err := repo.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, want := repo.String(), "acme-corp/acme.api_v2"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	ref := repo.Ref(123)
	if err := ref.Validate(); err != nil {
		t.Fatalf("the derived task ref does not validate: %v", err)
	}
	if got, want := ref.String(), "github:acme-corp/acme.api_v2#123"; got != want {
		t.Errorf("Ref(123) = %q, want %q", got, want)
	}
	if got := github.RepoFromRef(ref); got != repo {
		t.Errorf("RepoFromRef round trip = %+v, want %+v", got, repo)
	}
}
