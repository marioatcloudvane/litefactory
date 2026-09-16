package github_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestIssuePageHTTPBoundsAndPagination(t *testing.T) {
	for _, tc := range []struct {
		name, state, body string
		want              int
	}{
		{"mixed", "open", issuesAndPullRequestsJSON, 2},
		{"pr-only", "closed", `[{"number":12,"pull_request":{"url":"https://example.invalid/pr"}}]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/acme-api/issues" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				q := r.URL.Query()
				if q.Get("state") != tc.state || q.Get("page") != "3" || q.Get("per_page") != "4" {
					t.Errorf("query=%v", q)
				}
				w.Header().Set("Link", `<https://api.github.com/repos/acme/acme-api/issues?page=4>; rel="next"`)
				respondJSON(http.StatusOK, tc.body)(w, r)
			})
			page, err := f.Client.ListIssuesPage(t.Context(), testRepo, tc.state, 3, 4)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != tc.want || page.NextPage != 4 || calls.Load() != 1 {
				t.Fatalf("page=%+v calls=%d", page, calls.Load())
			}
		})
	}
}

func TestIssuePageHTTPRefusesOversizedRemotePage(t *testing.T) {
	f := newFixture(t, respondJSON(http.StatusOK, issuesAndPullRequestsJSON))
	if _, err := f.Client.ListIssuesPage(t.Context(), testRepo, "open", 1, 1); err == nil {
		t.Fatal("oversized remote response accepted")
	}
	f.Only(t)
}

func TestIssuePageHTTPInvalidInputDoesNoIO(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); respondJSON(200, `[]`)(w, r) })
	for _, tc := range []struct {
		state      string
		page, size int
	}{
		{"all", 1, 30}, {"", 1, 30}, {"open", 0, 30}, {"open", -1, 30}, {"closed", 1, 0}, {"open", 1, 101},
	} {
		if _, err := f.Client.ListIssuesPage(t.Context(), testRepo, tc.state, tc.page, tc.size); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
	if _, err := f.Client.ListIssuesPage(t.Context(), github.Repo{Owner: strings.Repeat("/", 2), Name: "repo"}, "open", 1, 30); err == nil {
		t.Fatal("invalid repository accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input dispatched %d calls", calls.Load())
	}
}
